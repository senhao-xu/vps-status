package enrollment_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/agent"
	controlapp "github.com/senhao-xu/vps-status/internal/control"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestControlEnrollmentProducesUsableAgentConfigOnce(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatalf("open Control store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close Control store: %v", err)
		}
	})
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatalf("create master key: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	notifications, err := controlapp.NewNotificationDispatcher(ctx, store, key, controlapp.NotificationDispatcherConfig{})
	if err != nil {
		t.Fatalf("create notification dispatcher: %v", err)
	}
	t.Cleanup(func() { cancel(); notifications.Wait() })
	live := controlapp.NewLiveStore()
	alerts, err := controlapp.NewAlertManager(ctx, store, live, notifications, controlapp.AlertManagerConfig{})
	if err != nil {
		t.Fatalf("create alert manager: %v", err)
	}
	t.Cleanup(func() { cancel(); alerts.Wait() })
	handler, err := controlapp.NewHandler(controlapp.APIConfig{
		Store: store, MasterKey: key, Web: http.NotFoundHandler(), WorkerContext: ctx,
		Notifications: notifications, Alerts: alerts, Live: live,
		PasswordParams: security.PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32},
	})
	if err != nil {
		t.Fatalf("create Control handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token, err := security.RandomToken()
	if err != nil {
		t.Fatalf("create enrollment token: %v", err)
	}
	now := time.Now().UTC()
	record := controlstore.EnrollmentToken{
		ID: "enrollment-regression", Digest: security.DigestEnrollmentToken(token), NodeName: "Local compatibility node",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.CreateEnrollmentToken(t.Context(), record); err != nil {
		t.Fatalf("save enrollment token: %v", err)
	}

	config, err := agent.Enroll(t.Context(), server.Client(), server.URL, token)
	if err != nil {
		t.Fatalf("enroll actual Agent against Control: %v", err)
	}
	if config.ControlURL != server.URL || config.NodeID == "" || config.Credential == "" || config.Credential == token {
		t.Fatal("enrollment did not produce an independent node credential and Control config")
	}
	node, _, err := store.AuthenticateNodeCredential(t.Context(), security.DigestNodeCredential(config.Credential))
	if err != nil || node.ID != config.NodeID || node.Name != record.NodeName {
		t.Fatalf("authenticate enrolled node: %v; matching identity=%v", err, node.ID == config.NodeID && node.Name == record.NodeName)
	}
	status, err := store.EnrollmentByID(t.Context(), record.ID)
	if err != nil || status.ConsumedNodeID != config.NodeID {
		t.Fatalf("read consumed enrollment: %v; matching node=%v", err, status.ConsumedNodeID == config.NodeID)
	}
	path := filepath.Join(directory, "agent", "config.json")
	if err := agent.SaveConfig(path, config); err != nil {
		t.Fatalf("save enrolled Agent config: %v", err)
	}
	loaded, err := agent.LoadConfig(path)
	if err != nil || loaded != config {
		t.Fatalf("load enrolled Agent config: %v; config matches=%v", err, loaded == config)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Agent config does not have user-only permissions: %v", err)
	}

	if replayed, err := agent.Enroll(t.Context(), server.Client(), server.URL, token); err == nil || replayed != (agent.Config{}) {
		t.Fatalf("enrollment token replay: %v; empty config=%v", err, replayed == (agent.Config{}))
	}
	nodes, err := store.ListNodes(t.Context())
	if err != nil || len(nodes) != 1 || nodes[0].ID != config.NodeID {
		t.Fatalf("token replay changed enrolled nodes: error=%v count=%d", err, len(nodes))
	}
}
