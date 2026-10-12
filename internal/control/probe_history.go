package control

import (
	"context"
	"sync"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

type probeSeriesKey struct{ nodeID, taskID string }

// ProbeLatest is one bounded current result kept only in Control memory.
type ProbeLatest struct {
	NodeID     string               `json:"node_id"`
	TaskID     string               `json:"task_id"`
	ReceivedAt time.Time            `json:"received_at"`
	Result     protocol.ProbeResult `json:"result"`
}

// ProbeHistory owns current TCP results and open minute aggregates. Individual
// results are never submitted to either database.
type ProbeHistory struct {
	mu               sync.RWMutex
	sink             *telemetry.Service
	buckets          map[probeSeriesKey]telemetry.ProbeBucket
	latest           map[probeSeriesKey]ProbeLatest
	syncStatus       map[string]protocol.ProbeSyncStatus
	skipUntil        time.Time
	needsFreshMinute bool
}

// NewProbeHistory creates the bounded in-memory probe history boundary.
func NewProbeHistory(sink *telemetry.Service) *ProbeHistory {
	return &ProbeHistory{sink: sink, buckets: make(map[probeSeriesKey]telemetry.ProbeBucket),
		latest: make(map[probeSeriesKey]ProbeLatest), syncStatus: make(map[string]protocol.ProbeSyncStatus)}
}

// Observe consumes one result only after WSS session, assignment, timestamp,
// taxonomy, replay and rate validation have all succeeded.
func (history *ProbeHistory) Observe(nodeID string, result protocol.ProbeResult, receivedAt time.Time) {
	if history == nil {
		return
	}
	key := probeSeriesKey{nodeID: nodeID, taskID: result.TaskID}
	minute := receivedAt.UTC().Truncate(time.Minute)
	writable := history.sink != nil && history.sink.State().Writable
	history.mu.Lock()
	history.latest[key] = ProbeLatest{NodeID: nodeID, TaskID: result.TaskID, ReceivedAt: receivedAt.UTC(), Result: result}
	if !writable {
		history.markGapLocked(minute)
		history.mu.Unlock()
		return
	}
	if history.needsFreshMinute {
		clear(history.buckets)
		history.skipUntil = minute.Add(time.Minute)
		history.needsFreshMinute = false
	}
	skip := minute.Before(history.skipUntil)
	if !skip && !history.skipUntil.IsZero() {
		history.skipUntil = time.Time{}
	}
	for existingKey, bucket := range history.buckets {
		if bucket.BucketStart.Before(minute) {
			if err := history.sink.SubmitProbe(bucket); err != nil {
				history.markGapLocked(minute)
				skip = true
				break
			}
			delete(history.buckets, existingKey)
		}
	}
	if !skip {
		bucket, exists := history.buckets[key]
		if !exists || bucket.BucketStart.Before(minute) {
			bucket = telemetry.ProbeBucket{NodeID: nodeID, TaskID: result.TaskID, BucketStart: minute}
		}
		if !bucket.BucketStart.After(minute) {
			addProbeResult(&bucket, result)
			history.buckets[key] = bucket
		}
	}
	history.mu.Unlock()
}

func addProbeResult(bucket *telemetry.ProbeBucket, result protocol.ProbeResult) {
	bucket.Attempts++
	if !result.Success {
		bucket.Failures++
		return
	}
	bucket.Successes++
	bucket.LatencyCount++
	bucket.LatencySum += result.LatencyMillis
	if bucket.LatencyCount == 1 || result.LatencyMillis < bucket.LatencyMin {
		bucket.LatencyMin = result.LatencyMillis
	}
	if bucket.LatencyCount == 1 || result.LatencyMillis > bucket.LatencyMax {
		bucket.LatencyMax = result.LatencyMillis
	}
}

// RecordSyncStatus stores one bounded acknowledgement per connected node.
func (history *ProbeHistory) RecordSyncStatus(nodeID string, status protocol.ProbeSyncStatus) {
	if history == nil {
		return
	}
	status.Rejected = append([]protocol.ProbeRejection(nil), status.Rejected...)
	history.mu.Lock()
	history.syncStatus[nodeID] = status
	history.mu.Unlock()
}

// Latest returns copied current results, optionally filtered by task/node.
func (history *ProbeHistory) Latest(nodeID, taskID string) []ProbeLatest {
	if history == nil {
		return []ProbeLatest{}
	}
	history.mu.RLock()
	result := make([]ProbeLatest, 0)
	for key, latest := range history.latest {
		if (nodeID == "" || key.nodeID == nodeID) && (taskID == "" || key.taskID == taskID) {
			result = append(result, latest)
		}
	}
	history.mu.RUnlock()
	return result
}

func (history *ProbeHistory) SyncStatus(nodeID string) (protocol.ProbeSyncStatus, bool) {
	if history == nil {
		return protocol.ProbeSyncStatus{}, false
	}
	history.mu.RLock()
	status, ok := history.syncStatus[nodeID]
	status.Rejected = append([]protocol.ProbeRejection(nil), status.Rejected...)
	history.mu.RUnlock()
	return status, ok
}

// Seal submits completed probe minutes. Degraded writes never affect WSS.
func (history *ProbeHistory) Seal(now time.Time) {
	if history == nil || history.sink == nil {
		return
	}
	cutoff := now.UTC().Truncate(time.Minute)
	history.mu.Lock()
	defer history.mu.Unlock()
	if !history.sink.State().Writable {
		history.markGapLocked(cutoff)
		return
	}
	if history.needsFreshMinute {
		clear(history.buckets)
		return
	}
	for key, bucket := range history.buckets {
		if bucket.BucketStart.Before(cutoff) {
			if err := history.sink.SubmitProbe(bucket); err != nil {
				history.markGapLocked(cutoff)
				return
			}
			delete(history.buckets, key)
		}
	}
}

func (history *ProbeHistory) markGapLocked(minute time.Time) {
	clear(history.buckets)
	history.needsFreshMinute = true
	if next := minute.Add(time.Minute); next.After(history.skipUntil) {
		history.skipUntil = next
	}
}

func (history *ProbeHistory) ClearBuckets() {
	if history == nil {
		return
	}
	history.mu.Lock()
	clear(history.buckets)
	history.skipUntil = time.Time{}
	history.needsFreshMinute = false
	history.mu.Unlock()
}

// ClearHistory serializes both in-memory aggregators with the durable clear.
func (history *ProbeHistory) ClearHistory(ctx context.Context, hosts *HostHistory) error {
	if history == nil || history.sink == nil {
		return telemetry.ErrUnavailable
	}
	if hosts != nil {
		hosts.mu.Lock()
		defer hosts.mu.Unlock()
	}
	history.mu.Lock()
	defer history.mu.Unlock()
	if hosts != nil {
		clear(hosts.buckets)
		hosts.skipUntil = time.Time{}
		hosts.needsFreshMinute = false
	}
	clear(history.buckets)
	history.skipUntil = time.Time{}
	history.needsFreshMinute = false
	return history.sink.ClearHistory(ctx)
}

// ResetAll serializes both open aggregators with physical telemetry reset.
func (history *ProbeHistory) ResetAll(ctx context.Context, hosts *HostHistory) error {
	if history == nil || history.sink == nil {
		return telemetry.ErrUnavailable
	}
	if hosts != nil {
		hosts.mu.Lock()
		defer hosts.mu.Unlock()
	}
	history.mu.Lock()
	defer history.mu.Unlock()
	if hosts != nil {
		clear(hosts.buckets)
		clear(hosts.baselines)
		hosts.skipUntil = time.Time{}
		hosts.needsFreshMinute = false
	}
	clear(history.buckets)
	clear(history.latest)
	clear(history.syncStatus)
	history.skipUntil = time.Time{}
	history.needsFreshMinute = false
	return history.sink.ResetAll(ctx)
}

func (history *ProbeHistory) Reset() {
	if history == nil {
		return
	}
	history.mu.Lock()
	clear(history.buckets)
	clear(history.latest)
	clear(history.syncStatus)
	history.skipUntil = time.Time{}
	history.needsFreshMinute = false
	history.mu.Unlock()
}

func (history *ProbeHistory) deleteNode(nodeID string) {
	if history == nil {
		return
	}
	history.mu.Lock()
	for key := range history.buckets {
		if key.nodeID == nodeID {
			delete(history.buckets, key)
		}
	}
	for key := range history.latest {
		if key.nodeID == nodeID {
			delete(history.latest, key)
		}
	}
	delete(history.syncStatus, nodeID)
	history.mu.Unlock()
}

func (history *ProbeHistory) deleteTask(taskID string) {
	if history == nil {
		return
	}
	history.mu.Lock()
	for key := range history.buckets {
		if key.taskID == taskID {
			delete(history.buckets, key)
		}
	}
	for key := range history.latest {
		if key.taskID == taskID {
			delete(history.latest, key)
		}
	}
	history.mu.Unlock()
}

func (history *ProbeHistory) deleteAssignment(nodeID, taskID string) {
	if history == nil {
		return
	}
	key := probeSeriesKey{nodeID: nodeID, taskID: taskID}
	history.mu.Lock()
	delete(history.buckets, key)
	delete(history.latest, key)
	history.mu.Unlock()
}
