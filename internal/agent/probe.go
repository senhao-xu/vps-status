package agent

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

const defaultProbeConcurrency = 8

type probeResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type probeDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// ProbeSchedulerConfig supplies bounded dependencies and test seams.
type ProbeSchedulerConfig struct {
	Resolver       probeResolver
	Dialer         probeDialer
	Concurrency    int
	TotalTimeout   time.Duration
	AddressTimeout time.Duration
	Now            func() time.Time
}

func (config ProbeSchedulerConfig) withDefaults() ProbeSchedulerConfig {
	if config.Resolver == nil {
		config.Resolver = net.DefaultResolver
	}
	if config.Dialer == nil {
		config.Dialer = &net.Dialer{}
	}
	if config.Concurrency <= 0 || config.Concurrency > defaultProbeConcurrency {
		config.Concurrency = defaultProbeConcurrency
	}
	if config.TotalTimeout <= 0 || config.TotalTimeout > 3*time.Second {
		config.TotalTimeout = 3 * time.Second
	}
	if config.AddressTimeout <= 0 || config.AddressTimeout > time.Second {
		config.AddressTimeout = time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config
}

type scheduledProbe struct {
	task   protocol.ProbeTask
	cancel context.CancelFunc
}

// ProbeScheduler owns a session-local complete task snapshot. Each task has a
// serial delay schedule and all tasks share one bounded semaphore.
type ProbeScheduler struct {
	ctx       context.Context
	cancel    context.CancelFunc
	config    ProbeSchedulerConfig
	report    func(protocol.ProbeResult) error
	semaphore chan struct{}
	mu        sync.Mutex
	version   uint64
	tasks     map[string]scheduledProbe
	wg        sync.WaitGroup
}

// NewProbeScheduler creates an idle session-local scheduler.
func NewProbeScheduler(parent context.Context, config ProbeSchedulerConfig, report func(protocol.ProbeResult) error) *ProbeScheduler {
	ctx, cancel := context.WithCancel(parent)
	config = config.withDefaults()
	return &ProbeScheduler{ctx: ctx, cancel: cancel, config: config, report: report,
		semaphore: make(chan struct{}, config.Concurrency), tasks: make(map[string]scheduledProbe)}
}

// Apply replaces the complete snapshot when its version advances. Malformed
// entries are omitted and reported, never clamped into a different task.
func (scheduler *ProbeScheduler) Apply(sync protocol.ProbeSync) protocol.ProbeSyncStatus {
	status := protocol.ProbeSyncStatus{Version: sync.Version}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if sync.Version == 0 || sync.Version < scheduler.version {
		status.Rejected = []protocol.ProbeRejection{{Code: "stale_version"}}
		return status
	}
	if sync.Version == scheduler.version {
		status.Accepted = uint32(len(scheduler.tasks))
		return status
	}
	if len(sync.Tasks) > protocol.MaxProbeTasks {
		status.Rejected = []protocol.ProbeRejection{{Code: "task_limit"}}
		return status
	}
	desired := make(map[string]protocol.ProbeTask, len(sync.Tasks))
	for _, task := range sync.Tasks {
		if _, duplicate := desired[task.ID]; duplicate {
			status.Rejected = append(status.Rejected, protocol.ProbeRejection{TaskID: boundedTaskID(task.ID), Code: "duplicate"})
			continue
		}
		if task.IntervalSeconds < protocol.MinProbeIntervalSeconds || task.IntervalSeconds > protocol.MaxProbeIntervalSeconds {
			status.Rejected = append(status.Rejected, protocol.ProbeRejection{TaskID: boundedTaskID(task.ID), Code: "interval"})
			continue
		}
		if protocol.ValidateProbeTask(task) != nil {
			status.Rejected = append(status.Rejected, protocol.ProbeRejection{TaskID: boundedTaskID(task.ID), Code: "target"})
			continue
		}
		desired[task.ID] = task
	}
	for id, running := range scheduler.tasks {
		if task, keep := desired[id]; keep && task == running.task {
			delete(desired, id)
			continue
		}
		running.cancel()
		delete(scheduler.tasks, id)
	}
	for id, task := range desired {
		ctx, cancel := context.WithCancel(scheduler.ctx)
		running := scheduledProbe{task: task, cancel: cancel}
		scheduler.tasks[id] = running
		scheduler.wg.Add(1)
		go scheduler.runTask(ctx, task)
	}
	scheduler.version = sync.Version
	status.Accepted = uint32(len(scheduler.tasks))
	return status
}

func boundedTaskID(value string) string {
	if !protocol.ValidProbeID(value) {
		return ""
	}
	return value
}

func (scheduler *ProbeScheduler) runTask(ctx context.Context, task protocol.ProbeTask) {
	scheduler.runTaskEvery(ctx, task, time.Duration(task.IntervalSeconds)*time.Second)
}

func (scheduler *ProbeScheduler) runTaskEvery(ctx context.Context, task protocol.ProbeTask, interval time.Duration) {
	defer scheduler.wg.Done()
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// A saturated global limiter skips this occurrence. Tasks never queue a
		// catch-up run and the next delay begins from this decision.
		if !scheduler.tick(ctx, task, scheduler.config.Now().UTC()) {
			if scheduler.ctx.Err() != nil {
				return
			}
		}
		timer.Reset(interval)
	}
}

func (scheduler *ProbeScheduler) tick(ctx context.Context, task protocol.ProbeTask, scheduledAt time.Time) bool {
	select {
	case scheduler.semaphore <- struct{}{}:
		defer func() { <-scheduler.semaphore }()
	default:
		return false
	}
	result, ok := scheduler.check(ctx, task, scheduledAt)
	// Cancellation can race the final latency calculation after check has
	// closed a successful connection. Re-check immediately before publishing
	// so a removed task cannot emit that completed-but-stale result.
	if !ok || ctx.Err() != nil {
		return false
	}
	if scheduler.report != nil {
		if err := scheduler.report(result); err != nil {
			scheduler.cancel()
			return false
		}
	}
	return true
}

func (scheduler *ProbeScheduler) check(parent context.Context, task protocol.ProbeTask, scheduledAt time.Time) (protocol.ProbeResult, bool) {
	ctx, cancel := context.WithTimeout(parent, scheduler.config.TotalTimeout)
	defer cancel()
	started := scheduler.config.Now()
	addresses, failure := scheduler.resolve(ctx, task.Host)
	if failure != "" {
		if parent.Err() != nil {
			return protocol.ProbeResult{}, false
		}
		return failedProbe(task.ID, scheduledAt, failure), true
	}
	lastFailure := protocol.ProbeErrorConnect
	for _, address := range addresses {
		if ctx.Err() != nil {
			if parent.Err() != nil {
				return protocol.ProbeResult{}, false
			}
			lastFailure = protocol.ProbeErrorTimeout
			break
		}
		attemptCtx, attemptCancel := context.WithTimeout(ctx, scheduler.config.AddressTimeout)
		connection, err := scheduler.config.Dialer.DialContext(attemptCtx, "tcp", net.JoinHostPort(address.String(), strconv.Itoa(int(task.Port))))
		attemptCancel()
		if err == nil {
			// The TCP probe contract is intentionally only connect then close. No
			// Read or Write method is reachable on the success path.
			_ = connection.Close()
			if parent.Err() != nil {
				return protocol.ProbeResult{}, false
			}
			latency := scheduler.config.Now().Sub(started).Seconds() * 1000
			if latency < 0 {
				latency = 0
			}
			if latency > protocol.MaxProbeLatencyMillis {
				return failedProbe(task.ID, scheduledAt, protocol.ProbeErrorTimeout), true
			}
			return protocol.ProbeResult{TaskID: task.ID, ScheduledAtUnixMilli: scheduledAt.UnixMilli(), Success: true, LatencyMillis: latency}, true
		}
		if parent.Err() != nil {
			return protocol.ProbeResult{}, false
		}
		lastFailure = classifyProbeError(err)
	}
	return failedProbe(task.ID, scheduledAt, lastFailure), true
}

func (scheduler *ProbeScheduler) resolve(ctx context.Context, host string) ([]netip.Addr, protocol.ProbeErrorCode) {
	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if !protocol.ValidProbeAddress(literal) {
			return nil, protocol.ProbeErrorAddressInvalid
		}
		return []netip.Addr{literal}, ""
	}
	addresses, err := scheduler.config.Resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, protocol.ProbeErrorTimeout
		}
		return nil, protocol.ProbeErrorDNS
	}
	if len(addresses) > 3 {
		addresses = addresses[:3]
	}
	valid := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if protocol.ValidProbeAddress(address) {
			valid = append(valid, address)
		}
	}
	if len(valid) == 0 {
		return nil, protocol.ProbeErrorAddressInvalid
	}
	return valid, ""
}

func failedProbe(taskID string, scheduledAt time.Time, code protocol.ProbeErrorCode) protocol.ProbeResult {
	return protocol.ProbeResult{TaskID: taskID, ScheduledAtUnixMilli: scheduledAt.UnixMilli(), Error: code}
}

func classifyProbeError(err error) protocol.ProbeErrorCode {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return protocol.ProbeErrorTimeout
	case errors.Is(err, syscall.ECONNREFUSED):
		return protocol.ProbeErrorConnectionRefused
	case errors.Is(err, syscall.ENETUNREACH):
		return protocol.ProbeErrorNetworkUnreachable
	case errors.Is(err, syscall.EHOSTUNREACH):
		return protocol.ProbeErrorHostUnreachable
	default:
		return protocol.ProbeErrorConnect
	}
}

// Close cancels all task/DNS/connect work and waits for its bounded goroutines.
func (scheduler *ProbeScheduler) Close() {
	if scheduler == nil {
		return
	}
	scheduler.cancel()
	scheduler.mu.Lock()
	for id, running := range scheduler.tasks {
		running.cancel()
		delete(scheduler.tasks, id)
	}
	scheduler.mu.Unlock()
	scheduler.wg.Wait()
}
