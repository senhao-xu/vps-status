package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	// MaxMetricBytes keeps counters within signed 64-bit storage and arithmetic
	// boundaries while remaining far above realistic host values.
	MaxMetricBytes uint64 = math.MaxInt64
	// MaxRateBytesPerSecond is a defensive one-petabyte-per-second wire bound.
	MaxRateBytesPerSecond uint64 = 1 << 50
	// MaxProcessCount bounds current process entries reported by one Agent.
	MaxProcessCount uint64 = 10_000_000
	// MaxConnectionCount bounds each current kernel socket-entry count.
	MaxConnectionCount uint64 = 100_000_000
	// MaxUptimeSeconds permits long-lived hosts while bounding wire arithmetic.
	MaxUptimeSeconds uint64 = 200 * 366 * 24 * 60 * 60
	// MaxUnavailableAreas is the complete v1 collection-area count.
	MaxUnavailableAreas = 8
)

// MetricArea identifies a bounded collection area that an unprivileged Agent
// could not read. It never carries a filesystem path or raw error.
type MetricArea string

const (
	AreaCPU         MetricArea = "cpu"
	AreaLoad        MetricArea = "load"
	AreaMemory      MetricArea = "memory"
	AreaDisk        MetricArea = "disk"
	AreaNetwork     MetricArea = "network"
	AreaUptime      MetricArea = "uptime"
	AreaProcesses   MetricArea = "processes"
	AreaConnections MetricArea = "connections"
)

// CPUStats contains aggregate host CPU use over the collector interval.
type CPUStats struct {
	UsagePercent float64 `json:"usage_percent"`
}

// LoadStats contains the Linux one, five, and fifteen minute load averages.
type LoadStats struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

// MemoryStats contains host byte capacities and current use.
type MemoryStats struct {
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`
}

// DiskStats contains aggregate statistics for the Agent's root filesystem.
type DiskStats struct {
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// NetworkStats contains reset-aware rates and current kernel cumulative
// counters. Durable traffic accounting belongs to telemetry.db, not here.
type NetworkStats struct {
	ReceiveBytesPerSecond  uint64 `json:"receive_bytes_per_second"`
	TransmitBytesPerSecond uint64 `json:"transmit_bytes_per_second"`
	ReceiveBytesTotal      uint64 `json:"receive_bytes_total"`
	TransmitBytesTotal     uint64 `json:"transmit_bytes_total"`
}

// ConnectionStats contains current kernel socket-entry counts.
type ConnectionStats struct {
	TCP uint64 `json:"tcp"`
	UDP uint64 `json:"udp"`
}

// Report is the only v1 TypeReport payload. Raw samples are live data and must
// not be persisted to control.db.
type Report struct {
	CollectedAtUnixMilli int64           `json:"collected_at_unix_milli"`
	CPU                  CPUStats        `json:"cpu"`
	Load                 LoadStats       `json:"load"`
	Memory               MemoryStats     `json:"memory"`
	Disk                 DiskStats       `json:"disk"`
	Network              NetworkStats    `json:"network"`
	UptimeSeconds        uint64          `json:"uptime_seconds"`
	ProcessCount         uint64          `json:"process_count"`
	Connections          ConnectionStats `json:"connections"`
	Unavailable          []MetricArea    `json:"unavailable,omitempty"`
}

// DecodeReport strictly decodes and validates the unique report payload,
// including its live timestamp boundary.
func DecodeReport(raw json.RawMessage, now time.Time) (Report, error) {
	var report Report
	if err := DecodePayload(raw, &report); err != nil {
		return Report{}, err
	}
	if err := ValidateReportAt(report, now); err != nil {
		return Report{}, err
	}
	return report, nil
}

// ValidateReport checks numeric, relational, and bounded-collection rules.
func ValidateReport(report Report) error {
	if report.CollectedAtUnixMilli <= 0 {
		return errors.New("report timestamp is missing")
	}
	if !boundedFloat(report.CPU.UsagePercent, 0, 100) {
		return errors.New("CPU usage is outside its bounds")
	}
	if !boundedFloat(report.Load.One, 0, 1_000_000) ||
		!boundedFloat(report.Load.Five, 0, 1_000_000) ||
		!boundedFloat(report.Load.Fifteen, 0, 1_000_000) {
		return errors.New("load average is outside its bounds")
	}
	if report.Memory.TotalBytes > MaxMetricBytes || report.Memory.UsedBytes > report.Memory.TotalBytes ||
		report.Memory.AvailableBytes > report.Memory.TotalBytes || report.Memory.SwapTotalBytes > MaxMetricBytes ||
		report.Memory.SwapUsedBytes > report.Memory.SwapTotalBytes ||
		report.Memory.UsedBytes != report.Memory.TotalBytes-report.Memory.AvailableBytes {
		return errors.New("memory values are inconsistent")
	}
	if report.Disk.TotalBytes > MaxMetricBytes || report.Disk.UsedBytes > report.Disk.TotalBytes ||
		report.Disk.FreeBytes > report.Disk.TotalBytes || report.Disk.UsedBytes != report.Disk.TotalBytes-report.Disk.FreeBytes {
		return errors.New("disk values are inconsistent")
	}
	if report.Network.ReceiveBytesTotal > MaxMetricBytes || report.Network.TransmitBytesTotal > MaxMetricBytes ||
		report.Network.ReceiveBytesPerSecond > MaxRateBytesPerSecond ||
		report.Network.TransmitBytesPerSecond > MaxRateBytesPerSecond {
		return errors.New("network values are outside their bounds")
	}
	if report.UptimeSeconds > MaxUptimeSeconds || report.ProcessCount > MaxProcessCount ||
		report.Connections.TCP > MaxConnectionCount || report.Connections.UDP > MaxConnectionCount {
		return errors.New("host count is outside its bounds")
	}
	if len(report.Unavailable) > MaxUnavailableAreas {
		return errors.New("too many unavailable metric areas")
	}
	seen := make(map[MetricArea]struct{}, len(report.Unavailable))
	for _, area := range report.Unavailable {
		if !validMetricArea(area) {
			return fmt.Errorf("unknown unavailable metric area")
		}
		if _, exists := seen[area]; exists {
			return errors.New("duplicate unavailable metric area")
		}
		seen[area] = struct{}{}
	}
	return nil
}

// ValidateReportAt additionally rejects stale and future live samples.
func ValidateReportAt(report Report, now time.Time) error {
	if err := ValidateReport(report); err != nil {
		return err
	}
	collectedAt := time.UnixMilli(report.CollectedAtUnixMilli)
	if collectedAt.Before(now.Add(-MaxReportClockSkew)) || collectedAt.After(now.Add(MaxReportClockSkew)) {
		return errors.New("report timestamp is outside the accepted clock skew")
	}
	return nil
}

func boundedFloat(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func validMetricArea(area MetricArea) bool {
	switch area {
	case AreaCPU, AreaLoad, AreaMemory, AreaDisk, AreaNetwork, AreaUptime, AreaProcesses, AreaConnections:
		return true
	default:
		return false
	}
}
