package telemetry

import (
	"context"
	"fmt"
	"time"
)

const retentionDeleteChunk = 500

func (s *Service) maintain(ctx context.Context, now time.Time) (MaintenanceResult, error) {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result MaintenanceResult
	count, err := s.rollup(ctx, "host_1m", "host_10m", 10*time.Minute, now.Truncate(10*time.Minute))
	if err != nil {
		return result, err
	}
	result.Promoted1m = count
	count, err = s.rollup(ctx, "host_10m", "host_1h", time.Hour, now.Truncate(time.Hour))
	if err != nil {
		return result, err
	}
	result.Promoted10m = count
	count, err = s.rollupProbe(ctx, "probe_1m", "probe_10m", 10*time.Minute, now.Truncate(10*time.Minute))
	if err != nil {
		return result, err
	}
	result.ProbePromoted1m = count
	count, err = s.rollupProbe(ctx, "probe_10m", "probe_1h", time.Hour, now.Truncate(time.Hour))
	if err != nil {
		return result, err
	}
	result.ProbePromoted10m = count

	// Align tier transitions to the next coarser bucket boundary. Maintenance
	// normally runs with non-zero seconds; raw cutoffs would otherwise retain a
	// partial fine window alongside the coarse bucket that already contains it.
	oneMinuteCutoff := ceilBucket(now.Add(-s.config.Retention.OneMinute), 10*time.Minute).Unix()
	tenMinuteCutoff := ceilBucket(now.Add(-s.config.Retention.TenMinute), time.Hour).Unix()
	oneHourCutoff := now.Add(-s.config.Retention.OneHour).Unix()
	if result.Deleted1m, err = s.deleteChunks(ctx, "host_1m", "bucket_start < ?", oneMinuteCutoff); err != nil {
		return result, err
	}
	// Tiers intentionally do not overlap: recent charts use the finest tier.
	if result.Deleted10m, err = s.deleteChunks(ctx, "host_10m", "bucket_start < ? OR bucket_start >= ?", tenMinuteCutoff, oneMinuteCutoff); err != nil {
		return result, err
	}
	if result.Deleted1h, err = s.deleteChunks(ctx, "host_1h", "bucket_start < ? OR bucket_start >= ?", oneHourCutoff, tenMinuteCutoff); err != nil {
		return result, err
	}
	if result.ProbeDeleted1m, err = s.deleteProbeChunks(ctx, "probe_1m", "bucket_start < ?", oneMinuteCutoff); err != nil {
		return result, err
	}
	if result.ProbeDeleted10m, err = s.deleteProbeChunks(ctx, "probe_10m", "bucket_start < ? OR bucket_start >= ?", tenMinuteCutoff, oneMinuteCutoff); err != nil {
		return result, err
	}
	if result.ProbeDeleted1h, err = s.deleteProbeChunks(ctx, "probe_1h", "bucket_start < ? OR bucket_start >= ?", oneHourCutoff, tenMinuteCutoff); err != nil {
		return result, err
	}
	if err := checkpointDB(ctx, s.store.writer, "PASSIVE"); err != nil {
		return result, err
	}
	_, err = s.store.writer.ExecContext(ctx, "PRAGMA incremental_vacuum(256)")
	return result, err
}

func ceilBucket(value time.Time, width time.Duration) time.Time {
	floor := value.Truncate(width)
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(width)
}

func (s *Service) rollup(ctx context.Context, source, target string, width time.Duration, before time.Time) (int64, error) {
	if !hostTable(source) || !hostTable(target) {
		return 0, fmt.Errorf("invalid host rollup table")
	}
	seconds := int64(width / time.Second)
	tx, err := s.store.writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	query := fmt.Sprintf(`WITH base AS (
	SELECT *,bucket_start-(bucket_start %% ?) AS rolled_start FROM %s WHERE bucket_start < ?
), ranked AS (
	SELECT *,
	ROW_NUMBER() OVER (PARTITION BY node_id,rolled_start ORDER BY (cpu_count > 0) DESC,bucket_start DESC) AS cpu_newest,
	ROW_NUMBER() OVER (PARTITION BY node_id,rolled_start ORDER BY (memory_count > 0) DESC,bucket_start DESC) AS memory_newest,
	ROW_NUMBER() OVER (PARTITION BY node_id,rolled_start ORDER BY (disk_count > 0) DESC,bucket_start DESC) AS disk_newest,
	ROW_NUMBER() OVER (PARTITION BY node_id,rolled_start ORDER BY (network_count > 0) DESC,bucket_start DESC) AS network_newest,
	ROW_NUMBER() OVER (PARTITION BY node_id,rolled_start ORDER BY (uptime_count > 0) DESC,bucket_start DESC) AS uptime_newest
	FROM base
)
INSERT INTO %s(
node_id,bucket_start,sample_count,cpu_count,load_count,memory_count,disk_count,network_count,uptime_count,process_count,connection_count,
cpu_sum,cpu_min,cpu_max,load1_sum,load5_sum,load15_sum,
memory_used_sum,memory_total_last,swap_used_sum,swap_total_last,disk_used_sum,disk_total_last,
rx_rate_sum,rx_rate_max,tx_rate_sum,tx_rate_max,uptime_last,process_sum,tcp_sum,udp_sum,rx_total_last,tx_total_last)
SELECT node_id,rolled_start,SUM(sample_count),SUM(cpu_count),SUM(load_count),SUM(memory_count),SUM(disk_count),
SUM(network_count),SUM(uptime_count),SUM(process_count),SUM(connection_count),
SUM(CASE WHEN cpu_count>0 THEN cpu_sum END),MIN(CASE WHEN cpu_count>0 THEN cpu_min END),MAX(CASE WHEN cpu_count>0 THEN cpu_max END),
SUM(CASE WHEN load_count>0 THEN load1_sum END),SUM(CASE WHEN load_count>0 THEN load5_sum END),SUM(CASE WHEN load_count>0 THEN load15_sum END),
SUM(CASE WHEN memory_count>0 THEN memory_used_sum END),MAX(CASE WHEN memory_newest=1 AND memory_count>0 THEN memory_total_last END),
SUM(CASE WHEN memory_count>0 THEN swap_used_sum END),MAX(CASE WHEN memory_newest=1 AND memory_count>0 THEN swap_total_last END),
SUM(CASE WHEN disk_count>0 THEN disk_used_sum END),MAX(CASE WHEN disk_newest=1 AND disk_count>0 THEN disk_total_last END),
SUM(CASE WHEN network_count>0 THEN rx_rate_sum END),MAX(CASE WHEN network_count>0 THEN rx_rate_max END),
SUM(CASE WHEN network_count>0 THEN tx_rate_sum END),MAX(CASE WHEN network_count>0 THEN tx_rate_max END),
MAX(CASE WHEN uptime_newest=1 AND uptime_count>0 THEN uptime_last END),SUM(CASE WHEN process_count>0 THEN process_sum END),
SUM(CASE WHEN connection_count>0 THEN tcp_sum END),SUM(CASE WHEN connection_count>0 THEN udp_sum END),
MAX(CASE WHEN network_newest=1 AND network_count>0 THEN rx_total_last END),MAX(CASE WHEN network_newest=1 AND network_count>0 THEN tx_total_last END)
FROM ranked GROUP BY node_id,rolled_start HAVING COUNT(*) > 0
ON CONFLICT(node_id,bucket_start) DO UPDATE SET
sample_count=excluded.sample_count,cpu_count=excluded.cpu_count,load_count=excluded.load_count,memory_count=excluded.memory_count,
disk_count=excluded.disk_count,network_count=excluded.network_count,uptime_count=excluded.uptime_count,
process_count=excluded.process_count,connection_count=excluded.connection_count,
cpu_sum=excluded.cpu_sum,cpu_min=excluded.cpu_min,cpu_max=excluded.cpu_max,
load1_sum=excluded.load1_sum,load5_sum=excluded.load5_sum,load15_sum=excluded.load15_sum,
memory_used_sum=excluded.memory_used_sum,memory_total_last=excluded.memory_total_last,
swap_used_sum=excluded.swap_used_sum,swap_total_last=excluded.swap_total_last,disk_used_sum=excluded.disk_used_sum,disk_total_last=excluded.disk_total_last,
rx_rate_sum=excluded.rx_rate_sum,rx_rate_max=excluded.rx_rate_max,tx_rate_sum=excluded.tx_rate_sum,tx_rate_max=excluded.tx_rate_max,
uptime_last=excluded.uptime_last,process_sum=excluded.process_sum,tcp_sum=excluded.tcp_sum,udp_sum=excluded.udp_sum,
rx_total_last=excluded.rx_total_last,tx_total_last=excluded.tx_total_last`, source, target)
	result, err := tx.ExecContext(ctx, query, seconds, before.Unix())
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return rows, nil
}

func hostTable(table string) bool {
	return table == "host_1m" || table == "host_10m" || table == "host_1h"
}

func probeTable(table string) bool {
	return table == "probe_1m" || table == "probe_10m" || table == "probe_1h"
}

func (s *Service) rollupProbe(ctx context.Context, source, target string, width time.Duration, before time.Time) (int64, error) {
	if !probeTable(source) || !probeTable(target) {
		return 0, fmt.Errorf("invalid probe rollup table")
	}
	seconds := int64(width / time.Second)
	query := fmt.Sprintf(`INSERT INTO %s(node_id,task_id,bucket_start,attempts,successes,failures,latency_count,latency_sum,latency_min,latency_max)
SELECT node_id,task_id,bucket_start-(bucket_start %% ?),SUM(attempts),SUM(successes),SUM(failures),SUM(latency_count),
SUM(CASE WHEN latency_count>0 THEN latency_sum END),MIN(CASE WHEN latency_count>0 THEN latency_min END),MAX(CASE WHEN latency_count>0 THEN latency_max END)
FROM %s WHERE bucket_start<? GROUP BY node_id,task_id,bucket_start-(bucket_start %% ?)
ON CONFLICT(node_id,task_id,bucket_start) DO UPDATE SET attempts=excluded.attempts,successes=excluded.successes,
failures=excluded.failures,latency_count=excluded.latency_count,latency_sum=excluded.latency_sum,
latency_min=excluded.latency_min,latency_max=excluded.latency_max`, target, source)
	result, err := s.store.writer.ExecContext(ctx, query, seconds, before.Unix(), seconds)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Service) deleteChunks(ctx context.Context, table, predicate string, args ...any) (int64, error) {
	if !hostTable(table) {
		return 0, fmt.Errorf("invalid retention table")
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		query := fmt.Sprintf(`DELETE FROM %s WHERE (node_id,bucket_start) IN (SELECT node_id,bucket_start FROM %s WHERE %s ORDER BY bucket_start LIMIT %d)`, table, table, predicate, retentionDeleteChunk)
		result, err := s.store.writer.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += rows
		if rows < retentionDeleteChunk {
			return total, nil
		}
	}
}

func (s *Service) deleteProbeChunks(ctx context.Context, table, predicate string, args ...any) (int64, error) {
	if !probeTable(table) {
		return 0, fmt.Errorf("invalid probe retention table")
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		query := fmt.Sprintf(`DELETE FROM %s WHERE (node_id,task_id,bucket_start) IN (SELECT node_id,task_id,bucket_start FROM %s WHERE %s ORDER BY bucket_start LIMIT %d)`, table, table, predicate, retentionDeleteChunk)
		result, err := s.store.writer.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += rows
		if rows < retentionDeleteChunk {
			return total, nil
		}
	}
}
