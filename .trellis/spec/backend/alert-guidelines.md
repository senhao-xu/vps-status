# Alerts and Notifications

## Scope and ownership

`internal/control/alert.go` owns the live state machine and metric projection;
`internal/control/notification.go` owns bounded external delivery;
`internal/storage/control/alert.go` owns low-growth configuration, current
state, and the bounded delivery outbox. Alerts consume copied, already
validated `LiveStore` reports and must never depend on `telemetry.db`.

Control schema v6 owns `notification_channels`, `alert_rules`,
`alert_rule_channels`, `alert_states`, and `notification_deliveries`. These
tables may contain encrypted configuration, one current cursor per rule/node,
and bounded transition/test delivery work. They must not contain raw reports,
history buckets, traffic ledgers, or probe results.

## Configuration bounds

- At most 32 channels, 256 rules, 16 channel assignments per rule, 65,536
  current state rows, and 4,096 delivery rows.
- Names, IDs, node/group scopes, URLs, tokens, chat IDs, event bodies,
  durations, reminder intervals/counts, attempts, and API bodies all have hard
  bounds enforced before persistence or network use.
- Scope is exactly `node` or `group`. Metrics are `offline`, `cpu`, `memory`,
  `disk`, or `traffic_rate`. Percentage thresholds are 0–100.
- `traffic_rate` is the current validated
  `receive_bytes_per_second + transmit_bytes_per_second`, not cumulative or
  historical traffic. An unavailable metric resets unproven pending duration
  but never manufactures recovery for an already firing alert.
- Reminder is disabled by default. When enabled its interval is 300–86,400
  seconds and count is 1–10 per firing generation.

## State machine

The only transition owner is `advanceAlertState`:

```text
normal -> pending -> firing -> resolved -> normal
   |          |                     |
   +-> firing +-> normal            +-> pending/firing on a new breach
```

Only `firing`, `resolved`, and explicitly bounded `reminder` events produce
deliveries. Duration is measured from the persisted `pending_since` cursor.
Numeric recovery requires `value <= threshold - recovery_hysteresis`.
Offline breach means stale or disconnected; only a valid current report
recovers it. State and outbox insertion are one `control.db` transaction, so a
restart neither loses a transition nor regenerates it with a different ID.
Rule edits reset obsolete state/pending delivery; node group changes reset the
node's old scope state; node deletion removes node-scoped rules and all node
state/delivery work.

`alert_rules.updated_at` is also the monotonic durable rule revision. Event IDs
derive from rule ID, node ID, that revision, firing generation, event kind, and
reminder ordinal. This keeps retries/restarts stable while preventing the first
post-edit firing from colliding with a completed delivery retained for the
previous rule configuration.

Live report/stale/disconnect paths only coalesce into a bounded evaluator
queue. Database and network work never runs on Agent report ingestion. The
periodic evaluator handles offline duration and reminders even when no report
arrives. Telemetry degradation has no effect on this path.

## Secrets and API

Webhook URL/HMAC secret and Telegram bot token/chat ID are AES-GCM envelopes
from the deployment `MasterKey`. API input may replace a secret, but responses
only expose `configured`; plaintext, ciphertext, URL, token, and chat ID are
never read back. All channel/rule mutations and test sends require an active
session plus CSRF.

Webhook accepts only absolute `http`/`https` URLs without userinfo. It sends
the stable JSON event body with:

- `X-VPS-Status-Event-ID`
- `X-VPS-Status-Timestamp` (Unix seconds)
- `X-VPS-Status-Signature: sha256=<hex HMAC(timestamp + "." + body)>`

Telegram calls `/bot<TOKEN>/sendMessage`. Neither client returns/logs the URL,
body, remote error, token, chat ID, or secret. Response bodies are read through
a 16 KiB limit and always closed.

## Delivery classification and shutdown

- 2xx succeeds; 429 and 5xx retry; other 4xx is terminal.
- Telegram 429 may use bounded `Retry-After` header or bounded JSON
  `parameters.retry_after`; the maximum honored delay is one hour.
- Network/timeout/response-read failures retry with bounded exponential
  backoff and jitter. Attempts are capped at five by default and ten by schema.
- The dispatcher has a finite queue and at most 16 workers. Queue saturation
  never blocks reports; durable transition rows remain for polling. The test
  API returns stable `202 accepted` or `503 queue_full`.
- Cancellation stops poller/workers. Unfinished durable work remains pending
  or retryable for the next Control process.

## Required verification

- Duration, jitter, hysteresis, offline/recovery, metric unavailable,
  reminder bound, edit/delete/regroup, restart, event-ID stability, and
  telemetry-degraded behavior.
- Queue full, slow/timeout channel, bounded body/close, 2xx, 429/Retry-After,
  5xx retry, terminal 4xx, maximum attempts, restart dedupe, and shutdown/race.
- Webhook HMAC bytes/headers; Telegram path and token/error redaction;
  ciphertext-at-rest and API secret-negative assertions.
- v5→v6 pre-migration backup must contain none of the v6 tables.
