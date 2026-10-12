package control

import (
	"errors"
	"sync"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

const (
	maxLiveSubscribers  = 32
	liveSubscriberQueue = 16
)

var errLiveSubscriberLimit = errors.New("live subscriber limit reached")

// LiveEventKind is the bounded browser event taxonomy for current state.
type LiveEventKind string

const (
	LiveConnected    LiveEventKind = "connected"
	LiveReport       LiveEventKind = "report"
	LiveDisconnected LiveEventKind = "disconnected"
	LiveStale        LiveEventKind = "stale"
	LiveDeleted      LiveEventKind = "deleted"
)

// LiveEvent is an in-memory event. It is projected to an explicit DTO before
// crossing the administrator HTTP boundary.
type LiveEvent struct {
	Kind       LiveEventKind
	NodeID     string
	Connected  bool
	Online     bool
	ReceivedAt time.Time
	Report     *protocol.Report
}

type liveNode struct {
	sessionID  string
	connected  bool
	stale      bool
	receivedAt time.Time
	report     *protocol.Report
}

// LiveSnapshot is a safe copy of one node's bounded memory-only state.
type LiveSnapshot struct {
	Connected  bool
	Online     bool
	Stale      bool
	ReceivedAt time.Time
	Report     *protocol.Report
}

// LiveStore owns current Agent reports and non-blocking SSE fanout. It has no
// database dependency by design.
type LiveStore struct {
	mu          sync.RWMutex
	nodes       map[string]liveNode
	subscribers map[uint64]chan LiveEvent
	nextID      uint64
}

// NewLiveStore creates an empty memory-only live state store.
func NewLiveStore() *LiveStore {
	return &LiveStore{nodes: make(map[string]liveNode), subscribers: make(map[uint64]chan LiveEvent)}
}

func (store *LiveStore) setConnected(nodeID, sessionID string, now time.Time) {
	store.mu.Lock()
	state := store.nodes[nodeID]
	state.sessionID, state.connected, state.stale = sessionID, true, true
	state.report, state.receivedAt = nil, time.Time{}
	store.nodes[nodeID] = state
	store.publishLocked(LiveEvent{Kind: LiveConnected, NodeID: nodeID, Connected: true, Online: false, ReceivedAt: now})
	store.mu.Unlock()
}

func (store *LiveStore) setDisconnected(nodeID, sessionID string, now time.Time) {
	store.mu.Lock()
	state, exists := store.nodes[nodeID]
	if !exists || state.sessionID != sessionID {
		store.mu.Unlock()
		return
	}
	state.connected, state.stale = false, true
	store.nodes[nodeID] = state
	store.publishLocked(LiveEvent{Kind: LiveDisconnected, NodeID: nodeID, ReceivedAt: now})
	store.mu.Unlock()
}

func (store *LiveStore) markStale(nodeID, sessionID string, now time.Time) {
	store.mu.Lock()
	state, exists := store.nodes[nodeID]
	if !exists || state.sessionID != sessionID || state.stale {
		store.mu.Unlock()
		return
	}
	state.stale = true
	store.nodes[nodeID] = state
	store.publishLocked(LiveEvent{Kind: LiveStale, NodeID: nodeID, Connected: state.connected, ReceivedAt: now})
	store.mu.Unlock()
}

func (store *LiveStore) acceptReport(nodeID, sessionID string, report protocol.Report, now time.Time) bool {
	store.mu.Lock()
	state, exists := store.nodes[nodeID]
	if !exists || state.sessionID != sessionID || !state.connected {
		store.mu.Unlock()
		return false
	}
	copy := cloneReport(report)
	state.report, state.receivedAt, state.stale = &copy, now, false
	store.nodes[nodeID] = state
	eventReport := cloneReport(copy)
	store.publishLocked(LiveEvent{Kind: LiveReport, NodeID: nodeID, Connected: true, Online: true, ReceivedAt: now, Report: &eventReport})
	store.mu.Unlock()
	return true
}

func (store *LiveStore) deleteNode(nodeID string, now time.Time) {
	store.mu.Lock()
	delete(store.nodes, nodeID)
	store.publishLocked(LiveEvent{Kind: LiveDeleted, NodeID: nodeID, ReceivedAt: now})
	store.mu.Unlock()
}

// Snapshot returns a copy of one node's current state.
func (store *LiveStore) Snapshot(nodeID string) LiveSnapshot {
	store.mu.RLock()
	state, exists := store.nodes[nodeID]
	if !exists {
		store.mu.RUnlock()
		return LiveSnapshot{}
	}
	snapshot := LiveSnapshot{Connected: state.connected, Online: state.connected && !state.stale && state.report != nil, Stale: state.stale, ReceivedAt: state.receivedAt}
	if state.report != nil {
		copy := cloneReport(*state.report)
		snapshot.Report = &copy
	}
	store.mu.RUnlock()
	return snapshot
}

// Subscribe registers a bounded live consumer. A slow consumer is removed
// rather than blocking Agent report processing.
func (store *LiveStore) Subscribe() (uint64, <-chan LiveEvent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.subscribers) >= maxLiveSubscribers {
		return 0, nil, errLiveSubscriberLimit
	}
	store.nextID++
	channel := make(chan LiveEvent, liveSubscriberQueue)
	store.subscribers[store.nextID] = channel
	return store.nextID, channel, nil
}

// Unsubscribe releases one browser live stream.
func (store *LiveStore) Unsubscribe(id uint64) {
	store.mu.Lock()
	if channel, exists := store.subscribers[id]; exists {
		delete(store.subscribers, id)
		close(channel)
	}
	store.mu.Unlock()
}

func (store *LiveStore) publishLocked(event LiveEvent) {
	for id, subscriber := range store.subscribers {
		copy := cloneLiveEvent(event)
		select {
		case subscriber <- copy:
		default:
			delete(store.subscribers, id)
			close(subscriber)
		}
	}
}

func cloneLiveEvent(event LiveEvent) LiveEvent {
	if event.Report != nil {
		copy := cloneReport(*event.Report)
		event.Report = &copy
	}
	return event
}

func cloneReport(report protocol.Report) protocol.Report {
	report.Unavailable = append([]protocol.MetricArea(nil), report.Unavailable...)
	return report
}
