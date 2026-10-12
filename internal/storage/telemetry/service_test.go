package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestService(t *testing.T, mutate func(*Config)) *Service {
	t.Helper()
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	config := Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, RecoveryBytes: 1, GuardInterval: time.Hour}
	if mutate != nil {
		mutate(&config)
	}
	service, err := NewService(store, config)
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return service
}

func testBucket(node string, at time.Time, value float64) HostBucket {
	return HostBucket{NodeID: node, BucketStart: at, SampleCount: 2,
		CPUCount: 2, LoadCount: 2, MemoryCount: 2, DiskCount: 2, NetworkCount: 2, UptimeCount: 2, ProcessCount: 2, ConnectionCount: 2,
		CPUSum: value * 2, CPUMin: value - 1, CPUMax: value + 1, Load1Sum: 2, Load5Sum: 2, Load15Sum: 2, MemoryUsedSum: 200, MemoryTotalLast: 1000, SwapUsedSum: 20, SwapTotalLast: 100, DiskUsedSum: 400, DiskTotalLast: 2000, ReceiveRateSum: 40, ReceiveRateMax: 30, TransmitRateSum: 60, TransmitRateMax: 40, UptimeLast: 100, ProcessSum: 20, TCPSum: 4, UDPSum: 6, ReceiveTotalLast: 1000, TransmitTotalLast: 2000}
}

func setBucketCounts(bucket *HostBucket, count uint64) {
	bucket.SampleCount = count
	bucket.CPUCount, bucket.LoadCount, bucket.MemoryCount, bucket.DiskCount = count, count, count, count
	bucket.NetworkCount, bucket.UptimeCount, bucket.ProcessCount, bucket.ConnectionCount = count, count, count, count
}

func syncWrites(t *testing.T, s *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := s.Checkpoint(ctx); err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}
}

func TestOpenCreatesIndependentIncrementalVacuumSchema(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "telemetry.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("telemetry mode = %v error = %v; want 0600", info.Mode().Perm(), err)
	}
	var autoVacuum int
	if err := store.reader.QueryRowContext(t.Context(), "PRAGMA auto_vacuum").Scan(&autoVacuum); err != nil || autoVacuum != 2 {
		t.Fatalf("auto_vacuum = %d, %v; want 2", autoVacuum, err)
	}
	for _, table := range []string{"host_1m", "host_10m", "host_1h", "probe_1m", "probe_10m", "probe_1h", "traffic_ledger"} {
		var exists int
		if err := store.reader.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil || exists != 1 {
			t.Fatalf("table %s exists = %d, %v", table, exists, err)
		}
	}
	for _, forbidden := range []string{"reports", "samples", "node_reports"} {
		var exists int
		_ = store.reader.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE name=?", forbidden).Scan(&exists)
		if exists != 0 {
			t.Fatalf("raw telemetry table %q exists", forbidden)
		}
	}
}

func TestOpenUpgradesV1MetricValidityWithImmutableBackup(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "telemetry.db")
	db, err := openWriter(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := telemetryMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyTelemetryMigrations(t.Context(), db, 0, migrations[:1]); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"host_1m", "host_10m", "host_1h"} {
		if _, err := db.Exec(`INSERT INTO ` + table + ` VALUES(
			'node',1700000000,2,40,10,30,2,4,6,1200,1000,40,100,3000,2000,20,15,30,20,100,20,4,6,1000,2000)`); err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.reader.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("version=%d, %v", version, err)
	}
	for _, table := range []string{"host_1m", "host_10m", "host_1h"} {
		var sampleCount uint64
		var counts [8]uint64
		var cpuSum float64
		var memoryTotal, receiveTotal uint64
		err := store.reader.QueryRow(`SELECT sample_count,cpu_count,load_count,memory_count,disk_count,network_count,uptime_count,process_count,connection_count,cpu_sum,memory_total_last,rx_total_last FROM `+table+` WHERE node_id='node'`).Scan(
			&sampleCount, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7], &cpuSum, &memoryTotal, &receiveTotal)
		if err != nil {
			t.Fatalf("read migrated %s: %v", table, err)
		}
		if sampleCount != 2 || counts != [8]uint64{2, 2, 2, 2, 2, 2, 2, 2} {
			t.Fatalf("%s migrated sample/counts=%d/%v", table, sampleCount, counts)
		}
		if cpuSum != 40 || memoryTotal != 1000 || receiveTotal != 1000 {
			t.Fatalf("%s migrated values=%v/%d/%d", table, cpuSum, memoryTotal, receiveTotal)
		}
	}
	backups, _ := filepath.Glob(path + ".backup-v1-*")
	if len(backups) != 1 {
		t.Fatalf("v1 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var cpuColumn int
	if err := backupDB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('host_1m') WHERE name='cpu_count'`).Scan(&cpuColumn); err != nil {
		t.Fatal(err)
	}
	if cpuColumn != 0 {
		t.Fatal("v1 backup was mutated with v2 validity columns")
	}
}

func TestOpenUpgradesV2WithBackupBeforeProbeSchema(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "telemetry.db")
	db, err := openWriter(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := telemetryMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyTelemetryMigrations(t.Context(), db, 0, migrations[:2]); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backups, _ := filepath.Glob(path + ".backup-v2-*")
	if len(backups) != 1 {
		t.Fatalf("v2 backup count=%d", len(backups))
	}
	backupDB, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	for _, table := range []string{"probe_1m", "probe_10m", "probe_1h"} {
		var count int
		if err := backupDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("v2 backup was mutated with v3 table %s", table)
		}
	}
}

func TestOpenRejectsCorruptDatabaseWithoutReplacingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	original := []byte("not a sqlite database")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), path); err == nil {
		t.Fatal("Open(corrupt) succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("corrupt database was replaced: %q", after)
	}
}

func TestWriterHistoryTrafficClearAndReset(t *testing.T) {
	s := newTestService(t, nil)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if err := s.SubmitHost(testBucket("node-1", now, 20)); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitTraffic(TrafficDelta{NodeID: "node-1", Month: "2026-10", ReceiveBytes: 100, TransmitBytes: 200, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	syncWrites(t, s)
	points, err := s.QueryHost(t.Context(), "node-1", Resolution1m, now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil || len(points) != 1 || points[0].CPUAverage == nil || *points[0].CPUAverage != 20 {
		t.Fatalf("QueryHost = %#v, %v", points, err)
	}
	totals, err := s.Traffic(t.Context(), "node-1")
	if err != nil || len(totals) != 2 {
		t.Fatalf("Traffic() = %#v, %v", totals, err)
	}
	if err := s.ClearHistory(t.Context()); err != nil {
		t.Fatalf("ClearHistory() error = %v", err)
	}
	points, _ = s.QueryHost(t.Context(), "node-1", Resolution1m, now.Add(-time.Minute), now.Add(time.Minute))
	totals, _ = s.Traffic(t.Context(), "node-1")
	if len(points) != 0 || len(totals) != 2 {
		t.Fatalf("clear left points=%d totals=%d; want 0,2", len(points), len(totals))
	}
	if err := s.ResetAll(t.Context()); err != nil {
		t.Fatalf("ResetAll() error = %v", err)
	}
	totals, _ = s.Traffic(t.Context(), "node-1")
	if len(totals) != 0 {
		t.Fatalf("reset left %d traffic rows", len(totals))
	}
}

func TestRollupRetentionIsIdempotentAndNonOverlapping(t *testing.T) {
	s := newTestService(t, nil)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, age := range []time.Duration{30 * time.Minute, 2 * 24 * time.Hour, 10 * 24 * time.Hour, 40 * 24 * time.Hour} {
		if err := s.SubmitHost(testBucket("node-1", now.Add(-age), 10)); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, s)
	for run := 0; run < 2; run++ {
		if _, err := s.RunMaintenance(t.Context(), now); err != nil {
			t.Fatalf("RunMaintenance(%d) error = %v", run, err)
		}
	}
	cases := []struct {
		resolution Resolution
		from, to   time.Time
		want       int
	}{{Resolution1m, now.Add(-time.Hour), now, 1}, {Resolution10m, now.Add(-3 * 24 * time.Hour), now.Add(-24 * time.Hour), 1}, {Resolution1h, now.Add(-11 * 24 * time.Hour), now.Add(-7 * 24 * time.Hour), 1}}
	for _, tc := range cases {
		points, err := s.QueryHost(t.Context(), "node-1", tc.resolution, tc.from, tc.to)
		if err != nil || len(points) != tc.want {
			t.Fatalf("%s points = %d, %v; want %d", tc.resolution, len(points), err, tc.want)
		}
	}
	for _, table := range []string{"host_1m", "host_10m", "host_1h"} {
		var count int
		if err := s.store.reader.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s row count = %d, %v; want 1", table, count, err)
		}
	}
}

func TestRollupPreservesNewestLastValuesAndWeightedAverage(t *testing.T) {
	s := newTestService(t, nil)
	start := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	older := testBucket("node-1", start, 10)
	setBucketCounts(&older, 1)
	older.CPUSum = 10
	older.MemoryTotalLast = 2000
	older.UptimeLast = 1000
	older.ReceiveTotalLast = 5000
	newer := testBucket("node-1", start.Add(time.Minute), 20)
	setBucketCounts(&newer, 3)
	newer.CPUSum = 60
	newer.MemoryTotalLast = 1000
	newer.UptimeLast = 10
	newer.ReceiveTotalLast = 50
	for _, bucket := range []HostBucket{older, newer} {
		if err := s.SubmitHost(bucket); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, s)
	if _, err := s.rollup(t.Context(), "host_1m", "host_10m", 10*time.Minute, start.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	points, err := s.QueryHost(t.Context(), "node-1", Resolution10m, start, start.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].SampleCount != 4 || points[0].CPUAverage == nil || *points[0].CPUAverage != 17.5 {
		t.Fatalf("weighted rollup = %#v", points)
	}
	var memoryTotal, uptime, receiveTotal uint64
	if err := s.store.reader.QueryRowContext(t.Context(), `SELECT memory_total_last,uptime_last,rx_total_last FROM host_10m WHERE node_id=? AND bucket_start=?`, "node-1", start.Unix()).Scan(&memoryTotal, &uptime, &receiveTotal); err != nil {
		t.Fatal(err)
	}
	if memoryTotal != newer.MemoryTotalLast || uptime != newer.UptimeLast || receiveTotal != newer.ReceiveTotalLast {
		t.Fatalf("last values = %d/%d/%d; want %d/%d/%d", memoryTotal, uptime, receiveTotal, newer.MemoryTotalLast, newer.UptimeLast, newer.ReceiveTotalLast)
	}
}

func TestUnavailableMetricsRemainNullAndRollupUsesNewestValidValue(t *testing.T) {
	s := newTestService(t, nil)
	start := time.Date(2026, 10, 10, 11, 0, 0, 0, time.UTC)
	older := testBucket("node-1", start, 10)
	older.MemoryTotalLast = 2000
	newer := testBucket("node-1", start.Add(time.Minute), 20)
	newer.MemoryCount = 0
	newer.MemoryUsedSum, newer.SwapUsedSum = 999, 999
	newer.MemoryTotalLast, newer.SwapTotalLast = 999, 999
	for _, bucket := range []HostBucket{older, newer} {
		if err := s.SubmitHost(bucket); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, s)
	points, err := s.QueryHost(t.Context(), "node-1", Resolution1m, start.Add(time.Minute), start.Add(2*time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("newest point=%#v, %v", points, err)
	}
	if points[0].MemoryCount != 0 || points[0].MemoryUsedAverage != nil || points[0].MemoryTotalLast != nil || points[0].SwapUsedAverage != nil {
		t.Fatalf("unavailable memory serialized as a value: %#v", points[0])
	}
	if _, err := s.rollup(t.Context(), "host_1m", "host_10m", 10*time.Minute, start.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	points, err = s.QueryHost(t.Context(), "node-1", Resolution10m, start, start.Add(10*time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("rollup point=%#v, %v", points, err)
	}
	if points[0].MemoryCount != older.MemoryCount || points[0].MemoryTotalLast == nil || *points[0].MemoryTotalLast != older.MemoryTotalLast {
		t.Fatalf("rollup did not retain newest valid memory value: %#v", points[0])
	}
}

func TestQueueFullStateRecoversAfterCapacityOneQueueDrains(t *testing.T) {
	s := newTestService(t, func(c *Config) {
		c.QueueCapacity = 1
		c.BatchSize = 1
	})
	s.pause("queue_full")
	s.recheck()
	if state := s.State(); !state.Writable || state.Reason != "" {
		t.Fatalf("capacity-one queue did not recover after drain: %#v", state)
	}
}

func TestDiskGuardUsesRecoveryHysteresis(t *testing.T) {
	var free atomic.Uint64
	free.Store(200)
	s := newTestService(t, func(c *Config) {
		c.MinimumFreeBytes = 100
		c.RecoveryBytes = 20
		c.DiskFree = func(string) (uint64, error) { return free.Load(), nil }
	})
	free.Store(50)
	s.recheck()
	state := s.State()
	if state.Writable || state.Reason != "low_disk" {
		t.Fatalf("low disk state = %#v", state)
	}
	free.Store(110)
	s.recheck()
	if s.State().Writable {
		t.Fatal("guard recovered before hysteresis threshold")
	}
	free.Store(130)
	s.recheck()
	if !s.State().Writable || s.State().Reason != "" {
		t.Fatalf("recovered state = %#v", s.State())
	}
}

func TestFileBudgetGuardUsesRecoveryHysteresis(t *testing.T) {
	var used atomic.Int64
	used.Store(80)
	service := newTestService(t, func(c *Config) {
		c.FileBudgetBytes = 100
		c.StorageBytes = func(string) (int64, error) { return used.Load(), nil }
	})
	used.Store(100)
	service.recheck()
	if state := service.State(); state.Writable || state.Reason != "file_budget" {
		t.Fatalf("budget state = %#v", state)
	}
	used.Store(95)
	service.recheck()
	if service.State().Writable {
		t.Fatal("file budget recovered above 90 percent")
	}
	used.Store(89)
	service.recheck()
	if state := service.State(); !state.Writable || state.Reason != "" {
		t.Fatalf("budget recovery = %#v", state)
	}
}

func TestOpenFailureAndTrafficOverflow(t *testing.T) {
	directory := t.TempDir()
	if _, err := Open(t.Context(), directory); err == nil {
		t.Fatal("Open(directory) succeeded")
	}
	s := newTestService(t, nil)
	now := time.Now().UTC()
	if err := s.SubmitTraffic(TrafficDelta{NodeID: "n", Month: "2026-10", ReceiveBytes: math.MaxInt64, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	syncWrites(t, s)
	if err := s.SubmitTraffic(TrafficDelta{NodeID: "n", Month: "2026-10", ReceiveBytes: 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && s.State().Reason != "write_failed" {
		time.Sleep(time.Millisecond)
	}
	if s.State().Reason != "write_failed" {
		t.Fatalf("overflow state = %#v; want write_failed", s.State())
	}
	if !errors.Is(NewUnavailable("open_failed").SubmitHost(testBucket("n", now, 1)), ErrUnavailable) {
		t.Fatal("unavailable service accepted history")
	}
}

func TestReadOnlyWriterDegradesWithoutLosingQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	created, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	writer.SetMaxOpenConns(1)
	reader, err := openReader(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(&Store{writer: writer, reader: reader, path: path}, Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour, WriteTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if err := service.SubmitHost(testBucket("node", time.Now().UTC(), 1)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && service.State().Reason != "write_failed" {
		time.Sleep(time.Millisecond)
	}
	if service.State().Reason != "write_failed" {
		t.Fatalf("read-only state = %#v", service.State())
	}
	service.recheck()
	if state := service.State(); state.Writable || state.Reason != "write_failed" {
		t.Fatalf("read-only writer falsely recovered = %#v", state)
	}
	if _, err := service.QueryHost(t.Context(), "node", Resolution1m, time.Now().Add(-time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("read query while paused = %v", err)
	}
}

func TestBusyTimeoutQueueFullAndCancellation(t *testing.T) {
	service := newTestService(t, func(c *Config) {
		c.QueueCapacity = 1
		c.BatchSize = 1
		c.WriteTimeout = 30 * time.Millisecond
		c.BatchDelay = time.Millisecond
	})
	conn, err := service.store.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SubmitHost(testBucket("one", time.Now().UTC(), 1)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(service.queue) != 0 {
		time.Sleep(time.Millisecond)
	}
	if err := service.SubmitHost(testBucket("two", time.Now().UTC(), 2)); err != nil {
		t.Fatal(err)
	}
	if err := service.SubmitHost(testBucket("three", time.Now().UTC(), 3)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third enqueue error = %v; want ErrQueueFull", err)
	}
	cancelCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := service.Checkpoint(cancelCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled checkpoint error = %v", err)
	}
	_ = tx.Rollback()
	_ = conn.Close()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) && service.State().Reason != "write_failed" {
		time.Sleep(time.Millisecond)
	}
	if service.State().Dropped == 0 {
		t.Fatalf("busy/full writer did not report dropped work: %#v", service.State())
	}
}

func TestCheckpointBoundsWALAndResetRecreatesSchema(t *testing.T) {
	service := newTestService(t, nil)
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 300; i++ {
		if err := service.SubmitHost(testBucket("node", now.Add(time.Duration(i)*time.Minute), float64(i%100))); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, service)
	stats, err := service.Stats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stats.WALBytes > 4<<20 {
		t.Fatalf("WAL bytes = %d; want <= 4 MiB", stats.WALBytes)
	}
	before, err := os.Stat(service.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ResetAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(service.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("ResetAll did not recreate telemetry.db")
	}
	var version int
	if err := service.store.reader.QueryRowContext(t.Context(), "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 3 {
		t.Fatalf("reset schema version = %d, %v", version, err)
	}
}

func TestResetAllSerializesConcurrentReadersAcrossStoreSwap(t *testing.T) {
	service := newTestService(t, nil)
	now := time.Now().UTC().Truncate(time.Minute)
	if err := service.SubmitHost(testBucket("node", now, 1)); err != nil {
		t.Fatal(err)
	}
	syncWrites(t, service)

	stop := make(chan struct{})
	errorsSeen := make(chan error, 8)
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := service.QueryHost(t.Context(), "node", Resolution1m, now.Add(-time.Minute), now.Add(time.Minute)); err != nil {
					errorsSeen <- err
					return
				}
				if _, err := service.Stats(t.Context()); err != nil {
					errorsSeen <- err
					return
				}
			}
		}()
	}
	for range 3 {
		if err := service.ResetAll(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	readers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent reader observed reset store swap: %v", err)
	}
}

func TestWaitingOperationReturnsWhenServiceCloses(t *testing.T) {
	service := newTestService(t, func(c *Config) {
		c.BatchSize = 1
		c.WriteTimeout = 100 * time.Millisecond
	})
	conn, err := service.store.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	if err := service.SubmitHost(testBucket("node", time.Now().UTC(), 1)); err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(service.queue) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(service.queue) != 0 {
		_ = tx.Rollback()
		_ = conn.Close()
		t.Fatal("writer did not begin the blocking write")
	}
	operationResult := make(chan error, 1)
	go func() { operationResult <- service.Checkpoint(context.Background()) }()
	closeResult := make(chan error, 1)
	go func() { closeResult <- service.Close() }()
	select {
	case err := <-operationResult:
		if !errors.Is(err, ErrClosed) {
			_ = tx.Rollback()
			_ = conn.Close()
			t.Fatalf("waiting operation error = %v; want ErrClosed", err)
		}
	case <-time.After(time.Second):
		_ = tx.Rollback()
		_ = conn.Close()
		t.Fatal("waiting operation did not return when service closed")
	}
	_ = tx.Rollback()
	_ = conn.Close()
	if err := <-closeResult; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestServiceCloseIsIdempotent(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("first Close() = %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
	if err := service.SubmitHost(testBucket("node", time.Now().UTC(), 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("submit after close = %v; want ErrClosed", err)
	}
}

func TestConcurrentTrafficUsesSingleWriterWithoutLostUpdates(t *testing.T) {
	service := newTestService(t, func(c *Config) { c.QueueCapacity = 1024; c.BatchSize = 64 })
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for worker := 0; worker < 20; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				for {
					err := service.SubmitTraffic(TrafficDelta{NodeID: "node", Month: "2026-10", ReceiveBytes: 1, TransmitBytes: 2, UpdatedAt: now})
					if err == nil {
						break
					}
					if !errors.Is(err, ErrQueueFull) {
						t.Errorf("SubmitTraffic() = %v", err)
						return
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()
	syncWrites(t, service)
	totals, err := service.Traffic(t.Context(), "node")
	if err != nil {
		t.Fatal(err)
	}
	for _, total := range totals {
		if total.ReceiveBytes != 200 || total.TransmitBytes != 400 {
			t.Fatalf("%s total = %d/%d; want 200/400", total.Period, total.ReceiveBytes, total.TransmitBytes)
		}
	}
}
