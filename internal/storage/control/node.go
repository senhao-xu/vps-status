package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreateEnrollmentToken persists a one-time token digest and its intended
// node name. The plaintext token never crosses the storage boundary.
func (s *Store) CreateEnrollmentToken(ctx context.Context, token EnrollmentToken) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin enrollment token creation: %w", err)
	}
	defer tx.Rollback()
	// Keep consumed records through their original expiry so the installer UI
	// can associate registration with its stable node ID.
	if _, err := tx.ExecContext(ctx, `DELETE FROM enrollment_tokens WHERE expires_at <= ?`, unix(token.CreatedAt)); err != nil {
		return fmt.Errorf("prune unavailable enrollment tokens: %w", err)
	}
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enrollment_tokens
		WHERE consumed_at IS NULL AND expires_at > ?`, unix(token.CreatedAt)).Scan(&active); err != nil {
		return fmt.Errorf("count active enrollment tokens: %w", err)
	}
	if active >= MaxActiveEnrollmentTokens {
		return ErrEnrollmentTokenLimit
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enrollment_tokens(id, token_digest, node_name, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`, token.ID, token.Digest[:], token.NodeName,
		unix(token.CreatedAt), unix(token.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create enrollment token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit enrollment token creation: %w", err)
	}
	return nil
}

// EnrollmentStatus is the non-secret projection of one enrollment record.
type EnrollmentStatus struct {
	ID             string
	NodeName       string
	ExpiresAt      time.Time
	ConsumedNodeID string
}

// EnrollmentByID reads installation metadata without reading token material.
func (s *Store) EnrollmentByID(ctx context.Context, id string) (EnrollmentStatus, error) {
	var record EnrollmentStatus
	var expires int64
	var nodeID sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,node_name,expires_at,consumed_node_id FROM enrollment_tokens WHERE id=?`, id).Scan(
		&record.ID, &record.NodeName, &expires, &nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return EnrollmentStatus{}, ErrNotFound
	}
	if err != nil {
		return EnrollmentStatus{}, fmt.Errorf("read enrollment status: %w", err)
	}
	record.ExpiresAt, record.ConsumedNodeID = fromUnix(expires), nodeID.String
	return record, nil
}

// RevokeEnrollment deletes only unconsumed tokens. Consumption and revocation
// serialize within control.db; a consumed record and its node remain intact.
func (s *Store) RevokeEnrollment(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin enrollment revocation: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM enrollment_tokens WHERE id=? AND consumed_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke enrollment: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read enrollment revocation result: %w", err)
	}
	if rows == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM enrollment_tokens WHERE id=?`, id).Scan(&exists); err != nil {
			return fmt.Errorf("read retained enrollment: %w", err)
		}
		if exists != 0 {
			return ErrEnrollmentConsumed
		}
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit enrollment revocation: %w", err)
	}
	return nil
}

// ConsumeEnrollment atomically consumes one valid enrollment token and
// creates a stable node identity with its first independent credential.
func (s *Store) ConsumeEnrollment(ctx context.Context, digest [32]byte, nodeID, credentialID string, credentialDigest [32]byte, now time.Time) (Node, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, fmt.Errorf("begin enrollment: %w", err)
	}
	defer tx.Rollback()

	var tokenID, nodeName string
	var expires int64
	var consumed sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, node_name, expires_at, consumed_at
		FROM enrollment_tokens WHERE token_digest = ?`, digest[:]).Scan(&tokenID, &nodeName, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrEnrollmentInvalid
	}
	if err != nil {
		return Node{}, fmt.Errorf("read enrollment token: %w", err)
	}
	if consumed.Valid {
		return Node{}, ErrEnrollmentConsumed
	}
	if expires <= unix(now) {
		return Node{}, ErrEnrollmentExpired
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO nodes(id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		nodeID, nodeName, unix(now), unix(now)); err != nil {
		return Node{}, fmt.Errorf("create enrolled node: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO node_credentials(id, node_id, credential_digest, created_at)
		VALUES (?, ?, ?, ?)`, credentialID, nodeID, credentialDigest[:], unix(now)); err != nil {
		return Node{}, fmt.Errorf("create node credential: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens SET consumed_at = ?, consumed_node_id = ?
		WHERE id = ? AND consumed_at IS NULL AND expires_at > ?`,
		unix(now), nodeID, tokenID, unix(now))
	if err != nil {
		return Node{}, fmt.Errorf("consume enrollment token: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Node{}, fmt.Errorf("read enrollment consumption result: %w", err)
	}
	if rows != 1 {
		return Node{}, ErrEnrollmentConsumed
	}
	// Full display capacity must not prevent a node from registering. The
	// administrator can free capacity and enable it later in node settings.
	publication, err := tx.ExecContext(ctx, `INSERT INTO public_nodes(node_id,published_at)
		SELECT ?,? WHERE (SELECT COUNT(*) FROM public_nodes) < ?`, nodeID, unix(now), MaxPublicNodes)
	if err != nil {
		return Node{}, fmt.Errorf("add enrolled node to homepage: %w", err)
	}
	published, err := publication.RowsAffected()
	if err != nil {
		return Node{}, fmt.Errorf("read enrolled homepage membership: %w", err)
	}
	assignments, err := tx.ExecContext(ctx, `INSERT INTO probe_assignments(task_id,node_id,created_at) SELECT id,?,? FROM probe_tasks WHERE auto_join=1`, nodeID, unix(now))
	if err != nil {
		return Node{}, fmt.Errorf("assign enrolled node probes: %w", err)
	}
	assigned, err := assignments.RowsAffected()
	if err != nil {
		return Node{}, fmt.Errorf("read enrolled probe assignment count: %w", err)
	}
	if assigned > 0 {
		if _, err := bumpProbeVersion(ctx, tx); err != nil {
			return Node{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Node{}, fmt.Errorf("commit enrollment: %w", err)
	}
	return Node{ID: nodeID, Name: nodeName, HomepageVisible: published > 0, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

// AuthenticateNodeCredential returns the active node and credential identity.
func (s *Store) AuthenticateNodeCredential(ctx context.Context, digest [32]byte) (Node, NodeCredential, error) {
	var node Node
	var credential NodeCredential
	var rawDigest []byte
	var nodeCreated, nodeUpdated, credentialCreated int64
	var hidden, homepageVisible int
	err := s.db.QueryRowContext(ctx, `
		SELECT n.id, n.name, n.group_name, n.hidden, n.hostname, n.os, n.kernel, n.architecture, n.virtualization,
		       n.created_at, n.updated_at, EXISTS(SELECT 1 FROM public_nodes p WHERE p.node_id=n.id),
		       c.id, c.node_id, c.credential_digest, c.created_at
		FROM node_credentials c JOIN nodes n ON n.id = c.node_id
		WHERE c.credential_digest = ? AND c.revoked_at IS NULL AND n.deleted_at IS NULL`, digest[:]).Scan(
		&node.ID, &node.Name, &node.Group, &hidden, &node.Facts.Hostname, &node.Facts.OS, &node.Facts.Kernel,
		&node.Facts.Architecture, &node.Facts.Virtualization, &nodeCreated, &nodeUpdated, &homepageVisible,
		&credential.ID, &credential.NodeID, &rawDigest, &credentialCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, NodeCredential{}, ErrCredentialRevoked
	}
	if err != nil {
		return Node{}, NodeCredential{}, fmt.Errorf("authenticate node credential: %w", err)
	}
	if len(rawDigest) != len(credential.Digest) {
		return Node{}, NodeCredential{}, errors.New("invalid node credential digest length in database")
	}
	copy(credential.Digest[:], rawDigest)
	node.Hidden, node.HomepageVisible = hidden != 0, homepageVisible != 0
	node.CreatedAt, node.UpdatedAt = fromUnix(nodeCreated), fromUnix(nodeUpdated)
	credential.CreatedAt = fromUnix(credentialCreated)
	return node, credential, nil
}

// ListNodes returns active low-growth node metadata in stable display order.
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, group_name, hidden, hostname, os, kernel, architecture, virtualization, created_at, updated_at,
		       EXISTS(SELECT 1 FROM public_nodes p WHERE p.node_id=nodes.id)
		FROM nodes WHERE deleted_at IS NULL ORDER BY group_name, name, id`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	result := make([]Node, 0)
	for rows.Next() {
		node, scanErr := scanNode(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan node: %w", scanErr)
		}
		result = append(result, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return result, nil
}

// NodeByID returns one active node's low-growth metadata.
func (s *Store) NodeByID(ctx context.Context, nodeID string) (Node, error) {
	node, err := scanNode(s.db.QueryRowContext(ctx, `
		SELECT id, name, group_name, hidden, hostname, os, kernel, architecture, virtualization, created_at, updated_at,
		       EXISTS(SELECT 1 FROM public_nodes p WHERE p.node_id=nodes.id)
		FROM nodes WHERE id = ? AND deleted_at IS NULL`, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("read node: %w", err)
	}
	return node, nil
}

func scanNode(row rowScanner) (Node, error) {
	var node Node
	var hidden, homepageVisible int
	var createdAt, updatedAt int64
	err := row.Scan(&node.ID, &node.Name, &node.Group, &hidden, &node.Facts.Hostname, &node.Facts.OS, &node.Facts.Kernel,
		&node.Facts.Architecture, &node.Facts.Virtualization, &createdAt, &updatedAt, &homepageVisible)
	if err != nil {
		return Node{}, err
	}
	node.Hidden, node.HomepageVisible = hidden != 0, homepageVisible != 0
	node.CreatedAt, node.UpdatedAt = fromUnix(createdAt), fromUnix(updatedAt)
	return node, nil
}

// NodePatch merges only supplied administrator-owned settings.
type NodePatch struct {
	Name            *string
	Group           *string
	Hidden          *bool
	HomepageVisible *bool
}

// UpdateNode keeps the legacy metadata operation without replacing membership.
func (s *Store) UpdateNode(ctx context.Context, nodeID, name, group string, hidden bool, now time.Time) error {
	_, err := s.PatchNode(ctx, nodeID, NodePatch{Name: &name, Group: &group, Hidden: &hidden}, now)
	return err
}

// PatchNode reads, validates and merges metadata and display membership in one
// transaction. Omitted fields cannot restore a stale handler snapshot.
func (s *Store) PatchNode(ctx context.Context, nodeID string, patch NodePatch, now time.Time) (Node, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, fmt.Errorf("begin node metadata update: %w", err)
	}
	defer tx.Rollback()
	current, err := scanNode(tx.QueryRowContext(ctx, `SELECT id,name,group_name,hidden,hostname,os,kernel,architecture,virtualization,created_at,updated_at,
		EXISTS(SELECT 1 FROM public_nodes p WHERE p.node_id=nodes.id)
		FROM nodes WHERE id=? AND deleted_at IS NULL`, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	} else if err != nil {
		return Node{}, fmt.Errorf("read node metadata before update: %w", err)
	}
	oldGroup := current.Group
	if patch.Name != nil {
		current.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Group != nil {
		current.Group = strings.TrimSpace(*patch.Group)
	}
	if patch.Hidden != nil {
		current.Hidden = *patch.Hidden
	}
	if current.Name == "" || len(current.Name) > 128 || len(current.Group) > 128 {
		return Node{}, ErrInvalidNodeConfiguration
	}
	if patch.HomepageVisible != nil && *patch.HomepageVisible != current.HomepageVisible {
		if *patch.HomepageVisible {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM public_nodes`).Scan(&count); err != nil {
				return Node{}, fmt.Errorf("count homepage nodes: %w", err)
			}
			if count >= MaxPublicNodes {
				return Node{}, ErrHomepageNodeLimit
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO public_nodes(node_id,published_at) VALUES(?,?)`, nodeID, unix(now)); err != nil {
				return Node{}, fmt.Errorf("add node to homepage: %w", err)
			}
		} else if _, err := tx.ExecContext(ctx, `DELETE FROM public_nodes WHERE node_id=?`, nodeID); err != nil {
			return Node{}, fmt.Errorf("remove node from homepage: %w", err)
		}
		current.HomepageVisible = *patch.HomepageVisible
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET name=?,group_name=?,hidden=?,updated_at=? WHERE id=?`,
		current.Name, current.Group, boolInt(current.Hidden), unix(now), nodeID); err != nil {
		return Node{}, fmt.Errorf("update node metadata: %w", err)
	}
	if oldGroup != current.Group {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE node_id=? AND status IN ('pending','retry') AND rule_id IN (
			SELECT id FROM alert_rules WHERE scope_kind='group' AND scope_value<>?)`, nodeID, current.Group); err != nil {
			return Node{}, fmt.Errorf("reset regrouped node notification deliveries: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM alert_states WHERE node_id=? AND rule_id IN (
			SELECT id FROM alert_rules WHERE scope_kind='group' AND scope_value<>?)`, nodeID, current.Group); err != nil {
			return Node{}, fmt.Errorf("reset regrouped node alert states: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Node{}, fmt.Errorf("commit node metadata update: %w", err)
	}
	current.UpdatedAt = now.UTC()
	return current, nil
}

// UpdateNodeFacts changes bounded low-frequency identity facts from hello. It
// never stores report values, rates, counters, or samples.
func (s *Store) UpdateNodeFacts(ctx context.Context, nodeID string, facts NodeFacts, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes SET hostname = ?, os = ?, kernel = ?, architecture = ?, virtualization = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL AND
		      (hostname <> ? OR os <> ? OR kernel <> ? OR architecture <> ? OR virtualization <> ?)`,
		facts.Hostname, facts.OS, facts.Kernel, facts.Architecture, facts.Virtualization, unix(now), nodeID,
		facts.Hostname, facts.OS, facts.Kernel, facts.Architecture, facts.Virtualization)
	if err != nil {
		return fmt.Errorf("update node facts: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read node facts update result: %w", err)
	}
	if rows == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id = ? AND deleted_at IS NULL)`, nodeID).Scan(&exists); err != nil {
			return fmt.Errorf("check node after facts update: %w", err)
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// DeleteNode soft-deletes metadata, revokes credentials, and durably records
// cross-database telemetry cleanup in one transaction.
func (s *Store) DeleteNode(ctx context.Context, nodeID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin node deletion: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE nodes SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, unix(now), unix(now), nodeID)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	if err := requireOneRow(result, "delete node"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE node_credentials SET revoked_at = ? WHERE node_id = ? AND revoked_at IS NULL`, unix(now), nodeID); err != nil {
		return fmt.Errorf("revoke deleted node credentials: %w", err)
	}
	// Node-scoped rules cannot remain valid after the node disappears. Group
	// rules remain, but their sparse state and pending deliveries for this node
	// are removed in the same transaction.
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rules WHERE scope_kind='node' AND scope_value=?`, nodeID); err != nil {
		return fmt.Errorf("remove deleted node alert rules: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE node_id=?`, nodeID); err != nil {
		return fmt.Errorf("remove deleted node notification deliveries: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_states WHERE node_id=?`, nodeID); err != nil {
		return fmt.Errorf("remove deleted node alert states: %w", err)
	}
	// Nodes are soft-deleted, so the public_nodes foreign-key cascade never
	// runs. Remove the publication row explicitly in the same transaction so a
	// deleted node cannot remain in the durable public allowlist.
	if _, err := tx.ExecContext(ctx, `DELETE FROM public_nodes WHERE node_id=?`, nodeID); err != nil {
		return fmt.Errorf("unpublish deleted node: %w", err)
	}
	assignments, err := tx.ExecContext(ctx, `DELETE FROM probe_assignments WHERE node_id=?`, nodeID)
	if err != nil {
		return fmt.Errorf("remove deleted node probe assignments: %w", err)
	}
	if affected, rowsErr := assignments.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("read deleted node probe assignments: %w", rowsErr)
	} else if affected > 0 {
		if _, err := bumpProbeVersion(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO telemetry_cleanup_jobs(node_id,created_at) VALUES(?,?)
		ON CONFLICT(node_id) DO NOTHING`, nodeID, unix(now)); err != nil {
		return fmt.Errorf("record telemetry cleanup: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node deletion: %w", err)
	}
	return nil
}

// ListTelemetryCleanupJobs returns the oldest bounded batch of durable work.
func (s *Store) ListTelemetryCleanupJobs(ctx context.Context, limit int) ([]TelemetryCleanupJob, error) {
	if limit < 1 || limit > 256 {
		return nil, errors.New("telemetry cleanup limit must be between 1 and 256")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_id,created_at,attempts,last_attempt_at
		FROM telemetry_cleanup_jobs ORDER BY created_at,node_id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list telemetry cleanup jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]TelemetryCleanupJob, 0)
	for rows.Next() {
		var job TelemetryCleanupJob
		var created int64
		var attempted sql.NullInt64
		if err := rows.Scan(&job.NodeID, &created, &job.Attempts, &attempted); err != nil {
			return nil, fmt.Errorf("scan telemetry cleanup job: %w", err)
		}
		job.CreatedAt = fromUnix(created)
		if attempted.Valid {
			value := fromUnix(attempted.Int64)
			job.LastAttemptAt = &value
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry cleanup jobs: %w", err)
	}
	return jobs, nil
}

// MarkTelemetryCleanupAttempt records bounded retry diagnostics before an
// external telemetry deletion is attempted.
func (s *Store) MarkTelemetryCleanupAttempt(ctx context.Context, nodeID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE telemetry_cleanup_jobs
		SET attempts=attempts+1,last_attempt_at=? WHERE node_id=?`, unix(now), nodeID)
	if err != nil {
		return fmt.Errorf("mark telemetry cleanup attempt: %w", err)
	}
	return requireOneRow(result, "mark telemetry cleanup attempt")
}

// CompleteTelemetryCleanup removes a job only after telemetry deletion has
// succeeded. Repeated completion is harmless.
func (s *Store) CompleteTelemetryCleanup(ctx context.Context, nodeID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM telemetry_cleanup_jobs WHERE node_id=?`, nodeID); err != nil {
		return fmt.Errorf("complete telemetry cleanup: %w", err)
	}
	return nil
}

func requireOneRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s result: %w", operation, err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// RotateNodeCredential revokes every active credential and creates one new
// credential in the same transaction.
func (s *Store) RotateNodeCredential(ctx context.Context, nodeID, credentialID string, digest [32]byte, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin node credential rotation: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE nodes SET updated_at = updated_at WHERE id = ? AND deleted_at IS NULL`, nodeID)
	if err != nil {
		return fmt.Errorf("validate node for credential rotation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read node validation result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE node_credentials SET revoked_at = ?
		WHERE node_id = ? AND revoked_at IS NULL`, unix(now), nodeID); err != nil {
		return fmt.Errorf("revoke old node credentials: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO node_credentials(id, node_id, credential_digest, created_at)
		VALUES (?, ?, ?, ?)`, credentialID, nodeID, digest[:], unix(now)); err != nil {
		return fmt.Errorf("create rotated node credential: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_credentials WHERE node_id = ? AND revoked_at IS NOT NULL`, nodeID); err != nil {
		return fmt.Errorf("prune rotated node credentials: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node credential rotation: %w", err)
	}
	return nil
}

// RevokeNodeCredentials immediately invalidates all active credentials.
func (s *Store) RevokeNodeCredentials(ctx context.Context, nodeID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE node_credentials SET revoked_at = ?
		WHERE node_id = ? AND revoked_at IS NULL`, unix(now), nodeID)
	if err != nil {
		return fmt.Errorf("revoke node credentials: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read node credential revocation result: %w", err)
	}
	if rows == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id = ? AND deleted_at IS NULL)`, nodeID).Scan(&exists); err != nil {
			return fmt.Errorf("check node after credential revocation: %w", err)
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}
