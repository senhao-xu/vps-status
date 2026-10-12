package control

import (
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

func TestProbeHistoryKeepsResultsMemoryOnlyUntilMinuteCompletes(t *testing.T) {
	_, service := testHistoryService(t)
	history := NewProbeHistory(service)
	start := time.Unix(1_700_000_000, 0).UTC().Truncate(time.Minute)
	history.Observe("node", protocol.ProbeResult{TaskID: "task", ScheduledAtUnixMilli: start.UnixMilli(), Success: true, LatencyMillis: 10}, start.Add(time.Second))
	history.Observe("node", protocol.ProbeResult{TaskID: "task", ScheduledAtUnixMilli: start.Add(5 * time.Second).UnixMilli(), Error: protocol.ProbeErrorTimeout}, start.Add(6*time.Second))
	points, err := service.QueryProbe(t.Context(), "node", "task", telemetry.Resolution1m, start, start.Add(time.Minute))
	if err != nil || len(points) != 0 {
		t.Fatalf("open minute persisted: %#v %v", points, err)
	}
	latest := history.Latest("node", "task")
	if len(latest) != 1 || latest[0].Result.Error != protocol.ProbeErrorTimeout {
		t.Fatalf("latest=%#v", latest)
	}
	history.Seal(start.Add(time.Minute))
	syncWritesForControl(t, service)
	points, err = service.QueryProbe(t.Context(), "node", "task", telemetry.Resolution1m, start, start.Add(time.Minute))
	if err != nil || len(points) != 1 || points[0].Attempts != 2 || points[0].Successes != 1 || points[0].Failures != 1 || points[0].LatencyAverage == nil || *points[0].LatencyAverage != 10 {
		t.Fatalf("sealed points=%#v err=%v", points, err)
	}
}

func syncWritesForControl(t *testing.T, service *telemetry.Service) {
	t.Helper()
	if err := service.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestProbeResultAdmissionRejectsUnknownFloodAndReplayWithoutGrowth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	session := newAgentSession(t.Context(), nil, nil, nil, "node", "credential", [32]byte{}, "session", "nonce", AgentSessionConfig{}, func() time.Time { return now })
	if session.acceptProbeResult(protocol.ProbeResult{TaskID: "unknown", ScheduledAtUnixMilli: now.UnixMilli(), Error: protocol.ProbeErrorTimeout}, now) {
		t.Fatal("unknown task accepted")
	}
	if len(session.probeLastScheduled) != 0 || len(session.probeLastReceived) != 0 {
		t.Fatal("unknown-task flood grew admission maps")
	}
	session.probeTasks["task"] = protocol.ProbeTask{ID: "task", Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}
	first := protocol.ProbeResult{TaskID: "task", ScheduledAtUnixMilli: now.UnixMilli(), Error: protocol.ProbeErrorTimeout}
	if !session.acceptProbeResult(first, now) {
		t.Fatal("first assigned result rejected")
	}
	if session.acceptProbeResult(first, now.Add(time.Second)) {
		t.Fatal("replayed result accepted")
	}
	flood := first
	flood.ScheduledAtUnixMilli = now.Add(2 * time.Second).UnixMilli()
	if session.acceptProbeResult(flood, now.Add(2*time.Second)) {
		t.Fatal("result faster than task interval guard accepted")
	}
}
