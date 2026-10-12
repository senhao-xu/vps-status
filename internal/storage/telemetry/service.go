package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const lifetimePeriod = "lifetime"

// Config bounds the telemetry writer and disk guard.
type Config struct {
	QueueCapacity    int
	BatchSize        int
	BatchDelay       time.Duration
	WriteTimeout     time.Duration
	GuardInterval    time.Duration
	FileBudgetBytes  int64
	MinimumFreeBytes uint64
	RecoveryBytes    uint64
	Retention        Retention
	DiskFree         func(string) (uint64, error)
	StorageBytes     func(string) (int64, error)
	StateChange      func(State)
}

func (c Config) withDefaults() Config {
	if c.QueueCapacity <= 0 {
		c.QueueCapacity = 1024
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 64
	}
	if c.BatchDelay <= 0 {
		c.BatchDelay = 50 * time.Millisecond
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 2 * time.Second
	}
	if c.GuardInterval <= 0 {
		c.GuardInterval = 5 * time.Second
	}
	if c.FileBudgetBytes == 0 {
		c.FileBudgetBytes = 512 << 20
	}
	if c.MinimumFreeBytes == 0 {
		c.MinimumFreeBytes = 256 << 20
	}
	if c.RecoveryBytes == 0 {
		c.RecoveryBytes = 64 << 20
	}
	if c.Retention == (Retention{}) {
		c.Retention = DefaultRetention
	}
	if c.DiskFree == nil {
		c.DiskFree = diskFree
	}
	if c.StorageBytes == nil {
		c.StorageBytes = sidecarTotal
	}
	return c
}

type requestKind uint8

const (
	requestWrite requestKind = iota
	requestMaintenance
	requestClearHistory
	requestResetAll
	requestCheckpoint
	requestIncrementalVacuum
	requestFullVacuum
	requestDeleteNode
	requestDeleteProbeTask
)

type serviceRequest struct {
	ctx      context.Context
	kind     requestKind
	bucket   *HostBucket
	probe    *ProbeBucket
	delta    *TrafficDelta
	now      time.Time
	pages    int
	nodeID   string
	taskID   string
	response chan serviceResponse
}
type serviceResponse struct {
	result MaintenanceResult
	err    error
}

// Service owns the only telemetry writer and its bounded work queue. Reads use
// the Store's separate read-only pool and remain available while writes pause.
type Service struct {
	store     *Store
	config    Config
	queue     chan serviceRequest
	done      chan struct{}
	stop      chan struct{}
	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
	storeMu   sync.RWMutex
	stateMu   sync.RWMutex
	state     State
}

// NewService starts the serialized telemetry writer.
func NewService(store *Store, config Config) (*Service, error) {
	if store == nil {
		return nil, errors.New("telemetry store is required")
	}
	config = config.withDefaults()
	if config.Retention.OneMinute <= 0 || config.Retention.TenMinute <= config.Retention.OneMinute || config.Retention.OneHour <= config.Retention.TenMinute {
		return nil, errors.New("telemetry retention ladder is invalid")
	}
	if config.FileBudgetBytes <= 0 {
		return nil, errors.New("telemetry file budget must be positive")
	}
	s := &Service{store: store, config: config, queue: make(chan serviceRequest, config.QueueCapacity), done: make(chan struct{}), stop: make(chan struct{})}
	s.state = State{Available: true, Writable: true, QueueCapacity: config.QueueCapacity, UpdatedAt: time.Now().UTC()}
	s.wg.Add(1)
	go s.run()
	return s, nil
}

// NewUnavailable creates an observable history boundary after telemetry open
// failed. It has no goroutines and never exposes the underlying error text.
func NewUnavailable(reason string) *Service {
	if reason == "" {
		reason = "open_failed"
	}
	return &Service{done: make(chan struct{}), stop: make(chan struct{}), state: State{Available: false, Writable: false, Reason: reason, UpdatedAt: time.Now().UTC()}}
}

// Close stops the writer and closes the underlying store. Queued history is
// deliberately not flushed after cancellation because current live state must
// never make Control shutdown unbounded.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.hasStore() {
			s.stateMu.Lock()
			s.state.Writable = false
			s.state.Reason = "closed"
			s.state.UpdatedAt = time.Now().UTC()
			s.stateMu.Unlock()
			close(s.stop)
			s.wg.Wait()
			s.storeMu.Lock()
			s.closeErr = s.store.Close()
			s.storeMu.Unlock()
		}
		close(s.done)
	})
	return s.closeErr
}

// State returns a race-safe copy of the current degradation state.
func (s *Service) State() State {
	if s == nil {
		return State{Available: false, Writable: false, Reason: "not_configured"}
	}
	s.stateMu.RLock()
	state := s.state
	s.stateMu.RUnlock()
	if s.queue != nil {
		state.QueueDepth = len(s.queue)
	}
	return state
}

// SubmitHost queues one completed minute bucket without blocking report ingestion.
func (s *Service) SubmitHost(bucket HostBucket) error {
	return s.submit(serviceRequest{kind: requestWrite, bucket: &bucket})
}

// SubmitProbe queues one completed TCP probe minute without storing results.
func (s *Service) SubmitProbe(bucket ProbeBucket) error {
	return s.submit(serviceRequest{kind: requestWrite, probe: &bucket})
}

// SubmitTraffic queues compact accounting deltas without storing a raw report.
func (s *Service) SubmitTraffic(delta TrafficDelta) error {
	return s.submit(serviceRequest{kind: requestWrite, delta: &delta})
}

func (s *Service) submit(req serviceRequest) error {
	if s == nil || !s.hasStore() {
		return ErrUnavailable
	}
	state := s.State()
	if state.Reason == "closed" {
		return ErrClosed
	}
	if !state.Writable {
		s.drop()
		return ErrUnavailable
	}
	select {
	case <-s.stop:
		return ErrClosed
	case s.queue <- req:
		return nil
	default:
		s.pause("queue_full")
		s.drop()
		return ErrQueueFull
	}
}

func (s *Service) drop() {
	s.stateMu.Lock()
	s.state.Dropped++
	s.state.UpdatedAt = time.Now().UTC()
	s.stateMu.Unlock()
}

func (s *Service) pause(reason string) {
	s.stateMu.Lock()
	changed := s.state.Writable || s.state.Reason != reason
	s.state.Available = true
	s.state.Writable = false
	s.state.Reason = reason
	s.state.UpdatedAt = time.Now().UTC()
	state := s.state
	s.stateMu.Unlock()
	if changed && s.config.StateChange != nil {
		state.QueueDepth = len(s.queue)
		s.config.StateChange(state)
	}
}

func (s *Service) resume() {
	s.stateMu.Lock()
	changed := !s.state.Writable || s.state.Reason != ""
	s.state.Available = true
	s.state.Writable = true
	s.state.Reason = ""
	s.state.UpdatedAt = time.Now().UTC()
	state := s.state
	s.stateMu.Unlock()
	if changed && s.config.StateChange != nil {
		state.QueueDepth = len(s.queue)
		s.config.StateChange(state)
	}
}

func (s *Service) setMaintenance(name string) {
	s.stateMu.Lock()
	s.state.Maintenance = name
	s.state.UpdatedAt = time.Now().UTC()
	s.stateMu.Unlock()
}

func (s *Service) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.GuardInterval)
	defer ticker.Stop()
	_, _ = s.checkGuard()
	var pending *serviceRequest
	for {
		var req serviceRequest
		if pending != nil {
			req = *pending
			pending = nil
		} else {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.recheck()
				continue
			case req = <-s.queue:
			}
		}
		if req.kind != requestWrite {
			s.handleOperation(req)
			continue
		}
		batch := []serviceRequest{req}
		timer := time.NewTimer(s.config.BatchDelay)
	collect:
		for len(batch) < s.config.BatchSize {
			select {
			case <-s.stop:
				timer.Stop()
				return
			case next := <-s.queue:
				if next.kind != requestWrite {
					pending = &next
					break collect
				}
				batch = append(batch, next)
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if !s.State().Writable {
			s.addDropped(uint64(len(batch)))
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.config.WriteTimeout)
		err := s.writeBatch(ctx, batch)
		cancel()
		if err != nil {
			s.pause("write_failed")
			s.addDropped(uint64(len(batch)))
		}
	}
}

func (s *Service) addDropped(count uint64) {
	s.stateMu.Lock()
	s.state.Dropped += count
	s.state.UpdatedAt = time.Now().UTC()
	s.stateMu.Unlock()
}

func (s *Service) recheck() {
	guarded, err := s.checkGuard()
	if err != nil || guarded {
		return
	}
	state := s.State()
	if state.Reason == "queue_full" && len(s.queue) > cap(s.queue)/2 {
		return
	}
	if state.Reason == "write_failed" {
		ctx, cancel := context.WithTimeout(context.Background(), s.config.WriteTimeout)
		s.storeMu.RLock()
		_, err := s.store.writer.ExecContext(ctx, `INSERT INTO maintenance_metadata(key,value) VALUES('writer_health','ok')
ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
		s.storeMu.RUnlock()
		cancel()
		if err != nil {
			return
		}
	}
	s.resume()
}

func (s *Service) checkGuard() (bool, error) {
	s.storeMu.RLock()
	path := s.store.path
	s.storeMu.RUnlock()
	total, err := s.config.StorageBytes(path)
	if err != nil {
		s.pause("storage_inspection_failed")
		return true, err
	}
	free, err := s.config.DiskFree(filepath.Dir(path))
	if err != nil {
		s.pause("storage_inspection_failed")
		return true, err
	}
	state := s.State()
	if s.config.FileBudgetBytes > 0 {
		limit := s.config.FileBudgetBytes
		if state.Reason == "file_budget" {
			limit -= limit / 10
		}
		if total >= limit {
			s.pause("file_budget")
			return true, nil
		}
	}
	if s.config.MinimumFreeBytes > 0 {
		limit := s.config.MinimumFreeBytes
		if state.Reason == "low_disk" {
			if math.MaxUint64-limit < s.config.RecoveryBytes {
				limit = math.MaxUint64
			} else {
				limit += s.config.RecoveryBytes
			}
		}
		if free <= limit {
			s.pause("low_disk")
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) writeBatch(ctx context.Context, batch []serviceRequest) error {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	tx, err := s.store.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, req := range batch {
		if req.bucket != nil {
			if err := writeHost(ctx, tx, *req.bucket); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if req.delta != nil {
			if err := writeTraffic(ctx, tx, *req.delta); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if req.probe != nil {
			if err := writeProbe(ctx, tx, *req.probe); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func writeProbe(ctx context.Context, tx *sql.Tx, b ProbeBucket) error {
	if b.NodeID == "" || len(b.NodeID) > 128 || b.TaskID == "" || len(b.TaskID) > 128 || b.BucketStart.IsZero() ||
		b.Attempts == 0 || b.Attempts > math.MaxInt64 || b.Successes > b.Attempts || b.Failures > b.Attempts ||
		b.Successes+b.Failures != b.Attempts || b.LatencyCount != b.Successes || math.IsNaN(b.LatencySum) ||
		math.IsInf(b.LatencySum, 0) || math.IsNaN(b.LatencyMin) || math.IsNaN(b.LatencyMax) ||
		b.LatencySum < 0 || b.LatencyMin < 0 || b.LatencyMax < b.LatencyMin || b.LatencyMax > 3000 || b.LatencySum > float64(b.LatencyCount)*3000 {
		return errors.New("invalid probe bucket")
	}
	var latencySum, latencyMin, latencyMax any
	if b.LatencyCount > 0 {
		latencySum, latencyMin, latencyMax = b.LatencySum, b.LatencyMin, b.LatencyMax
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO probe_1m(node_id,task_id,bucket_start,attempts,successes,failures,latency_count,latency_sum,latency_min,latency_max)
VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id,task_id,bucket_start) DO UPDATE SET
attempts=excluded.attempts,successes=excluded.successes,failures=excluded.failures,latency_count=excluded.latency_count,
latency_sum=excluded.latency_sum,latency_min=excluded.latency_min,latency_max=excluded.latency_max`,
		b.NodeID, b.TaskID, b.BucketStart.UTC().Truncate(time.Minute).Unix(), b.Attempts, b.Successes, b.Failures,
		b.LatencyCount, latencySum, latencyMin, latencyMax)
	return err
}

func writeHost(ctx context.Context, tx *sql.Tx, b HostBucket) error {
	if b.NodeID == "" || len(b.NodeID) > 128 || b.SampleCount == 0 || b.SampleCount > math.MaxInt64 || b.BucketStart.IsZero() ||
		b.CPUCount > b.SampleCount || b.LoadCount > b.SampleCount || b.MemoryCount > b.SampleCount || b.DiskCount > b.SampleCount ||
		b.NetworkCount > b.SampleCount || b.UptimeCount > b.SampleCount || b.ProcessCount > b.SampleCount || b.ConnectionCount > b.SampleCount ||
		b.MemoryTotalLast > math.MaxInt64 || b.SwapTotalLast > math.MaxInt64 || b.DiskTotalLast > math.MaxInt64 ||
		b.ReceiveRateMax > math.MaxInt64 || b.TransmitRateMax > math.MaxInt64 || b.UptimeLast > math.MaxInt64 ||
		b.ReceiveTotalLast > math.MaxInt64 || b.TransmitTotalLast > math.MaxInt64 {
		return errors.New("invalid host bucket")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO host_1m(
node_id,bucket_start,sample_count,cpu_count,load_count,memory_count,disk_count,network_count,uptime_count,process_count,connection_count,
cpu_sum,cpu_min,cpu_max,load1_sum,load5_sum,load15_sum,
memory_used_sum,memory_total_last,swap_used_sum,swap_total_last,disk_used_sum,disk_total_last,
rx_rate_sum,rx_rate_max,tx_rate_sum,tx_rate_max,uptime_last,process_sum,tcp_sum,udp_sum,rx_total_last,tx_total_last)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(node_id,bucket_start) DO UPDATE SET
sample_count=excluded.sample_count,cpu_count=excluded.cpu_count,load_count=excluded.load_count,memory_count=excluded.memory_count,
disk_count=excluded.disk_count,network_count=excluded.network_count,uptime_count=excluded.uptime_count,
process_count=excluded.process_count,connection_count=excluded.connection_count,
cpu_sum=excluded.cpu_sum,cpu_min=excluded.cpu_min,cpu_max=excluded.cpu_max,
load1_sum=excluded.load1_sum,load5_sum=excluded.load5_sum,load15_sum=excluded.load15_sum,
memory_used_sum=excluded.memory_used_sum,memory_total_last=excluded.memory_total_last,
swap_used_sum=excluded.swap_used_sum,swap_total_last=excluded.swap_total_last,disk_used_sum=excluded.disk_used_sum,disk_total_last=excluded.disk_total_last,
rx_rate_sum=excluded.rx_rate_sum,rx_rate_max=excluded.rx_rate_max,tx_rate_sum=excluded.tx_rate_sum,tx_rate_max=excluded.tx_rate_max,
uptime_last=excluded.uptime_last,process_sum=excluded.process_sum,tcp_sum=excluded.tcp_sum,udp_sum=excluded.udp_sum,
rx_total_last=excluded.rx_total_last,tx_total_last=excluded.tx_total_last`,
		b.NodeID, b.BucketStart.UTC().Truncate(time.Minute).Unix(), b.SampleCount,
		b.CPUCount, b.LoadCount, b.MemoryCount, b.DiskCount, b.NetworkCount, b.UptimeCount, b.ProcessCount, b.ConnectionCount,
		nullableMetric(b.CPUCount, b.CPUSum), nullableMetric(b.CPUCount, b.CPUMin), nullableMetric(b.CPUCount, b.CPUMax),
		nullableMetric(b.LoadCount, b.Load1Sum), nullableMetric(b.LoadCount, b.Load5Sum), nullableMetric(b.LoadCount, b.Load15Sum),
		nullableMetric(b.MemoryCount, b.MemoryUsedSum), nullableMetric(b.MemoryCount, b.MemoryTotalLast),
		nullableMetric(b.MemoryCount, b.SwapUsedSum), nullableMetric(b.MemoryCount, b.SwapTotalLast),
		nullableMetric(b.DiskCount, b.DiskUsedSum), nullableMetric(b.DiskCount, b.DiskTotalLast),
		nullableMetric(b.NetworkCount, b.ReceiveRateSum), nullableMetric(b.NetworkCount, b.ReceiveRateMax),
		nullableMetric(b.NetworkCount, b.TransmitRateSum), nullableMetric(b.NetworkCount, b.TransmitRateMax),
		nullableMetric(b.UptimeCount, b.UptimeLast), nullableMetric(b.ProcessCount, b.ProcessSum),
		nullableMetric(b.ConnectionCount, b.TCPSum), nullableMetric(b.ConnectionCount, b.UDPSum),
		nullableMetric(b.NetworkCount, b.ReceiveTotalLast), nullableMetric(b.NetworkCount, b.TransmitTotalLast))
	return err
}

func nullableMetric(count uint64, value any) any {
	if count == 0 {
		return nil
	}
	return value
}

func writeTraffic(ctx context.Context, tx *sql.Tx, d TrafficDelta) error {
	if d.NodeID == "" || len(d.NodeID) > 128 || len(d.Month) != 7 || d.Month[4] != '-' || d.ReceiveBytes > math.MaxInt64 || d.TransmitBytes > math.MaxInt64 || d.UpdatedAt.IsZero() {
		return errors.New("invalid traffic delta")
	}
	for _, period := range []string{lifetimePeriod, d.Month} {
		result, err := tx.ExecContext(ctx, `INSERT INTO traffic_ledger(node_id,period,receive_bytes,transmit_bytes,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(node_id,period) DO UPDATE SET receive_bytes=traffic_ledger.receive_bytes+excluded.receive_bytes,
transmit_bytes=traffic_ledger.transmit_bytes+excluded.transmit_bytes,updated_at=MAX(traffic_ledger.updated_at,excluded.updated_at)
WHERE traffic_ledger.receive_bytes <= ?-excluded.receive_bytes AND traffic_ledger.transmit_bytes <= ?-excluded.transmit_bytes`,
			d.NodeID, period, d.ReceiveBytes, d.TransmitBytes, d.UpdatedAt.UTC().Unix(), int64(math.MaxInt64), int64(math.MaxInt64))
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return errors.New("traffic ledger overflow")
		}
	}
	return nil
}

func (s *Service) call(ctx context.Context, req serviceRequest) (MaintenanceResult, error) {
	if s == nil || !s.hasStore() {
		return MaintenanceResult{}, ErrUnavailable
	}
	if s.State().Reason == "closed" {
		return MaintenanceResult{}, ErrClosed
	}
	req.response = make(chan serviceResponse, 1)
	req.ctx = ctx
	select {
	case <-ctx.Done():
		return MaintenanceResult{}, ctx.Err()
	case <-s.stop:
		return MaintenanceResult{}, ErrClosed
	case s.queue <- req:
	}
	select {
	case <-ctx.Done():
		return MaintenanceResult{}, ctx.Err()
	case <-s.stop:
		return MaintenanceResult{}, ErrClosed
	case response := <-req.response:
		return response.result, response.err
	}
}

// RunMaintenance promotes complete windows and applies chunked retention.
func (s *Service) RunMaintenance(ctx context.Context, now time.Time) (MaintenanceResult, error) {
	return s.call(ctx, serviceRequest{kind: requestMaintenance, now: now})
}

// ClearHistory removes all host rollups while preserving traffic accounting.
func (s *Service) ClearHistory(ctx context.Context) error {
	_, err := s.call(ctx, serviceRequest{kind: requestClearHistory})
	return err
}

// ResetAll removes history and the traffic ledger as an explicit destructive operation.
func (s *Service) ResetAll(ctx context.Context) error {
	_, err := s.call(ctx, serviceRequest{kind: requestResetAll})
	return err
}

// Checkpoint truncates the telemetry WAL after queued writes finish.
func (s *Service) Checkpoint(ctx context.Context) error {
	_, err := s.call(ctx, serviceRequest{kind: requestCheckpoint})
	return err
}

// IncrementalVacuum returns at most pages free pages to the filesystem.
func (s *Service) IncrementalVacuum(ctx context.Context, pages int) error {
	if pages <= 0 {
		pages = 1024
	}
	_, err := s.call(ctx, serviceRequest{kind: requestIncrementalVacuum, pages: pages})
	return err
}

// FullVacuum runs administrator-requested full compaction after a disk safety check.
func (s *Service) FullVacuum(ctx context.Context) error {
	_, err := s.call(ctx, serviceRequest{kind: requestFullVacuum})
	return err
}

// DeleteNode removes orphaned history and accounting idempotently.
func (s *Service) DeleteNode(ctx context.Context, nodeID string) error {
	if nodeID == "" || len(nodeID) > 128 {
		return errors.New("invalid telemetry node")
	}
	_, err := s.call(ctx, serviceRequest{kind: requestDeleteNode, nodeID: nodeID})
	return err
}

// DeleteProbeTask idempotently removes every node's history for one task.
func (s *Service) DeleteProbeTask(ctx context.Context, taskID string) error {
	if taskID == "" || len(taskID) > 128 {
		return errors.New("invalid telemetry probe task")
	}
	_, err := s.call(ctx, serviceRequest{kind: requestDeleteProbeTask, taskID: taskID})
	return err
}

func (s *Service) hasStore() bool {
	if s == nil {
		return false
	}
	s.storeMu.RLock()
	ok := s.store != nil
	s.storeMu.RUnlock()
	return ok
}

func (s *Service) handleOperation(req serviceRequest) {
	name := map[requestKind]string{requestMaintenance: "retention", requestClearHistory: "clear_history", requestResetAll: "reset_all", requestCheckpoint: "checkpoint", requestIncrementalVacuum: "incremental_vacuum", requestFullVacuum: "full_vacuum", requestDeleteNode: "delete_node", requestDeleteProbeTask: "delete_probe_task"}[req.kind]
	s.setMaintenance(name)
	parent := req.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	var result MaintenanceResult
	var err error
	switch req.kind {
	case requestMaintenance:
		result, err = s.maintain(ctx, req.now)
	case requestClearHistory:
		err = s.clearHistory(ctx)
	case requestResetAll:
		err = s.resetAll(ctx)
	case requestCheckpoint:
		err = checkpointDB(ctx, s.store.writer, "TRUNCATE")
	case requestIncrementalVacuum:
		_, err = s.store.writer.ExecContext(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", req.pages))
	case requestFullVacuum:
		err = s.fullVacuum(ctx)
	case requestDeleteNode:
		err = s.deleteNode(ctx, req.nodeID)
	case requestDeleteProbeTask:
		err = s.deleteProbeTask(ctx, req.taskID)
	}
	cancel()
	s.setMaintenance("")
	if err != nil {
		s.pause("maintenance_failed")
	} else {
		s.recheck()
	}
	if req.response != nil {
		req.response <- serviceResponse{result: result, err: err}
	}
}

func (s *Service) deleteNode(ctx context.Context, nodeID string) error {
	tx, err := s.store.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, table := range []string{"host_1m", "host_10m", "host_1h", "probe_1m", "probe_10m", "probe_1h", "traffic_ledger"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE node_id=?", nodeID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) deleteProbeTask(ctx context.Context, taskID string) error {
	tx, err := s.store.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, table := range []string{"probe_1m", "probe_10m", "probe_1h"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE task_id=?", taskID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) clearHistory(ctx context.Context) error {
	tx, err := s.store.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, table := range []string{"host_1m", "host_10m", "host_1h", "probe_1m", "probe_10m", "probe_1h"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err := checkpointDB(ctx, s.store.writer, "TRUNCATE"); err != nil {
		return err
	}
	_, err = s.store.writer.ExecContext(ctx, "PRAGMA incremental_vacuum(4096)")
	return err
}

func (s *Service) resetAll(ctx context.Context) error {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	path := s.store.path
	if err := checkpointDB(ctx, s.store.writer, "TRUNCATE"); err != nil {
		return err
	}
	if err := s.store.reader.Close(); err != nil {
		return err
	}
	if err := s.store.writer.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	oldImage := fmt.Sprintf("%s.reset-%s", path, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Rename(path, oldImage); err != nil {
		return err
	}
	replacement, err := Open(ctx, path)
	if err != nil {
		_ = os.Rename(oldImage, path)
		if recovered, recoverErr := Open(context.Background(), path); recoverErr == nil {
			s.store = recovered
		}
		return err
	}
	s.store = replacement
	if err := os.Remove(oldImage); err != nil {
		return fmt.Errorf("remove reset telemetry image: %w", err)
	}
	return nil
}

func (s *Service) fullVacuum(ctx context.Context) error {
	total, err := sidecarTotal(s.store.path)
	if err != nil {
		return err
	}
	free, err := s.config.DiskFree(filepath.Dir(s.store.path))
	if err != nil {
		return err
	}
	required := uint64(total)*2 + s.config.MinimumFreeBytes
	if free < required {
		return errors.New("insufficient free disk for full telemetry vacuum")
	}
	if err := checkpointDB(ctx, s.store.writer, "TRUNCATE"); err != nil {
		return err
	}
	if _, err := s.store.writer.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	return checkpointDB(ctx, s.store.writer, "TRUNCATE")
}

func sidecarTotal(path string) (int64, error) {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
}

func diskFree(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
