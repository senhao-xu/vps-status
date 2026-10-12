//go:build linux

package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

func TestLinuxCollectorCollectsAndDerivesResetSafeMetrics(t *testing.T) {
	files := map[string]string{
		"/proc-test/stat":                    "cpu 100 0 100 800 0 0 0 0\n",
		"/proc-test/loadavg":                 "0.10 0.20 0.30 1/20 42\n",
		"/proc-test/meminfo":                 "MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 100 kB\nSwapFree: 40 kB\n",
		"/proc-test/net/dev":                 "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\n",
		"/sys-test/class/net/eth0/operstate": "up\n",
		"/proc-test/uptime":                  "123.9 0.0\n",
		"/proc-test/net/tcp":                 "header\nrow\n",
		"/proc-test/net/udp":                 "header\n",
	}
	source := linuxSource{
		procRoot: "/proc-test", sysRoot: "/sys-test", rootPath: "/",
		readFile: func(path string, _ int64) ([]byte, error) {
			value, ok := files[filepath.Clean(path)]
			if !ok {
				return nil, fs.ErrNotExist
			}
			return []byte(value), nil
		},
		countProcesses: func(string, uint64) (uint64, error) { return 2, nil },
		statFS: func(string) (fileSystemStats, error) {
			return fileSystemStats{blocks: 100, availableBlocks: 25, blockSize: 4096}, nil
		},
	}
	collector := newLinuxCollector(source)
	start := time.Unix(1_700_000_000, 0).UTC()
	first, err := collector.Collect(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Unavailable) != 0 || first.CPU.UsagePercent != 20 || first.Network.ReceiveBytesPerSecond != 0 ||
		first.Memory.UsedBytes != 600*1024 || first.Disk.UsedBytes != 75*4096 || first.ProcessCount != 2 ||
		first.Connections.TCP != 1 || first.Connections.UDP != 0 || first.UptimeSeconds != 123 {
		t.Fatalf("first report=%#v", first)
	}

	files["/proc-test/stat"] = "cpu 150 0 150 900 0 0 0 0\n"
	files["/proc-test/net/dev"] = "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 300 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n"
	second, err := collector.Collect(start.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.CPU.UsagePercent != 50 || second.Network.ReceiveBytesPerSecond != 100 || second.Network.TransmitBytesPerSecond != 150 {
		t.Fatalf("second report=%#v", second)
	}

	files["/proc-test/net/dev"] = "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\neth0: 10 0 0 0 0 0 0 0 20 0 0 0 0 0 0 0\neth1: 50 0 0 0 0 0 0 0 70 0 0 0 0 0 0 0\n"
	files["/sys-test/class/net/eth1/operstate"] = "up\n"
	reset, err := collector.Collect(start.Add(4 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if reset.Network.ReceiveBytesPerSecond != 0 || reset.Network.TransmitBytesPerSecond != 0 ||
		reset.Network.ReceiveBytesTotal != 60 || reset.Network.TransmitBytesTotal != 90 {
		t.Fatalf("reset/interface-change network=%#v", reset.Network)
	}
}

func TestLinuxCollectorDegradesEveryUnavailableAreaWithoutRawErrors(t *testing.T) {
	source := linuxSource{
		procRoot: "/missing-proc", sysRoot: "/missing-sys", rootPath: "/missing-root",
		readFile:       func(string, int64) ([]byte, error) { return nil, fs.ErrPermission },
		countProcesses: func(string, uint64) (uint64, error) { return 0, fs.ErrPermission },
		statFS:         func(string) (fileSystemStats, error) { return fileSystemStats{}, fs.ErrPermission },
	}
	report, err := newLinuxCollector(source).Collect(time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatalf("partial collection must remain usable: %v", err)
	}
	if len(report.Unavailable) != protocol.MaxUnavailableAreas {
		t.Fatalf("unavailable=%v", report.Unavailable)
	}
	want := map[protocol.MetricArea]bool{
		protocol.AreaCPU: true, protocol.AreaLoad: true, protocol.AreaMemory: true, protocol.AreaDisk: true,
		protocol.AreaNetwork: true, protocol.AreaUptime: true, protocol.AreaProcesses: true, protocol.AreaConnections: true,
	}
	for _, area := range report.Unavailable {
		if !want[area] {
			t.Fatalf("unexpected unavailable area %q", area)
		}
		delete(want, area)
	}
	if len(want) != 0 {
		t.Fatalf("missing unavailable areas=%v", want)
	}
}

func TestLinuxParsingSupportsOlderMemoryAndBoundedHostFacts(t *testing.T) {
	memory, err := parseMemory([]byte("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 200 kB\nSReclaimable: 20 kB\nShmem: 10 kB\nSwapTotal: 100 kB\nSwapFree: 25 kB\n"))
	if err != nil || memory.AvailableBytes != 360*1024 || memory.SwapUsedBytes != 75*1024 {
		t.Fatalf("older meminfo=%#v, %v", memory, err)
	}
	if _, err := parseCPU([]byte("cpu broken\n")); err == nil {
		t.Fatal("malformed CPU fixture accepted")
	}
	cpu, err := parseCPU([]byte("cpu 100 20 30 800 10 5 5 30 500 400\n"))
	if err != nil || cpu.total != 1000 || cpu.idle != 810 {
		t.Fatalf("CPU guest counters were double-counted: %#v, %v", cpu, err)
	}
	facts := boundedHostFacts(HostFacts{Hostname: strings.Repeat("主", 100), OS: " Linux\x00 ", Kernel: "6.1", Architecture: "amd64", Virtualization: "kvm"})
	if len(facts.Hostname) > 255 || !utf8.ValidString(facts.Hostname) || facts.OS != "Linux" {
		t.Fatalf("bounded facts=%#v", facts)
	}
}

func TestCollectHostFactsNormalizesAndBoundsBootID(t *testing.T) {
	const canonical = "550e8400-e29b-41d4-a716-446655440000"
	collect := func(value string) HostFacts {
		return collectHostFacts(linuxSource{
			procRoot: "/proc-fixture", sysRoot: "/sys-fixture",
			hostname: func() (string, error) { return "fixture", nil },
			readFile: func(path string, _ int64) ([]byte, error) {
				if strings.HasSuffix(path, filepath.Join("random", "boot_id")) {
					return []byte(value), nil
				}
				return nil, fs.ErrNotExist
			},
		})
	}
	if facts := collect("  550E8400-E29B-41D4-A716-446655440000\n"); facts.BootID != canonical {
		t.Fatalf("normalized boot ID=%q", facts.BootID)
	}
	for _, value := range []string{"malformed", canonical + strings.Repeat("0", 100), canonical[:35] + "主"} {
		if facts := collect(value); facts.BootID != "" {
			t.Fatalf("invalid boot ID retained: %q", facts.BootID)
		}
	}
}

func TestProcessDirectoryCountIsStreamingAndBounded(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"1", "2", "self", "net"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	count, err := countProcessDirectories(root, 2)
	if err != nil || count != 2 {
		t.Fatalf("countProcessDirectories()=%d, %v", count, err)
	}
	if _, err := countProcessDirectories(root, 1); err == nil {
		t.Fatal("process bound was not enforced")
	}
}

func TestConnectionCountsOnlyTreatsMissingIPv6TablesAsOptional(t *testing.T) {
	readError := fs.ErrNotExist
	collector := newLinuxCollector(linuxSource{
		procRoot: "/proc-test",
		readFile: func(path string, _ int64) ([]byte, error) {
			switch filepath.Base(path) {
			case "tcp", "udp":
				return []byte("header\nrow\n"), nil
			case "tcp6", "udp6":
				return nil, readError
			default:
				return nil, fs.ErrNotExist
			}
		},
	})
	connections, err := collector.connectionCounts()
	if err != nil || connections.TCP != 1 || connections.UDP != 1 {
		t.Fatalf("missing IPv6 tables: connections=%#v error=%v", connections, err)
	}
	readError = fs.ErrPermission
	if _, err := collector.connectionCounts(); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unreadable IPv6 table error=%v", err)
	}
}

func TestVirtualizationDetectionUsesReadOnlyKernelFacts(t *testing.T) {
	containerSource := linuxSource{
		procRoot: "/proc-test", sysRoot: "/sys-test",
		readFile: func(path string, _ int64) ([]byte, error) {
			if filepath.Clean(path) == "/proc-test/1/cgroup" {
				return []byte("0::/docker/example"), nil
			}
			return nil, fs.ErrNotExist
		},
	}
	if got := detectVirtualization(containerSource); got != "container" {
		t.Fatalf("container virtualization=%q", got)
	}
	virtualMachineSource := linuxSource{
		procRoot: "/proc-test", sysRoot: "/sys-test",
		readFile: func(path string, _ int64) ([]byte, error) {
			if filepath.Base(path) == "product_name" {
				return []byte("QEMU Virtual Machine"), nil
			}
			return nil, fs.ErrNotExist
		},
	}
	if got := detectVirtualization(virtualMachineSource); got != "kvm" {
		t.Fatalf("VM virtualization=%q", got)
	}
}
