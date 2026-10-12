package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

var ErrAlertEvaluationQueueFull = errors.New("alert evaluation queue is full")

// AlertManagerConfig bounds live evaluation and offline scheduling.
type AlertManagerConfig struct {
	QueueSize    int
	TickInterval time.Duration
	StartupGrace time.Duration
	Now          func() time.Time
}

func (config AlertManagerConfig) withDefaults() AlertManagerConfig {
	if config.QueueSize <= 0 {
		config.QueueSize = 256
	} else if config.QueueSize > controlstore.MaxAlertStates {
		config.QueueSize = controlstore.MaxAlertStates
	}
	if config.TickInterval <= 0 {
		config.TickInterval = 5 * time.Second
	}
	if config.StartupGrace <= 0 {
		config.StartupGrace = protocol.OfflineAfter(protocol.DefaultReportInterval)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config
}

type alertEvaluation struct {
	nodeID   string
	snapshot LiveSnapshot
	at       time.Time
}

// AlertEvent is the stable JSON contract delivered by all notification channels.
type AlertEvent struct {
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	RuleID    string    `json:"rule_id"`
	RuleName  string    `json:"rule_name"`
	NodeID    string    `json:"node_id"`
	NodeName  string    `json:"node_name"`
	Metric    string    `json:"metric"`
	Value     *float64  `json:"value,omitempty"`
	Threshold float64   `json:"threshold"`
}

// AlertManager serializes one current-state cursor per node without blocking
// report ingestion. It has no telemetry.db dependency.
type AlertManager struct {
	ctx        context.Context
	store      *controlstore.Store
	live       *LiveStore
	dispatcher *NotificationDispatcher
	config     AlertManagerConfig
	startedAt  time.Time
	queue      chan string
	mu         sync.Mutex
	pending    map[string]alertEvaluation
	wg         sync.WaitGroup
}

// NewAlertManager starts one bounded evaluator and its offline/reminder tick.
func NewAlertManager(ctx context.Context, store *controlstore.Store, live *LiveStore, dispatcher *NotificationDispatcher, config AlertManagerConfig) (*AlertManager, error) {
	if ctx == nil || store == nil || live == nil {
		return nil, errors.New("alert manager dependencies are required")
	}
	config = config.withDefaults()
	manager := &AlertManager{ctx: ctx, store: store, live: live, dispatcher: dispatcher, config: config,
		startedAt: config.Now().UTC(), queue: make(chan string, config.QueueSize), pending: make(map[string]alertEvaluation)}
	manager.wg.Add(2)
	go manager.worker()
	go manager.tick()
	return manager, nil
}

// Observe non-blockingly coalesces the newest snapshot for a node.
func (manager *AlertManager) Observe(nodeID string, snapshot LiveSnapshot, at time.Time) error {
	if manager == nil || nodeID == "" {
		return nil
	}
	evaluation := alertEvaluation{nodeID: nodeID, snapshot: snapshot, at: at.UTC()}
	manager.mu.Lock()
	if _, exists := manager.pending[nodeID]; exists {
		manager.pending[nodeID] = evaluation
		manager.mu.Unlock()
		return nil
	}
	if len(manager.pending) >= cap(manager.queue) {
		manager.mu.Unlock()
		return ErrAlertEvaluationQueueFull
	}
	manager.pending[nodeID] = evaluation
	manager.mu.Unlock()
	select {
	case <-manager.ctx.Done():
		manager.mu.Lock()
		delete(manager.pending, nodeID)
		manager.mu.Unlock()
		return manager.ctx.Err()
	case manager.queue <- nodeID:
		return nil
	default:
		manager.mu.Lock()
		delete(manager.pending, nodeID)
		manager.mu.Unlock()
		return ErrAlertEvaluationQueueFull
	}
}

// Refresh schedules every covered node after a rule/channel or node-group edit.
func (manager *AlertManager) Refresh() {
	if manager == nil {
		return
	}
	nodes, err := manager.store.ActiveAlertNodes(manager.ctx)
	if err != nil {
		return
	}
	now := manager.config.Now().UTC()
	for _, node := range nodes {
		_ = manager.Observe(node.ID, manager.live.Snapshot(node.ID), now)
	}
}

// Wait waits for evaluator shutdown after the owner cancels its context.
func (manager *AlertManager) Wait() { manager.wg.Wait() }

func (manager *AlertManager) worker() {
	defer manager.wg.Done()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case nodeID := <-manager.queue:
			manager.mu.Lock()
			evaluation, exists := manager.pending[nodeID]
			delete(manager.pending, nodeID)
			manager.mu.Unlock()
			if exists {
				_ = manager.evaluate(evaluation)
			}
		}
	}
}

func (manager *AlertManager) tick() {
	defer manager.wg.Done()
	ticker := time.NewTicker(manager.config.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case now := <-ticker.C:
			if now.Sub(manager.startedAt) < manager.config.StartupGrace {
				continue
			}
			manager.Refresh()
		}
	}
}

func (manager *AlertManager) evaluate(evaluation alertEvaluation) error {
	node, err := manager.store.NodeByID(manager.ctx, evaluation.nodeID)
	if errors.Is(err, controlstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	rules, err := manager.store.AlertRulesForNode(manager.ctx, node.ID, node.Group)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		known, value, breach, recovered := evaluateAlertMetric(rule, evaluation.snapshot)
		state, err := manager.store.AlertState(manager.ctx, rule.ID, node.ID)
		if err != nil {
			return err
		}
		if !known {
			// A duration means continuously observed breach. Missing live metric
			// data cancels pending evidence, but cannot manufacture recovery for
			// an already firing alert.
			if state.Status == "pending" {
				state.Status, state.PendingSince, state.UpdatedAt = "normal", nil, evaluation.at
				if err := manager.store.ApplyAlertState(manager.ctx, state, nil); err != nil && !errors.Is(err, controlstore.ErrNotFound) {
					return err
				}
			}
			continue
		}
		next, eventKind, changed := advanceAlertState(rule, state, breach, recovered, evaluation.at)
		if !changed {
			continue
		}
		deliveries, err := buildAlertDeliveries(rule, node, next, eventKind, value, evaluation.at)
		if err != nil {
			return err
		}
		if err := manager.store.ApplyAlertState(manager.ctx, next, deliveries); err != nil {
			if errors.Is(err, controlstore.ErrNotFound) {
				continue
			}
			return err
		}
		if manager.dispatcher != nil {
			for _, delivery := range deliveries {
				_ = manager.dispatcher.Submit(delivery.ID)
			}
		}
	}
	return nil
}

func evaluateAlertMetric(rule controlstore.AlertRule, snapshot LiveSnapshot) (known bool, value float64, breach bool, recovered bool) {
	if rule.Metric == "offline" {
		// Online is a derived convenience flag, but recovery requires the full
		// LiveStore invariant: this connection has supplied a current validated
		// report. Do not let an inconsistent or synthetic snapshot manufacture a
		// recovery without report evidence.
		if snapshot.Online && snapshot.Connected && !snapshot.Stale && snapshot.Report != nil {
			return true, 0, false, true
		}
		if !snapshot.Connected || snapshot.Stale {
			return true, 1, true, false
		}
		return false, 0, false, false
	}
	if !snapshot.Online || snapshot.Report == nil {
		return false, 0, false, false
	}
	report := *snapshot.Report
	switch rule.Metric {
	case "cpu":
		if reportAreaUnavailable(report, protocol.AreaCPU) {
			return false, 0, false, false
		}
		value = report.CPU.UsagePercent
	case "memory":
		if reportAreaUnavailable(report, protocol.AreaMemory) || report.Memory.TotalBytes == 0 {
			return false, 0, false, false
		}
		value = float64(report.Memory.UsedBytes) * 100 / float64(report.Memory.TotalBytes)
	case "disk":
		if reportAreaUnavailable(report, protocol.AreaDisk) || report.Disk.TotalBytes == 0 {
			return false, 0, false, false
		}
		value = float64(report.Disk.UsedBytes) * 100 / float64(report.Disk.TotalBytes)
	case "traffic_rate":
		if reportAreaUnavailable(report, protocol.AreaNetwork) {
			return false, 0, false, false
		}
		value = float64(report.Network.ReceiveBytesPerSecond) + float64(report.Network.TransmitBytesPerSecond)
	default:
		return false, 0, false, false
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false, 0, false, false
	}
	return true, value, value > rule.Threshold, value <= rule.Threshold-rule.RecoveryHysteresis
}

func advanceAlertState(rule controlstore.AlertRule, state controlstore.AlertState, breach, recovered bool, now time.Time) (controlstore.AlertState, string, bool) {
	now = now.UTC()
	state.RuleID, state.UpdatedAt = rule.ID, now
	if state.Status == "" {
		state.Status = "normal"
	}
	switch state.Status {
	case "normal":
		if !breach {
			return state, "", false
		}
		state.PendingSince = timePointer(now)
		state.ResolvedAt, state.LastReminderAt = nil, nil
		state.ReminderCount = 0
		if rule.DurationSeconds == 0 {
			state.Status = "firing"
			state.Generation++
			state.FiringSince = timePointer(now)
			return state, "firing", true
		}
		state.Status = "pending"
		return state, "", true
	case "pending":
		if !breach {
			state.Status, state.PendingSince = "normal", nil
			return state, "", true
		}
		if state.PendingSince == nil {
			state.PendingSince = timePointer(now)
			return state, "", true
		}
		if now.Sub(*state.PendingSince) < time.Duration(rule.DurationSeconds)*time.Second {
			return state, "", false
		}
		state.Status = "firing"
		state.Generation++
		state.FiringSince = timePointer(now)
		state.LastReminderAt = nil
		state.ReminderCount = 0
		return state, "firing", true
	case "firing":
		if recovered {
			state.Status = "resolved"
			state.ResolvedAt = timePointer(now)
			return state, "resolved", true
		}
		if rule.ReminderIntervalSeconds > 0 && state.ReminderCount < rule.ReminderMaxCount {
			base := state.FiringSince
			if state.LastReminderAt != nil {
				base = state.LastReminderAt
			}
			if base != nil && now.Sub(*base) >= time.Duration(rule.ReminderIntervalSeconds)*time.Second {
				state.ReminderCount++
				state.LastReminderAt = timePointer(now)
				return state, "reminder", true
			}
		}
		return state, "", false
	case "resolved":
		if breach {
			state.PendingSince = timePointer(now)
			state.ResolvedAt, state.LastReminderAt = nil, nil
			state.ReminderCount = 0
			if rule.DurationSeconds == 0 {
				state.Status = "firing"
				state.Generation++
				state.FiringSince = timePointer(now)
				return state, "firing", true
			}
			state.Status = "pending"
			state.FiringSince = nil
			return state, "", true
		}
		state.Status = "normal"
		state.PendingSince, state.FiringSince, state.ResolvedAt, state.LastReminderAt = nil, nil, nil, nil
		state.ReminderCount = 0
		return state, "", true
	default:
		state.Status = "normal"
		return state, "", true
	}
}

func buildAlertDeliveries(rule controlstore.AlertRule, node controlstore.Node, state controlstore.AlertState, kind string, value float64, now time.Time) ([]controlstore.NotificationDelivery, error) {
	if kind == "" || len(rule.ChannelIDs) == 0 {
		return nil, nil
	}
	eventID := alertEventID(rule.ID, node.ID, rule.UpdatedAt, state.Generation, kind, state.ReminderCount)
	eventValue := value
	if rule.Metric == "offline" {
		eventValue = value
	}
	event := AlertEvent{EventID: eventID, Timestamp: now.UTC(), Kind: kind, State: state.Status, RuleID: rule.ID,
		RuleName: rule.Name, NodeID: node.ID, NodeName: node.Name, Metric: rule.Metric, Value: &eventValue, Threshold: rule.Threshold}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal alert event: %w", err)
	}
	result := make([]controlstore.NotificationDelivery, 0, len(rule.ChannelIDs))
	for _, channelID := range rule.ChannelIDs {
		result = append(result, controlstore.NotificationDelivery{ID: deliveryID(eventID, channelID), EventID: eventID,
			ChannelID: channelID, RuleID: rule.ID, NodeID: node.ID, EventKind: kind, EventPayload: payload,
			Status: "pending", NextAttemptAt: now.UTC(), CreatedAt: now.UTC()})
	}
	return result, nil
}

func alertEventID(ruleID, nodeID string, ruleRevision time.Time, generation uint64, kind string, reminder uint8) string {
	// Rule edits reset state cursors. The durable, monotonically increasing
	// updated_at revision keeps the next transition distinct from completed
	// deliveries retained for the prior configuration while remaining stable
	// across process restarts and retries.
	sum := sha256.Sum256([]byte(fmt.Sprintf("alert/v1\x00%s\x00%s\x00%d\x00%d\x00%s\x00%d",
		ruleID, nodeID, ruleRevision.UTC().Unix(), generation, kind, reminder)))
	return hex.EncodeToString(sum[:])
}

func deliveryID(eventID, channelID string) string {
	sum := sha256.Sum256([]byte("delivery/v1\x00" + eventID + "\x00" + channelID))
	return hex.EncodeToString(sum[:])
}

func timePointer(value time.Time) *time.Time {
	result := value.UTC()
	return &result
}
