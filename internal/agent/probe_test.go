package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (function resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return function(ctx, network, host)
}

type dialerFunc func(context.Context, string, string) (net.Conn, error)

func (function dialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return function(ctx, network, address)
}

type observeConn struct{ reads, writes, closes atomic.Int32 }

func (connection *observeConn) Read([]byte) (int, error) { connection.reads.Add(1); return 0, io.EOF }
func (connection *observeConn) Write(data []byte) (int, error) {
	connection.writes.Add(1)
	return len(data), nil
}
func (connection *observeConn) Close() error          { connection.closes.Add(1); return nil }
func (*observeConn) LocalAddr() net.Addr              { return testAddr("local") }
func (*observeConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (*observeConn) SetDeadline(time.Time) error      { return nil }
func (*observeConn) SetReadDeadline(time.Time) error  { return nil }
func (*observeConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr string

func (address testAddr) Network() string { return "tcp" }
func (address testAddr) String() string  { return string(address) }

func TestProbeSchedulerRejectsMaliciousSnapshotsWithoutClamping(t *testing.T) {
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{}, nil)
	defer scheduler.Close()
	tasks := make([]protocol.ProbeTask, protocol.MaxProbeTasks+1)
	for index := range tasks {
		tasks[index] = protocol.ProbeTask{ID: string(rune(0x100 + index)), Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}
	}
	status := scheduler.Apply(protocol.ProbeSync{Version: 1, Tasks: tasks})
	if status.Accepted != 0 || len(status.Rejected) != 1 || status.Rejected[0].Code != "task_limit" {
		t.Fatalf("65-task status=%#v", status)
	}
	status = scheduler.Apply(protocol.ProbeSync{Version: 2, Tasks: []protocol.ProbeTask{
		{ID: "fast", Host: "127.0.0.1", Port: 80, IntervalSeconds: 4},
		{ID: "port", Host: "127.0.0.1", Port: 0, IntervalSeconds: 5},
		{ID: "good", Host: "127.0.0.1", Port: 80, IntervalSeconds: 5},
	}})
	if status.Accepted != 1 || len(status.Rejected) != 2 {
		t.Fatalf("malicious snapshot status=%#v", status)
	}
	duplicate := scheduler.Apply(protocol.ProbeSync{Version: 2})
	if duplicate.Accepted != 1 || len(duplicate.Rejected) != 0 {
		t.Fatalf("duplicate version restarted/rejected: %#v", duplicate)
	}
	stale := scheduler.Apply(protocol.ProbeSync{Version: 1})
	if len(stale.Rejected) != 1 || stale.Rejected[0].Code != "stale_version" {
		t.Fatalf("stale status=%#v", stale)
	}
	unsafe := scheduler.Apply(protocol.ProbeSync{Version: 3, Tasks: []protocol.ProbeTask{{
		ID: "bad\nid", Host: "127.0.0.1", Port: 80, IntervalSeconds: 5,
	}}})
	if len(unsafe.Rejected) != 1 || unsafe.Rejected[0].TaskID != "" || protocol.ValidateProbeSyncStatus(unsafe) != nil {
		t.Fatalf("unsafe task ID was echoed or produced invalid status: %#v", unsafe)
	}
	if _, err := protocol.NewEnvelope(protocol.TypeProbeSync, "session", 1, unsafe); err != nil {
		t.Fatalf("bounded rejection status could not be encoded: %v", err)
	}
}

func TestTCPProbeCapsDNSAndNeverReadsOrWrites(t *testing.T) {
	addresses := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.0.2.4")}
	connection := &observeConn{}
	var attempts atomic.Int32
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil }),
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			if attempts.Add(1) < 3 {
				return nil, syscallErr{errors.New("connect failed")}
			}
			return connection, nil
		}),
	}, nil)
	defer scheduler.Close()
	result, ok := scheduler.check(t.Context(), protocol.ProbeTask{ID: "task", Host: "example.com", Port: 443, IntervalSeconds: 5}, time.Now().UTC())
	if !ok || !result.Success || attempts.Load() != 3 {
		t.Fatalf("result=%#v ok=%v attempts=%d", result, ok, attempts.Load())
	}
	if connection.reads.Load() != 0 || connection.writes.Load() != 0 || connection.closes.Load() != 1 {
		t.Fatalf("connection read/write/close=%d/%d/%d", connection.reads.Load(), connection.writes.Load(), connection.closes.Load())
	}
}

type syscallErr struct{ error }

func TestTCPProbeTotalDeadlineCoversDNSAndConnect(t *testing.T) {
	t.Run("slow DNS", func(t *testing.T) {
		scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{
			TotalTimeout: 20 * time.Millisecond,
			Resolver:     resolverFunc(func(ctx context.Context, _, _ string) ([]netip.Addr, error) { <-ctx.Done(); return nil, ctx.Err() }),
		}, nil)
		defer scheduler.Close()
		result, ok := scheduler.check(t.Context(), protocol.ProbeTask{ID: "task", Host: "example.com", Port: 80, IntervalSeconds: 5}, time.Now().UTC())
		if !ok || result.Error != protocol.ProbeErrorTimeout {
			t.Fatalf("slow DNS result=%#v ok=%v", result, ok)
		}
	})
	t.Run("slow TCP and cancellation", func(t *testing.T) {
		scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{
			TotalTimeout: 30 * time.Millisecond, AddressTimeout: 20 * time.Millisecond,
			Dialer: dialerFunc(func(ctx context.Context, _, _ string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }),
		}, nil)
		defer scheduler.Close()
		result, ok := scheduler.check(t.Context(), protocol.ProbeTask{ID: "task", Host: "127.0.0.1", Port: 80, IntervalSeconds: 5}, time.Now().UTC())
		if !ok || result.Error != protocol.ProbeErrorTimeout {
			t.Fatalf("slow TCP result=%#v ok=%v", result, ok)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, ok := scheduler.check(ctx, protocol.ProbeTask{ID: "task", Host: "127.0.0.1", Port: 80, IntervalSeconds: 5}, time.Now().UTC()); ok {
			t.Fatal("cancelled probe emitted a result")
		}
	})
}

func TestProbeGlobalConcurrencySkipsRatherThanQueues(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{Concurrency: 1,
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			entered <- struct{}{}
			<-release
			return nil, errors.New("failed")
		})}, nil)
	defer scheduler.Close()
	task := protocol.ProbeTask{ID: "one", Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}
	var wait sync.WaitGroup
	wait.Add(1)
	go func() { defer wait.Done(); scheduler.tick(t.Context(), task, time.Now().UTC()) }()
	<-entered
	if scheduler.tick(t.Context(), protocol.ProbeTask{ID: "two", Host: "127.0.0.1", Port: 2, IntervalSeconds: 5}, time.Now().UTC()) {
		t.Fatal("saturated semaphore queued or executed a missed tick")
	}
	close(release)
	wait.Wait()
}

func TestProbeConcurrencyCannotExceedEight(t *testing.T) {
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{Concurrency: protocol.MaxProbeTasks}, nil)
	defer scheduler.Close()
	if capacity := cap(scheduler.semaphore); capacity != defaultProbeConcurrency {
		t.Fatalf("probe semaphore capacity=%d; want %d", capacity, defaultProbeConcurrency)
	}
}

func TestCancelledSuccessfulProbeEmitsNoStaleResult(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	connection := &observeConn{}
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			cancel()
			return connection, nil
		}),
	}, nil)
	defer scheduler.Close()
	result, ok := scheduler.check(ctx, protocol.ProbeTask{ID: "task", Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}, time.Now().UTC())
	if ok || result != (protocol.ProbeResult{}) {
		t.Fatalf("cancelled successful probe result=%#v ok=%v", result, ok)
	}
	if connection.closes.Load() != 1 {
		t.Fatalf("cancelled successful probe closes=%d; want 1", connection.closes.Load())
	}
}

func TestProbeCancellationAfterCheckSuppressesResult(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	connection := &observeConn{}
	var clockCalls atomic.Int32
	var reports atomic.Int32
	now := time.Now().UTC()
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			return connection, nil
		}),
		Now: func() time.Time {
			if clockCalls.Add(1) == 2 {
				cancel()
			}
			return now
		},
	}, func(protocol.ProbeResult) error {
		reports.Add(1)
		return nil
	})
	defer scheduler.Close()
	task := protocol.ProbeTask{ID: "task", Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}
	if scheduler.tick(ctx, task, now) {
		t.Fatal("probe cancelled after its check was reported as complete")
	}
	if reports.Load() != 0 {
		t.Fatalf("cancelled probe reports=%d; want 0", reports.Load())
	}
	if connection.closes.Load() != 1 {
		t.Fatalf("cancelled probe closes=%d; want 1", connection.closes.Load())
	}
}

func TestProbeScheduleDelaysAfterCompletionInsteadOfCatchingUp(t *testing.T) {
	starts := make(chan time.Time, 3)
	var attempts atomic.Int32
	scheduler := NewProbeScheduler(t.Context(), ProbeSchedulerConfig{
		Dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			starts <- time.Now()
			if attempts.Add(1) == 1 {
				time.Sleep(50 * time.Millisecond)
			}
			return nil, errors.New("failed")
		}),
	}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	scheduler.wg.Add(1)
	go scheduler.runTaskEvery(ctx, protocol.ProbeTask{ID: "task", Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}, 20*time.Millisecond)
	first := <-starts
	second := <-starts
	cancel()
	scheduler.Close()
	if gap := second.Sub(first); gap < 65*time.Millisecond {
		t.Fatalf("next probe started after %v; missed tick was caught up", gap)
	}
}
