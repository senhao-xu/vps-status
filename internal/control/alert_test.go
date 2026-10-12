package control

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

func TestAlertStateDurationJitterHysteresisAndReminderBound(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	rule := controlstore.AlertRule{ID: "rule", Metric: "cpu", Threshold: 80, RecoveryHysteresis: 5,
		DurationSeconds: 10, ReminderIntervalSeconds: 300, ReminderMaxCount: 1}
	state := controlstore.AlertState{RuleID: rule.ID, NodeID: "node", Status: "normal"}

	state, event, changed := advanceAlertState(rule, state, true, false, now)
	if !changed || state.Status != "pending" || event != "" {
		t.Fatalf("normal -> %#v event=%q changed=%v", state, event, changed)
	}
	state, event, changed = advanceAlertState(rule, state, false, true, now.Add(9*time.Second))
	if !changed || state.Status != "normal" || event != "" {
		t.Fatalf("jitter recovery -> %#v event=%q changed=%v", state, event, changed)
	}
	state, _, _ = advanceAlertState(rule, state, true, false, now.Add(20*time.Second))
	state, event, changed = advanceAlertState(rule, state, true, false, now.Add(30*time.Second))
	if !changed || state.Status != "firing" || event != "firing" || state.Generation != 1 {
		t.Fatalf("duration firing -> %#v event=%q changed=%v", state, event, changed)
	}
	state, event, changed = advanceAlertState(rule, state, true, false, now.Add(330*time.Second))
	if !changed || event != "reminder" || state.ReminderCount != 1 {
		t.Fatalf("reminder -> %#v event=%q changed=%v", state, event, changed)
	}
	_, event, changed = advanceAlertState(rule, state, true, false, now.Add(630*time.Second))
	if changed || event != "" {
		t.Fatal("bounded reminder repeated after max count")
	}
	state, event, changed = advanceAlertState(rule, state, false, false, now.Add(640*time.Second))
	if changed || event != "" || state.Status != "firing" {
		t.Fatal("value inside hysteresis resolved a firing alert")
	}
	state, event, changed = advanceAlertState(rule, state, false, true, now.Add(650*time.Second))
	if !changed || event != "resolved" || state.Status != "resolved" {
		t.Fatalf("hysteresis recovery -> %#v event=%q", state, event)
	}
	state, _, _ = advanceAlertState(rule, state, false, true, now.Add(651*time.Second))
	if state.Status != "normal" || state.Generation != 1 {
		t.Fatalf("resolved -> normal lost generation: %#v", state)
	}
}

func TestEvaluateAlertMetricOfflineValidityAndTrafficDefinition(t *testing.T) {
	rule := controlstore.AlertRule{Metric: "offline"}
	known, _, breach, recovered := evaluateAlertMetric(rule, LiveSnapshot{Connected: false})
	if !known || !breach || recovered {
		t.Fatal("disconnected node did not breach offline rule")
	}
	known, _, breach, recovered = evaluateAlertMetric(rule, LiveSnapshot{Connected: true, Online: true})
	if known || breach || recovered {
		t.Fatal("online flag without a current report manufactured offline recovery")
	}
	report := protocol.Report{CollectedAtUnixMilli: time.Now().UnixMilli(), Network: protocol.NetworkStats{ReceiveBytesPerSecond: 7, TransmitBytesPerSecond: 11}}
	known, _, breach, recovered = evaluateAlertMetric(rule, LiveSnapshot{Connected: true, Online: true, Report: &report})
	if !known || breach || !recovered {
		t.Fatal("valid report did not recover offline rule")
	}
	traffic := controlstore.AlertRule{Metric: "traffic_rate", Threshold: 17}
	known, value, breach, _ := evaluateAlertMetric(traffic, LiveSnapshot{Connected: true, Online: true, Report: &report})
	if !known || value != 18 || !breach {
		t.Fatalf("traffic value=%v known=%v breach=%v; want rx+tx=18", value, known, breach)
	}
	report.Unavailable = []protocol.MetricArea{protocol.AreaNetwork}
	known, _, _, _ = evaluateAlertMetric(traffic, LiveSnapshot{Connected: true, Online: true, Report: &report})
	if known {
		t.Fatal("unavailable network metric was evaluated")
	}
}

type notificationRoundTripper func(*http.Request) (*http.Response, error)

func (function notificationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackingBody struct {
	reader io.Reader
	closed bool
}

func (body *trackingBody) Read(buffer []byte) (int, error) { return body.reader.Read(buffer) }
func (body *trackingBody) Close() error {
	body.closed = true
	return nil
}

func TestNotificationHTTPClassificationBodyBoundAndQueueFull(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      []byte
		retryable bool
		code      string
	}{
		{name: "server", status: http.StatusServiceUnavailable, retryable: true, code: "server_error"},
		{name: "rate-limit", status: http.StatusTooManyRequests, retryable: true, code: "rate_limited"},
		{name: "terminal", status: http.StatusBadRequest, code: "client_error"},
		{name: "oversized", status: http.StatusOK, body: bytes.Repeat([]byte("x"), maxNotificationResponseBytes+1), retryable: true, code: "response_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			responseBody := &trackingBody{reader: bytes.NewReader(test.body)}
			client := &http.Client{Timeout: time.Second, Transport: notificationRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: responseBody}, nil
			})}
			dispatcher := &NotificationDispatcher{ctx: t.Context(), config: NotificationDispatcherConfig{HTTPClient: client}.withDefaults()}
			request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://notify.example", nil)
			outcome := dispatcher.doRequest(request, false)
			if outcome.retryable != test.retryable || outcome.code != test.code || !responseBody.closed {
				t.Fatalf("outcome=%#v closed=%v", outcome, responseBody.closed)
			}
		})
	}
	queueDispatcher := &NotificationDispatcher{ctx: t.Context(), queue: make(chan string, 1), inFlight: make(map[string]struct{})}
	if err := queueDispatcher.Submit("one"); err != nil {
		t.Fatal(err)
	}
	if err := queueDispatcher.Submit("two"); !errors.Is(err, ErrNotificationQueueFull) {
		t.Fatalf("second Submit error=%v; want queue full", err)
	}
}

func TestNotificationTimeoutAndContextShutdown(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Millisecond, Transport: notificationRoundTripper(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	dispatcher := &NotificationDispatcher{ctx: t.Context(), config: NotificationDispatcherConfig{HTTPClient: client}.withDefaults()}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://slow.example", nil)
	started := time.Now()
	outcome := dispatcher.doRequest(request, false)
	if !outcome.retryable || outcome.code != "network" || time.Since(started) > time.Second {
		t.Fatalf("timeout outcome=%#v elapsed=%v", outcome, time.Since(started))
	}

	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	running, err := NewNotificationDispatcher(ctx, store, key, NotificationDispatcherConfig{PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan struct{})
	go func() { running.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notification dispatcher did not stop after cancellation")
	}
}

func TestWebhookSignatureStableEventAndBoundedDispatcher(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	urlCipher, _ := key.Encrypt([]byte("https://hooks.example/notify"))
	secret := []byte("webhook-signing-secret")
	secretCipher, _ := key.Encrypt(secret)
	now := time.Unix(1_700_000_000, 0).UTC()
	channel := controlstore.NotificationChannel{ID: "channel", Name: "webhook", Kind: "webhook", Enabled: true,
		WebhookURLCipher: urlCipher, WebhookSecretCipher: secretCipher, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"event_id":"stable-event","kind":"test"}`)
	delivery := controlstore.NotificationDelivery{ID: "delivery", EventID: "stable-event", ChannelID: channel.ID,
		EventKind: "test", EventPayload: payload, NextAttemptAt: now, CreatedAt: now}
	if err := store.CreateNotificationDelivery(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{}, 1)
	client := &http.Client{Timeout: time.Second, Transport: notificationRoundTripper(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Error(readErr)
		}
		if !bytes.Equal(body, payload) || request.Header.Get("X-VPS-Status-Event-ID") != delivery.EventID {
			t.Errorf("webhook body/event mismatch: %q %#v", body, request.Header)
		}
		timestamp := request.Header.Get("X-VPS-Status-Timestamp")
		if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
			t.Errorf("invalid timestamp header %q", timestamp)
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(timestamp + "."))
		_, _ = mac.Write(payload)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := request.Header.Get("X-VPS-Status-Signature"); got != want {
			t.Errorf("signature=%q want=%q", got, want)
		}
		called <- struct{}{}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher, err := NewNotificationDispatcher(ctx, store, key, NotificationDispatcherConfig{HTTPClient: client, Now: func() time.Time { return now }, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Submit(delivery.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was not delivered")
	}
	deadline := time.Now().Add(2 * time.Second)
	var stored controlstore.NotificationDelivery
	for time.Now().Before(deadline) {
		stored, err = store.NotificationDeliveryByID(t.Context(), delivery.ID)
		if err == nil && stored.Status == "delivered" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	dispatcher.Wait()
	if err != nil || stored.Status != "delivered" || stored.Attempts != 1 {
		t.Fatalf("delivery=%#v err=%v", stored, err)
	}
}

func TestNotificationRetriesKeepEventIDAndExhaustAtMaximum(t *testing.T) {
	for _, test := range []struct {
		name       string
		recover    bool
		wantStatus string
	}{
		{name: "recovers", recover: true, wantStatus: "delivered"},
		{name: "exhausted", wantStatus: "terminal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
			if err != nil {
				t.Fatal(err)
			}
			urlCipher, _ := key.Encrypt([]byte("https://hooks.example/notify"))
			secretCipher, _ := key.Encrypt([]byte("secret"))
			now := time.Now().UTC()
			channel := controlstore.NotificationChannel{ID: "channel", Name: "webhook", Kind: "webhook", Enabled: true,
				WebhookURLCipher: urlCipher, WebhookSecretCipher: secretCipher, CreatedAt: now, UpdatedAt: now}
			if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
				t.Fatal(err)
			}
			delivery := controlstore.NotificationDelivery{ID: "delivery", EventID: "stable-event", ChannelID: channel.ID,
				EventKind: "test", EventPayload: []byte(`{"kind":"test"}`), NextAttemptAt: now, CreatedAt: now}
			if err := store.CreateNotificationDelivery(t.Context(), delivery); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			calls := 0
			eventIDs := make([]string, 0, 2)
			client := &http.Client{Timeout: time.Second, Transport: notificationRoundTripper(func(request *http.Request) (*http.Response, error) {
				mu.Lock()
				calls++
				eventIDs = append(eventIDs, request.Header.Get("X-VPS-Status-Event-ID"))
				current := calls
				mu.Unlock()
				status := http.StatusServiceUnavailable
				if test.recover && current == 2 {
					status = http.StatusNoContent
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
			})}
			ctx, cancel := context.WithCancel(context.Background())
			dispatcher, err := NewNotificationDispatcher(ctx, store, key, NotificationDispatcherConfig{
				HTTPClient: client, MaxAttempts: 2, BaseBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond,
				PollInterval: time.Millisecond, Jitter: func(time.Duration) time.Duration { return 0 },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); dispatcher.Wait() }()
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				stored, readErr := store.NotificationDeliveryByID(t.Context(), delivery.ID)
				if readErr == nil && stored.Status == test.wantStatus {
					if stored.Attempts != 2 {
						t.Fatalf("attempts=%d; want 2", stored.Attempts)
					}
					mu.Lock()
					ids := append([]string(nil), eventIDs...)
					mu.Unlock()
					if len(ids) != 2 || ids[0] != delivery.EventID || ids[1] != delivery.EventID {
						t.Fatalf("retry event IDs=%#v", ids)
					}
					return
				}
				time.Sleep(time.Millisecond)
			}
			stored, _ := store.NotificationDeliveryByID(t.Context(), delivery.ID)
			t.Fatalf("delivery did not reach %s: %#v", test.wantStatus, stored)
		})
	}
}

func TestTelegramRetryAfterIsBoundedAndTokenNeverInOutcome(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	token := "123456:super-secret-token"
	tokenCipher, _ := key.Encrypt([]byte(token))
	chatCipher, _ := key.Encrypt([]byte("-100123"))
	var mu sync.Mutex
	seenPath := ""
	client := &http.Client{Timeout: time.Second, Transport: notificationRoundTripper(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		seenPath = request.URL.Path
		mu.Unlock()
		body := `{"ok":false,"parameters":{"retry_after":999999}}`
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body))}, nil
	})}
	dispatcher := &NotificationDispatcher{ctx: t.Context(), store: store, masterKey: key,
		config: NotificationDispatcherConfig{HTTPClient: client, TelegramBaseURL: "https://api.telegram.test", Now: time.Now}.withDefaults()}
	outcome := dispatcher.sendTelegram(controlstore.NotificationDelivery{EventPayload: []byte(`{"kind":"test"}`)}, controlstore.NotificationChannel{
		Kind: "telegram", TelegramTokenCipher: tokenCipher, TelegramChatIDCipher: chatCipher})
	if !outcome.retryable || outcome.retryAfter != time.Hour || outcome.code != "rate_limited" {
		t.Fatalf("outcome=%#v", outcome)
	}
	mu.Lock()
	path := seenPath
	mu.Unlock()
	if path != "/bot"+token+"/sendMessage" {
		t.Fatalf("Telegram path=%q", path)
	}
	if outcome.code == token {
		t.Fatal("Telegram token leaked through delivery outcome")
	}
	if got := parseRetryAfter("9223372036854775807", nil, true); got != time.Hour {
		t.Fatalf("overflowing Retry-After=%v; want one-hour bound", got)
	}
}

func TestAlertEventIDsDeduplicateRetriesAndSeparateGenerations(t *testing.T) {
	revision := time.Unix(1_700_000_000, 0).UTC()
	first := alertEventID("rule", "node", revision, 1, "firing", 0)
	if first != alertEventID("rule", "node", revision, 1, "firing", 0) {
		t.Fatal("same transition did not produce stable event ID")
	}
	if first == alertEventID("rule", "node", revision, 2, "firing", 0) ||
		first == alertEventID("rule", "node", revision, 1, "resolved", 0) ||
		first == alertEventID("rule", "node", revision.Add(time.Second), 1, "firing", 0) {
		t.Fatal("distinct transitions reused an event ID")
	}
}

func TestRuleEditUsesNewRevisionAndDoesNotDeduplicateNextFiring(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Unix(1_700_000_000, 0).UTC()
	token := controlstore.EnrollmentToken{ID: "token", Digest: security.DigestEnrollmentToken("token"), NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	node, err := store.ConsumeEnrollment(t.Context(), token.Digest, "node", "credential", security.DigestNodeCredential("credential"), now)
	if err != nil {
		t.Fatal(err)
	}
	channel := controlstore.NotificationChannel{ID: "channel", Name: "primary", Kind: "webhook", Enabled: true,
		WebhookURLCipher: []byte("cipher-url"), WebhookSecretCipher: []byte("cipher-secret"), CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
	rule := controlstore.AlertRule{ID: "rule", Name: "CPU", ScopeKind: "node", ScopeValue: node.ID, Metric: "cpu", Threshold: 80,
		Enabled: true, ChannelIDs: []string{channel.ID}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	state, eventKind, changed := advanceAlertState(rule, controlstore.AlertState{RuleID: rule.ID, NodeID: node.ID}, true, false, now)
	if !changed || eventKind != "firing" {
		t.Fatalf("first transition state=%#v event=%q changed=%v", state, eventKind, changed)
	}
	first, err := buildAlertDeliveries(rule, node, state, eventKind, 90, now)
	if err != nil || len(first) != 1 {
		t.Fatalf("first deliveries=%#v err=%v", first, err)
	}
	if err := store.ApplyAlertState(t.Context(), state, first); err != nil {
		t.Fatal(err)
	}
	completed := now.Add(time.Second)
	if err := store.CompleteNotificationDelivery(t.Context(), first[0].ID, "delivered", "", 1, completed, &completed); err != nil {
		t.Fatal(err)
	}

	// Deliberately reuse the same caller timestamp. Storage must still advance
	// the durable revision because SQLite persists timestamps to seconds.
	rule.Threshold = 85
	if err := store.UpdateAlertRule(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	rule, err = store.AlertRuleByID(t.Context(), rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rule.UpdatedAt.After(now) {
		t.Fatalf("edited rule revision=%v; want after %v", rule.UpdatedAt, now)
	}
	state, eventKind, changed = advanceAlertState(rule, controlstore.AlertState{RuleID: rule.ID, NodeID: node.ID}, true, false, now.Add(2*time.Second))
	if !changed || eventKind != "firing" {
		t.Fatalf("edited transition state=%#v event=%q changed=%v", state, eventKind, changed)
	}
	second, err := buildAlertDeliveries(rule, node, state, eventKind, 90, now.Add(2*time.Second))
	if err != nil || len(second) != 1 {
		t.Fatalf("second deliveries=%#v err=%v", second, err)
	}
	if second[0].EventID == first[0].EventID || second[0].ID == first[0].ID {
		t.Fatalf("edited rule reused completed delivery identity: first=%s second=%s", first[0].EventID, second[0].EventID)
	}
	if err := store.ApplyAlertState(t.Context(), state, second); err != nil {
		t.Fatal(err)
	}
	stored, err := store.NotificationDeliveryByID(t.Context(), second[0].ID)
	if err != nil || stored.Status != "pending" {
		t.Fatalf("new firing delivery=%#v err=%v", stored, err)
	}
}

func TestNotificationRetryAfterAndShutdownRemainDurable(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	urlCipher, _ := key.Encrypt([]byte("https://hooks.example/notify"))
	secretCipher, _ := key.Encrypt([]byte("secret"))
	channel := controlstore.NotificationChannel{ID: "channel", Name: "webhook", Kind: "webhook", Enabled: true,
		WebhookURLCipher: urlCipher, WebhookSecretCipher: secretCipher, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateNotificationChannel(t.Context(), channel); err != nil {
		t.Fatal(err)
	}
	delivery := controlstore.NotificationDelivery{ID: "retry-after", EventID: "retry-after", ChannelID: channel.ID,
		EventKind: "test", EventPayload: []byte(`{"kind":"test"}`), NextAttemptAt: now, CreatedAt: now}
	if err := store.CreateNotificationDelivery(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	dispatcher := &NotificationDispatcher{ctx: t.Context(), store: store,
		config: NotificationDispatcherConfig{Now: func() time.Time { return now }, MaxAttempts: 5, MaxBackoff: time.Minute, Jitter: func(time.Duration) time.Duration { return 0 }}.withDefaults()}
	dispatcher.finish(delivery, deliveryOutcome{retryable: true, retryAfter: time.Hour, code: "rate_limited"})
	stored, err := store.NotificationDeliveryByID(t.Context(), delivery.ID)
	if err != nil || stored.Status != "retry" || !stored.NextAttemptAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("Retry-After delivery=%#v err=%v", stored, err)
	}

	shutdownDelivery := controlstore.NotificationDelivery{ID: "shutdown", EventID: "shutdown", ChannelID: channel.ID,
		EventKind: "test", EventPayload: []byte(`{"kind":"test"}`), NextAttemptAt: now, CreatedAt: now}
	if err := store.CreateNotificationDelivery(t.Context(), shutdownDelivery); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	client := &http.Client{Timeout: time.Second, Transport: notificationRoundTripper(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	running, err := NewNotificationDispatcher(ctx, store, key, NotificationDispatcherConfig{HTTPClient: client, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := running.Submit(shutdownDelivery.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shutdown delivery did not start")
	}
	cancel()
	running.Wait()
	stored, err = store.NotificationDeliveryByID(t.Context(), shutdownDelivery.ID)
	if err != nil || stored.Status != "pending" || stored.Attempts != 0 {
		t.Fatalf("shutdown delivery was not left retryable: %#v err=%v", stored, err)
	}
	if got := (NotificationDispatcherConfig{QueueSize: maxNotificationQueueSize + 1}).withDefaults().QueueSize; got != maxNotificationQueueSize {
		t.Fatalf("notification queue bound=%d; want %d", got, maxNotificationQueueSize)
	}
}

func TestAlertManagerRunsWithoutTelemetryAndUnavailableBreaksPendingDuration(t *testing.T) {
	directory := t.TempDir()
	store, err := controlstore.Open(t.Context(), filepath.Join(directory, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Unix(1_700_000_000, 0).UTC()
	token := controlstore.EnrollmentToken{ID: "token", Digest: security.DigestEnrollmentToken("token"), NodeName: "node", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "node", "credential", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatal(err)
	}
	rule := controlstore.AlertRule{ID: "cpu", Name: "CPU", ScopeKind: "node", ScopeValue: "node", Metric: "cpu",
		Threshold: 80, DurationSeconds: 10, RecoveryHysteresis: 5, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateAlertRule(t.Context(), rule); err != nil {
		t.Fatal(err)
	}
	live := NewLiveStore()
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := NewAlertManager(ctx, store, live, nil, AlertManagerConfig{TickInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); manager.Wait() }()
	report := protocol.Report{CollectedAtUnixMilli: now.UnixMilli(), CPU: protocol.CPUStats{UsagePercent: 90}}
	if err := manager.Observe("node", LiveSnapshot{Connected: true, Online: true, Report: &report}, now); err != nil {
		t.Fatal(err)
	}
	waitForAlertStatus(t, store, rule.ID, "node", "pending")
	report.Unavailable = []protocol.MetricArea{protocol.AreaCPU}
	if err := manager.Observe("node", LiveSnapshot{Connected: true, Online: true, Report: &report}, now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	waitForAlertStatus(t, store, rule.ID, "node", "normal")
}

func waitForAlertStatus(t *testing.T, store *controlstore.Store, ruleID, nodeID, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := store.AlertState(t.Context(), ruleID, nodeID)
		if err == nil && state.Status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	state, err := store.AlertState(t.Context(), ruleID, nodeID)
	t.Fatalf("alert state=%#v err=%v; want %s", state, err, want)
}
