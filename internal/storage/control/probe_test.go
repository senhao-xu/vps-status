package control

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func seedProbeNode(t *testing.T, store *Store, id string, now time.Time) {
	t.Helper()
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO nodes(id,name,created_at,updated_at) VALUES(?,?,?,?)`, id, id, unix(now), unix(now)); err != nil {
		t.Fatal(err)
	}
}

func TestProbeTaskTransactionalLimitsAndSnapshot(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedProbeNode(t, store, "node", now)
	for index := 0; index < 64; index++ {
		task := ProbeTask{ID: fmt.Sprintf("task-%02d", index), Name: "TCP", Host: "127.0.0.1", Port: uint16(index + 1), IntervalSeconds: 5}
		if _, err := store.CreateProbeTask(t.Context(), task, []string{"node"}, now); err != nil {
			t.Fatalf("create task %d: %v", index, err)
		}
	}
	overflow := ProbeTask{ID: "task-65", Name: "overflow", Host: "localhost", Port: 443, IntervalSeconds: 5}
	if _, err := store.CreateProbeTask(t.Context(), overflow, []string{"node"}, now); !errors.Is(err, ErrProbeTaskLimit) {
		t.Fatalf("65th task error=%v", err)
	}
	for _, interval := range []uint32{4, 3601} {
		invalid := ProbeTask{ID: fmt.Sprintf("interval-%d", interval), Name: "bad", Host: "127.0.0.1", Port: 1, IntervalSeconds: interval}
		if _, err := store.CreateProbeTask(t.Context(), invalid, nil, now); err == nil {
			t.Fatalf("interval %d accepted", interval)
		}
	}
	snapshot, err := store.ProbeSnapshot(t.Context(), "node")
	if err != nil || snapshot.Version <= 1 || len(snapshot.Tasks) != 64 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}

func TestProbeTaskDeleteCreatesIdempotentCleanupAndControlHasNoResults(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedProbeNode(t, store, "node", now)
	task := ProbeTask{ID: "task", Name: "TCP", Host: "example.com", Port: 443, IntervalSeconds: 3600}
	if _, err := store.CreateProbeTask(t.Context(), task, []string{"node"}, now); err != nil {
		t.Fatal(err)
	}
	if _, nodes, err := store.DeleteProbeTask(t.Context(), task.ID, now.Add(time.Minute)); err != nil || len(nodes) != 1 || nodes[0] != "node" {
		t.Fatalf("delete nodes=%v err=%v", nodes, err)
	}
	jobs, err := store.ListProbeCleanupJobs(t.Context(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].TaskID != task.ID {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
	if err := store.MarkProbeCleanupAttempt(t.Context(), task.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteProbeCleanup(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := store.db.QueryContext(t.Context(), `SELECT name,sql FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(name + " " + definition)
		for _, forbidden := range []string{"probe_result", "latency_millis", "latency_sum", "successes", "failures"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("control.db schema contains probe telemetry %q in %s", forbidden, name)
			}
		}
	}
}

func TestProbeTaskLimitRollsBackDefinitionAssignmentsAndVersion(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedProbeNode(t, store, "node-a", now)
	seedProbeNode(t, store, "node-b", now)
	target := ProbeTask{ID: "target", Name: "target", Host: "localhost", Port: 443, IntervalSeconds: 5}
	if _, err := store.CreateProbeTask(t.Context(), target, []string{"node-a"}, now); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 64; index++ {
		filler := ProbeTask{ID: fmt.Sprintf("filler-%02d", index), Name: "filler", Host: "127.0.0.1", Port: uint16(index + 1), IntervalSeconds: 5}
		if _, err := store.CreateProbeTask(t.Context(), filler, []string{"node-b"}, now); err != nil {
			t.Fatalf("create filler %d: %v", index, err)
		}
	}
	before, err := store.ProbeSnapshot(t.Context(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	target.Name = "must-roll-back"
	if _, err := store.UpdateProbeTask(t.Context(), target, []string{"node-b"}, now.Add(time.Minute)); !errors.Is(err, ErrProbeTaskLimit) {
		t.Fatalf("limit update error=%v", err)
	}
	after, err := store.ProbeSnapshot(t.Context(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.ProbeTaskByID(t.Context(), target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version != after.Version || len(after.Tasks) != 1 || stored.Name != "target" || len(stored.NodeIDs) != 1 || stored.NodeIDs[0] != "node-a" {
		t.Fatalf("failed update was not rolled back: before=%#v after=%#v stored=%#v", before, after, stored)
	}
}
