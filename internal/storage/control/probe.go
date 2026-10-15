package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
)

// CreateProbeTask creates one definition and all assignments atomically.
func (s *Store) CreateProbeTask(ctx context.Context, task ProbeTask, nodeIDs []string, now time.Time) (uint64, error) {
	if err := validateStoredProbe(task, nodeIDs); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin probe task creation: %w", err)
	}
	defer tx.Rollback()
	if err := validateProbeAutoJoin(ctx, tx, "", task.AutoJoin); err != nil {
		return 0, err
	}
	if err := validateProbeAssignments(ctx, tx, "", nodeIDs); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO probe_tasks(id,name,host,port,interval_seconds,auto_join,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		task.ID, task.Name, task.Host, task.Port, task.IntervalSeconds, task.AutoJoin, unix(now), unix(now)); err != nil {
		return 0, fmt.Errorf("create probe task: %w", err)
	}
	for _, nodeID := range uniqueSorted(nodeIDs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO probe_assignments(task_id,node_id,created_at) VALUES(?,?,?)`, task.ID, nodeID, unix(now)); err != nil {
			return 0, fmt.Errorf("create probe assignment: %w", err)
		}
	}
	version, err := bumpProbeVersion(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit probe task creation: %w", err)
	}
	return version, nil
}

// UpdateProbeTask replaces a task and its complete assignment set atomically.
func (s *Store) UpdateProbeTask(ctx context.Context, task ProbeTask, nodeIDs []string, now time.Time) (uint64, error) {
	_, _, version, err := s.mutateProbeTask(ctx, task.ID, now, func(current *ProbeTask) error { *current = task; current.NodeIDs = nodeIDs; return nil })
	return version, err
}

// ProbeTaskPatch changes only explicitly supplied configuration fields.
type ProbeTaskPatch struct {
	Name            *string
	Host            *string
	Port            *uint16
	IntervalSeconds *uint32
	AutoJoin        *bool
	NodeIDs         *[]string
}

// PatchProbeTask merges fields with committed configuration inside one transaction.
// It returns the saved task and previous assignments for synchronization/cleanup.
func (s *Store) PatchProbeTask(ctx context.Context, id string, patch ProbeTaskPatch, now time.Time) (ProbeTask, []string, error) {
	task, old, _, err := s.mutateProbeTask(ctx, id, now, func(task *ProbeTask) error {
		if patch.Name != nil {
			task.Name = *patch.Name
		}
		if patch.Host != nil {
			task.Host = *patch.Host
		}
		if patch.Port != nil {
			task.Port = *patch.Port
		}
		if patch.IntervalSeconds != nil {
			task.IntervalSeconds = *patch.IntervalSeconds
		}
		if patch.AutoJoin != nil {
			task.AutoJoin = *patch.AutoJoin
		}
		if patch.NodeIDs != nil {
			task.NodeIDs = append([]string{}, (*patch.NodeIDs)...)
		}
		return nil
	})
	return task, old, err
}

// ChangeProbeAssignment modifies one assignment without replacing concurrent assignments.
func (s *Store) ChangeProbeAssignment(ctx context.Context, id, nodeID string, add bool, now time.Time) error {
	_, _, _, err := s.mutateProbeTask(ctx, id, now, func(task *ProbeTask) error {
		index := sort.SearchStrings(task.NodeIDs, nodeID)
		exists := index < len(task.NodeIDs) && task.NodeIDs[index] == nodeID
		if add {
			if exists {
				return ErrProbeAssignmentExists
			}
			task.NodeIDs = append(task.NodeIDs, nodeID)
		} else {
			if !exists {
				return ErrNotFound
			}
			task.NodeIDs = append(task.NodeIDs[:index], task.NodeIDs[index+1:]...)
		}
		return nil
	})
	return err
}

func (s *Store) mutateProbeTask(ctx context.Context, id string, now time.Time, mutate func(*ProbeTask) error) (ProbeTask, []string, uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProbeTask{}, nil, 0, fmt.Errorf("begin probe task update: %w", err)
	}
	defer tx.Rollback()
	task, err := probeTaskByID(ctx, tx, id)
	if err != nil {
		return ProbeTask{}, nil, 0, err
	}
	oldNodes := append([]string{}, task.NodeIDs...)
	if err := mutate(&task); err != nil {
		return ProbeTask{}, nil, 0, err
	}
	nodeIDs := task.NodeIDs
	if err := validateStoredProbe(task, nodeIDs); err != nil {
		return ProbeTask{}, nil, 0, err
	}
	if err := validateProbeAutoJoin(ctx, tx, task.ID, task.AutoJoin); err != nil {
		return ProbeTask{}, nil, 0, err
	}
	if err := validateProbeAssignments(ctx, tx, task.ID, nodeIDs); err != nil {
		return ProbeTask{}, nil, 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE probe_tasks SET name=?,host=?,port=?,interval_seconds=?,auto_join=?,updated_at=? WHERE id=?`,
		task.Name, task.Host, task.Port, task.IntervalSeconds, task.AutoJoin, unix(now), task.ID)
	if err != nil {
		return ProbeTask{}, nil, 0, fmt.Errorf("update probe task: %w", err)
	}
	if err := requireOneRow(result, "update probe task"); err != nil {
		return ProbeTask{}, nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM probe_assignments WHERE task_id=?`, task.ID); err != nil {
		return ProbeTask{}, nil, 0, fmt.Errorf("replace probe assignments: %w", err)
	}
	for _, nodeID := range uniqueSorted(nodeIDs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO probe_assignments(task_id,node_id,created_at) VALUES(?,?,?)`, task.ID, nodeID, unix(now)); err != nil {
			return ProbeTask{}, nil, 0, fmt.Errorf("replace probe assignment: %w", err)
		}
	}
	version, err := bumpProbeVersion(ctx, tx)
	if err != nil {
		return ProbeTask{}, nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return ProbeTask{}, nil, 0, fmt.Errorf("commit probe task update: %w", err)
	}
	task.NodeIDs = uniqueSorted(nodeIDs)
	task.UpdatedAt = now.UTC()
	return task, oldNodes, version, nil
}

// DeleteProbeTask deletes configuration and records idempotent telemetry
// cleanup in the same control.db transaction.
func (s *Store) DeleteProbeTask(ctx context.Context, taskID string, now time.Time) (uint64, []string, error) {
	if taskID == "" || len(taskID) > protocol.MaxProbeIDBytes {
		return 0, nil, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("begin probe task deletion: %w", err)
	}
	defer tx.Rollback()
	nodes, err := probeNodeIDs(ctx, tx, taskID)
	if err != nil {
		return 0, nil, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM probe_tasks WHERE id=?`, taskID)
	if err != nil {
		return 0, nil, fmt.Errorf("delete probe task: %w", err)
	}
	if err := requireOneRow(result, "delete probe task"); err != nil {
		return 0, nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO telemetry_probe_cleanup_jobs(task_id,created_at) VALUES(?,?) ON CONFLICT(task_id) DO NOTHING`, taskID, unix(now)); err != nil {
		return 0, nil, fmt.Errorf("record probe telemetry cleanup: %w", err)
	}
	version, err := bumpProbeVersion(ctx, tx)
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("commit probe task deletion: %w", err)
	}
	return version, nodes, nil
}

// ListProbeTasks returns definitions with stable sorted assignments.
func (s *Store) ListProbeTasks(ctx context.Context) ([]ProbeTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,interval_seconds,auto_join,created_at,updated_at FROM probe_tasks ORDER BY name,id`)
	if err != nil {
		return nil, fmt.Errorf("list probe tasks: %w", err)
	}
	result := make([]ProbeTask, 0)
	for rows.Next() {
		var task ProbeTask
		var port, interval uint64
		var created, updated int64
		if err := rows.Scan(&task.ID, &task.Name, &task.Host, &port, &interval, &task.AutoJoin, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan probe task: %w", err)
		}
		task.Port, task.IntervalSeconds = uint16(port), uint32(interval)
		task.CreatedAt, task.UpdatedAt = fromUnix(created), fromUnix(updated)
		result = append(result, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close probe task rows: %w", err)
	}
	for index := range result {
		nodes, err := probeNodeIDs(ctx, s.db, result[index].ID)
		if err != nil {
			return nil, err
		}
		result[index].NodeIDs = nodes
	}
	return result, nil
}

// ProbeTaskByID returns one definition and its assignments.
func (s *Store) ProbeTaskByID(ctx context.Context, taskID string) (ProbeTask, error) {
	return probeTaskByID(ctx, s.db, taskID)
}

type probeTaskQuerier interface {
	probeQuerier
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func probeTaskByID(ctx context.Context, query probeTaskQuerier, taskID string) (ProbeTask, error) {
	var task ProbeTask
	var port, interval uint64
	var created, updated int64
	err := query.QueryRowContext(ctx, `SELECT id,name,host,port,interval_seconds,auto_join,created_at,updated_at FROM probe_tasks WHERE id=?`, taskID).
		Scan(&task.ID, &task.Name, &task.Host, &port, &interval, &task.AutoJoin, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ProbeTask{}, ErrNotFound
	}
	if err != nil {
		return ProbeTask{}, fmt.Errorf("read probe task: %w", err)
	}
	task.Port, task.IntervalSeconds = uint16(port), uint32(interval)
	task.CreatedAt, task.UpdatedAt = fromUnix(created), fromUnix(updated)
	task.NodeIDs, err = probeNodeIDs(ctx, query, task.ID)
	return task, err
}

// ProbeSnapshot returns one node's complete current assignment set.
func (s *Store) ProbeSnapshot(ctx context.Context, nodeID string) (ProbeSnapshot, error) {
	var version uint64
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM probe_config_state WHERE singleton=1`).Scan(&version); err != nil {
		return ProbeSnapshot{}, fmt.Errorf("read probe config version: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,p.host,p.port,p.interval_seconds,p.auto_join,p.created_at,p.updated_at
		FROM probe_assignments a JOIN probe_tasks p ON p.id=a.task_id JOIN nodes n ON n.id=a.node_id
		WHERE a.node_id=? AND n.deleted_at IS NULL ORDER BY p.id`, nodeID)
	if err != nil {
		return ProbeSnapshot{}, fmt.Errorf("read probe snapshot: %w", err)
	}
	defer rows.Close()
	result := ProbeSnapshot{Version: version, Tasks: make([]ProbeTask, 0)}
	for rows.Next() {
		var task ProbeTask
		var port, interval uint64
		var created, updated int64
		if err := rows.Scan(&task.ID, &task.Name, &task.Host, &port, &interval, &task.AutoJoin, &created, &updated); err != nil {
			return ProbeSnapshot{}, fmt.Errorf("scan probe snapshot: %w", err)
		}
		task.Port, task.IntervalSeconds = uint16(port), uint32(interval)
		task.CreatedAt, task.UpdatedAt = fromUnix(created), fromUnix(updated)
		result.Tasks = append(result.Tasks, task)
	}
	return result, rows.Err()
}

func validateStoredProbe(task ProbeTask, nodeIDs []string) error {
	if task.Name == "" || len(task.Name) > 128 || len(nodeIDs) > 10000 {
		return errors.New("invalid probe task configuration")
	}
	return protocol.ValidateProbeTask(protocol.ProbeTask{ID: task.ID, Host: task.Host, Port: task.Port, IntervalSeconds: task.IntervalSeconds})
}

type probeQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func probeNodeIDs(ctx context.Context, query probeQuerier, taskID string) ([]string, error) {
	rows, err := query.QueryContext(ctx, `SELECT a.node_id FROM probe_assignments a JOIN nodes n ON n.id=a.node_id WHERE a.task_id=? AND n.deleted_at IS NULL ORDER BY a.node_id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list probe assignments: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, fmt.Errorf("scan probe assignment: %w", err)
		}
		result = append(result, nodeID)
	}
	return result, rows.Err()
}

func validateProbeAssignments(ctx context.Context, tx *sql.Tx, excludeTaskID string, nodeIDs []string) error {
	nodes := uniqueSorted(nodeIDs)
	if len(nodes) != len(nodeIDs) {
		return errors.New("duplicate probe assignment")
	}
	for _, nodeID := range nodes {
		if nodeID == "" || len(nodeID) > 128 {
			return errors.New("invalid probe assignment node")
		}
		var exists, count int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id=? AND deleted_at IS NULL)`, nodeID).Scan(&exists); err != nil {
			return fmt.Errorf("validate probe assignment node: %w", err)
		}
		if exists == 0 {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM probe_assignments WHERE node_id=? AND task_id<>?`, nodeID, excludeTaskID).Scan(&count); err != nil {
			return fmt.Errorf("count node probe assignments: %w", err)
		}
		if count >= protocol.MaxProbeTasks {
			return ErrProbeTaskLimit
		}
	}
	return nil
}

func uniqueSorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	write := 1
	for read := 1; read < len(result); read++ {
		if result[read] != result[write-1] {
			result[write] = result[read]
			write++
		}
	}
	return result[:write]
}

func bumpProbeVersion(ctx context.Context, tx *sql.Tx) (uint64, error) {
	var version uint64
	if err := tx.QueryRowContext(ctx, `UPDATE probe_config_state SET version=version+1 WHERE singleton=1 AND version<? RETURNING version`, int64(math.MaxInt64)).Scan(&version); err != nil {
		return 0, fmt.Errorf("advance probe config version: %w", err)
	}
	return version, nil
}

// ListProbeCleanupJobs returns the oldest bounded task-history cleanup work.
func (s *Store) ListProbeCleanupJobs(ctx context.Context, limit int) ([]ProbeCleanupJob, error) {
	if limit < 1 || limit > 256 {
		return nil, errors.New("probe cleanup limit must be between 1 and 256")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT task_id,created_at,attempts,last_attempt_at FROM telemetry_probe_cleanup_jobs ORDER BY created_at,task_id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list probe cleanup jobs: %w", err)
	}
	defer rows.Close()
	result := make([]ProbeCleanupJob, 0)
	for rows.Next() {
		var job ProbeCleanupJob
		var created int64
		var attempted sql.NullInt64
		if err := rows.Scan(&job.TaskID, &created, &job.Attempts, &attempted); err != nil {
			return nil, fmt.Errorf("scan probe cleanup job: %w", err)
		}
		job.CreatedAt = fromUnix(created)
		if attempted.Valid {
			value := fromUnix(attempted.Int64)
			job.LastAttemptAt = &value
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *Store) MarkProbeCleanupAttempt(ctx context.Context, taskID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE telemetry_probe_cleanup_jobs SET attempts=attempts+1,last_attempt_at=? WHERE task_id=?`, unix(now), taskID)
	if err != nil {
		return fmt.Errorf("mark probe cleanup attempt: %w", err)
	}
	return requireOneRow(result, "mark probe cleanup attempt")
}

func (s *Store) CompleteProbeCleanup(ctx context.Context, taskID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM telemetry_probe_cleanup_jobs WHERE task_id=?`, taskID); err != nil {
		return fmt.Errorf("complete probe cleanup: %w", err)
	}
	return nil
}

func validateProbeAutoJoin(ctx context.Context, tx *sql.Tx, excludeTaskID string, enabled bool) error {
	if !enabled {
		return nil
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM probe_tasks WHERE auto_join=1 AND id<>?`, excludeTaskID).Scan(&count); err != nil {
		return fmt.Errorf("count auto-join probe tasks: %w", err)
	}
	if count >= protocol.MaxProbeTasks {
		return ErrProbeAutoJoinLimit
	}
	return nil
}
