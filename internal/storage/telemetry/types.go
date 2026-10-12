package telemetry

import (
	"errors"
	"time"
)

var (
	// ErrUnavailable means history storage is unavailable or deliberately paused.
	ErrUnavailable = errors.New("telemetry storage unavailable")
	// ErrQueueFull means the bounded history writer cannot accept more work.
	ErrQueueFull = errors.New("telemetry writer queue full")
	// ErrClosed means the history service has stopped.
	ErrClosed = errors.New("telemetry service closed")
	// ErrInvalidResolution means a history query requested an unknown tier.
	ErrInvalidResolution = errors.New("invalid telemetry resolution")
)

// Resolution is one of the durable host-history tiers.
type Resolution string

const (
	Resolution1m  Resolution = "1m"
	Resolution10m Resolution = "10m"
	Resolution1h  Resolution = "1h"
)

// HostBucket is one completed host interval. Sums are retained with their
// sample count so later rollups remain weighted and idempotent.
type HostBucket struct {
	NodeID            string
	BucketStart       time.Time
	SampleCount       uint64
	CPUCount          uint64
	LoadCount         uint64
	MemoryCount       uint64
	DiskCount         uint64
	NetworkCount      uint64
	UptimeCount       uint64
	ProcessCount      uint64
	ConnectionCount   uint64
	CPUSum            float64
	CPUMin            float64
	CPUMax            float64
	Load1Sum          float64
	Load5Sum          float64
	Load15Sum         float64
	MemoryUsedSum     float64
	MemoryTotalLast   uint64
	SwapUsedSum       float64
	SwapTotalLast     uint64
	DiskUsedSum       float64
	DiskTotalLast     uint64
	ReceiveRateSum    float64
	ReceiveRateMax    uint64
	TransmitRateSum   float64
	TransmitRateMax   uint64
	UptimeLast        uint64
	ProcessSum        float64
	TCPSum            float64
	UDPSum            float64
	ReceiveTotalLast  uint64
	TransmitTotalLast uint64
}

// HostPoint is the read projection returned to chart consumers.
type HostPoint struct {
	NodeID              string     `json:"node_id"`
	BucketStart         time.Time  `json:"bucket_start"`
	Resolution          Resolution `json:"resolution"`
	SampleCount         uint64     `json:"sample_count"`
	CPUCount            uint64     `json:"cpu_count"`
	LoadCount           uint64     `json:"load_count"`
	MemoryCount         uint64     `json:"memory_count"`
	DiskCount           uint64     `json:"disk_count"`
	NetworkCount        uint64     `json:"network_count"`
	UptimeCount         uint64     `json:"uptime_count"`
	ProcessCount        uint64     `json:"process_count"`
	ConnectionCount     uint64     `json:"connection_count"`
	CPUAverage          *float64   `json:"cpu_average"`
	CPUMin              *float64   `json:"cpu_min"`
	CPUMax              *float64   `json:"cpu_max"`
	Load1Average        *float64   `json:"load_1_average"`
	Load5Average        *float64   `json:"load_5_average"`
	Load15Average       *float64   `json:"load_15_average"`
	MemoryUsedAverage   *float64   `json:"memory_used_average"`
	MemoryTotalLast     *uint64    `json:"memory_total_last"`
	SwapUsedAverage     *float64   `json:"swap_used_average"`
	SwapTotalLast       *uint64    `json:"swap_total_last"`
	DiskUsedAverage     *float64   `json:"disk_used_average"`
	DiskTotalLast       *uint64    `json:"disk_total_last"`
	ReceiveRateAverage  *float64   `json:"receive_rate_average"`
	ReceiveRateMax      *uint64    `json:"receive_rate_max"`
	TransmitRateAverage *float64   `json:"transmit_rate_average"`
	TransmitRateMax     *uint64    `json:"transmit_rate_max"`
	UptimeLast          *uint64    `json:"uptime_last"`
	ProcessAverage      *float64   `json:"process_average"`
	TCPAverage          *float64   `json:"tcp_average"`
	UDPAverage          *float64   `json:"udp_average"`
}

// ProbeBucket is one completed node/task minute. Only successful samples
// contribute latency statistics; attempts always equal successes + failures.
type ProbeBucket struct {
	NodeID       string
	TaskID       string
	BucketStart  time.Time
	Attempts     uint64
	Successes    uint64
	Failures     uint64
	LatencyCount uint64
	LatencySum   float64
	LatencyMin   float64
	LatencyMax   float64
}

// ProbePoint is the read projection for TCP availability and latency charts.
type ProbePoint struct {
	NodeID         string     `json:"node_id"`
	TaskID         string     `json:"task_id"`
	BucketStart    time.Time  `json:"bucket_start"`
	Resolution     Resolution `json:"resolution"`
	Attempts       uint64     `json:"attempts"`
	Successes      uint64     `json:"successes"`
	Failures       uint64     `json:"failures"`
	SuccessRate    float64    `json:"success_rate"`
	LatencyCount   uint64     `json:"latency_count"`
	LatencyAverage *float64   `json:"latency_average"`
	LatencyMin     *float64   `json:"latency_min"`
	LatencyMax     *float64   `json:"latency_max"`
}

// TrafficDelta adds reset-aware bytes to the lifetime and UTC-month ledgers.
type TrafficDelta struct {
	NodeID        string
	Month         string
	ReceiveBytes  uint64
	TransmitBytes uint64
	UpdatedAt     time.Time
}

// TrafficTotal is a compact durable accounting row.
type TrafficTotal struct {
	NodeID        string    `json:"node_id"`
	Period        string    `json:"period"`
	ReceiveBytes  uint64    `json:"receive_bytes"`
	TransmitBytes uint64    `json:"transmit_bytes"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Retention controls the non-overlapping resolution ladder.
type Retention struct {
	OneMinute time.Duration
	TenMinute time.Duration
	OneHour   time.Duration
}

// DefaultRetention is 1m through 24h, 10m through 7d, and 1h through 30d.
var DefaultRetention = Retention{OneMinute: 24 * time.Hour, TenMinute: 7 * 24 * time.Hour, OneHour: 30 * 24 * time.Hour}

// ExtendedRetention returns the explicit 90- or 365-day hourly policy.
func ExtendedRetention(days int) (Retention, error) {
	if days != 90 && days != 365 {
		return Retention{}, errors.New("extended telemetry retention must be 90 or 365 days")
	}
	result := DefaultRetention
	result.OneHour = time.Duration(days) * 24 * time.Hour
	return result, nil
}

// State is the operator-visible history availability state.
type State struct {
	Available     bool      `json:"available"`
	Writable      bool      `json:"writable"`
	Reason        string    `json:"reason,omitempty"`
	QueueDepth    int       `json:"queue_depth"`
	QueueCapacity int       `json:"queue_capacity"`
	Dropped       uint64    `json:"dropped"`
	Maintenance   string    `json:"maintenance,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// StorageStats describes telemetry files and SQLite reclaim state.
type StorageStats struct {
	State         State      `json:"state"`
	MainBytes     int64      `json:"main_bytes"`
	WALBytes      int64      `json:"wal_bytes"`
	SHMBytes      int64      `json:"shm_bytes"`
	TotalBytes    int64      `json:"total_bytes"`
	FreeDiskBytes uint64     `json:"free_disk_bytes"`
	OldestBucket  *time.Time `json:"oldest_bucket,omitempty"`
	PageCount     int64      `json:"page_count"`
	FreelistPages int64      `json:"freelist_pages"`
}

// MaintenanceResult reports bounded maintenance work.
type MaintenanceResult struct {
	Promoted1m       int64 `json:"promoted_1m"`
	Promoted10m      int64 `json:"promoted_10m"`
	Deleted1m        int64 `json:"deleted_1m"`
	Deleted10m       int64 `json:"deleted_10m"`
	Deleted1h        int64 `json:"deleted_1h"`
	ProbePromoted1m  int64 `json:"probe_promoted_1m"`
	ProbePromoted10m int64 `json:"probe_promoted_10m"`
	ProbeDeleted1m   int64 `json:"probe_deleted_1m"`
	ProbeDeleted10m  int64 `json:"probe_deleted_10m"`
	ProbeDeleted1h   int64 `json:"probe_deleted_1h"`
}
