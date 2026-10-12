package control

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
)

func seedAlertNode(t *testing.T, store *Store, nodeID, group string, now time.Time) {
	t.Helper()
	token := EnrollmentToken{ID: "token-" + nodeID, Digest: security.DigestEnrollmentToken("token-" + nodeID),
		NodeName: nodeID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, nodeID, "credential-"+nodeID,
		security.DigestNodeCredential("credential-"+nodeID), now); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNode(t.Context(), nodeID, nodeID, group, false, now); err != nil {
		t.Fatal(err)
	}
}

func TestAlertConfigurationStateRestartAndEditReset(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "control.db")
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	seedAlertNode(t, store, "node", "edge", now)
	channel := NotificationChannel{ID: "channel", Name: "primary", Kind: "webhook", Enabled: true,
		WebhookURLCipher: []byte("encrypted-url"), WebhookSecretCipher: []byte("encrypted-secret"), CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
	rule := AlertRule{ID: "rule", Name: "CPU", ScopeKind: "node", ScopeValue: "node", Metric: "cpu", Threshold: 80,
		DurationSeconds: 30, RecoveryHysteresis: 5, Enabled: true, ChannelIDs: []string{channel.ID}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	firing := now.Add(time.Minute)
	state := AlertState{RuleID: rule.ID, NodeID: "node", Status: "firing", Generation: 1, FiringSince: &firing, UpdatedAt: firing}
	delivery := NotificationDelivery{ID: "delivery", EventID: "event", ChannelID: channel.ID, RuleID: rule.ID, NodeID: "node",
		EventKind: "firing", EventPayload: []byte(`{"event_id":"event"}`), NextAttemptAt: firing, CreatedAt: firing}
	if err := store.ApplyAlertState(t.Context(), state, []NotificationDelivery{delivery}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restored, err := store.AlertState(t.Context(), rule.ID, "node")
	if err != nil || restored.Status != "firing" || restored.Generation != 1 || restored.FiringSince == nil {
		t.Fatalf("restored state=%#v err=%v", restored, err)
	}
	rule.Threshold = 90
	rule.UpdatedAt = now.Add(2 * time.Minute)
	if err := store.UpdateAlertRule(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	reset, err := store.AlertState(t.Context(), rule.ID, "node")
	if err != nil || reset.Status != "normal" || reset.Generation != 0 {
		t.Fatalf("state after edit=%#v err=%v", reset, err)
	}
	if _, err := store.NotificationDeliveryByID(t.Context(), delivery.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending delivery survived rule edit: %v", err)
	}
}

func TestAlertNodeDeletionAndRegroupClearObsoleteState(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedAlertNode(t, store, "node", "old", now)
	channel := NotificationChannel{ID: "channel", Name: "primary", Kind: "webhook", Enabled: true,
		WebhookURLCipher: []byte("cipher-url"), WebhookSecretCipher: []byte("cipher-secret"), CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
	groupRule := AlertRule{ID: "group-rule", Name: "Offline", ScopeKind: "group", ScopeValue: "old", Metric: "offline",
		Enabled: true, ChannelIDs: []string{channel.ID}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(t.Context(), groupRule); err != nil {
		t.Fatal(err)
	}
	state := AlertState{RuleID: groupRule.ID, NodeID: "node", Status: "pending", PendingSince: &now, UpdatedAt: now}
	if err := store.ApplyAlertState(t.Context(), state, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNode(t.Context(), "node", "node", "new", false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	reset, err := store.AlertState(t.Context(), groupRule.ID, "node")
	if err != nil || reset.Status != "normal" {
		t.Fatalf("regroup state=%#v err=%v", reset, err)
	}
	nodeRule := AlertRule{ID: "node-rule", Name: "Offline", ScopeKind: "node", ScopeValue: "node", Metric: "offline",
		Enabled: true, ChannelIDs: []string{channel.ID}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(t.Context(), nodeRule); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNode(t.Context(), "node", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AlertRuleByID(t.Context(), nodeRule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("node-scoped rule survived node deletion: %v", err)
	}
	states, err := store.ListAlertStates(t.Context(), groupRule.ID)
	if err != nil || len(states) != 0 {
		t.Fatalf("group states after node deletion=%#v err=%v", states, err)
	}
}

func TestAlertRuleValidationReturnsSentinel(t *testing.T) {
	store := openTestStore(t)
	rule := AlertRule{
		ID:         "rule",
		Name:       "CPU",
		ScopeKind:  "invalid",
		ScopeValue: "node",
		Metric:     "cpu",
		Threshold:  80,
		CreatedAt:  time.Unix(1_700_000_000, 0).UTC(),
		UpdatedAt:  time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := store.CreateAlertRule(t.Context(), rule); !errors.Is(err, ErrInvalidAlertConfiguration) {
		t.Fatalf("CreateAlertRule() error = %v, want ErrInvalidAlertConfiguration", err)
	}
}

func TestApplyAlertStateRollsBackStateWhenOutboxInsertFails(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedAlertNode(t, store, "node", "edge", now)
	channel := NotificationChannel{ID: "channel", Name: "primary", Kind: "webhook", Enabled: true,
		WebhookURLCipher: []byte("cipher-url"), WebhookSecretCipher: []byte("cipher-secret"), CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
	rule := AlertRule{ID: "rule", Name: "CPU", ScopeKind: "node", ScopeValue: "node", Metric: "cpu", Threshold: 80,
		Enabled: true, ChannelIDs: []string{channel.ID}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	state := AlertState{RuleID: rule.ID, NodeID: "node", Status: "firing", Generation: 1, FiringSince: &now, UpdatedAt: now}
	invalid := NotificationDelivery{ID: "delivery", EventID: "event", ChannelID: channel.ID, RuleID: rule.ID, NodeID: "node",
		EventKind: "firing", EventPayload: []byte("x"), NextAttemptAt: now, CreatedAt: now}
	if err := store.ApplyAlertState(t.Context(), state, []NotificationDelivery{invalid}); err == nil {
		t.Fatal("ApplyAlertState accepted an invalid outbox record")
	}
	stored, err := store.AlertState(t.Context(), rule.ID, "node")
	if err != nil || stored.Status != "normal" || stored.Generation != 0 {
		t.Fatalf("state escaped rolled-back transaction: %#v err=%v", stored, err)
	}
}
