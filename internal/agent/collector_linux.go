//go:build linux

package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

const (
	maxProcFileBytes      = 256 << 10
	maxSocketFileBytes    = 32 << 20
	maxNetworkInterfaces  = 128
	maxInterfaceNameBytes = 64
)

type interfaceCounters struct{ receive, transmit uint64 }

type fileSystemStats struct {
	blocks, availableBlocks, blockSize uint64
}

type linuxSource struct {
	procRoot       string
	sysRoot        string
	rootPath       string
	readFile       func(string, int64) ([]byte, error)
	countProcesses func(string, uint64) (uint64, error)
	statFS         func(string) (fileSystemStats, error)
	hostname       func() (string, error)
}

// LinuxCollector reads only procfs, sysfs, and statfs. It maintains bounded
// per-interface baselines solely to derive reset-safe live rates.
type LinuxCollector struct {
	source        linuxSource
	previousCPU   cpuCounters
	haveCPU       bool
	previousNet   map[string]interfaceCounters
	previousNetAt time.Time
}

// NewCollector creates the Linux read-only collector used in production.
func NewCollector() Collector { return newLinuxCollector(defaultLinuxSource()) }

func defaultLinuxSource() linuxSource {
	return linuxSource{
		procRoot: "/proc", sysRoot: "/sys", rootPath: "/",
		readFile:       readBoundedFile,
		countProcesses: countProcessDirectories,
		statFS: func(path string) (fileSystemStats, error) {
			var result syscall.Statfs_t
			if err := syscall.Statfs(path, &result); err != nil {
				return fileSystemStats{}, err
			}
			return fileSystemStats{blocks: result.Blocks, availableBlocks: result.Bavail, blockSize: uint64(result.Bsize)}, nil
		},
		hostname: os.Hostname,
	}
}

func newLinuxCollector(source linuxSource) *LinuxCollector {
	return &LinuxCollector{source: source, previousNet: make(map[string]interfaceCounters)}
}

// Collect returns a partial report when an unprivileged kernel view is
// missing. Unavailable areas are explicit and filesystem details stay local.
func (collector *LinuxCollector) Collect(now time.Time) (protocol.Report, error) {
	report := protocol.Report{CollectedAtUnixMilli: now.UnixMilli()}
	unavailable := make(map[protocol.MetricArea]struct{}, protocol.MaxUnavailableAreas)
	mark := func(area protocol.MetricArea) { unavailable[area] = struct{}{} }

	if data, err := collector.readProc("stat", maxProcFileBytes); err != nil {
		mark(protocol.AreaCPU)
	} else if current, err := parseCPU(data); err != nil {
		mark(protocol.AreaCPU)
	} else {
		report.CPU.UsagePercent = cpuUsage(collector.previousCPU, current, collector.haveCPU)
		collector.previousCPU, collector.haveCPU = current, true
	}
	if data, err := collector.readProc("loadavg", maxProcFileBytes); err != nil {
		mark(protocol.AreaLoad)
	} else if report.Load, err = parseLoad(data); err != nil {
		mark(protocol.AreaLoad)
	}
	if data, err := collector.readProc("meminfo", maxProcFileBytes); err != nil {
		mark(protocol.AreaMemory)
	} else if report.Memory, err = parseMemory(data); err != nil {
		mark(protocol.AreaMemory)
	}
	if report.Disk, _ = collector.collectDisk(); report.Disk.TotalBytes == 0 {
		mark(protocol.AreaDisk)
	}
	if data, err := collector.readProc(filepath.Join("net", "dev"), maxProcFileBytes); err != nil {
		mark(protocol.AreaNetwork)
	} else if current, err := parseNetwork(data); err != nil {
		mark(protocol.AreaNetwork)
	} else {
		current = collector.filterInterfaces(current)
		report.Network = collector.networkStats(current, now)
	}
	if data, err := collector.readProc("uptime", maxProcFileBytes); err != nil {
		mark(protocol.AreaUptime)
	} else if report.UptimeSeconds, err = parseUptime(data); err != nil {
		mark(protocol.AreaUptime)
	}
	if count, err := collector.processCount(); err != nil {
		mark(protocol.AreaProcesses)
	} else {
		report.ProcessCount = count
	}
	if connections, err := collector.connectionCounts(); err != nil {
		mark(protocol.AreaConnections)
	} else {
		report.Connections = connections
	}

	report.Unavailable = make([]protocol.MetricArea, 0, len(unavailable))
	for area := range unavailable {
		report.Unavailable = append(report.Unavailable, area)
	}
	sort.Slice(report.Unavailable, func(i, j int) bool { return report.Unavailable[i] < report.Unavailable[j] })
	if err := protocol.ValidateReport(report); err != nil {
		return protocol.Report{}, fmt.Errorf("validate collected report: %w", err)
	}
	return report, nil
}

func (collector *LinuxCollector) readProc(name string, limit int64) ([]byte, error) {
	return collector.source.readFile(filepath.Join(collector.source.procRoot, name), limit)
}

func (collector *LinuxCollector) collectDisk() (protocol.DiskStats, error) {
	stats, err := collector.source.statFS(collector.source.rootPath)
	if err != nil || stats.blockSize == 0 {
		return protocol.DiskStats{}, errors.New("root filesystem is unavailable")
	}
	total, ok := checkedMultiply(stats.blocks, stats.blockSize)
	if !ok || total > protocol.MaxMetricBytes {
		return protocol.DiskStats{}, errors.New("root filesystem size is outside bounds")
	}
	free, ok := checkedMultiply(stats.availableBlocks, stats.blockSize)
	if !ok || free > total {
		return protocol.DiskStats{}, errors.New("root filesystem free space is outside bounds")
	}
	return protocol.DiskStats{TotalBytes: total, UsedBytes: total - free, FreeBytes: free}, nil
}

func (collector *LinuxCollector) filterInterfaces(current map[string]interfaceCounters) map[string]interfaceCounters {
	filtered := make(map[string]interfaceCounters, len(current))
	for name, counters := range current {
		if name == "lo" {
			continue
		}
		data, err := collector.source.readFile(filepath.Join(collector.source.sysRoot, "class", "net", name, "operstate"), 32)
		if err == nil && strings.TrimSpace(string(data)) == "down" {
			continue
		}
		filtered[name] = counters
	}
	return filtered
}

func (collector *LinuxCollector) networkStats(current map[string]interfaceCounters, now time.Time) protocol.NetworkStats {
	var stats protocol.NetworkStats
	var receiveDelta, transmitDelta uint64
	for name, counters := range current {
		stats.ReceiveBytesTotal = saturatedAdd(stats.ReceiveBytesTotal, counters.receive, protocol.MaxMetricBytes)
		stats.TransmitBytesTotal = saturatedAdd(stats.TransmitBytesTotal, counters.transmit, protocol.MaxMetricBytes)
		if previous, ok := collector.previousNet[name]; ok {
			if counters.receive >= previous.receive {
				receiveDelta = saturatedAdd(receiveDelta, counters.receive-previous.receive, protocol.MaxMetricBytes)
			}
			if counters.transmit >= previous.transmit {
				transmitDelta = saturatedAdd(transmitDelta, counters.transmit-previous.transmit, protocol.MaxMetricBytes)
			}
		}
	}
	if elapsed := now.Sub(collector.previousNetAt); !collector.previousNetAt.IsZero() && elapsed > 0 {
		stats.ReceiveBytesPerSecond = boundedRate(receiveDelta, elapsed)
		stats.TransmitBytesPerSecond = boundedRate(transmitDelta, elapsed)
	}
	collector.previousNet = current
	collector.previousNetAt = now
	return stats
}

type cpuCounters struct{ idle, total uint64 }

func parseCPU(data []byte) (cpuCounters, error) {
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, errors.New("aggregate CPU row is missing")
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}, errors.New("invalid CPU counter")
		}
		values = append(values, value)
	}
	// Linux reports guest and guest_nice after steal, but those times are
	// already included in user and nice. Summing more than the first eight
	// counters would double-count guest CPU time.
	totalFields := min(len(values), 8)
	var total uint64
	for _, value := range values[:totalFields] {
		if math.MaxUint64-total < value {
			return cpuCounters{}, errors.New("CPU counter overflow")
		}
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle = saturatedAdd(idle, values[4], math.MaxUint64)
	}
	return cpuCounters{idle: idle, total: total}, nil
}

func cpuUsage(previous, current cpuCounters, havePrevious bool) float64 {
	if current.total == 0 {
		return 0
	}
	if !havePrevious || current.total < previous.total || current.idle < previous.idle {
		return clampPercent(float64(current.total-current.idle) * 100 / float64(current.total))
	}
	totalDelta, idleDelta := current.total-previous.total, current.idle-previous.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return clampPercent(float64(totalDelta-idleDelta) * 100 / float64(totalDelta))
}

func parseLoad(data []byte) (protocol.LoadStats, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return protocol.LoadStats{}, errors.New("load averages are missing")
	}
	values := make([]float64, 3)
	for index := range values {
		value, err := strconv.ParseFloat(fields[index], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return protocol.LoadStats{}, errors.New("invalid load average")
		}
		values[index] = value
	}
	return protocol.LoadStats{One: values[0], Five: values[1], Fifteen: values[2]}, nil
}

func parseMemory(data []byte) (protocol.MemoryStats, error) {
	values := make(map[string]uint64)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || value > protocol.MaxMetricBytes/1024 {
			return protocol.MemoryStats{}, errors.New("invalid memory counter")
		}
		values[name] = value * 1024
	}
	if err := scanner.Err(); err != nil || values["MemTotal"] == 0 {
		return protocol.MemoryStats{}, errors.New("memory total is missing")
	}
	total := values["MemTotal"]
	available, ok := values["MemAvailable"]
	if !ok {
		available = saturatedAdd(values["MemFree"], values["Buffers"], total)
		available = saturatedAdd(available, values["Cached"], total)
		available = saturatedAdd(available, values["SReclaimable"], total)
		if available >= values["Shmem"] {
			available -= values["Shmem"]
		}
	}
	if available > total || values["SwapFree"] > values["SwapTotal"] {
		return protocol.MemoryStats{}, errors.New("memory counters are inconsistent")
	}
	return protocol.MemoryStats{TotalBytes: total, UsedBytes: total - available, AvailableBytes: available,
		SwapTotalBytes: values["SwapTotal"], SwapUsedBytes: values["SwapTotal"] - values["SwapFree"]}, nil
}

func parseNetwork(data []byte) (map[string]interfaceCounters, error) {
	result := make(map[string]interfaceCounters)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	for scanner.Scan() {
		line++
		if line <= 2 {
			continue
		}
		left, right, ok := strings.Cut(scanner.Text(), ":")
		name := strings.TrimSpace(left)
		fields := strings.Fields(right)
		if !ok || name == "" || len(name) > maxInterfaceNameBytes || len(fields) < 9 {
			return nil, errors.New("invalid network interface row")
		}
		if len(result) >= maxNetworkInterfaces {
			return nil, errors.New("too many network interfaces")
		}
		receive, receiveErr := strconv.ParseUint(fields[0], 10, 64)
		transmit, transmitErr := strconv.ParseUint(fields[8], 10, 64)
		if receiveErr != nil || transmitErr != nil || receive > protocol.MaxMetricBytes || transmit > protocol.MaxMetricBytes {
			return nil, errors.New("invalid network counter")
		}
		result[name] = interfaceCounters{receive: receive, transmit: transmit}
	}
	if err := scanner.Err(); err != nil || len(result) == 0 {
		return nil, errors.New("network interface data is missing")
	}
	return result, nil
}

func parseUptime(data []byte) (uint64, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, errors.New("uptime is missing")
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > float64(protocol.MaxUptimeSeconds) {
		return 0, errors.New("uptime is outside bounds")
	}
	return uint64(value), nil
}

func (collector *LinuxCollector) processCount() (uint64, error) {
	count, err := collector.source.countProcesses(collector.source.procRoot, protocol.MaxProcessCount)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func countProcessDirectories(path string, maximum uint64) (uint64, error) {
	directory, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer directory.Close()
	var count uint64
	for {
		names, readErr := directory.Readdirnames(1024)
		for _, name := range names {
			if numericName(name) {
				count++
				if count > maximum {
					return 0, errors.New("process count exceeds bounds")
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return count, nil
		}
		if readErr != nil {
			return 0, readErr
		}
	}
}

func (collector *LinuxCollector) connectionCounts() (protocol.ConnectionStats, error) {
	var result protocol.ConnectionStats
	for _, item := range []struct {
		name     string
		required bool
		tcp      bool
	}{{"tcp", true, true}, {"tcp6", false, true}, {"udp", true, false}, {"udp6", false, false}} {
		data, err := collector.readProc(filepath.Join("net", item.name), maxSocketFileBytes)
		if err != nil {
			if !item.required && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return protocol.ConnectionStats{}, err
		}
		count, err := countSocketRows(data)
		if err != nil {
			return protocol.ConnectionStats{}, err
		}
		if item.tcp {
			result.TCP = saturatedAdd(result.TCP, count, protocol.MaxConnectionCount+1)
		} else {
			result.UDP = saturatedAdd(result.UDP, count, protocol.MaxConnectionCount+1)
		}
	}
	if result.TCP > protocol.MaxConnectionCount || result.UDP > protocol.MaxConnectionCount {
		return protocol.ConnectionStats{}, errors.New("connection count exceeds bounds")
	}
	return result, nil
}

func countSocketRows(data []byte) (uint64, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	var count uint64
	for scanner.Scan() {
		line++
		if line == 1 || strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		count++
		if count > protocol.MaxConnectionCount {
			return 0, errors.New("connection count exceeds bounds")
		}
	}
	return count, scanner.Err()
}

// CollectHostFacts reads bounded, low-frequency host facts without executing
// commands or accessing a container runtime socket.
func CollectHostFacts() HostFacts {
	return collectHostFacts(defaultLinuxSource())
}

func collectHostFacts(source linuxSource) HostFacts {
	facts := HostFacts{Architecture: runtime.GOARCH}
	facts.Hostname, _ = source.hostname()
	if data, err := source.readFile(filepath.Join(source.procRoot, "sys", "kernel", "osrelease"), 4096); err == nil {
		facts.Kernel = strings.TrimSpace(string(data))
	}
	if data, err := source.readFile("/etc/os-release", 64<<10); err == nil {
		facts.OS = parseOSRelease(data)
	}
	if facts.OS == "" {
		facts.OS = "linux"
	}
	facts.Virtualization = detectVirtualization(source)
	if data, err := source.readFile(filepath.Join(source.procRoot, "sys", "kernel", "random", "boot_id"), 128); err == nil {
		bootID := strings.ToLower(strings.TrimSpace(string(data)))
		if protocol.ValidBootID(bootID) {
			facts.BootID = bootID
		}
	}
	return boundedHostFacts(facts)
}

func detectVirtualization(source linuxSource) string {
	if data, err := source.readFile(filepath.Join(source.procRoot, "1", "cgroup"), 64<<10); err == nil {
		value := strings.ToLower(string(data))
		for _, marker := range []string{"docker", "containerd", "kubepods", "lxc"} {
			if strings.Contains(value, marker) {
				return "container"
			}
		}
	}
	for _, file := range []string{"product_name", "sys_vendor"} {
		data, err := source.readFile(filepath.Join(source.sysRoot, "class", "dmi", "id", file), 4096)
		if err != nil {
			continue
		}
		value := strings.ToLower(string(data))
		for marker, name := range map[string]string{"kvm": "kvm", "qemu": "kvm", "vmware": "vmware", "virtualbox": "virtualbox", "microsoft": "hyper-v", "xen": "xen"} {
			if strings.Contains(value, marker) {
				return name
			}
		}
	}
	return ""
}

func parseOSRelease(data []byte) string {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		values[key] = value
	}
	if values["PRETTY_NAME"] != "" {
		return values["PRETTY_NAME"]
	}
	return strings.TrimSpace(values["NAME"] + " " + values["VERSION_ID"])
}

func boundedHostFacts(facts HostFacts) HostFacts {
	facts.Hostname = truncateString(sanitizeFact(facts.Hostname), 255)
	facts.OS = truncateString(sanitizeFact(facts.OS), 255)
	facts.Kernel = truncateString(sanitizeFact(facts.Kernel), 255)
	facts.Architecture = truncateString(sanitizeFact(facts.Architecture), 32)
	facts.Virtualization = truncateString(sanitizeFact(facts.Virtualization), 64)
	if !protocol.ValidBootID(facts.BootID) {
		facts.BootID = ""
	}
	return facts
}

func sanitizeFact(value string) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value))
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("kernel file exceeds size limit")
	}
	return data, nil
}

func checkedMultiply(left, right uint64) (uint64, bool) {
	if left != 0 && right > math.MaxUint64/left {
		return 0, false
	}
	return left * right, true
}

func saturatedAdd(left, right, maximum uint64) uint64 {
	if left >= maximum || right > maximum-left {
		return maximum
	}
	return left + right
}

func boundedRate(delta uint64, elapsed time.Duration) uint64 {
	if delta == 0 || elapsed <= 0 {
		return 0
	}
	rate := float64(delta) / elapsed.Seconds()
	if rate >= float64(protocol.MaxRateBytesPerSecond) {
		return protocol.MaxRateBytesPerSecond
	}
	return uint64(rate)
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func numericName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func truncateString(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maximum {
		return value
	}
	for maximum > 0 && !utf8.ValidString(value[:maximum]) {
		maximum--
	}
	return value[:maximum]
}
