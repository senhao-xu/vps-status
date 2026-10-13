package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const maxActiveSessions = 32

// CreateSession prunes inactive rows and stores only token and CSRF digests in
// one transaction, preventing expired/revoked session history from growing the
// configuration database indefinitely. Both expectedMFA and expectedPasswordHash
// must match the state used to authenticate this session.
func (s *Store) CreateSession(ctx context.Context, session Session, expectedMFA bool, expectedPasswordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session creation: %w", err)
	}
	defer tx.Rollback()
	expectedMFAValue := 0
	if expectedMFA {
		expectedMFAValue = 1
	}
	// The conditional UPDATE acquires SQLite's write lock before checking the
	// authentication state. A concurrent password change or TOTP confirm/reset runs
	// before this transaction (and causes a mismatch) or entirely after it (and
	// revokes the newly inserted session when required).
	result, err := tx.ExecContext(ctx, `
		UPDATE admins SET updated_at = updated_at
		WHERE id = ? AND totp_confirmed = ? AND password_hash = ?`, session.AdminID, expectedMFAValue, expectedPasswordHash)
	if err != nil {
		return fmt.Errorf("validate administrator state for session creation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read administrator state validation result: %w", err)
	}
	if rows != 1 {
		return ErrAdminStateChanged
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE expires_at <= ? OR revoked_at IS NOT NULL`, unix(session.CreatedAt)); err != nil {
		return fmt.Errorf("prune inactive sessions: %w", err)
	}
	// Keep room for the new session by evicting the oldest active sessions.
	// This hard bound prevents a valid credential from growing control.db
	// indefinitely through repeated logins.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE id IN (
			SELECT id FROM sessions
			WHERE admin_id = ?
			ORDER BY created_at DESC, rowid DESC
			LIMIT -1 OFFSET ?
		)`, session.AdminID, maxActiveSessions-1); err != nil {
		return fmt.Errorf("enforce active session limit: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO sessions(id, admin_id, token_digest, csrf_digest, created_at, expires_at)
        VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.AdminID, session.TokenDigest[:], session.CSRFDigest[:],
		unix(session.CreatedAt), unix(session.ExpiresAt)); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session creation: %w", err)
	}
	return nil
}

// SessionByTokenDigest returns an active, unexpired session.
func (s *Store) SessionByTokenDigest(ctx context.Context, digest [32]byte, now time.Time) (Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, `
        SELECT id, admin_id, token_digest, csrf_digest, created_at, expires_at, revoked_at
        FROM sessions
        WHERE token_digest = ? AND revoked_at IS NULL AND expires_at > ?`, digest[:], unix(now)))
}

// ListSessions lists active and historical sessions without token material.
func (s *Store) ListSessions(ctx context.Context, adminID int64) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, admin_id, token_digest, csrf_digest, created_at, expires_at, revoked_at
        FROM sessions WHERE admin_id = ? ORDER BY created_at DESC, rowid DESC`, adminID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

func scanSession(row rowScanner) (Session, error) {
	var session Session
	var tokenDigest, csrfDigest []byte
	var created, expires int64
	var revoked sql.NullInt64
	if err := row.Scan(&session.ID, &session.AdminID, &tokenDigest, &csrfDigest, &created, &expires, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	if len(tokenDigest) != len(session.TokenDigest) || len(csrfDigest) != len(session.CSRFDigest) {
		return Session{}, errors.New("invalid session digest length in database")
	}
	copy(session.TokenDigest[:], tokenDigest)
	copy(session.CSRFDigest[:], csrfDigest)
	session.CreatedAt = fromUnix(created)
	session.ExpiresAt = fromUnix(expires)
	if revoked.Valid {
		value := fromUnix(revoked.Int64)
		session.RevokedAt = &value
	}
	return session, nil
}

// RevokeSession revokes a session owned by the administrator.
func (s *Store) RevokeSession(ctx context.Context, adminID int64, id string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
        UPDATE sessions SET revoked_at = ?
        WHERE id = ? AND admin_id = ? AND revoked_at IS NULL`, unix(now), id, adminID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read session revocation result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

// RevokeSessionByToken revokes the current session during logout.
func (s *Store) RevokeSessionByToken(ctx context.Context, digest [32]byte, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
        UPDATE sessions SET revoked_at = ?
        WHERE token_digest = ? AND revoked_at IS NULL`, unix(now), digest[:])
	if err != nil {
		return fmt.Errorf("revoke current session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read current-session revocation result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}
