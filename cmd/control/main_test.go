package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

func TestAdminResetMFARequiresLocalDatabaseAndRevokesSessions(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "control.db")
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	store, err := controlstore.Open(ctx, path)
	if err != nil {
		t.Fatalf("open control store: %v", err)
	}
	admin, err := store.CreateAdmin(ctx, "owner", "password-hash", now)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := store.SetPendingTOTP(ctx, admin.ID, []byte("encrypted-secret"), now); err != nil {
		t.Fatalf("set pending TOTP: %v", err)
	}
	var token, csrf [32]byte
	token[0], csrf[0] = 2, 3
	if err := store.CreateSession(ctx, controlstore.Session{
		ID: "session", AdminID: admin.ID, TokenDigest: token, CSRFDigest: csrf,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, false); err != nil {
		t.Fatalf("create session: %v", err)
	}
	var recovery [32]byte
	recovery[0] = 1
	if err := store.ConfirmTOTP(ctx, admin.ID, "session", [][32]byte{recovery}, now); err != nil {
		t.Fatalf("confirm TOTP: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close control store: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"admin", "reset-mfa", "--data-dir", directory}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(reset-mfa) code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "MFA reset") || stderr.Len() != 0 {
		t.Fatalf("reset output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	store, err = controlstore.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen control store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close reopened store: %v", err)
		}
	})
	admin, err = store.AdminByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("read admin after reset: %v", err)
	}
	if admin.TOTPConfirmed || len(admin.TOTPSecretCipher) != 0 {
		t.Fatalf("MFA state after reset = confirmed:%v cipher:%q", admin.TOTPConfirmed, admin.TOTPSecretCipher)
	}
	if _, err := store.SessionByTokenDigest(ctx, token, now); !errors.Is(err, controlstore.ErrNotFound) {
		t.Fatalf("session after reset error = %v; want ErrNotFound", err)
	}
	if err := store.ConsumeRecoveryCode(ctx, admin.ID, recovery, now); !errors.Is(err, controlstore.ErrRecoveryCode) {
		t.Fatalf("recovery code after reset error = %v; want ErrRecoveryCode", err)
	}
}

func TestAdminResetMFARefusesMissingDatabase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"admin", "reset-mfa", "--data-dir", t.TempDir()}, &stdout, &stderr); code != 1 {
		t.Fatalf("run(reset-mfa missing db) code = %d; want 1", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "access control database") {
		t.Fatalf("missing-db output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestTelemetryCleanupRetriesDurablyAfterRestart(t *testing.T) {
	directory := t.TempDir()
	controlPath := filepath.Join(directory, "control.db")
	ctx := t.Context()
	now := time.Unix(1_700_000_000, 0).UTC()
	store, err := controlstore.Open(ctx, controlPath)
	if err != nil {
		t.Fatal(err)
	}
	token := controlstore.EnrollmentToken{ID: "token", Digest: security.DigestEnrollmentToken("token"), NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(ctx, token.Digest, "node", "credential", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNode(ctx, "node", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	unavailable := telemetry.NewUnavailable("open_failed")
	runTelemetryCleanup(ctx, store, unavailable, now.Add(2*time.Minute))
	jobs, err := store.ListTelemetryCleanupJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("failed cleanup jobs=%#v, %v", jobs, err)
	}
	if err := unavailable.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = controlstore.Open(ctx, controlPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	telemetryStore, err := telemetry.Open(ctx, filepath.Join(directory, "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := telemetry.NewService(telemetryStore, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	runTelemetryCleanup(ctx, store, service, now.Add(3*time.Minute))
	jobs, err = store.ListTelemetryCleanupJobs(ctx, 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("successful retry jobs=%#v, %v", jobs, err)
	}
	// The crash window after external deletion but before job completion is
	// safe because telemetry deletion and the next retry are idempotent.
	if err := service.DeleteNode(ctx, "node"); err != nil {
		t.Fatalf("repeated telemetry deletion: %v", err)
	}
}

func TestProbeCleanupRetriesDurablyAfterRestart(t *testing.T) {
	directory := t.TempDir()
	controlPath := filepath.Join(directory, "control.db")
	ctx := t.Context()
	now := time.Unix(1_700_000_000, 0).UTC()
	store, err := controlstore.Open(ctx, controlPath)
	if err != nil {
		t.Fatal(err)
	}
	token := controlstore.EnrollmentToken{ID: "token", Digest: security.DigestEnrollmentToken("token"), NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(ctx, token.Digest, "node", "credential", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatal(err)
	}
	task := controlstore.ProbeTask{ID: "task", Name: "TCP", Host: "127.0.0.1", Port: 443, IntervalSeconds: 5}
	if _, err := store.CreateProbeTask(ctx, task, []string{"node"}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.DeleteProbeTask(ctx, task.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	unavailable := telemetry.NewUnavailable("open_failed")
	runTelemetryCleanup(ctx, store, unavailable, now.Add(2*time.Minute))
	jobs, err := store.ListProbeCleanupJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("failed probe cleanup jobs=%#v, %v", jobs, err)
	}
	if err := unavailable.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = controlstore.Open(ctx, controlPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	telemetryStore, err := telemetry.Open(ctx, filepath.Join(directory, "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := telemetry.NewService(telemetryStore, telemetry.Config{FileBudgetBytes: 1 << 30, MinimumFreeBytes: 1, GuardInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	bucketStart := now.Truncate(time.Minute)
	if err := service.SubmitProbe(telemetry.ProbeBucket{NodeID: "node", TaskID: task.ID, BucketStart: bucketStart, Attempts: 1, Failures: 1}); err != nil {
		t.Fatal(err)
	}
	if err := service.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	runTelemetryCleanup(ctx, store, service, now.Add(3*time.Minute))
	jobs, err = store.ListProbeCleanupJobs(ctx, 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("successful probe cleanup jobs=%#v, %v", jobs, err)
	}
	points, err := service.QueryProbe(ctx, "node", task.ID, telemetry.Resolution1m, bucketStart, bucketStart.Add(time.Minute))
	if err != nil || len(points) != 0 {
		t.Fatalf("probe history after cleanup=%#v, %v", points, err)
	}
	if err := service.DeleteProbeTask(ctx, task.ID); err != nil {
		t.Fatalf("repeated probe telemetry deletion: %v", err)
	}
}
