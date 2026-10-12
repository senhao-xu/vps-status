package protocol

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"
)

func TestProbeTaskBoundsAndAddressClasses(t *testing.T) {
	validHosts := []string{"example.com", "localhost", "127.0.0.1", "10.0.0.1", "::1", "2001:db8::1"}
	for _, host := range validHosts {
		task := ProbeTask{ID: "task", Host: host, Port: 1, IntervalSeconds: MinProbeIntervalSeconds}
		if err := ValidateProbeTask(task); err != nil {
			t.Fatalf("ValidateProbeTask(%q)=%v", host, err)
		}
	}
	for _, task := range []ProbeTask{
		{ID: "task", Host: "0.0.0.0", Port: 80, IntervalSeconds: 5},
		{ID: "task", Host: "::", Port: 80, IntervalSeconds: 5},
		{ID: "task", Host: "224.0.0.1", Port: 80, IntervalSeconds: 5},
		{ID: "task", Host: "ff02::1", Port: 80, IntervalSeconds: 5},
		{ID: "task", Host: "example.com", Port: 0, IntervalSeconds: 5},
		{ID: "task", Host: "example.com", Port: 80, IntervalSeconds: 4},
		{ID: "task", Host: "example.com", Port: 80, IntervalSeconds: 3601},
		{ID: "task", Host: "bad host", Port: 80, IntervalSeconds: 5},
	} {
		if err := ValidateProbeTask(task); err == nil {
			t.Fatalf("invalid task accepted: %#v", task)
		}
	}
	if ValidProbeAddress(netip.MustParseAddr("0.0.0.0")) || ValidProbeAddress(netip.MustParseAddr("ff02::1")) {
		t.Fatal("invalid resolved address class accepted")
	}
}

func TestProbeSnapshotAndResultStrictContracts(t *testing.T) {
	tasks := make([]ProbeTask, MaxProbeTasks+1)
	for index := range tasks {
		tasks[index] = ProbeTask{ID: string(rune('a' + index)), Host: "127.0.0.1", Port: 1, IntervalSeconds: 5}
	}
	if err := ValidateProbeSync(ProbeSync{Version: 1, Tasks: tasks}); err == nil {
		t.Fatal("65-task snapshot accepted")
	}
	if _, err := DecodeProbeSync(json.RawMessage(`{"version":1,"tasks":[],"extra":true}`)); err == nil {
		t.Fatal("probe snapshot with unknown field accepted")
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	valid := ProbeResult{TaskID: "task", ScheduledAtUnixMilli: now.UnixMilli(), Success: true, LatencyMillis: 1.25}
	if err := ValidateProbeResultAt(valid, now); err != nil {
		t.Fatal(err)
	}
	for _, result := range []ProbeResult{
		{TaskID: "task", ScheduledAtUnixMilli: now.Add(-MaxProbeClockSkew - time.Millisecond).UnixMilli(), Success: true},
		{TaskID: "task", ScheduledAtUnixMilli: now.UnixMilli(), Success: true, Error: ProbeErrorTimeout},
		{TaskID: "task", ScheduledAtUnixMilli: now.UnixMilli(), Error: "raw socket error"},
		{TaskID: "task", ScheduledAtUnixMilli: now.UnixMilli(), Error: ProbeErrorTimeout, LatencyMillis: 1},
	} {
		if err := ValidateProbeResultAt(result, now); err == nil {
			t.Fatalf("invalid result accepted: %#v", result)
		}
	}
}

func TestProbeIDAndSyncStatusNeverEchoUnsafeTaskID(t *testing.T) {
	for _, value := range []string{"", "line\nbreak", string([]byte{0xff}), string(make([]byte, MaxProbeIDBytes+1))} {
		if ValidProbeID(value) {
			t.Fatalf("ValidProbeID(%q)=true", value)
		}
	}
	if !ValidProbeID("task-safe_1") {
		t.Fatal("safe probe task ID rejected")
	}
	if err := ValidateProbeSyncStatus(ProbeSyncStatus{Version: 1, Rejected: []ProbeRejection{{TaskID: "line\nbreak", Code: "target"}}}); err == nil {
		t.Fatal("unsafe rejection task ID accepted")
	}
}
