package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
)

type notificationChannelDTO struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Enabled    bool      `json:"enabled"`
	Configured bool      `json:"configured"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func channelDTO(channel controlstore.NotificationChannel) notificationChannelDTO {
	configured := channel.Kind == "webhook" && len(channel.WebhookURLCipher) > 0 && len(channel.WebhookSecretCipher) > 0 ||
		channel.Kind == "telegram" && len(channel.TelegramTokenCipher) > 0 && len(channel.TelegramChatIDCipher) > 0
	return notificationChannelDTO{ID: channel.ID, Name: channel.Name, Kind: channel.Kind, Enabled: channel.Enabled,
		Configured: configured, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt}
}

type notificationChannelRequest struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Enabled  *bool   `json:"enabled"`
	URL      *string `json:"url"`
	Secret   *string `json:"secret"`
	BotToken *string `json:"bot_token"`
	ChatID   *string `json:"chat_id"`
}

func (api *adminAPI) notificationChannels(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	channels, err := api.config.Store.ListNotificationChannels(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	result := make([]notificationChannelDTO, 0, len(channels))
	for _, channel := range channels {
		result = append(result, channelDTO(channel))
	}
	writeJSON(response, http.StatusOK, map[string]any{"channels": result})
}

func (api *adminAPI) notificationChannel(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	channel, err := api.config.Store.NotificationChannelByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The notification channel was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, channelDTO(channel))
}

func (api *adminAPI) createNotificationChannel(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input notificationChannelRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.Name == nil || input.Kind == nil {
		writeError(response, http.StatusBadRequest, "invalid_input", "The notification channel configuration is invalid.")
		return
	}
	id, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	now := api.now()
	channel := controlstore.NotificationChannel{ID: id, Name: strings.TrimSpace(*input.Name), Kind: strings.TrimSpace(*input.Kind), Enabled: true, CreatedAt: now, UpdatedAt: now}
	if input.Enabled != nil {
		channel.Enabled = *input.Enabled
	}
	if !api.encryptChannelInput(&channel, input, true) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The notification channel configuration is invalid.")
		return
	}
	if err := api.config.Store.CreateNotificationChannel(request.Context(), channel); errors.Is(err, controlstore.ErrNotificationChannelLimit) {
		writeError(response, http.StatusConflict, "notification_channel_limit", "The notification channel limit has been reached.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusCreated, channelDTO(channel))
}

func (api *adminAPI) updateNotificationChannel(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	current, err := api.config.Store.NotificationChannelByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The notification channel was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	var input notificationChannelRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	kindChanged := input.Kind != nil && strings.TrimSpace(*input.Kind) != current.Kind
	if input.Kind != nil {
		current.Kind = strings.TrimSpace(*input.Kind)
	}
	if kindChanged {
		current.WebhookURLCipher, current.WebhookSecretCipher = nil, nil
		current.TelegramTokenCipher, current.TelegramChatIDCipher = nil, nil
	}
	current.UpdatedAt = api.now()
	if !api.encryptChannelInput(&current, input, kindChanged) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The notification channel configuration is invalid.")
		return
	}
	if err := api.config.Store.UpdateNotificationChannel(request.Context(), current); err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, channelDTO(current))
}

func (api *adminAPI) encryptChannelInput(channel *controlstore.NotificationChannel, input notificationChannelRequest, requireSecrets bool) bool {
	if channel.Name == "" || len(channel.Name) > 128 {
		return false
	}
	encrypt := func(value *string, maximum int) ([]byte, bool) {
		if value == nil || *value == "" || len(*value) > maximum {
			return nil, false
		}
		plain := []byte(*value)
		defer clear(plain)
		ciphertext, err := api.config.MasterKey.Encrypt(plain)
		return ciphertext, err == nil
	}
	switch channel.Kind {
	case "webhook":
		if input.URL != nil {
			trimmed := strings.TrimSpace(*input.URL)
			if validateWebhookURL(trimmed) != nil {
				return false
			}
			input.URL = &trimmed
			var ok bool
			channel.WebhookURLCipher, ok = encrypt(input.URL, 2048)
			if !ok {
				return false
			}
		}
		if input.Secret != nil {
			var ok bool
			channel.WebhookSecretCipher, ok = encrypt(input.Secret, 4096)
			if !ok {
				return false
			}
		}
		if input.BotToken != nil || input.ChatID != nil || requireSecrets && (input.URL == nil || input.Secret == nil) {
			return false
		}
	case "telegram":
		if input.BotToken != nil {
			if !validTelegramToken(*input.BotToken) {
				return false
			}
			var ok bool
			channel.TelegramTokenCipher, ok = encrypt(input.BotToken, 256)
			if !ok {
				return false
			}
		}
		if input.ChatID != nil {
			if !validTelegramChatID(*input.ChatID) {
				return false
			}
			var ok bool
			channel.TelegramChatIDCipher, ok = encrypt(input.ChatID, 256)
			if !ok {
				return false
			}
		}
		if input.URL != nil || input.Secret != nil || requireSecrets && (input.BotToken == nil || input.ChatID == nil) {
			return false
		}
	default:
		return false
	}
	return true
}

func (api *adminAPI) deleteNotificationChannel(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if err := api.config.Store.DeleteNotificationChannel(request.Context(), request.PathValue("id")); errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The notification channel was not found.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (api *adminAPI) testNotificationChannel(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	channel, err := api.config.Store.NotificationChannelByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The notification channel was not found.")
		return
	}
	if err != nil || !channel.Enabled {
		writeError(response, http.StatusConflict, "channel_unavailable", "The notification channel is not enabled.")
		return
	}
	eventID, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	now := api.now()
	event := AlertEvent{EventID: eventID, Timestamp: now, Kind: "test", State: "test", RuleName: "Test notification", NodeName: "Control", Metric: "test"}
	payload, err := json.Marshal(event)
	if err != nil {
		writeInternal(response)
		return
	}
	delivery := controlstore.NotificationDelivery{ID: deliveryID(eventID, channel.ID), EventID: eventID, ChannelID: channel.ID,
		EventKind: "test", EventPayload: payload, Status: "pending", NextAttemptAt: now, CreatedAt: now}
	if err := api.config.Store.CreateNotificationDelivery(request.Context(), delivery); errors.Is(err, controlstore.ErrDeliveryLimit) {
		writeError(response, http.StatusServiceUnavailable, "queue_full", "The notification queue is full.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	if err := api.config.Notifications.Submit(delivery.ID); errors.Is(err, ErrNotificationQueueFull) {
		_ = api.config.Store.DeleteNotificationDelivery(request.Context(), delivery.ID)
		writeError(response, http.StatusServiceUnavailable, "queue_full", "The notification queue is full.")
		return
	} else if err != nil {
		_ = api.config.Store.DeleteNotificationDelivery(request.Context(), delivery.ID)
		writeError(response, http.StatusServiceUnavailable, "dispatcher_unavailable", "The notification dispatcher is unavailable.")
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]string{"status": "accepted", "event_id": eventID})
}

type alertRuleDTO struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	ScopeKind               string    `json:"scope_kind"`
	ScopeValue              string    `json:"scope_value"`
	Metric                  string    `json:"metric"`
	Threshold               float64   `json:"threshold"`
	DurationSeconds         uint32    `json:"duration_seconds"`
	RecoveryHysteresis      float64   `json:"recovery_hysteresis"`
	Enabled                 bool      `json:"enabled"`
	ReminderIntervalSeconds uint32    `json:"reminder_interval_seconds"`
	ReminderMaxCount        uint8     `json:"reminder_max_count"`
	ChannelIDs              []string  `json:"channel_ids"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func ruleDTO(rule controlstore.AlertRule) alertRuleDTO {
	return alertRuleDTO{ID: rule.ID, Name: rule.Name, ScopeKind: rule.ScopeKind, ScopeValue: rule.ScopeValue,
		Metric: rule.Metric, Threshold: rule.Threshold, DurationSeconds: rule.DurationSeconds,
		RecoveryHysteresis: rule.RecoveryHysteresis, Enabled: rule.Enabled,
		ReminderIntervalSeconds: rule.ReminderIntervalSeconds, ReminderMaxCount: rule.ReminderMaxCount,
		ChannelIDs: append([]string{}, rule.ChannelIDs...), CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt}
}

type alertRuleRequest struct {
	Name                    *string   `json:"name"`
	ScopeKind               *string   `json:"scope_kind"`
	ScopeValue              *string   `json:"scope_value"`
	Metric                  *string   `json:"metric"`
	Threshold               *float64  `json:"threshold"`
	DurationSeconds         *uint32   `json:"duration_seconds"`
	RecoveryHysteresis      *float64  `json:"recovery_hysteresis"`
	Enabled                 *bool     `json:"enabled"`
	ReminderIntervalSeconds *uint32   `json:"reminder_interval_seconds"`
	ReminderMaxCount        *uint8    `json:"reminder_max_count"`
	ChannelIDs              *[]string `json:"channel_ids"`
}

func (api *adminAPI) alertRules(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	rules, err := api.config.Store.ListAlertRules(request.Context())
	if err != nil {
		writeInternal(response)
		return
	}
	result := make([]alertRuleDTO, 0, len(rules))
	for _, rule := range rules {
		result = append(result, ruleDTO(rule))
	}
	writeJSON(response, http.StatusOK, map[string]any{"rules": result})
}

func (api *adminAPI) alertRule(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	rule, err := api.config.Store.AlertRuleByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The alert rule was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	writeJSON(response, http.StatusOK, ruleDTO(rule))
}

func applyAlertRuleInput(rule *controlstore.AlertRule, input alertRuleRequest, create bool) bool {
	if input.Name != nil {
		rule.Name = strings.TrimSpace(*input.Name)
	}
	if input.ScopeKind != nil {
		rule.ScopeKind = strings.TrimSpace(*input.ScopeKind)
	}
	if input.ScopeValue != nil {
		rule.ScopeValue = strings.TrimSpace(*input.ScopeValue)
	}
	if input.Metric != nil {
		rule.Metric = strings.TrimSpace(*input.Metric)
	}
	if input.Threshold != nil {
		rule.Threshold = *input.Threshold
	}
	if input.DurationSeconds != nil {
		rule.DurationSeconds = *input.DurationSeconds
	}
	if input.RecoveryHysteresis != nil {
		rule.RecoveryHysteresis = *input.RecoveryHysteresis
	}
	if input.Enabled != nil {
		rule.Enabled = *input.Enabled
	}
	if input.ReminderIntervalSeconds != nil {
		rule.ReminderIntervalSeconds = *input.ReminderIntervalSeconds
	}
	if input.ReminderMaxCount != nil {
		rule.ReminderMaxCount = *input.ReminderMaxCount
	}
	if input.ChannelIDs != nil {
		rule.ChannelIDs = append([]string(nil), (*input.ChannelIDs)...)
	}
	if create {
		return input.Name != nil && input.ScopeKind != nil && input.ScopeValue != nil && input.Metric != nil && input.ChannelIDs != nil
	}
	return true
}

func (api *adminAPI) createAlertRule(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	var input alertRuleRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	id, err := security.RandomToken()
	if err != nil {
		writeInternal(response)
		return
	}
	now := api.now()
	rule := controlstore.AlertRule{ID: id, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if !applyAlertRuleInput(&rule, input, true) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The alert rule configuration is invalid.")
		return
	}
	if err := api.config.Store.CreateAlertRule(request.Context(), rule); err != nil {
		api.writeAlertRuleError(response, err)
		return
	}
	api.config.Alerts.Refresh()
	writeJSON(response, http.StatusCreated, ruleDTO(rule))
}

func (api *adminAPI) updateAlertRule(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	rule, err := api.config.Store.AlertRuleByID(request.Context(), request.PathValue("id"))
	if errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The alert rule was not found.")
		return
	}
	if err != nil {
		writeInternal(response)
		return
	}
	var input alertRuleRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if !applyAlertRuleInput(&rule, input, false) {
		writeError(response, http.StatusBadRequest, "invalid_input", "The alert rule configuration is invalid.")
		return
	}
	rule.UpdatedAt = api.now()
	if err := api.config.Store.UpdateAlertRule(request.Context(), rule); err != nil {
		api.writeAlertRuleError(response, err)
		return
	}
	// Storage makes updated_at a monotonic durable rule revision, including
	// when two edits arrive within one wall-clock second. Return that persisted
	// value because it also participates in stable notification event IDs.
	rule, err = api.config.Store.AlertRuleByID(request.Context(), rule.ID)
	if err != nil {
		writeInternal(response)
		return
	}
	api.config.Alerts.Refresh()
	writeJSON(response, http.StatusOK, ruleDTO(rule))
}

func (api *adminAPI) writeAlertRuleError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, controlstore.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The alert scope or channel was not found.")
	case errors.Is(err, controlstore.ErrAlertRuleLimit):
		writeError(response, http.StatusConflict, "alert_rule_limit", "The alert rule limit has been reached.")
	case errors.Is(err, controlstore.ErrInvalidAlertConfiguration):
		writeError(response, http.StatusBadRequest, "invalid_input", "The alert rule configuration is invalid.")
	default:
		writeInternal(response)
	}
}

func (api *adminAPI) deleteAlertRule(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if err := api.config.Store.DeleteAlertRule(request.Context(), request.PathValue("id")); errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The alert rule was not found.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

type alertStateDTO struct {
	RuleID         string     `json:"rule_id"`
	NodeID         string     `json:"node_id"`
	Status         string     `json:"status"`
	Generation     uint64     `json:"generation"`
	PendingSince   *time.Time `json:"pending_since,omitempty"`
	FiringSince    *time.Time `json:"firing_since,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	LastReminderAt *time.Time `json:"last_reminder_at,omitempty"`
	ReminderCount  uint8      `json:"reminder_count"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (api *adminAPI) alertRuleStates(response http.ResponseWriter, request *http.Request, _ authenticatedRequest) {
	if _, err := api.config.Store.AlertRuleByID(request.Context(), request.PathValue("id")); errors.Is(err, controlstore.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The alert rule was not found.")
		return
	} else if err != nil {
		writeInternal(response)
		return
	}
	states, err := api.config.Store.ListAlertStates(request.Context(), request.PathValue("id"))
	if err != nil {
		writeInternal(response)
		return
	}
	result := make([]alertStateDTO, 0, len(states))
	for _, state := range states {
		result = append(result, alertStateDTO{RuleID: state.RuleID, NodeID: state.NodeID, Status: state.Status,
			Generation: state.Generation, PendingSince: state.PendingSince, FiringSince: state.FiringSince,
			ResolvedAt: state.ResolvedAt, LastReminderAt: state.LastReminderAt, ReminderCount: state.ReminderCount, UpdatedAt: state.UpdatedAt})
	}
	writeJSON(response, http.StatusOK, map[string]any{"states": result})
}
