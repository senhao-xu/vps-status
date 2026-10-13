package control

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func passwordFixture(t *testing.T) (*Store, Admin, []Session, time.Time) {
	t.Helper()
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	admin, err := store.CreateAdmin(t.Context(), "owner", "old-hash", now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := make([]Session, 2)
	for index := range sessions {
		sessions[index] = Session{
			ID: fmt.Sprintf("session-%d", index), AdminID: admin.ID,
			TokenDigest: [32]byte{byte(index + 1)}, CSRFDigest: [32]byte{byte(index + 10)},
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}
		if err := store.CreateSession(t.Context(), sessions[index], false, admin.PasswordHash); err != nil {
			t.Fatal(err)
		}
	}
	return store, admin, sessions, now
}

func TestChangePasswordRevokesAllSessionsAndPreservesMFA(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mfa=%v", confirmed), func(t *testing.T) {
			store, admin, sessions, now := passwordFixture(t)
			cipher := []byte("encrypted-totp-secret")
			if err := store.SetPendingTOTP(t.Context(), admin.ID, cipher, now); err != nil {
				t.Fatal(err)
			}
			digest := [32]byte{42}
			if confirmed {
				if err := store.ConfirmTOTP(t.Context(), admin.ID, sessions[0].ID, [][32]byte{digest}, now); err != nil {
					t.Fatal(err)
				}
				// Add another MFA-authenticated session after confirmation.
				if err := store.CreateSession(t.Context(), sessions[1], true, admin.PasswordHash); err != nil {
					t.Fatal(err)
				}
			}
			changedAt := now.Add(time.Minute)
			if err := store.ChangePassword(t.Context(), admin.ID, sessions[0].ID, admin.PasswordHash, "new-hash", confirmed, changedAt); err != nil {
				t.Fatal(err)
			}
			got, err := store.AdminByID(t.Context(), admin.ID)
			if err != nil || got.PasswordHash != "new-hash" || !got.UpdatedAt.Equal(changedAt) || !got.CreatedAt.Equal(admin.CreatedAt) || got.Username != admin.Username {
				t.Fatalf("changed admin = %#v, %v", got, err)
			}
			if got.TOTPConfirmed != confirmed || !bytes.Equal(got.TOTPSecretCipher, cipher) {
				t.Fatal("password change modified MFA state")
			}
			for _, session := range sessions {
				if _, err := store.SessionByTokenDigest(t.Context(), session.TokenDigest, changedAt); !errors.Is(err, ErrNotFound) {
					t.Fatalf("old session still usable: %v", err)
				}
			}
			records, err := store.ListSessions(t.Context(), admin.ID)
			if err != nil || len(records) != 2 {
				t.Fatalf("sessions = %#v, %v", records, err)
			}
			for _, record := range records {
				if record.RevokedAt == nil || !record.RevokedAt.Equal(changedAt) {
					t.Fatalf("missing revocation timestamp: %#v", record)
				}
			}
			if confirmed {
				if err := store.ConsumeRecoveryCode(t.Context(), admin.ID, digest, changedAt); err != nil {
					t.Fatalf("recovery code lost after change: %v", err)
				}
			}
		})
	}
}

func TestChangePasswordRejectsStaleStateWithoutMutation(t *testing.T) {
	for _, scenario := range []string{"password", "mfa", "missing_session", "revoked_session", "expired_session", "other_admin"} {
		t.Run(scenario, func(t *testing.T) {
			store, admin, sessions, now := passwordFixture(t)
			adminID, sessionID, expectedHash, expectedMFA := admin.ID, sessions[0].ID, admin.PasswordHash, false
			changedAt := now.Add(time.Minute)
			switch scenario {
			case "password":
				expectedHash = "stale-hash"
			case "mfa":
				expectedMFA = true
			case "missing_session":
				sessionID = "missing"
			case "revoked_session":
				if err := store.RevokeSession(t.Context(), admin.ID, sessionID, now); err != nil {
					t.Fatal(err)
				}
			case "expired_session":
				changedAt = sessions[0].ExpiresAt
			case "other_admin":
				adminID++
			}
			before, err := store.ListSessions(t.Context(), admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.ChangePassword(t.Context(), adminID, sessionID, expectedHash, "new-hash", expectedMFA, changedAt); !errors.Is(err, ErrAdminStateChanged) {
				t.Fatalf("ChangePassword() = %v; want state changed", err)
			}
			got, err := store.AdminByID(t.Context(), admin.ID)
			if err != nil || got.PasswordHash != admin.PasswordHash || !got.UpdatedAt.Equal(admin.UpdatedAt) {
				t.Fatalf("failed change mutated admin: %#v, %v", got, err)
			}
			after, err := store.ListSessions(t.Context(), admin.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed change mutated sessions: before=%#v after=%#v err=%v", before, after, err)
			}
		})
	}
}

func TestChangePasswordRollsBackWhenSessionRevocationFails(t *testing.T) {
	store, admin, sessions, now := passwordFixture(t)
	if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER fail_revoke BEFORE UPDATE OF revoked_at ON sessions BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangePassword(t.Context(), admin.ID, sessions[0].ID, admin.PasswordHash, "new-hash", false, now.Add(time.Minute)); err == nil {
		t.Fatal("injected failure succeeded")
	}
	got, err := store.AdminByID(t.Context(), admin.ID)
	if err != nil || got.PasswordHash != admin.PasswordHash || !got.UpdatedAt.Equal(admin.UpdatedAt) {
		t.Fatalf("failed transaction changed admin: %#v, %v", got, err)
	}
	for _, session := range sessions {
		if _, err := store.SessionByTokenDigest(t.Context(), session.TokenDigest, now.Add(time.Minute)); err != nil {
			t.Fatalf("failed transaction revoked session: %v", err)
		}
	}
}

func TestCreateSessionRejectsStalePasswordAfterChange(t *testing.T) {
	store, admin, sessions, now := passwordFixture(t)
	if err := store.ChangePassword(t.Context(), admin.ID, sessions[0].ID, admin.PasswordHash, "new-hash", false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	fresh := Session{ID: "new-login", AdminID: admin.ID, TokenDigest: [32]byte{99}, CSRFDigest: [32]byte{98}, CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateSession(t.Context(), fresh, false, admin.PasswordHash); !errors.Is(err, ErrAdminStateChanged) {
		t.Fatalf("stale password created session: %v", err)
	}
	if _, err := store.SessionByTokenDigest(t.Context(), fresh.TokenDigest, now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale session exists: %v", err)
	}
	if err := store.CreateSession(t.Context(), fresh, false, "new-hash"); err != nil {
		t.Fatalf("new password could not create session: %v", err)
	}
}

func TestConcurrentPasswordChangesAllowOnlyOneWinner(t *testing.T) {
	store, admin, sessions, now := passwordFixture(t)
	second, err := Open(t.Context(), store.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index, connection := range []*Store{store, second} {
		workers.Add(1)
		go func(index int, connection *Store) {
			defer workers.Done()
			<-start
			results <- connection.ChangePassword(t.Context(), admin.ID, sessions[index].ID, admin.PasswordHash, fmt.Sprintf("new-hash-%d", index), false, now.Add(time.Minute))
		}(index, connection)
	}
	close(start)
	workers.Wait()
	close(results)
	var succeeded, conflicted int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAdminStateChanged):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("success=%d conflict=%d; want 1 each", succeeded, conflicted)
	}
	for _, session := range sessions {
		if _, err := store.SessionByTokenDigest(t.Context(), session.TokenDigest, now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("concurrent change left a session valid: %v", err)
		}
	}
}

func TestConcurrentOldPasswordSessionIssuanceCannotSurviveChange(t *testing.T) {
	store, admin, sessions, now := passwordFixture(t)
	second, err := Open(t.Context(), store.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	stale := Session{ID: "old-password-login", AdminID: admin.ID, TokenDigest: [32]byte{99}, CSRFDigest: [32]byte{98}, CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}
	start := make(chan struct{})
	issued := make(chan error, 1)
	go func() {
		<-start
		issued <- second.CreateSession(t.Context(), stale, false, admin.PasswordHash)
	}()
	close(start)
	if err := store.ChangePassword(t.Context(), admin.ID, sessions[0].ID, admin.PasswordHash, "new-hash", false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := <-issued; err != nil && !errors.Is(err, ErrAdminStateChanged) {
		t.Fatalf("unexpected session issuance failure: %v", err)
	}
	if _, err := store.SessionByTokenDigest(t.Context(), stale.TokenDigest, now.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old-password login survived change: %v", err)
	}
}
