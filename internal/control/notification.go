package control

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

const (
	maxNotificationResponseBytes = 16 << 10
	maxNotificationQueueSize     = 256
	maxTelegramRetryAfter        = time.Hour
)

var ErrNotificationQueueFull = errors.New("notification queue is full")

// NotificationDispatcherConfig bounds outbound notification work.
type NotificationDispatcherConfig struct {
	QueueSize       int
	Workers         int
	HTTPClient      *http.Client
	TelegramBaseURL string
	MaxAttempts     uint8
	BaseBackoff     time.Duration
	MaxBackoff      time.Duration
	PollInterval    time.Duration
	Now             func() time.Time
	Jitter          func(time.Duration) time.Duration
}

func (config NotificationDispatcherConfig) withDefaults() NotificationDispatcherConfig {
	if config.QueueSize <= 0 {
		config.QueueSize = 128
	} else if config.QueueSize > maxNotificationQueueSize {
		config.QueueSize = maxNotificationQueueSize
	}
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.Workers > 16 {
		config.Workers = 16
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	} else {
		copy := *config.HTTPClient
		if copy.Timeout <= 0 || copy.Timeout > 30*time.Second {
			copy.Timeout = 10 * time.Second
		}
		config.HTTPClient = &copy
	}
	// Redirects can disclose a secret-bearing Telegram path or signed webhook
	// body to a second origin. Treat 3xx as a bounded delivery failure instead.
	config.HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if config.TelegramBaseURL == "" {
		config.TelegramBaseURL = "https://api.telegram.org"
	}
	config.TelegramBaseURL = strings.TrimRight(config.TelegramBaseURL, "/")
	if config.MaxAttempts == 0 || config.MaxAttempts > 10 {
		config.MaxAttempts = 5
	}
	if config.BaseBackoff <= 0 {
		config.BaseBackoff = time.Second
	}
	if config.MaxBackoff <= 0 || config.MaxBackoff > maxTelegramRetryAfter {
		config.MaxBackoff = time.Minute
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Jitter == nil {
		config.Jitter = boundedJitter
	}
	return config
}

// NotificationDispatcher owns a bounded queue and finite worker set. Durable
// transition work remains in control.db when the queue is saturated or the
// process exits.
type NotificationDispatcher struct {
	ctx       context.Context
	store     *controlstore.Store
	masterKey *security.MasterKey
	config    NotificationDispatcherConfig
	queue     chan string
	mu        sync.Mutex
	inFlight  map[string]struct{}
	wg        sync.WaitGroup
}

// NewNotificationDispatcher starts bounded delivery workers and an outbox poller.
func NewNotificationDispatcher(ctx context.Context, store *controlstore.Store, masterKey *security.MasterKey, config NotificationDispatcherConfig) (*NotificationDispatcher, error) {
	if ctx == nil || store == nil || masterKey == nil {
		return nil, errors.New("notification dispatcher dependencies are required")
	}
	config = config.withDefaults()
	dispatcher := &NotificationDispatcher{ctx: ctx, store: store, masterKey: masterKey, config: config,
		queue: make(chan string, config.QueueSize), inFlight: make(map[string]struct{})}
	for index := 0; index < config.Workers; index++ {
		dispatcher.wg.Add(1)
		go dispatcher.worker()
	}
	dispatcher.wg.Add(1)
	go dispatcher.poll()
	return dispatcher, nil
}

// Submit non-blockingly admits one durable delivery ID.
func (dispatcher *NotificationDispatcher) Submit(id string) error {
	if dispatcher == nil || id == "" {
		return errors.New("invalid notification delivery")
	}
	dispatcher.mu.Lock()
	if _, exists := dispatcher.inFlight[id]; exists {
		dispatcher.mu.Unlock()
		return nil
	}
	dispatcher.inFlight[id] = struct{}{}
	dispatcher.mu.Unlock()
	select {
	case <-dispatcher.ctx.Done():
		dispatcher.release(id)
		return dispatcher.ctx.Err()
	case dispatcher.queue <- id:
		return nil
	default:
		dispatcher.release(id)
		return ErrNotificationQueueFull
	}
}

// Wait waits for finite workers to exit after the owning context is cancelled.
func (dispatcher *NotificationDispatcher) Wait() { dispatcher.wg.Wait() }

func (dispatcher *NotificationDispatcher) poll() {
	defer dispatcher.wg.Done()
	dispatcher.enqueueDue()
	ticker := time.NewTicker(dispatcher.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-dispatcher.ctx.Done():
			return
		case <-ticker.C:
			dispatcher.enqueueDue()
		}
	}
}

func (dispatcher *NotificationDispatcher) enqueueDue() {
	due, err := dispatcher.store.ListDueNotificationDeliveries(dispatcher.ctx, dispatcher.config.Now(), dispatcher.config.QueueSize)
	if err != nil {
		return
	}
	for _, delivery := range due {
		if err := dispatcher.Submit(delivery.ID); errors.Is(err, ErrNotificationQueueFull) || errors.Is(err, context.Canceled) {
			return
		}
	}
}

func (dispatcher *NotificationDispatcher) worker() {
	defer dispatcher.wg.Done()
	for {
		select {
		case <-dispatcher.ctx.Done():
			return
		case id := <-dispatcher.queue:
			dispatcher.deliver(id)
			dispatcher.release(id)
		}
	}
}

func (dispatcher *NotificationDispatcher) release(id string) {
	dispatcher.mu.Lock()
	delete(dispatcher.inFlight, id)
	dispatcher.mu.Unlock()
}

type deliveryOutcome struct {
	retryable  bool
	retryAfter time.Duration
	code       string
}

func (dispatcher *NotificationDispatcher) deliver(id string) {
	delivery, err := dispatcher.store.NotificationDeliveryByID(dispatcher.ctx, id)
	if err != nil || delivery.Status == "delivered" || delivery.Status == "terminal" || delivery.NextAttemptAt.After(dispatcher.config.Now()) {
		return
	}
	channel, err := dispatcher.store.NotificationChannelByID(dispatcher.ctx, delivery.ChannelID)
	if err != nil || !channel.Enabled {
		dispatcher.finish(delivery, deliveryOutcome{code: "channel_unavailable"})
		return
	}
	var outcome deliveryOutcome
	switch channel.Kind {
	case "webhook":
		outcome = dispatcher.sendWebhook(delivery, channel)
	case "telegram":
		outcome = dispatcher.sendTelegram(delivery, channel)
	default:
		outcome = deliveryOutcome{code: "channel_invalid"}
	}
	dispatcher.finish(delivery, outcome)
}

func (dispatcher *NotificationDispatcher) finish(delivery controlstore.NotificationDelivery, outcome deliveryOutcome) {
	now := dispatcher.config.Now().UTC()
	attempts := delivery.Attempts + 1
	if outcome.code == "" {
		_ = dispatcher.store.CompleteNotificationDelivery(dispatcher.ctx, delivery.ID, "delivered", "", attempts, now, &now)
		return
	}
	if !outcome.retryable || attempts >= dispatcher.config.MaxAttempts {
		_ = dispatcher.store.CompleteNotificationDelivery(dispatcher.ctx, delivery.ID, "terminal", outcome.code, attempts, now, &now)
		return
	}
	delay := outcome.retryAfter
	if delay > 0 {
		if delay > maxTelegramRetryAfter {
			delay = maxTelegramRetryAfter
		}
	} else {
		delay = dispatcher.config.BaseBackoff
		for index := uint8(1); index < attempts && delay < dispatcher.config.MaxBackoff; index++ {
			delay *= 2
		}
		if delay > dispatcher.config.MaxBackoff {
			delay = dispatcher.config.MaxBackoff
		}
		delay += dispatcher.config.Jitter(delay)
		if delay > dispatcher.config.MaxBackoff {
			delay = dispatcher.config.MaxBackoff
		}
	}
	_ = dispatcher.store.CompleteNotificationDelivery(dispatcher.ctx, delivery.ID, "retry", outcome.code, attempts, now.Add(delay), nil)
}

func (dispatcher *NotificationDispatcher) sendWebhook(delivery controlstore.NotificationDelivery, channel controlstore.NotificationChannel) deliveryOutcome {
	rawURL, err := dispatcher.masterKey.Decrypt(channel.WebhookURLCipher)
	if err != nil {
		return deliveryOutcome{code: "secret_invalid"}
	}
	defer clear(rawURL)
	secret, err := dispatcher.masterKey.Decrypt(channel.WebhookSecretCipher)
	if err != nil {
		return deliveryOutcome{code: "secret_invalid"}
	}
	defer clear(secret)
	if validateWebhookURL(string(rawURL)) != nil {
		return deliveryOutcome{code: "url_invalid"}
	}
	timestamp := strconv.FormatInt(dispatcher.config.Now().UTC().Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(delivery.EventPayload)
	request, err := http.NewRequestWithContext(dispatcher.ctx, http.MethodPost, string(rawURL), bytes.NewReader(delivery.EventPayload))
	if err != nil {
		return deliveryOutcome{code: "request_invalid"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-VPS-Status-Event-ID", delivery.EventID)
	request.Header.Set("X-VPS-Status-Timestamp", timestamp)
	request.Header.Set("X-VPS-Status-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return dispatcher.doRequest(request, false)
}

func (dispatcher *NotificationDispatcher) sendTelegram(delivery controlstore.NotificationDelivery, channel controlstore.NotificationChannel) deliveryOutcome {
	token, err := dispatcher.masterKey.Decrypt(channel.TelegramTokenCipher)
	if err != nil {
		return deliveryOutcome{code: "secret_invalid"}
	}
	defer clear(token)
	chatID, err := dispatcher.masterKey.Decrypt(channel.TelegramChatIDCipher)
	if err != nil {
		return deliveryOutcome{code: "secret_invalid"}
	}
	defer clear(chatID)
	if !validTelegramToken(string(token)) || !validTelegramChatID(string(chatID)) {
		return deliveryOutcome{code: "secret_invalid"}
	}
	message := telegramMessage(delivery.EventPayload)
	body, err := json.Marshal(map[string]any{"chat_id": string(chatID), "text": message, "disable_web_page_preview": true})
	if err != nil {
		return deliveryOutcome{code: "request_invalid"}
	}
	defer clear(body)
	endpoint := dispatcher.config.TelegramBaseURL + "/bot" + string(token) + "/sendMessage"
	request, err := http.NewRequestWithContext(dispatcher.ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return deliveryOutcome{code: "request_invalid"}
	}
	request.Header.Set("Content-Type", "application/json")
	return dispatcher.doRequest(request, true)
}

func telegramMessage(payload []byte) string {
	var event struct {
		Kind     string  `json:"kind"`
		RuleName string  `json:"rule_name"`
		NodeName string  `json:"node_name"`
		Metric   string  `json:"metric"`
		Value    float64 `json:"value"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return "VPS Status notification"
	}
	message := fmt.Sprintf("VPS Status: %s\nRule: %s\nNode: %s\nMetric: %s\nValue: %.2f", event.Kind, event.RuleName, event.NodeName, event.Metric, event.Value)
	if len(message) > 4096 {
		message = message[:4096]
	}
	return message
}

func (dispatcher *NotificationDispatcher) doRequest(request *http.Request, telegram bool) deliveryOutcome {
	response, err := dispatcher.config.HTTPClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) && dispatcher.ctx.Err() != nil {
			return deliveryOutcome{code: "shutdown"}
		}
		return deliveryOutcome{retryable: true, code: "network"}
	}
	defer response.Body.Close()
	limited, readErr := io.ReadAll(io.LimitReader(response.Body, maxNotificationResponseBytes+1))
	if readErr != nil {
		return deliveryOutcome{retryable: true, code: "response_read"}
	}
	if len(limited) > maxNotificationResponseBytes {
		return deliveryOutcome{retryable: true, code: "response_too_large"}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return deliveryOutcome{}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return deliveryOutcome{retryable: true, retryAfter: parseRetryAfter(response.Header.Get("Retry-After"), limited, telegram), code: "rate_limited"}
	}
	if response.StatusCode >= 500 {
		return deliveryOutcome{retryable: true, code: "server_error"}
	}
	if response.StatusCode >= 400 {
		return deliveryOutcome{code: "client_error"}
	}
	return deliveryOutcome{retryable: true, code: "unexpected_status"}
}

func parseRetryAfter(header string, body []byte, telegram bool) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64)
	if err != nil && telegram {
		var response struct {
			Parameters struct {
				RetryAfter int64 `json:"retry_after"`
			} `json:"parameters"`
		}
		if json.Unmarshal(body, &response) == nil {
			seconds = response.Parameters.RetryAfter
		}
	}
	if seconds < 1 {
		return 0
	}
	if seconds >= int64(maxTelegramRetryAfter/time.Second) {
		return maxTelegramRetryAfter
	}
	delay := time.Duration(seconds) * time.Second
	return delay
}

func validateWebhookURL(raw string) error {
	if len(raw) < 1 || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n") {
		return errors.New("invalid webhook URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("invalid webhook URL")
	}
	return nil
}

func validTelegramToken(value string) bool {
	if len(value) < 8 || len(value) > 256 || strings.ContainsAny(value, "/?#\r\n ") {
		return false
	}
	return strings.Contains(value, ":")
}

func validTelegramChatID(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\r\n")
}

func boundedJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	var value [1]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0
	}
	return time.Duration(value[0]) * (base / 4) / 255
}
