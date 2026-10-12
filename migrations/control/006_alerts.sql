CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 128),
    kind TEXT NOT NULL CHECK(kind IN ('webhook', 'telegram')),
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    webhook_url_cipher BLOB,
    webhook_secret_cipher BLOB,
    telegram_token_cipher BLOB,
    telegram_chat_id_cipher BLOB,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK(
        (kind = 'webhook' AND webhook_url_cipher IS NOT NULL AND webhook_secret_cipher IS NOT NULL
            AND telegram_token_cipher IS NULL AND telegram_chat_id_cipher IS NULL)
        OR
        (kind = 'telegram' AND webhook_url_cipher IS NULL AND webhook_secret_cipher IS NULL
            AND telegram_token_cipher IS NOT NULL AND telegram_chat_id_cipher IS NOT NULL)
    )
);

CREATE TABLE alert_rules (
    id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK(scope_kind IN ('node', 'group')),
    scope_value TEXT NOT NULL CHECK(length(scope_value) BETWEEN 1 AND 128),
    metric TEXT NOT NULL CHECK(metric IN ('offline', 'cpu', 'memory', 'disk', 'traffic_rate')),
    threshold REAL NOT NULL CHECK(threshold >= 0),
    duration_seconds INTEGER NOT NULL CHECK(duration_seconds BETWEEN 0 AND 86400),
    recovery_hysteresis REAL NOT NULL CHECK(recovery_hysteresis >= 0),
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    reminder_interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK(reminder_interval_seconds = 0 OR reminder_interval_seconds BETWEEN 300 AND 86400),
    reminder_max_count INTEGER NOT NULL DEFAULT 0 CHECK(reminder_max_count BETWEEN 0 AND 10),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK((reminder_interval_seconds = 0 AND reminder_max_count = 0) OR
          (reminder_interval_seconds > 0 AND reminder_max_count > 0)),
    CHECK((metric = 'offline' AND threshold = 0 AND recovery_hysteresis = 0) OR
          (metric <> 'offline' AND recovery_hysteresis <= threshold))
);

CREATE INDEX alert_rules_scope_idx ON alert_rules(scope_kind, scope_value, enabled, id);

CREATE TABLE alert_rule_channels (
    rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(rule_id, channel_id)
) WITHOUT ROWID;

CREATE INDEX alert_rule_channels_channel_idx ON alert_rule_channels(channel_id, rule_id);

CREATE TABLE alert_states (
    rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('normal', 'pending', 'firing', 'resolved')),
    generation INTEGER NOT NULL CHECK(generation BETWEEN 0 AND 9223372036854775807),
    pending_since INTEGER,
    firing_since INTEGER,
    resolved_at INTEGER,
    last_reminder_at INTEGER,
    reminder_count INTEGER NOT NULL DEFAULT 0 CHECK(reminder_count BETWEEN 0 AND 10),
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(rule_id, node_id)
) WITHOUT ROWID;

CREATE TABLE notification_deliveries (
    id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
    event_id TEXT NOT NULL CHECK(length(event_id) BETWEEN 1 AND 128),
    channel_id TEXT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    rule_id TEXT REFERENCES alert_rules(id) ON DELETE CASCADE,
    node_id TEXT REFERENCES nodes(id) ON DELETE CASCADE,
    event_kind TEXT NOT NULL CHECK(event_kind IN ('firing', 'resolved', 'reminder', 'test')),
    event_payload BLOB NOT NULL CHECK(length(event_payload) BETWEEN 2 AND 16384),
    status TEXT NOT NULL CHECK(status IN ('pending', 'retry', 'delivered', 'terminal')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 10),
    next_attempt_at INTEGER NOT NULL,
    last_error_code TEXT NOT NULL DEFAULT '' CHECK(length(last_error_code) <= 64),
    created_at INTEGER NOT NULL,
    completed_at INTEGER,
    UNIQUE(event_id, channel_id)
);

CREATE INDEX notification_deliveries_due_idx
    ON notification_deliveries(status, next_attempt_at, created_at, id);
CREATE INDEX notification_deliveries_completed_idx
    ON notification_deliveries(completed_at, id);
