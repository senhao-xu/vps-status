package control

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

func testHistoryService(t *testing.T) (*HostHistory, *telemetry.Service) {
	t.Helper()
	store, err := telemetry.Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := telemetry.NewService(store, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return NewHostHistory(service), service
}

func historyReport(at time.Time, cpu float64, receive, transmit, uptime uint64) protocol.Report {
	return protocol.Report{CollectedAtUnixMilli: at.UnixMilli(), CPU: protocol.CPUStats{UsagePercent: cpu}, Load: protocol.LoadStats{One: 1, Five: 2, Fifteen: 3},
		Memory: protocol.MemoryStats{TotalBytes: 1000, UsedBytes: 600, AvailableBytes: 400, SwapTotalBytes: 100, SwapUsedBytes: 20}, Disk: protocol.DiskStats{TotalBytes: 2000, UsedBytes: 1500, FreeBytes: 500},
		Network: protocol.NetworkStats{ReceiveBytesPerSecond: 10, TransmitBytesPerSecond: 20, ReceiveBytesTotal: receive, TransmitBytesTotal: transmit}, UptimeSeconds: uptime, ProcessCount: 10, Connections: protocol.ConnectionStats{TCP: 2, UDP: 3}}
}

func TestHostHistoryKeepsRawReportsInMemoryUntilMinuteCompletes(t *testing.T) {
	history, service := testHistoryService(t)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	history.Observe("node", historyReport(start.Add(3*time.Second), 10, 100, 200, 100))
	history.Observe("node", historyReport(start.Add(6*time.Second), 30, 110, 220, 103))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	points, err := service.QueryHost(t.Context(), "node", telemetry.Resolution1m, start.Add(-time.Minute), start.Add(time.Minute))
	if err != nil || len(points) != 0 {
		t.Fatalf("raw reports persisted as points: %#v, %v", points, err)
	}
	history.Seal(start.Add(time.Minute))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	points, err = service.QueryHost(t.Context(), "node", telemetry.Resolution1m, start.Add(-time.Minute), start.Add(time.Minute))
	if err != nil || len(points) != 1 || points[0].SampleCount != 2 || points[0].CPUAverage == nil || *points[0].CPUAverage != 20 {
		t.Fatalf("completed bucket = %#v, %v", points, err)
	}
}

func TestTrafficLedgerHandlesNewNodeMonthResetAndReboot(t *testing.T) {
	history, service := testHistoryService(t)
	oct := time.Date(2026, 10, 31, 23, 59, 50, 0, time.UTC)
	history.Observe("node", historyReport(oct, 10, 100, 200, 100))
	history.Observe("node", historyReport(oct.Add(20*time.Second), 10, 150, 270, 103))
	history.Observe("node", historyReport(oct.Add(23*time.Second), 10, 10, 20, 106))
	history.Observe("node", historyReport(oct.Add(26*time.Second), 10, 20, 30, 1))
	history.Observe("node", historyReport(oct.Add(29*time.Second), 10, 30, 50, 4))
	// One direction may reset independently (for example after an interface
	// change); the still-monotonic direction must continue accounting.
	history.Observe("node", historyReport(oct.Add(32*time.Second), 10, 5, 60, 7))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	totals, err := service.Traffic(t.Context(), "node")
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string][2]uint64)
	for _, item := range totals {
		values[item.Period] = [2]uint64{item.ReceiveBytes, item.TransmitBytes}
	}
	if got := values["2026-10"]; got != [2]uint64{0, 0} {
		t.Fatalf("October total = %v; want zero baseline", got)
	}
	if got := values["2026-11"]; got != [2]uint64{60, 100} {
		t.Fatalf("November total = %v; want [60 100]", got)
	}
	if got := values["lifetime"]; got != [2]uint64{60, 100} {
		t.Fatalf("lifetime total = %v; want [60 100]", got)
	}
}

func TestTrafficLedgerIgnoresUnavailableAndOutOfOrderBaselines(t *testing.T) {
	history, service := testHistoryService(t)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	history.ObserveAt("node", historyReport(start, 10, 100, 200, 100), start)
	unavailable := historyReport(start.Add(3*time.Second), 10, 0, 0, 0)
	unavailable.Unavailable = []protocol.MetricArea{protocol.AreaNetwork, protocol.AreaUptime}
	history.ObserveAt("node", unavailable, start.Add(3*time.Second))
	history.ObserveAt("node", historyReport(start.Add(-time.Second), 10, 50, 60, 99), start.Add(6*time.Second))
	history.ObserveAt("node", historyReport(start.Add(9*time.Second), 10, 130, 250, 109), start.Add(9*time.Second))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	totals, err := service.Traffic(t.Context(), "node")
	if err != nil {
		t.Fatal(err)
	}
	for _, total := range totals {
		if total.ReceiveBytes != 30 || total.TransmitBytes != 50 {
			t.Fatalf("%s total = %d/%d; want 30/50", total.Period, total.ReceiveBytes, total.TransmitBytes)
		}
	}
}

func TestTrafficLedgerUsesBootIdentityAcrossSessions(t *testing.T) {
	history, service := testHistoryService(t)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	bootA := "550e8400-e29b-41d4-a716-446655440000"
	bootB := "550e8400-e29b-41d4-a716-446655440001"
	history.ObserveSessionAt("node", historyReport(start, 10, 100, 200, 100), start, bootA)
	// A replacement WSS session for the same boot keeps accounting even when
	// uptime is unexpectedly lower than the prior sample.
	history.ObserveSessionAt("node", historyReport(start.Add(3*time.Second), 10, 150, 250, 10), start.Add(3*time.Second), bootA)
	// A different boot is a reset even when the node was offline long enough
	// for its new uptime to exceed the previous boot's uptime.
	history.ObserveSessionAt("node", historyReport(start.Add(6*time.Second), 10, 200, 300, 1000), start.Add(6*time.Second), bootB)
	history.ObserveSessionAt("node", historyReport(start.Add(9*time.Second), 10, 230, 350, 1003), start.Add(9*time.Second), bootB)

	// Agents without boot identity retain the conservative uptime fallback.
	history.ObserveSessionAt("legacy", historyReport(start, 10, 100, 200, 100), start, "")
	history.ObserveSessionAt("legacy", historyReport(start.Add(3*time.Second), 10, 500, 600, 1000), start.Add(3*time.Second), "")
	history.ObserveSessionAt("legacy", historyReport(start.Add(6*time.Second), 10, 10, 20, 1), start.Add(6*time.Second), "")
	history.ObserveSessionAt("legacy", historyReport(start.Add(9*time.Second), 10, 20, 35, 4), start.Add(9*time.Second), "")
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	for node, want := range map[string][2]uint64{"node": {80, 100}, "legacy": {410, 415}} {
		totals, err := service.Traffic(t.Context(), node)
		if err != nil {
			t.Fatal(err)
		}
		for _, total := range totals {
			if got := [2]uint64{total.ReceiveBytes, total.TransmitBytes}; got != want {
				t.Fatalf("%s %s total=%v; want %v", node, total.Period, got, want)
			}
		}
	}
}

func TestHostHistoryKeepsPerMetricValidity(t *testing.T) {
	history, service := testHistoryService(t)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	first := historyReport(start.Add(3*time.Second), 99, 100, 200, 100)
	first.Unavailable = []protocol.MetricArea{protocol.AreaCPU}
	second := historyReport(start.Add(6*time.Second), 25, 110, 220, 103)
	second.Unavailable = []protocol.MetricArea{protocol.AreaMemory}
	history.ObserveAt("node", first, start.Add(3*time.Second))
	history.ObserveAt("node", second, start.Add(6*time.Second))
	history.Seal(start.Add(time.Minute))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	points, err := service.QueryHost(t.Context(), "node", telemetry.Resolution1m, start, start.Add(time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("points=%#v, %v", points, err)
	}
	point := points[0]
	if point.SampleCount != 2 || point.CPUCount != 1 || point.MemoryCount != 1 || point.CPUAverage == nil || *point.CPUAverage != 25 || point.MemoryUsedAverage == nil || *point.MemoryUsedAverage != 600 {
		t.Fatalf("metric validity point=%#v", point)
	}
	missing := historyReport(start.Add(9*time.Second), 99, 999, 999, 999)
	missing.Unavailable = []protocol.MetricArea{protocol.AreaCPU, protocol.AreaLoad, protocol.AreaMemory, protocol.AreaDisk, protocol.AreaNetwork, protocol.AreaUptime, protocol.AreaProcesses, protocol.AreaConnections}
	history.ObserveAt("missing", missing, start.Add(9*time.Second))
	history.Seal(start.Add(time.Minute))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	points, err = service.QueryHost(t.Context(), "missing", telemetry.Resolution1m, start, start.Add(time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("missing points=%#v, %v", points, err)
	}
	missingPoint := points[0]
	if missingPoint.SampleCount != 1 || missingPoint.CPUCount != 0 || missingPoint.LoadCount != 0 || missingPoint.MemoryCount != 0 || missingPoint.DiskCount != 0 ||
		missingPoint.NetworkCount != 0 || missingPoint.UptimeCount != 0 || missingPoint.ProcessCount != 0 || missingPoint.ConnectionCount != 0 ||
		missingPoint.CPUAverage != nil || missingPoint.Load1Average != nil || missingPoint.MemoryUsedAverage != nil || missingPoint.DiskUsedAverage != nil ||
		missingPoint.ReceiveRateAverage != nil || missingPoint.UptimeLast != nil || missingPoint.ProcessAverage != nil || missingPoint.TCPAverage != nil {
		t.Fatalf("all-unavailable report fabricated values: %#v", missingPoint)
	}
}

func TestHostHistoryRecoveryStartsAtNextCompleteMinute(t *testing.T) {
	store, err := telemetry.Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	var free atomic.Uint64
	free.Store(1000)
	service, err := telemetry.NewService(store, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 100, RecoveryBytes: 10, GuardInterval: time.Millisecond, DiskFree: func(string) (uint64, error) { return free.Load(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	history := NewHostHistory(service)
	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	history.ObserveAt("node", historyReport(start.Add(3*time.Second), 10, 100, 100, 100), start.Add(3*time.Second))
	free.Store(50)
	waitTelemetryWritable(t, service, false)
	history.ObserveAt("node", historyReport(start.Add(30*time.Second), 20, 110, 110, 127), start.Add(30*time.Second))
	free.Store(1000)
	waitTelemetryWritable(t, service, true)
	history.ObserveAt("node", historyReport(start.Add(time.Minute+3*time.Second), 30, 120, 120, 160), start.Add(time.Minute+3*time.Second))
	history.ObserveAt("node", historyReport(start.Add(2*time.Minute+3*time.Second), 40, 130, 130, 220), start.Add(2*time.Minute+3*time.Second))
	history.Seal(start.Add(3 * time.Minute))
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	points, err := service.QueryHost(t.Context(), "node", telemetry.Resolution1m, start, start.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || !points[0].BucketStart.Equal(start.Add(2*time.Minute)) || points[0].CPUAverage == nil || *points[0].CPUAverage != 40 {
		t.Fatalf("recovered points = %#v; want only complete 12:02 bucket", points)
	}
}

func waitTelemetryWritable(t *testing.T, service *telemetry.Service, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.State().Writable == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("telemetry writable = %v; want %v, state=%#v", service.State().Writable, want, service.State())
}
