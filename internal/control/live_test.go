package control

import (
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

func TestLiveStoreSessionIsolationCopiesAndStateTransitions(t *testing.T) {
	store := NewLiveStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	store.setConnected("node", "session-1", now)
	report := protocol.Report{CollectedAtUnixMilli: now.UnixMilli(), Unavailable: []protocol.MetricArea{protocol.AreaDisk}}
	if !store.acceptReport("node", "session-1", report, now.Add(time.Second)) {
		t.Fatal("current session report rejected")
	}
	report.Unavailable[0] = protocol.AreaCPU
	snapshot := store.Snapshot("node")
	if !snapshot.Connected || !snapshot.Online || snapshot.Report == nil || snapshot.Report.Unavailable[0] != protocol.AreaDisk {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	snapshot.Report.Unavailable[0] = protocol.AreaMemory
	if got := store.Snapshot("node").Report.Unavailable[0]; got != protocol.AreaDisk {
		t.Fatalf("Snapshot returned aliased report: %q", got)
	}

	store.setConnected("node", "session-2", now.Add(2*time.Second))
	snapshot = store.Snapshot("node")
	if !snapshot.Connected || snapshot.Online || snapshot.Report != nil || !snapshot.ReceivedAt.IsZero() {
		t.Fatalf("replacement session reused old report: %#v", snapshot)
	}
	if store.acceptReport("node", "session-1", report, now.Add(3*time.Second)) {
		t.Fatal("replaced session report accepted")
	}
	if !store.acceptReport("node", "session-2", report, now.Add(3*time.Second)) {
		t.Fatal("replacement session report rejected")
	}
	store.markStale("node", "session-2", now.Add(4*time.Second))
	if snapshot = store.Snapshot("node"); !snapshot.Connected || snapshot.Online {
		t.Fatalf("stale snapshot=%#v", snapshot)
	}
	store.setDisconnected("node", "session-2", now.Add(5*time.Second))
	if snapshot = store.Snapshot("node"); snapshot.Connected || snapshot.Online {
		t.Fatalf("disconnected snapshot=%#v", snapshot)
	}
	store.deleteNode("node", now.Add(6*time.Second))
	if snapshot = store.Snapshot("node"); snapshot != (LiveSnapshot{}) {
		t.Fatalf("deleted snapshot=%#v", snapshot)
	}
}

func TestLiveStoreBoundsSubscribersAndEvictsSlowConsumer(t *testing.T) {
	store := NewLiveStore()
	ids := make([]uint64, 0, maxLiveSubscribers)
	for index := 0; index < maxLiveSubscribers; index++ {
		id, _, err := store.Subscribe()
		if err != nil {
			t.Fatalf("Subscribe(%d): %v", index, err)
		}
		ids = append(ids, id)
	}
	if _, _, err := store.Subscribe(); err != errLiveSubscriberLimit {
		t.Fatalf("subscriber limit error=%v", err)
	}
	for index := 0; index <= liveSubscriberQueue; index++ {
		store.setConnected("node", "session", time.Unix(int64(index+1), 0))
	}
	store.mu.RLock()
	remaining := len(store.subscribers)
	store.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("slow subscribers remaining=%d", remaining)
	}
	if _, _, err := store.Subscribe(); err != nil {
		t.Fatalf("capacity not reclaimed after slow-consumer eviction: %v", err)
	}
	for _, id := range ids {
		store.Unsubscribe(id)
	}
}

func TestLiveStoreFanoutCopiesReportsForEverySubscriber(t *testing.T) {
	store := NewLiveStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	store.setConnected("node", "session", now)
	_, first, err := store.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := store.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	report := protocol.Report{
		CollectedAtUnixMilli: now.UnixMilli(),
		Unavailable:          []protocol.MetricArea{protocol.AreaDisk},
	}
	if !store.acceptReport("node", "session", report, now) {
		t.Fatal("live report rejected")
	}
	firstEvent, secondEvent := <-first, <-second
	firstEvent.Report.Unavailable[0] = protocol.AreaCPU
	if got := secondEvent.Report.Unavailable[0]; got != protocol.AreaDisk {
		t.Fatalf("second subscriber received aliased report: %q", got)
	}
	if got := store.Snapshot("node").Report.Unavailable[0]; got != protocol.AreaDisk {
		t.Fatalf("subscriber mutation changed snapshot: %q", got)
	}
}

func TestRegistryDisconnectAllClosesEverySession(t *testing.T) {
	registry := NewRegistry()
	closed := make(chan string, 2)
	registry.register("node-a", "session-a", func(reason string) { closed <- "a:" + reason })
	registry.register("node-b", "session-b", func(reason string) { closed <- "b:" + reason })
	if count := registry.DisconnectAll("shutdown"); count != 2 {
		t.Fatalf("DisconnectAll()=%d; want 2", count)
	}
	if registry.Connected("node-a") || registry.Connected("node-b") {
		t.Fatal("registry retained a disconnected session")
	}
	first, second := <-closed, <-closed
	if first != "a:shutdown" && first != "b:shutdown" || second != "a:shutdown" && second != "b:shutdown" || first == second {
		t.Fatalf("close events = %q, %q", first, second)
	}
}
