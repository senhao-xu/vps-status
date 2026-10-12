package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const maxEncryptedChannelField = 8192

// CreateNotificationChannel inserts one bounded encrypted channel.
func (s *Store) CreateNotificationChannel(ctx context.Context, channel NotificationChannel) error {
	if err := validateNotificationChannel(channel); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notification channel creation: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_channels`).Scan(&count); err != nil {
		return fmt.Errorf("count notification channels: %w", err)
	}
	if count >= MaxNotificationChannels {
		return ErrNotificationChannelLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_channels(
		id,name,kind,enabled,webhook_url_cipher,webhook_secret_cipher,telegram_token_cipher,telegram_chat_id_cipher,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, channel.ID, channel.Name, channel.Kind, boolInt(channel.Enabled),
		nullBytes(channel.WebhookURLCipher), nullBytes(channel.WebhookSecretCipher), nullBytes(channel.TelegramTokenCipher),
		nullBytes(channel.TelegramChatIDCipher), unix(channel.CreatedAt), unix(channel.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create notification channel: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification channel creation: %w", err)
	}
	return nil
}

// UpdateNotificationChannel replaces one encrypted channel configuration.
func (s *Store) UpdateNotificationChannel(ctx context.Context, channel NotificationChannel) error {
	if err := validateNotificationChannel(channel); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE notification_channels SET
		name=?,kind=?,enabled=?,webhook_url_cipher=?,webhook_secret_cipher=?,telegram_token_cipher=?,telegram_chat_id_cipher=?,updated_at=?
		WHERE id=?`, channel.Name, channel.Kind, boolInt(channel.Enabled), nullBytes(channel.WebhookURLCipher),
		nullBytes(channel.WebhookSecretCipher), nullBytes(channel.TelegramTokenCipher), nullBytes(channel.TelegramChatIDCipher),
		unix(channel.UpdatedAt), channel.ID)
	if err != nil {
		return fmt.Errorf("update notification channel: %w", err)
	}
	return requireOneRow(result, "update notification channel")
}

// NotificationChannelByID returns one internal encrypted channel.
func (s *Store) NotificationChannelByID(ctx context.Context, id string) (NotificationChannel, error) {
	channel, err := scanNotificationChannel(s.db.QueryRowContext(ctx, `SELECT id,name,kind,enabled,
		webhook_url_cipher,webhook_secret_cipher,telegram_token_cipher,telegram_chat_id_cipher,created_at,updated_at
		FROM notification_channels WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return NotificationChannel{}, ErrNotFound
	}
	if err != nil {
		return NotificationChannel{}, fmt.Errorf("read notification channel: %w", err)
	}
	return channel, nil
}

// ListNotificationChannels returns every channel without decrypting secrets.
func (s *Store) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,enabled,
		webhook_url_cipher,webhook_secret_cipher,telegram_token_cipher,telegram_chat_id_cipher,created_at,updated_at
		FROM notification_channels ORDER BY name,id`)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer rows.Close()
	result := make([]NotificationChannel, 0)
	for rows.Next() {
		channel, scanErr := scanNotificationChannel(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan notification channel: %w", scanErr)
		}
		result = append(result, channel)
	}
	return result, rows.Err()
}

// DeleteNotificationChannel deletes a channel and its assignments/outbox.
func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	return requireOneRow(result, "delete notification channel")
}

func scanNotificationChannel(row rowScanner) (NotificationChannel, error) {
	var channel NotificationChannel
	var enabled int
	var created, updated int64
	err := row.Scan(&channel.ID, &channel.Name, &channel.Kind, &enabled, &channel.WebhookURLCipher,
		&channel.WebhookSecretCipher, &channel.TelegramTokenCipher, &channel.TelegramChatIDCipher, &created, &updated)
	channel.Enabled = enabled != 0
	channel.CreatedAt, channel.UpdatedAt = fromUnix(created), fromUnix(updated)
	return channel, err
}

func validateNotificationChannel(channel NotificationChannel) error {
	if channel.ID == "" || len(channel.ID) > 128 || channel.Name == "" || len(channel.Name) > 128 ||
		channel.CreatedAt.IsZero() || channel.UpdatedAt.IsZero() {
		return errors.New("invalid notification channel")
	}
	for _, value := range [][]byte{channel.WebhookURLCipher, channel.WebhookSecretCipher, channel.TelegramTokenCipher, channel.TelegramChatIDCipher} {
		if len(value) > maxEncryptedChannelField {
			return errors.New("invalid notification channel secret")
		}
	}
	switch channel.Kind {
	case "webhook":
		if len(channel.WebhookURLCipher) == 0 || len(channel.WebhookSecretCipher) == 0 || len(channel.TelegramTokenCipher) != 0 || len(channel.TelegramChatIDCipher) != 0 {
			return errors.New("invalid webhook channel")
		}
	case "telegram":
		if len(channel.TelegramTokenCipher) == 0 || len(channel.TelegramChatIDCipher) == 0 || len(channel.WebhookURLCipher) != 0 || len(channel.WebhookSecretCipher) != 0 {
			return errors.New("invalid Telegram channel")
		}
	default:
		return errors.New("invalid notification channel kind")
	}
	return nil
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// CreateAlertRule creates a bounded rule and its complete channel set.
func (s *Store) CreateAlertRule(ctx context.Context, rule AlertRule) error {
	return s.writeAlertRule(ctx, rule, true)
}

// UpdateAlertRule replaces a rule and resets its prior state/delivery work so
// an edited threshold cannot inherit an obsolete firing cursor.
func (s *Store) UpdateAlertRule(ctx context.Context, rule AlertRule) error {
	return s.writeAlertRule(ctx, rule, false)
}

func (s *Store) writeAlertRule(ctx context.Context, rule AlertRule, create bool) error {
	if err := validateAlertRule(rule); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin alert rule write: %w", err)
	}
	defer tx.Rollback()
	if err := validateAlertScopeAndChannels(ctx, tx, rule); err != nil {
		return err
	}
	if create {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_rules`).Scan(&count); err != nil {
			return fmt.Errorf("count alert rules: %w", err)
		}
		if count >= MaxAlertRules {
			return ErrAlertRuleLimit
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO alert_rules(id,name,scope_kind,scope_value,metric,threshold,duration_seconds,
			recovery_hysteresis,enabled,reminder_interval_seconds,reminder_max_count,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, rule.ID, rule.Name, rule.ScopeKind, rule.ScopeValue, rule.Metric,
			rule.Threshold, rule.DurationSeconds, rule.RecoveryHysteresis, boolInt(rule.Enabled), rule.ReminderIntervalSeconds,
			rule.ReminderMaxCount, unix(rule.CreatedAt), unix(rule.UpdatedAt))
	} else {
		var previousUpdated int64
		if err := tx.QueryRowContext(ctx, `SELECT updated_at FROM alert_rules WHERE id=?`, rule.ID).Scan(&previousUpdated); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("read alert rule revision: %w", err)
		}
		updated := unix(rule.UpdatedAt)
		if updated <= previousUpdated {
			updated = previousUpdated + 1
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE alert_rules SET name=?,scope_kind=?,scope_value=?,metric=?,threshold=?,
			duration_seconds=?,recovery_hysteresis=?,enabled=?,reminder_interval_seconds=?,reminder_max_count=?,updated_at=? WHERE id=?`,
			rule.Name, rule.ScopeKind, rule.ScopeValue, rule.Metric, rule.Threshold, rule.DurationSeconds,
			rule.RecoveryHysteresis, boolInt(rule.Enabled), rule.ReminderIntervalSeconds, rule.ReminderMaxCount,
			updated, rule.ID)
		if updateErr != nil {
			return fmt.Errorf("update alert rule: %w", updateErr)
		}
		if err := requireOneRow(result, "update alert rule"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM alert_states WHERE rule_id=?`, rule.ID); err != nil {
			return fmt.Errorf("reset edited alert states: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE rule_id=? AND status IN ('pending','retry')`, rule.ID); err != nil {
			return fmt.Errorf("reset edited alert deliveries: %w", err)
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM alert_rule_channels WHERE rule_id=?`, rule.ID)
	}
	if err != nil {
		return fmt.Errorf("write alert rule: %w", err)
	}
	for _, channelID := range uniqueSorted(rule.ChannelIDs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rule_channels(rule_id,channel_id,created_at) VALUES(?,?,?)`, rule.ID, channelID, unix(rule.UpdatedAt)); err != nil {
			return fmt.Errorf("write alert rule channel: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit alert rule write: %w", err)
	}
	return nil
}

func validateAlertScopeAndChannels(ctx context.Context, tx *sql.Tx, rule AlertRule) error {
	if rule.ScopeKind == "node" {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id=? AND deleted_at IS NULL)`, rule.ScopeValue).Scan(&exists); err != nil {
			return fmt.Errorf("validate alert node scope: %w", err)
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	channels := uniqueSorted(rule.ChannelIDs)
	if len(channels) != len(rule.ChannelIDs) || len(channels) > MaxChannelsPerAlertRule {
		return ErrInvalidAlertConfiguration
	}
	for _, channelID := range channels {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=?)`, channelID).Scan(&exists); err != nil {
			return fmt.Errorf("validate alert channel assignment: %w", err)
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func validateAlertRule(rule AlertRule) error {
	if rule.ID == "" || len(rule.ID) > 128 || rule.Name == "" || len(rule.Name) > 128 ||
		rule.ScopeValue == "" || len(rule.ScopeValue) > 128 || rule.DurationSeconds > 86400 ||
		math.IsNaN(rule.Threshold) || math.IsInf(rule.Threshold, 0) || rule.Threshold < 0 ||
		math.IsNaN(rule.RecoveryHysteresis) || math.IsInf(rule.RecoveryHysteresis, 0) || rule.RecoveryHysteresis < 0 ||
		rule.RecoveryHysteresis > rule.Threshold || rule.ReminderMaxCount > 10 {
		return ErrInvalidAlertConfiguration
	}
	if rule.ScopeKind != "node" && rule.ScopeKind != "group" {
		return ErrInvalidAlertConfiguration
	}
	switch rule.Metric {
	case "offline":
		if rule.Threshold != 0 || rule.RecoveryHysteresis != 0 {
			return ErrInvalidAlertConfiguration
		}
	case "cpu", "memory", "disk":
		if rule.Threshold > 100 {
			return ErrInvalidAlertConfiguration
		}
	case "traffic_rate":
		if rule.Threshold > float64(uint64(1)<<51) {
			return ErrInvalidAlertConfiguration
		}
	default:
		return ErrInvalidAlertConfiguration
	}
	if rule.ReminderIntervalSeconds == 0 {
		if rule.ReminderMaxCount != 0 {
			return ErrInvalidAlertConfiguration
		}
	} else if rule.ReminderIntervalSeconds < 300 || rule.ReminderIntervalSeconds > 86400 || rule.ReminderMaxCount == 0 {
		return ErrInvalidAlertConfiguration
	}
	return nil
}

// AlertRuleByID returns one rule and stable channel assignments.
func (s *Store) AlertRuleByID(ctx context.Context, id string) (AlertRule, error) {
	rule, err := scanAlertRule(s.db.QueryRowContext(ctx, alertRuleSelect+` WHERE r.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AlertRule{}, ErrNotFound
	}
	if err != nil {
		return AlertRule{}, fmt.Errorf("read alert rule: %w", err)
	}
	rule.ChannelIDs, err = alertRuleChannelIDs(ctx, s.db, id)
	return rule, err
}

const alertRuleSelect = `SELECT r.id,r.name,r.scope_kind,r.scope_value,r.metric,r.threshold,r.duration_seconds,
	r.recovery_hysteresis,r.enabled,r.reminder_interval_seconds,r.reminder_max_count,r.created_at,r.updated_at FROM alert_rules r`

// ListAlertRules returns every rule in stable display order.
func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, alertRuleSelect+` ORDER BY r.name,r.id`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()
	result := make([]AlertRule, 0)
	for rows.Next() {
		rule, scanErr := scanAlertRule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan alert rule: %w", scanErr)
		}
		result = append(result, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range result {
		result[index].ChannelIDs, err = alertRuleChannelIDs(ctx, s.db, result[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// AlertRulesForNode returns enabled node/group rules and enabled assignments.
func (s *Store) AlertRulesForNode(ctx context.Context, nodeID, group string) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, alertRuleSelect+` WHERE r.enabled=1 AND
		((r.scope_kind='node' AND r.scope_value=?) OR (r.scope_kind='group' AND r.scope_value=?)) ORDER BY r.id`, nodeID, group)
	if err != nil {
		return nil, fmt.Errorf("list node alert rules: %w", err)
	}
	defer rows.Close()
	result := make([]AlertRule, 0)
	for rows.Next() {
		rule, scanErr := scanAlertRule(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range result {
		result[index].ChannelIDs, err = alertRuleEnabledChannelIDs(ctx, s.db, result[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func scanAlertRule(row rowScanner) (AlertRule, error) {
	var rule AlertRule
	var enabled int
	var duration, reminderInterval, reminderCount uint64
	var created, updated int64
	err := row.Scan(&rule.ID, &rule.Name, &rule.ScopeKind, &rule.ScopeValue, &rule.Metric, &rule.Threshold,
		&duration, &rule.RecoveryHysteresis, &enabled, &reminderInterval, &reminderCount, &created, &updated)
	rule.DurationSeconds, rule.ReminderIntervalSeconds, rule.ReminderMaxCount = uint32(duration), uint32(reminderInterval), uint8(reminderCount)
	rule.Enabled = enabled != 0
	rule.CreatedAt, rule.UpdatedAt = fromUnix(created), fromUnix(updated)
	return rule, err
}

type alertChannelQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func alertRuleChannelIDs(ctx context.Context, query alertChannelQuerier, ruleID string) ([]string, error) {
	return queryAlertChannelIDs(ctx, query, `SELECT channel_id FROM alert_rule_channels WHERE rule_id=? ORDER BY channel_id`, ruleID)
}

func alertRuleEnabledChannelIDs(ctx context.Context, query alertChannelQuerier, ruleID string) ([]string, error) {
	return queryAlertChannelIDs(ctx, query, `SELECT a.channel_id FROM alert_rule_channels a JOIN notification_channels c ON c.id=a.channel_id
		WHERE a.rule_id=? AND c.enabled=1 ORDER BY a.channel_id`, ruleID)
}

func queryAlertChannelIDs(ctx context.Context, query alertChannelQuerier, statement, ruleID string) ([]string, error) {
	rows, err := query.QueryContext(ctx, statement, ruleID)
	if err != nil {
		return nil, fmt.Errorf("list alert rule channels: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// DeleteAlertRule removes one rule, its states, assignments, and delivery work.
func (s *Store) DeleteAlertRule(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	return requireOneRow(result, "delete alert rule")
}

// AlertState returns the durable non-normal state for one rule/node pair.
func (s *Store) AlertState(ctx context.Context, ruleID, nodeID string) (AlertState, error) {
	state, err := scanAlertState(s.db.QueryRowContext(ctx, `SELECT rule_id,node_id,status,generation,pending_since,firing_since,
		resolved_at,last_reminder_at,reminder_count,updated_at FROM alert_states WHERE rule_id=? AND node_id=?`, ruleID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return AlertState{RuleID: ruleID, NodeID: nodeID, Status: "normal"}, nil
	}
	if err != nil {
		return AlertState{}, fmt.Errorf("read alert state: %w", err)
	}
	return state, nil
}

// ListAlertStates returns bounded current states, optionally for one rule.
func (s *Store) ListAlertStates(ctx context.Context, ruleID string) ([]AlertState, error) {
	statement := `SELECT rule_id,node_id,status,generation,pending_since,firing_since,resolved_at,last_reminder_at,reminder_count,updated_at FROM alert_states`
	args := []any{}
	if ruleID != "" {
		statement += ` WHERE rule_id=?`
		args = append(args, ruleID)
	}
	statement += ` ORDER BY rule_id,node_id`
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert states: %w", err)
	}
	defer rows.Close()
	result := make([]AlertState, 0)
	for rows.Next() {
		state, scanErr := scanAlertState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, state)
	}
	return result, rows.Err()
}

func scanAlertState(row rowScanner) (AlertState, error) {
	var state AlertState
	var generation, reminders uint64
	var pending, firing, resolved, reminded sql.NullInt64
	var updated int64
	err := row.Scan(&state.RuleID, &state.NodeID, &state.Status, &generation, &pending, &firing, &resolved, &reminded, &reminders, &updated)
	state.Generation, state.ReminderCount, state.UpdatedAt = generation, uint8(reminders), fromUnix(updated)
	state.PendingSince, state.FiringSince, state.ResolvedAt, state.LastReminderAt = nullableTime(pending), nullableTime(firing), nullableTime(resolved), nullableTime(reminded)
	return state, err
}

func nullableTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := fromUnix(value.Int64)
	return &result
}

func unixPtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return unix(*value)
}

// ApplyAlertState atomically persists one state transition and its outbox
// records. Normal rows retain the bounded generation used for stable event IDs.
func (s *Store) ApplyAlertState(ctx context.Context, state AlertState, deliveries []NotificationDelivery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin alert state update: %w", err)
	}
	defer tx.Rollback()
	if state.Status != "normal" && state.Status != "pending" && state.Status != "firing" && state.Status != "resolved" {
		return errors.New("invalid alert state")
	}
	{
		var exists, count int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_states WHERE rule_id=? AND node_id=?), (SELECT COUNT(*) FROM alert_states)`, state.RuleID, state.NodeID).Scan(&exists, &count); err != nil {
			return fmt.Errorf("count alert states: %w", err)
		}
		if exists == 0 && count >= MaxAlertStates {
			return ErrAlertStateLimit
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO alert_states(rule_id,node_id,status,generation,pending_since,firing_since,resolved_at,last_reminder_at,reminder_count,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(rule_id,node_id) DO UPDATE SET status=excluded.status,generation=excluded.generation,
			pending_since=excluded.pending_since,firing_since=excluded.firing_since,resolved_at=excluded.resolved_at,
			last_reminder_at=excluded.last_reminder_at,reminder_count=excluded.reminder_count,updated_at=excluded.updated_at`,
			state.RuleID, state.NodeID, state.Status, state.Generation, unixPtr(state.PendingSince), unixPtr(state.FiringSince),
			unixPtr(state.ResolvedAt), unixPtr(state.LastReminderAt), state.ReminderCount, unix(state.UpdatedAt))
		if err != nil {
			return fmt.Errorf("persist alert state: %w", err)
		}
	}
	for _, delivery := range deliveries {
		if err := insertDelivery(ctx, tx, delivery); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit alert state update: %w", err)
	}
	return nil
}

// CreateNotificationDelivery inserts a standalone bounded test delivery.
func (s *Store) CreateNotificationDelivery(ctx context.Context, delivery NotificationDelivery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertDelivery(ctx, tx, delivery); err != nil {
		return err
	}
	return tx.Commit()
}

func insertDelivery(ctx context.Context, tx *sql.Tx, delivery NotificationDelivery) error {
	if delivery.ID == "" || len(delivery.ID) > 128 || delivery.EventID == "" || len(delivery.EventID) > 128 ||
		delivery.ChannelID == "" || len(delivery.EventPayload) < 2 || len(delivery.EventPayload) > 16384 {
		return errors.New("invalid notification delivery")
	}
	if delivery.EventKind != "firing" && delivery.EventKind != "resolved" && delivery.EventKind != "reminder" && delivery.EventKind != "test" {
		return errors.New("invalid notification event kind")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE completed_at IS NOT NULL AND completed_at<?`, unix(time.Now().UTC().Add(-7*24*time.Hour))); err != nil {
		return fmt.Errorf("prune old notification deliveries: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&count); err != nil {
		return fmt.Errorf("count notification deliveries: %w", err)
	}
	if count >= MaxNotificationDeliveries {
		remove := count - MaxNotificationDeliveries + 1
		if _, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE id IN (
			SELECT id FROM notification_deliveries WHERE completed_at IS NOT NULL ORDER BY completed_at,id LIMIT ?)`, remove); err != nil {
			return fmt.Errorf("prune completed notification deliveries: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&count); err != nil {
			return err
		}
		if count >= MaxNotificationDeliveries {
			return ErrDeliveryLimit
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries(id,event_id,channel_id,rule_id,node_id,event_kind,event_payload,status,attempts,next_attempt_at,last_error_code,created_at)
		VALUES(?,?,?,?,?,?,?,'pending',0,?,'',?) ON CONFLICT(event_id,channel_id) DO NOTHING`, delivery.ID, delivery.EventID,
		delivery.ChannelID, nullString(delivery.RuleID), nullString(delivery.NodeID), delivery.EventKind, delivery.EventPayload,
		unix(delivery.NextAttemptAt), unix(delivery.CreatedAt))
	if err != nil {
		return fmt.Errorf("create notification delivery: %w", err)
	}
	return nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// NotificationDeliveryByID returns one outbox record.
func (s *Store) NotificationDeliveryByID(ctx context.Context, id string) (NotificationDelivery, error) {
	delivery, err := scanNotificationDelivery(s.db.QueryRowContext(ctx, deliverySelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return NotificationDelivery{}, ErrNotFound
	}
	return delivery, err
}

const deliverySelect = `SELECT id,event_id,channel_id,COALESCE(rule_id,''),COALESCE(node_id,''),event_kind,event_payload,status,
	attempts,next_attempt_at,last_error_code,created_at,completed_at FROM notification_deliveries`

// ListDueNotificationDeliveries returns bounded due retry work.
func (s *Store) ListDueNotificationDeliveries(ctx context.Context, now time.Time, limit int) ([]NotificationDelivery, error) {
	if limit < 1 || limit > 256 {
		return nil, errors.New("invalid notification delivery limit")
	}
	rows, err := s.db.QueryContext(ctx, deliverySelect+` WHERE status IN ('pending','retry') AND next_attempt_at<=? ORDER BY next_attempt_at,created_at,id LIMIT ?`, unix(now), limit)
	if err != nil {
		return nil, fmt.Errorf("list due notification deliveries: %w", err)
	}
	defer rows.Close()
	result := make([]NotificationDelivery, 0)
	for rows.Next() {
		delivery, scanErr := scanNotificationDelivery(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func scanNotificationDelivery(row rowScanner) (NotificationDelivery, error) {
	var delivery NotificationDelivery
	var attempts uint64
	var next, created int64
	var completed sql.NullInt64
	err := row.Scan(&delivery.ID, &delivery.EventID, &delivery.ChannelID, &delivery.RuleID, &delivery.NodeID,
		&delivery.EventKind, &delivery.EventPayload, &delivery.Status, &attempts, &next, &delivery.LastErrorCode, &created, &completed)
	delivery.Attempts = uint8(attempts)
	delivery.NextAttemptAt, delivery.CreatedAt, delivery.CompletedAt = fromUnix(next), fromUnix(created), nullableTime(completed)
	return delivery, err
}

// CompleteNotificationDelivery records only bounded outcome metadata.
func (s *Store) CompleteNotificationDelivery(ctx context.Context, id, status, errorCode string, attempts uint8, next time.Time, completed *time.Time) error {
	if status != "retry" && status != "delivered" && status != "terminal" || len(errorCode) > 64 || attempts > 10 {
		return errors.New("invalid notification delivery outcome")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status=?,attempts=?,next_attempt_at=?,last_error_code=?,completed_at=? WHERE id=?`,
		status, attempts, unix(next), errorCode, unixPtr(completed), id)
	if err != nil {
		return fmt.Errorf("complete notification delivery: %w", err)
	}
	return requireOneRow(result, "complete notification delivery")
}

// DeleteNotificationDelivery removes a test job that could not enter the
// bounded in-memory dispatcher queue.
func (s *Store) DeleteNotificationDelivery(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE id=?`, id)
	return err
}

// ActiveAlertNodeIDs returns nodes currently covered by enabled rules. It is
// used only by the bounded offline evaluation tick.
func (s *Store) ActiveAlertNodes(ctx context.Context) ([]Node, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	result := nodes[:0]
	for _, node := range nodes {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_rules WHERE enabled=1 AND
			(scope_kind='node' AND scope_value=? OR scope_kind='group' AND scope_value=?))`, node.ID, node.Group).Scan(&exists); err != nil {
			return nil, err
		}
		if exists != 0 {
			result = append(result, node)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
