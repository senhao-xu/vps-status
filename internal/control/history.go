package control

import (
	"context"
	"sync"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

type trafficBaseline struct {
	receive     uint64
	transmit    uint64
	uptime      uint64
	collectedAt time.Time
	bootID      string
}

// HostHistory converts validated reports into completed minute buckets and a
// reset-aware compact traffic ledger. It never stores a raw report.
type HostHistory struct {
	mu               sync.Mutex
	sink             *telemetry.Service
	buckets          map[string]telemetry.HostBucket
	baselines        map[string]trafficBaseline
	skipUntil        time.Time
	needsFreshMinute bool
}

// NewHostHistory creates the in-memory aggregation boundary.
func NewHostHistory(sink *telemetry.Service) *HostHistory {
	return &HostHistory{sink: sink, buckets: make(map[string]telemetry.HostBucket), baselines: make(map[string]trafficBaseline)}
}

// Observe consumes only reports that passed protocol and session validation.
// Queue/degraded errors intentionally do not affect the live Agent session.
func (h *HostHistory) Observe(nodeID string, report protocol.Report) {
	h.ObserveAt(nodeID, report, time.UnixMilli(report.CollectedAtUnixMilli))
}

// ObserveAt uses Control receive time for minute boundaries so one Agent with
// an accepted clock skew cannot prematurely seal other nodes' current buckets.
func (h *HostHistory) ObserveAt(nodeID string, report protocol.Report, receivedAt time.Time) {
	h.ObserveSessionAt(nodeID, report, receivedAt, "")
}

// ObserveSessionAt includes the connection's validated Linux boot identity so
// traffic resets remain detectable across long disconnects.
func (h *HostHistory) ObserveSessionAt(nodeID string, report protocol.Report, receivedAt time.Time, bootID string) {
	if h == nil || h.sink == nil {
		return
	}
	collected := time.UnixMilli(report.CollectedAtUnixMilli).UTC()
	minute := receivedAt.UTC().Truncate(time.Minute)
	writable := h.sink.State().Writable
	h.mu.Lock()
	previous, seen := h.baselines[nodeID]
	delta := telemetry.TrafficDelta{NodeID: nodeID, Month: collected.Format("2006-01"), UpdatedAt: collected}
	trafficAccepted := !reportAreaUnavailable(report, protocol.AreaNetwork) && !reportAreaUnavailable(report, protocol.AreaUptime)
	if trafficAccepted && (!seen || collected.After(previous.collectedAt)) {
		continuousBoot := false
		if seen && previous.bootID != "" && bootID != "" {
			continuousBoot = previous.bootID == bootID
		} else if seen {
			continuousBoot = report.UptimeSeconds >= previous.uptime
		}
		if continuousBoot {
			if report.Network.ReceiveBytesTotal >= previous.receive {
				delta.ReceiveBytes = report.Network.ReceiveBytesTotal - previous.receive
			}
			if report.Network.TransmitBytesTotal >= previous.transmit {
				delta.TransmitBytes = report.Network.TransmitBytesTotal - previous.transmit
			}
		}
		h.baselines[nodeID] = trafficBaseline{receive: report.Network.ReceiveBytesTotal, transmit: report.Network.TransmitBytesTotal, uptime: report.UptimeSeconds, collectedAt: collected, bootID: bootID}
	} else {
		trafficAccepted = false
	}
	if !writable {
		h.markPersistenceGapLocked(minute)
		h.mu.Unlock()
		return
	}
	if h.needsFreshMinute {
		clear(h.buckets)
		h.skipUntil = minute.Add(time.Minute)
		h.needsFreshMinute = false
	}
	skipHost := minute.Before(h.skipUntil)
	if !skipHost && !h.skipUntil.IsZero() {
		h.skipUntil = time.Time{}
	}
	writeFailed := false
	for id, bucket := range h.buckets {
		if bucket.BucketStart.Before(minute) {
			if err := h.sink.SubmitHost(bucket); err != nil {
				writeFailed = true
			}
			delete(h.buckets, id)
		}
	}
	if writeFailed {
		h.markPersistenceGapLocked(minute)
		skipHost = true
	}
	if !skipHost {
		bucket, exists := h.buckets[nodeID]
		if !exists || bucket.BucketStart.Before(minute) {
			bucket = newHostBucket(nodeID, minute, report)
		} else if bucket.BucketStart.Equal(minute) {
			addHostSample(&bucket, report)
		}
		// A late report never rewinds a node's current bucket.
		if !exists || !bucket.BucketStart.After(minute) {
			h.buckets[nodeID] = bucket
		}
	}
	h.mu.Unlock()
	if trafficAccepted && (!seen || delta.ReceiveBytes != 0 || delta.TransmitBytes != 0) {
		if err := h.sink.SubmitTraffic(delta); err != nil {
			h.mu.Lock()
			h.markPersistenceGapLocked(minute)
			h.mu.Unlock()
		}
	}
}

func reportAreaUnavailable(report protocol.Report, want protocol.MetricArea) bool {
	for _, area := range report.Unavailable {
		if area == want {
			return true
		}
	}
	return false
}

// ClearHistory atomically discards open buckets and durable chart history
// with respect to concurrent report aggregation. Traffic baselines are kept.
func (h *HostHistory) ClearHistory(ctx context.Context) error {
	if h == nil || h.sink == nil {
		return telemetry.ErrUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.buckets)
	h.skipUntil = time.Time{}
	h.needsFreshMinute = false
	return h.sink.ClearHistory(ctx)
}

// ResetAll atomically resets open aggregation, counter baselines, and the
// physically recreated telemetry database with respect to ObserveAt.
func (h *HostHistory) ResetAll(ctx context.Context) error {
	if h == nil || h.sink == nil {
		return telemetry.ErrUnavailable
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.buckets)
	clear(h.baselines)
	h.skipUntil = time.Time{}
	h.needsFreshMinute = false
	return h.sink.ResetAll(ctx)
}

// Seal submits every bucket strictly before the supplied minute. It is useful
// to make an idle node's last completed minute durable from a supervisor tick.
func (h *HostHistory) Seal(now time.Time) {
	if h == nil || h.sink == nil {
		return
	}
	cutoff := now.UTC().Truncate(time.Minute)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.sink.State().Writable {
		h.markPersistenceGapLocked(cutoff)
		return
	}
	if h.needsFreshMinute {
		clear(h.buckets)
		return
	}
	for id, bucket := range h.buckets {
		if bucket.BucketStart.Before(cutoff) {
			if err := h.sink.SubmitHost(bucket); err != nil {
				h.markPersistenceGapLocked(cutoff)
				return
			}
			delete(h.buckets, id)
		}
	}
}

func (h *HostHistory) markPersistenceGapLocked(minute time.Time) {
	clear(h.buckets)
	h.needsFreshMinute = true
	if next := minute.Add(time.Minute); next.After(h.skipUntil) {
		h.skipUntil = next
	}
}

// ClearBuckets discards open history windows without changing traffic baselines.
func (h *HostHistory) ClearBuckets() {
	if h == nil {
		return
	}
	h.mu.Lock()
	clear(h.buckets)
	h.skipUntil = time.Time{}
	h.needsFreshMinute = false
	h.mu.Unlock()
}

// Reset makes the next report establish a fresh counter baseline and discards
// every open minute so reset-all cannot repopulate pre-reset telemetry.
func (h *HostHistory) Reset() {
	if h == nil {
		return
	}
	h.mu.Lock()
	clear(h.baselines)
	clear(h.buckets)
	h.skipUntil = time.Time{}
	h.needsFreshMinute = false
	h.mu.Unlock()
}

func (h *HostHistory) deleteNode(nodeID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	delete(h.baselines, nodeID)
	delete(h.buckets, nodeID)
	h.mu.Unlock()
}

func newHostBucket(nodeID string, start time.Time, r protocol.Report) telemetry.HostBucket {
	bucket := telemetry.HostBucket{NodeID: nodeID, BucketStart: start}
	addHostSample(&bucket, r)
	return bucket
}

func addHostSample(b *telemetry.HostBucket, r protocol.Report) {
	b.SampleCount++
	if !reportAreaUnavailable(r, protocol.AreaCPU) {
		b.CPUCount++
		b.CPUSum += r.CPU.UsagePercent
		if b.CPUCount == 1 || r.CPU.UsagePercent < b.CPUMin {
			b.CPUMin = r.CPU.UsagePercent
		}
		if b.CPUCount == 1 || r.CPU.UsagePercent > b.CPUMax {
			b.CPUMax = r.CPU.UsagePercent
		}
	}
	if !reportAreaUnavailable(r, protocol.AreaLoad) {
		b.LoadCount++
		b.Load1Sum += r.Load.One
		b.Load5Sum += r.Load.Five
		b.Load15Sum += r.Load.Fifteen
	}
	if !reportAreaUnavailable(r, protocol.AreaMemory) {
		b.MemoryCount++
		b.MemoryUsedSum += float64(r.Memory.UsedBytes)
		b.MemoryTotalLast = r.Memory.TotalBytes
		b.SwapUsedSum += float64(r.Memory.SwapUsedBytes)
		b.SwapTotalLast = r.Memory.SwapTotalBytes
	}
	if !reportAreaUnavailable(r, protocol.AreaDisk) {
		b.DiskCount++
		b.DiskUsedSum += float64(r.Disk.UsedBytes)
		b.DiskTotalLast = r.Disk.TotalBytes
	}
	if !reportAreaUnavailable(r, protocol.AreaNetwork) {
		b.NetworkCount++
		b.ReceiveRateSum += float64(r.Network.ReceiveBytesPerSecond)
		if b.NetworkCount == 1 || r.Network.ReceiveBytesPerSecond > b.ReceiveRateMax {
			b.ReceiveRateMax = r.Network.ReceiveBytesPerSecond
		}
		b.TransmitRateSum += float64(r.Network.TransmitBytesPerSecond)
		if b.NetworkCount == 1 || r.Network.TransmitBytesPerSecond > b.TransmitRateMax {
			b.TransmitRateMax = r.Network.TransmitBytesPerSecond
		}
		b.ReceiveTotalLast = r.Network.ReceiveBytesTotal
		b.TransmitTotalLast = r.Network.TransmitBytesTotal
	}
	if !reportAreaUnavailable(r, protocol.AreaUptime) {
		b.UptimeCount++
		b.UptimeLast = r.UptimeSeconds
	}
	if !reportAreaUnavailable(r, protocol.AreaProcesses) {
		b.ProcessCount++
		b.ProcessSum += float64(r.ProcessCount)
	}
	if !reportAreaUnavailable(r, protocol.AreaConnections) {
		b.ConnectionCount++
		b.TCPSum += float64(r.Connections.TCP)
		b.UDPSum += float64(r.Connections.UDP)
	}
}
