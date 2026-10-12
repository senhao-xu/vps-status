package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// QueryHost reads at most 10,000 points from one allowlisted tier.
func (s *Service) QueryHost(ctx context.Context, nodeID string, resolution Resolution, from, to time.Time) ([]HostPoint, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	if s.store == nil {
		return nil, ErrUnavailable
	}
	if nodeID == "" || len(nodeID) > 128 || from.IsZero() || to.IsZero() || !from.Before(to) {
		return nil, errors.New("invalid host history query")
	}
	table := ""
	switch resolution {
	case Resolution1m:
		table = "host_1m"
	case Resolution10m:
		table = "host_10m"
	case Resolution1h:
		table = "host_1h"
	default:
		return nil, ErrInvalidResolution
	}
	rows, err := s.store.reader.QueryContext(ctx, fmt.Sprintf(`SELECT node_id,bucket_start,sample_count,
cpu_count,load_count,memory_count,disk_count,network_count,uptime_count,process_count,connection_count,
cpu_sum,cpu_min,cpu_max,
load1_sum,load5_sum,load15_sum,memory_used_sum,memory_total_last,swap_used_sum,swap_total_last,
disk_used_sum,disk_total_last,rx_rate_sum,rx_rate_max,tx_rate_sum,tx_rate_max,uptime_last,process_sum,tcp_sum,udp_sum
FROM %s WHERE node_id=? AND bucket_start>=? AND bucket_start<? ORDER BY bucket_start LIMIT 10000`, table), nodeID, from.UTC().Unix(), to.UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]HostPoint, 0)
	for rows.Next() {
		var p HostPoint
		var timestamp int64
		var cpuSum, cpuMin, cpuMax, load1, load5, load15, memory, swap, disk, rx, tx, process, tcp, udp sql.NullFloat64
		var memoryTotal, swapTotal, diskTotal, rxMax, txMax, uptime sql.NullInt64
		if err := rows.Scan(&p.NodeID, &timestamp, &p.SampleCount,
			&p.CPUCount, &p.LoadCount, &p.MemoryCount, &p.DiskCount, &p.NetworkCount, &p.UptimeCount, &p.ProcessCount, &p.ConnectionCount,
			&cpuSum, &cpuMin, &cpuMax, &load1, &load5, &load15, &memory, &memoryTotal, &swap, &swapTotal,
			&disk, &diskTotal, &rx, &rxMax, &tx, &txMax, &uptime, &process, &tcp, &udp); err != nil {
			return nil, err
		}
		p.BucketStart = time.Unix(timestamp, 0).UTC()
		p.Resolution = resolution
		p.CPUAverage = nullableAverage(cpuSum, p.CPUCount)
		p.CPUMin, p.CPUMax = nullableFloat(cpuMin), nullableFloat(cpuMax)
		p.Load1Average, p.Load5Average, p.Load15Average = nullableAverage(load1, p.LoadCount), nullableAverage(load5, p.LoadCount), nullableAverage(load15, p.LoadCount)
		p.MemoryUsedAverage, p.MemoryTotalLast = nullableAverage(memory, p.MemoryCount), nullableUint(memoryTotal)
		p.SwapUsedAverage, p.SwapTotalLast = nullableAverage(swap, p.MemoryCount), nullableUint(swapTotal)
		p.DiskUsedAverage, p.DiskTotalLast = nullableAverage(disk, p.DiskCount), nullableUint(diskTotal)
		p.ReceiveRateAverage, p.ReceiveRateMax = nullableAverage(rx, p.NetworkCount), nullableUint(rxMax)
		p.TransmitRateAverage, p.TransmitRateMax = nullableAverage(tx, p.NetworkCount), nullableUint(txMax)
		p.UptimeLast = nullableUint(uptime)
		p.ProcessAverage = nullableAverage(process, p.ProcessCount)
		p.TCPAverage, p.UDPAverage = nullableAverage(tcp, p.ConnectionCount), nullableAverage(udp, p.ConnectionCount)
		points = append(points, p)
	}
	return points, rows.Err()
}

func nullableAverage(value sql.NullFloat64, count uint64) *float64 {
	if !value.Valid || count == 0 {
		return nil
	}
	average := value.Float64 / float64(count)
	return &average
}

func nullableFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func nullableUint(value sql.NullInt64) *uint64 {
	if !value.Valid || value.Int64 < 0 {
		return nil
	}
	result := uint64(value.Int64)
	return &result
}

// QueryProbe reads at most 10,000 points from one node/task history series.
func (s *Service) QueryProbe(ctx context.Context, nodeID, taskID string, resolution Resolution, from, to time.Time) ([]ProbePoint, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	if s.store == nil {
		return nil, ErrUnavailable
	}
	if nodeID == "" || len(nodeID) > 128 || taskID == "" || len(taskID) > 128 || from.IsZero() || to.IsZero() || !from.Before(to) {
		return nil, errors.New("invalid probe history query")
	}
	table := ""
	switch resolution {
	case Resolution1m:
		table = "probe_1m"
	case Resolution10m:
		table = "probe_10m"
	case Resolution1h:
		table = "probe_1h"
	default:
		return nil, ErrInvalidResolution
	}
	rows, err := s.store.reader.QueryContext(ctx, fmt.Sprintf(`SELECT node_id,task_id,bucket_start,attempts,successes,failures,latency_count,latency_sum,latency_min,latency_max
FROM %s WHERE node_id=? AND task_id=? AND bucket_start>=? AND bucket_start<? ORDER BY bucket_start LIMIT 10000`, table),
		nodeID, taskID, from.UTC().Unix(), to.UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ProbePoint, 0)
	for rows.Next() {
		var point ProbePoint
		var timestamp int64
		var sum, minimum, maximum sql.NullFloat64
		if err := rows.Scan(&point.NodeID, &point.TaskID, &timestamp, &point.Attempts, &point.Successes, &point.Failures,
			&point.LatencyCount, &sum, &minimum, &maximum); err != nil {
			return nil, err
		}
		point.BucketStart = time.Unix(timestamp, 0).UTC()
		point.Resolution = resolution
		if point.Attempts > 0 {
			point.SuccessRate = float64(point.Successes) / float64(point.Attempts)
		}
		point.LatencyAverage = nullableAverage(sum, point.LatencyCount)
		point.LatencyMin, point.LatencyMax = nullableFloat(minimum), nullableFloat(maximum)
		result = append(result, point)
	}
	return result, rows.Err()
}

// Traffic returns lifetime and month accounting for one node.
func (s *Service) Traffic(ctx context.Context, nodeID string) ([]TrafficTotal, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	if s.store == nil {
		return nil, ErrUnavailable
	}
	if nodeID == "" || len(nodeID) > 128 {
		return nil, errors.New("invalid traffic node")
	}
	rows, err := s.store.reader.QueryContext(ctx, "SELECT node_id,period,receive_bytes,transmit_bytes,updated_at FROM traffic_ledger WHERE node_id=? ORDER BY period", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]TrafficTotal, 0)
	for rows.Next() {
		var item TrafficTotal
		var updated int64
		if err := rows.Scan(&item.NodeID, &item.Period, &item.ReceiveBytes, &item.TransmitBytes, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt = time.Unix(updated, 0).UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

// Stats inspects DB/WAL/SHM bytes, free pages, free disk, and oldest history.
func (s *Service) Stats(ctx context.Context) (StorageStats, error) {
	state := s.State()
	stats := StorageStats{State: state}
	if s == nil {
		return stats, ErrUnavailable
	}
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	if s.store == nil {
		return stats, ErrUnavailable
	}
	stats.MainBytes = fileSize(s.store.path)
	stats.WALBytes = fileSize(s.store.path + "-wal")
	stats.SHMBytes = fileSize(s.store.path + "-shm")
	stats.TotalBytes = stats.MainBytes + stats.WALBytes + stats.SHMBytes
	free, err := s.config.DiskFree(filepath.Dir(s.store.path))
	if err != nil {
		return stats, err
	}
	stats.FreeDiskBytes = free
	if err = s.store.reader.QueryRowContext(ctx, "PRAGMA page_count").Scan(&stats.PageCount); err != nil {
		return stats, err
	}
	if err = s.store.reader.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&stats.FreelistPages); err != nil {
		return stats, err
	}
	var oldest sql.NullInt64
	err = s.store.reader.QueryRowContext(ctx, `SELECT MIN(bucket_start) FROM (
		SELECT bucket_start FROM host_1m UNION ALL SELECT bucket_start FROM host_10m UNION ALL SELECT bucket_start FROM host_1h
		UNION ALL SELECT bucket_start FROM probe_1m UNION ALL SELECT bucket_start FROM probe_10m UNION ALL SELECT bucket_start FROM probe_1h
	)`).Scan(&oldest)
	if err != nil {
		return stats, err
	}
	if oldest.Valid {
		value := time.Unix(oldest.Int64, 0).UTC()
		stats.OldestBucket = &value
	}
	return stats, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
