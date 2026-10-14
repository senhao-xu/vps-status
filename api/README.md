# API contracts

Control currently exposes the following administrator-security routes under
`/api/v1/admin`:

- `GET/POST /setup`
- `POST /login` and `POST /logout`
- `POST /password`
- `GET /session` and `GET /sessions`
- `DELETE /sessions/{id}`
- `POST /mfa/totp/setup`, `/mfa/totp/confirm`, and `/mfa/totp/disable`

Responses use JSON and stable error codes. Authenticated mutations require the
server-side session cookie plus `X-CSRF-Token`; login and setup have bounded
in-memory rate limits. The session cookie is `Secure`, `HttpOnly`, and
`SameSite=Strict`. At most 32 active sessions are retained; the oldest active
session is evicted when the limit is exceeded. Confirming TOTP retains the
current session and revokes all other sessions. TOTP setup and recovery codes
are one-time secret-bearing responses and every `/api/` response is marked
`Cache-Control: no-store`.

Session issuance revalidates the administrator password digest and MFA state in the storage write
transaction. If authentication settings changed while a login was in flight,
Control returns `409 auth_state_changed` and the client must retry instead of
creating a session from stale authentication state.

## Administrator password

The Security screen calls `POST /api/v1/admin/password` with a session,
`X-CSRF-Token`, and `{"current_password":"…","new_password":"…"}`.
Both fields must be supplied as strings; empty, short and long values follow
the existing setup rules and body-size cap. Password attempts are rate-limited
under a separate administrator key using the login limiter.

An incorrect current password returns `400 current_password_invalid` without
logging out the browser. Missing/invalid fields return `400`, absent session
returns `401`, invalid CSRF returns `403`, and rate exhaustion returns
`429 rate_limited` with `Retry-After`. A concurrent authentication-state change
returns `409 auth_state_changed` without committing a password update.

Success is `204 No Content`: the new Argon2id digest and revocation of all
administrator sessions commit together, and session/CSRF cookies are cleared.
The UI returns to login with a success notice. MFA configuration/recovery
codes remain unchanged; the new password still requires MFA when enabled.

This directory will hold machine-readable or generated contract inputs as the
remaining alert and public APIs are introduced.

## Public status page

`GET/PUT /api/v1/admin/public-page` reads or atomically replaces the public
page title, description, enabled flag, and complete node publication
allowlist. Nodes are private by default; only active node IDs explicitly
listed by the administrator are published. The mutation requires CSRF.

`GET /api/v1/public/status` is unauthenticated and uses a separate DTO built
server-side. It exposes only page title/description and, for published nodes,
display name, group/region, online state, current uptime/CPU/memory/disk,
current RX/TX rates, and current UTC-month traffic totals. Missing/degraded
values are JSON `null`. It never contains node IDs, real hostname, IP, OS,
kernel, architecture, virtualization, credentials, probe targets, alert or
notification secrets, or historical points/curves.

## Built-in appearance

`GET/PUT /api/v1/admin/appearance` reads or saves the site default theme.
Both return exactly `{"default_theme":"emerald"}` or
`{"default_theme":"classic"}`; PUT accepts the same shape and requires an
administrator session and CSRF token. Unsupported theme IDs return
`400 invalid_input`. Configuration is persisted in the singleton `appearance`
table in `control.db`; the initial default is Emerald.

`GET /api/v1/public/appearance` returns only that presentation field without
authentication, including when the public status page is disabled. Login and
public pages use it to choose their default theme. Browser overrides are
frontend session preferences and do not write this configuration. Visual
theme and light/dark mode are independent. All appearance responses use the
existing API `Cache-Control: no-store` policy.

## Agent enrollment and credentials

An authenticated administrator creates a short-lived enrollment through
`POST /api/v1/admin/enrollments` with `node_name`, optional `control_url`, and optional
`expires_in_seconds` (300–86400). The response shows the 256-bit enrollment
token exactly once and contains two distinct commands: `install_command` is a
single-line download of the configured version's `bootstrap-agent.sh` from
GitHub Releases or a configured HTTPS mirror, while `enroll_command` is the compatibility/manual
`vps-status-agent enroll` command for an Agent already installed on the node.
Control stores only the domain-separated token digest. The shared saved node
entry takes precedence over `control_url`; when neither is provided, a valid
non-loopback HTTPS request Origin is used. Explicit loopback `control_url` is
retained for local development clients. Missing release configuration returns
`409 agent_release_required` before an enrollment token is created.
At most 256 unconsumed, unexpired enrollment tokens may exist globally.
Creation first prunes expired rows in the same storage transaction;
the next token is rejected with `409 enrollment_token_limit` when the bound is
still full. Consumed records do not count toward this limit and remain until
their original expiry so installation status can be read.

`GET /api/v1/admin/enrollments/{id}` requires an administrator session and
returns only `id`, `node_name`, RFC3339 `expires_at`, and `status`, with `node_id`
included only after consumption. States are `pending` (unconsumed/unexpired),
`expired` (unconsumed/expired), `registered` (consumed, not online), and `online`
(consumed node has a current online LiveStore snapshot). A connected Agent
without a valid report is still `registered`. Consumption takes precedence
over expiry while the record is retained. Status never exposes token,
digest, installation command, or node credential material.

`DELETE /api/v1/admin/enrollments/{id}` requires session and CSRF and revokes
only an unconsumed enrollment, returning `204`. Concurrent consumption and
revocation are atomic: if consumption won, deletion returns
`409 enrollment_consumed` and preserves the registered node. Clients should
refresh that enrollment's status instead of creating a replacement node.
Unknown, revoked, and expiry-pruned IDs return `404 not_found` for both routes;
a pruned status does not imply that a successfully registered node expired.

The bootstrap script contains no enrollment secret. The standalone asset accepts
the Control URL, one-time token, explicit release version and HTTPS release base
as arguments, supports only Linux amd64/arm64 with systemd,
and downloads a configured explicit Release version over HTTPS. It fetches the
matching Agent, `SHA256SUMS`, the systemd installer, and unit, verifies each
download before execution, installs the Agent under the locked
`vps-status-agent` account, enrolls it, and starts the service. Its Release base
URL and version are restricted at Control startup to prevent shell/path
injection. OpenRC, Docker Agent installation, automatic updates, and signature
verification are not part of this endpoint.

### Shared installation settings

Administrator `GET/PUT /api/v1/admin/installation` uses the existing session
boundary; PUT also requires CSRF. PUT accepts only `control_url` and
`agent_release_version`, both strings. Empty values mean automatic browser
entry and the Control startup version respectively. A saved entry must be
HTTPS and non-loopback; a saved version must be a safe path segment and cannot
be a development placeholder. Invalid settings return `400 invalid_input`.

Both responses contain only:

```json
{
  "control_url": "https://monitor.example.com",
  "agent_release_version": "v0.1.0",
  "effective_agent_release_version": "v0.1.0",
  "agent_release_base_url": "https://github.com/senhao-xu/vps-status/releases/download",
  "release_configured": true
}
```

Settings persist in the control database. The saved Agent version overrides
`--agent-release-version`; `--agent-release-base-url` remains a startup setting.
`release_configured` describes eligible local configuration, not a GitHub
existence check. The configured Release must publish the bootstrap and all
matching checksum-covered assets. The public `GET /install-agent.sh` remains
compatible with two-argument commands and uses the canonical bootstrap source;
it has no token and reflects the current saved release override.

The Agent sends that token in `Authorization: Bearer <token>` to
`POST /agent/v1/enroll`. It is never accepted in a URL or query parameter. A
successful atomic exchange returns a stable `node_id` and a separate 256-bit
`credential` once; the Agent saves it in a mode-0600 configuration file. An
expired, consumed, or unknown token receives the same `401 enrollment_invalid`
response. Production Control URLs must use HTTPS; plaintext HTTP is accepted
only for loopback development. The Agent does not follow enrollment redirects,
bounds the response body, and strictly rejects unknown or trailing JSON.

Credential rotation uses
`POST /api/v1/admin/nodes/{id}/credentials/rotate`; revocation uses
`DELETE /api/v1/admin/nodes/{id}/credentials`. Both immediately invalidate old
credentials and close the current Agent connection. Rotation returns the new
credential exactly once.

## Agent WebSocket v1

Agents connect to `/agent/v1/ws` with the node credential only in the
`Authorization` header. Public deployments use WSS. Every JSON text frame is
at most 64 KiB and has `version`, `type`, `session_id`, strictly increasing
`seq`, and `payload`. The complete v1 type allowlist is `hello`, `report`,
`probe.sync`, `probe.result`, `ping`, `pong`, and `error`; both probe messages
are now active with the bounded contract below. There is no command, shell, terminal, file,
process, or generic RPC message.

Control sends the first `hello` with sequence 1, a random session ID, a random
server nonce, the authenticated node ID, heartbeat timing, and a report
interval (default 3000 ms; accepted range 1000–60000 ms). Agent replies with
sequence 1, echoes the nonce, and supplies bounded hostname/OS/kernel/
architecture/virtualization facts. Both peers then require their peer's
sequence to increase by exactly one. Connections have bounded read/write
queues, write deadlines, hello and heartbeat timeouts, and a single active
connection per node. Peer-visible errors use a bounded stable code/message
payload and never include credentials or raw internal errors.

Agent hello may also include `boot_id`, a canonical lowercase Linux boot UUID.
It is used only in the current WSS session to detect counter resets across long
disconnects. It is never persisted, logged, or returned by node/live/public APIs;
missing boot ID falls back to conservative uptime-based reboot detection.

The unique `report` payload contains collection time, CPU percentage, 1/5/15
load, memory/Swap, root-disk use, aggregate network rates and counters, uptime,
process count, TCP/UDP counts, and a bounded `unavailable` area list. Control
strictly validates JSON, values, relationships, clock skew, sequence, session,
and frequency. Four missed report intervals mark a connected node stale.
Every raw report replaces one in-memory snapshot; no raw report, rate, traffic
counter, or online transition is written to `control.db`.

## Telemetry history and maintenance

`telemetry.db` is an optional, physically separate history store. Validated
reports update an in-memory minute bucket; only completed `host_1m` buckets are
queued to its serialized writer. Cumulative counters update compact
`lifetime` and UTC `YYYY-MM` traffic rows with reboot/reset-aware deltas. The
default non-overlapping ladder is 1m through 24h, 10m through 7d, and 1h
through 30d; Control accepts explicit 90/365-day hourly retention.
Each history point includes total `sample_count` plus per-area valid counts.
Metric values are `null` when their area has no valid samples, so collector
unavailability is never presented as a measured zero.

Authenticated read routes:

- `GET /api/v1/admin/telemetry/storage` returns `state`, main/WAL/SHM/total
  bytes, free disk bytes, oldest retained bucket, page count, and freelist.
  It remains `200` with an unavailable state when telemetry failed to open so
  the administrator can diagnose history without coupling it to login.
- `GET /api/v1/admin/telemetry/host` requires `node_id`, `resolution`
  (`1m`, `10m`, or `1h`), and RFC3339 `from`/`to` no more than 366 days apart.
- `GET /api/v1/admin/telemetry/traffic?node_id=...` returns lifetime and
  available monthly ledger rows.
- `GET /api/v1/admin/telemetry/probe` requires `node_id`, `task_id`,
  `resolution`, and RFC3339 `from`/`to`, and returns attempts, success/failure,
  success rate, and successful latency min/max/weighted average.

CSRF-protected maintenance routes accept strict JSON confirmations:

| Route | Required body | Effect |
|---|---|---|
| `POST /api/v1/admin/telemetry/clear-history` | `{"confirm":"clear_history"}` | removes rollups/open buckets; preserves traffic |
| `POST /api/v1/admin/telemetry/reset-all` | `{"confirm":"reset_all_telemetry"}` | physically recreates telemetry and resets traffic baselines |
| `POST /api/v1/admin/telemetry/checkpoint` | `{"confirm":"checkpoint"}` | truncates the WAL |
| `POST /api/v1/admin/telemetry/incremental-vacuum` | `{"confirm":"incremental_vacuum","pages":1024}` | bounded free-page reclaim |
| `POST /api/v1/admin/telemetry/full-vacuum` | `{"confirm":"full_vacuum"}` | full reclaim only when the disk safety check passes |

Unavailable history uses stable `503 telemetry_unavailable` for history and
mutation routes. A disk/writer pause remains readable and exposes its stable
reason through the storage route, while maintenance actions can still reclaim
space. Neither state prevents administrator login, node configuration, Agent
WSS sessions, current `LiveStore` state, or SSE.

## Administrator nodes and live stream

- `GET /api/v1/admin/nodes` and `GET /api/v1/admin/nodes/{id}` return explicit
  administrator DTOs combining node metadata/static facts with a copied live
  snapshot.
- `PATCH /api/v1/admin/nodes/{id}` updates optional `name`, `group`, and
  `hidden` fields and requires CSRF. `DELETE` soft-deletes the node, revokes
  its credential, disconnects it, clears current memory state, and durably
  queues idempotent telemetry cleanup for startup/background retry.
- `GET /api/v1/admin/live` is an authenticated SSE stream. Its bounded event
  allowlist is `connected`, `report`, `stale`, `disconnected`, and `deleted`.
  At most 32 streams exist, each with 16 queued events; slow consumers are
  dropped without blocking Agent ingestion. Session expiry or revocation
  closes the stream.

## Bounded TCP probes

TCP definitions are low-growth configuration in `control.db`:

- `GET/POST /api/v1/admin/probes` lists or creates tasks.
- `GET/PATCH/DELETE /api/v1/admin/probes/{id}` reads, replaces selected fields,
  or deletes a task.
- `PUT/DELETE /api/v1/admin/probes/{id}/assignments/{node_id}` adds or removes
  one assignment.
- `GET /api/v1/admin/probe-results?node_id=&task_id=` returns copied current
  in-memory results; either filter is optional.

Create accepts strict JSON `name`, `host`, `port`, `interval_seconds`, and
`node_ids`. Patch accepts the same fields optionally and replaces `node_ids`
as a complete set when present. Mutations require CSRF. A node may have at
most 64 tasks, intervals are 5–3600 seconds, and ports are 1–65535. DNS names
and public/private/loopback IP literals are allowed; unspecified and multicast
addresses are rejected. The stable quota error is `409 probe_task_limit`.

After Agent hello, Control sends a strongly typed, versioned `probe.sync`
complete snapshot. Agent treats it as untrusted: it never clamps invalid
values into different work, omits invalid tasks, and returns a bounded
`probe.sync` status with rejection codes. A malformed task identifier is not
echoed in that status. Deleted tasks disappear from the next snapshot;
duplicate/older versions do not restart work, and reconnects receive a fresh
full snapshot. Control publishes the snapshot version and assignment admission
state before the queued frame can be acknowledged, so a fast acknowledgement
or result cannot be evaluated against the preceding snapshot.

Each task is serial and uses delay-after-completion scheduling, so missed ticks
are skipped. All tasks share an eight-slot semaphore. DNS and all connection
attempts share a three-second deadline; at most the first three DNS addresses
are tried serially, each for at most one second. The first successful TCP
connection is immediately closed. There are no HTTP, ICMP, TLS, payload,
banner, or arbitrary execution probe modes.

Control validates every `probe.result` against the current WSS session,
sequence, current assignment, task cadence, connection-wide rate bound,
scheduled timestamp, 0–3000 ms latency, and the fixed failure taxonomy
`dns`, `timeout`, `connection_refused`, `network_unreachable`,
`host_unreachable`, `address_invalid`, or `connect`. Unknown/flooded results
close the protocol session before they can grow runtime dimensions.
Individual results update only bounded current memory and the open minute
bucket. Completed `probe_1m` buckets enter `telemetry.db`, then roll up
idempotently to 10m/1h under the same 24h/7d/30d non-overlapping retention as
host history. Telemetry degradation drops history only; scheduling, WSS, and
current results remain available.

## Alerts and notification channels

All routes below require an administrator session. Mutations and test sends
also require `X-CSRF-Token`.

Notification channels:

- `GET/POST /api/v1/admin/notification-channels`
- `GET/PATCH/DELETE /api/v1/admin/notification-channels/{id}`
- `POST /api/v1/admin/notification-channels/{id}/test`

Channel `kind` is `webhook` or `telegram`. Webhook input uses `url` and
`secret`; Telegram uses `bot_token` and `chat_id`. These values are encrypted
with the deployment master key and are input-only: every response contains
only `configured`, never plaintext, ciphertext, a complete URL, token, chat
ID, or secret. There are at most 32 channels. The test route uses the same
bounded dispatcher and returns `202 {"status":"accepted",...}` or stable
`503 queue_full`.

Alert rules:

- `GET/POST /api/v1/admin/alert-rules`
- `GET/PATCH/DELETE /api/v1/admin/alert-rules/{id}`
- `GET /api/v1/admin/alert-rules/{id}/states`

A rule has `scope_kind` (`node` or `group`), `scope_value`, `metric`
(`offline`, `cpu`, `memory`, `disk`, or `traffic_rate`), `threshold`,
`duration_seconds` (0–86400), `recovery_hysteresis`, `enabled`, a complete
`channel_ids` set, and optional reminder interval/count. Reminder is off when
both fields are zero; otherwise interval is 300–86400 seconds and maximum
count is 1–10. There are at most 256 rules and 16 channels per rule.

`traffic_rate` is explicitly the current validated RX + TX bytes/second.
Percentage metrics use validated percentage/current-capacity values. Missing
metric areas reset pending duration and never manufacture a measured zero or
recovery for an already firing alert. Offline means
stale or disconnected and recovers only on a valid current report. Current
state is `normal`, `pending`, `firing`, or `resolved`; only firing/resolved and
bounded reminders produce notifications.

Every notification body has stable `event_id`, RFC3339 `timestamp`, `kind`,
state, rule/node identifiers and display names, metric, value, and threshold.
Webhook requests also include `X-VPS-Status-Event-ID`, Unix-seconds
`X-VPS-Status-Timestamp`, and
`X-VPS-Status-Signature: sha256=<HMAC-SHA256(timestamp + "." + body)>`.
Telegram uses Bot API `sendMessage`. Both clients have a timeout and 16 KiB
response limit. 2xx succeeds, 429/5xx retries with bounded backoff, and other
4xx is terminal. Telegram `Retry-After` is capped at one hour. Transition work
is durably deduplicated in a 4,096-row bounded outbox, so a slow channel cannot
block report/WSS/live processing and pending work survives a Control restart.
