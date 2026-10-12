package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AdminExists reports whether the singleton administrator has been created.
func (s *Store) AdminExists(ctx context.Context) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM admins WHERE id = 1)").Scan(&exists); err != nil {
		return false, fmt.Errorf("check administrator: %w", err)
	}
	return exists == 1, nil
}

// CreateAdmin atomically creates the only administrator.
func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string, now time.Time) (Admin, error) {
	result, err := s.db.ExecContext(ctx, `
        INSERT OR IGNORE INTO admins(id, username, password_hash, created_at, updated_at)
        VALUES (1, ?, ?, ?, ?)`, username, passwordHash, unix(now), unix(now))
	if err != nil {
		return Admin{}, fmt.Errorf("create administrator: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Admin{}, fmt.Errorf("read administrator insert result: %w", err)
	}
	if rows != 1 {
		return Admin{}, ErrAdminExists
	}
	return s.AdminByID(ctx, 1)
}

// AdminByUsername retrieves the administrator for password verification.
func (s *Store) AdminByUsername(ctx context.Context, username string) (Admin, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `
        SELECT id, username, password_hash, totp_secret_cipher, totp_confirmed, created_at, updated_at
        FROM admins WHERE username = ?`, username))
}

// AdminByID retrieves an administrator by stable ID.
func (s *Store) AdminByID(ctx context.Context, id int64) (Admin, error) {
	return scanAdmin(s.db.QueryRowContext(ctx, `
        SELECT id, username, password_hash, totp_secret_cipher, totp_confirmed, created_at, updated_at
        FROM admins WHERE id = ?`, id))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAdmin(row rowScanner) (Admin, error) {
	var admin Admin
	var secret []byte
	var confirmed int
	var created, updated int64
	if err := row.Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &secret, &confirmed, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Admin{}, ErrNotFound
		}
		return Admin{}, fmt.Errorf("read administrator: %w", err)
	}
	admin.TOTPSecretCipher = append([]byte(nil), secret...)
	admin.TOTPConfirmed = confirmed == 1
	admin.CreatedAt = fromUnix(created)
	admin.UpdatedAt = fromUnix(updated)
	return admin, nil
}

// SetPendingTOTP stores an encrypted, unconfirmed TOTP secret.
func (s *Store) SetPendingTOTP(ctx context.Context, adminID int64, cipher []byte, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
        UPDATE admins SET totp_secret_cipher = ?, totp_confirmed = 0, updated_at = ?
        WHERE id = ? AND totp_confirmed = 0`, cipher, unix(now), adminID)
	if err != nil {
		return fmt.Errorf("store pending TOTP secret: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read pending TOTP update result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

// ConfirmTOTP marks the pending secret active, replaces all recovery-code
// digests, and revokes every session except keepSessionID in one transaction.
func (s *Store) ConfirmTOTP(ctx context.Context, adminID int64, keepSessionID string, codeDigests [][32]byte, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin TOTP confirmation: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
        UPDATE admins SET totp_confirmed = 1, updated_at = ?
        WHERE id = ? AND totp_confirmed = 0 AND totp_secret_cipher IS NOT NULL`, unix(now), adminID)
	if err != nil {
		return fmt.Errorf("confirm TOTP secret: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read TOTP confirmation result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	var keepSessionExists int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM sessions
			WHERE id = ? AND admin_id = ? AND revoked_at IS NULL AND expires_at > ?
		)`, keepSessionID, adminID, unix(now)).Scan(&keepSessionExists); err != nil {
		return fmt.Errorf("validate current session for TOTP confirmation: %w", err)
	}
	if keepSessionExists != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE admin_id = ?", adminID); err != nil {
		return fmt.Errorf("replace recovery codes: %w", err)
	}
	for _, digest := range codeDigests {
		if _, err := tx.ExecContext(ctx, `
            INSERT INTO recovery_codes(admin_id, code_digest, created_at)
            VALUES (?, ?, ?)`, adminID, digest[:], unix(now)); err != nil {
			return fmt.Errorf("store recovery code digest: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = ?
		WHERE admin_id = ? AND id <> ? AND revoked_at IS NULL`, unix(now), adminID, keepSessionID); err != nil {
		return fmt.Errorf("revoke other sessions after TOTP confirmation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit TOTP confirmation: %w", err)
	}
	return nil
}

// DisableTOTP clears the encrypted secret and every recovery code.
func (s *Store) DisableTOTP(ctx context.Context, adminID int64, now time.Time) error {
	return s.disableTOTP(ctx, adminID, nil, now)
}

// DisableTOTPWithRecovery atomically validates an unused recovery code before
// clearing MFA state.
func (s *Store) DisableTOTPWithRecovery(ctx context.Context, adminID int64, digest [32]byte, now time.Time) error {
	return s.disableTOTP(ctx, adminID, digest[:], now)
}

func (s *Store) disableTOTP(ctx context.Context, adminID int64, recoveryDigest []byte, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin TOTP disable: %w", err)
	}
	defer tx.Rollback()
	if recoveryDigest != nil {
		result, err := tx.ExecContext(ctx, `
            UPDATE recovery_codes SET consumed_at = ?
            WHERE admin_id = ? AND code_digest = ? AND consumed_at IS NULL`, unix(now), adminID, recoveryDigest)
		if err != nil {
			return fmt.Errorf("consume recovery code: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read recovery-code result: %w", err)
		}
		if rows != 1 {
			return ErrRecoveryCode
		}
	}
	result, err := tx.ExecContext(ctx, `
        UPDATE admins SET totp_secret_cipher = NULL, totp_confirmed = 0, updated_at = ?
        WHERE id = ?`, unix(now), adminID)
	if err != nil {
		return fmt.Errorf("disable TOTP: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read TOTP disable result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE admin_id = ?", adminID); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit TOTP disable: %w", err)
	}
	return nil
}

// ConsumeRecoveryCode atomically marks one recovery code as consumed.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, adminID int64, digest [32]byte, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
        UPDATE recovery_codes SET consumed_at = ?
        WHERE admin_id = ? AND code_digest = ? AND consumed_at IS NULL`, unix(now), adminID, digest[:])
	if err != nil {
		return fmt.Errorf("consume recovery code: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read recovery-code result: %w", err)
	}
	if rows != 1 {
		return ErrRecoveryCode
	}
	return nil
}

// ResetMFA clears MFA and revokes all sessions. It is intended only for the
// local data-directory CLI command.
func (s *Store) ResetMFA(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local MFA reset: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
        UPDATE admins SET totp_secret_cipher = NULL, totp_confirmed = 0, updated_at = ?
        WHERE id = 1`, unix(now))
	if err != nil {
		return fmt.Errorf("reset administrator MFA: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read MFA reset result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE admin_id = 1"); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET revoked_at = ? WHERE revoked_at IS NULL", unix(now)); err != nil {
		return fmt.Errorf("revoke sessions after MFA reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local MFA reset: %w", err)
	}
	return nil
}

func unix(value time.Time) int64 { return value.UTC().Unix() }

func fromUnix(value int64) time.Time { return time.Unix(value, 0).UTC() }
