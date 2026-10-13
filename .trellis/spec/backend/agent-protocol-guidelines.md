# Agent Enrollment and Protocol Guidelines

## 1. Scope / Trigger

Use this contract whenever code changes Agent enrollment, node credentials,
`/agent/v1/ws`, protocol envelopes, connection replacement, or Agent config
loading. These paths carry one-time secrets and establish the only long-lived
untrusted connection to Control.

The Agent surface is deliberately read-only. Protocol changes may add host
reports or bounded TCP probe messages, but must never add shell, terminal,
file, process-execution, inbound-listener, or generic RPC capabilities.

## 2. Signatures

The storage boundary owns these operations:

```go
func (*Store) CreateEnrollmentToken(context.Context, EnrollmentToken) error
func (*Store) EnrollmentByID(context.Context, string) (EnrollmentStatus, error)
func (*Store) RevokeEnrollment(context.Context, string) error
func (*Store) ConsumeEnrollment(context.Context, [32]byte, string, string, [32]byte, time.Time) (Node, error)
func (*Store) AuthenticateNodeCredential(context.Context, [32]byte) (Node, NodeCredential, error)
func (*Store) RotateNodeCredential(context.Context, string, string, [32]byte, time.Time) error
func (*Store) RevokeNodeCredentials(context.Context, string, time.Time) error
```

The Agent boundary owns:

```go
func Enroll(context.Context, *http.Client, string, string) (Config, error)
func LoadConfig(string) (Config, error)
func SaveConfig(string, Config) error
func (*Client) ConnectOnce(context.Context) error
```

Control schema v2 owns `nodes`, `node_credentials`, and `enrollment_tokens`.
Only 32-byte domain-separated digests enter `credential_digest` and
`token_digest`; plaintext enrollment tokens and node credentials must not
cross the storage boundary.

## 3. Contracts

### Enrollment HTTP

- `POST /api/v1/admin/enrollments` accepts `node_name` (1–128 bytes),
  `control_url`, and optional `expires_in_seconds` (300–86400, default 3600).
- At most `control.MaxActiveEnrollmentTokens` (256) unconsumed, unexpired
  tokens exist globally. Creation prunes expired rows, counts active tokens, and inserts
  in one write transaction.
- Administrator `GET /api/v1/admin/enrollments/{id}` returns only `id`,
  `node_name`, `expires_at`, `status` and optional consumed `node_id`.
  Status is `pending|registered|online|expired`; consumed records are
  registered/online according to LiveStore, regardless of their original TTL
  while retained. No token plaintext/digest or node credential is returned.
- Consumed records survive creation of other enrollments until original
  expiry. A later creation may prune them, and querying removed records gives
  `404 not_found` rather than guessing registration by node name.
- Administrator `DELETE /api/v1/admin/enrollments/{id}` requires CSRF and
  atomically deletes only unconsumed records. Consumption racing revocation
  either creates one node or revokes the token; it never deletes a created
  node. Regeneration after `409 enrollment_consumed` must refresh the existing
  installation state instead of silently registering a second node.
- `POST /agent/v1/enroll` accepts the token only as
  `Authorization: Bearer <token>` and returns `node_id`, `node_name`,
  `credential`, and `websocket_path` once.
- The Agent refuses redirects, limits the response to 64 KiB, and rejects
  unknown fields or trailing JSON. Its config is a regular user-only file,
  limited to 16 KiB and strictly decoded from one already-checked descriptor.
- HTTPS/WSS is required except for `localhost` or a literal loopback address.
  Userinfo, query strings, and fragments are rejected.

### WebSocket v1

- Endpoint: `GET /agent/v1/ws`, credential only in the Authorization header.
- Every text frame is at most 64 KiB and contains `version`, `type`,
  `session_id`, `seq`, and `payload`; unknown envelope/payload fields and
  trailing JSON are rejected.
- `session_id` is non-empty and at most 128 bytes. Sequence numbers start at 1
  independently in each direction and must then increase by exactly one.
- The complete type allowlist is `hello`, `report`, `probe.sync`,
  `probe.result`, `ping`, `pong`, and `error`. A type being allowlisted does not
  make it executable before its milestone handler validates its typed payload.
- Control sends the first hello with a random session ID/server nonce. Agent's
  first frame must echo the nonce. The server hello also carries a validated
  1000–60000 ms report interval (default 3000 ms). Queues, frames, hello time,
  heartbeat time, report frequency, and writes are bounded.
- Agent hello may include only a canonical lowercase Linux boot UUID read from
  bounded `/proc/sys/kernel/random/boot_id`. Missing/unreadable/invalid input
  becomes an omitted value; malformed, non-ASCII, uppercase, or overlong wire
  values make the hello invalid. Boot ID is session-only and is never logged,
  stored in `control.db`, or projected through node/live/public DTOs.
- `Registry` owns one active connection per node. Registering a replacement
  closes the previous session. Rotation/revocation commits first, removes the
  registry slot, and closes that connection; post-registration credential
  reauthentication closes the handshake race.
- `report` uses only `protocol.Report`; untyped maps are rejected by
  `NewEnvelope`. See [Linux Collection and Live Monitoring](./live-monitoring-guidelines.md)
  for its fields, value rules, offline semantics, and storage prohibition.
- `probe.sync` is a versioned complete `[]protocol.ProbeTask` snapshot and a
  bounded Agent acknowledgement; `probe.result` is a typed connect-only
  result. Both are strictly decoded and follow
  [Bounded TCP Probes](./tcp-probe-guidelines.md).

## 4. Validation & Error Matrix

| Condition | Boundary result |
|---|---|
| Invalid name, URL, or enrollment TTL | HTTP `400 invalid_input` or `400 invalid_expiry` |
| 257th active enrollment token | `ErrEnrollmentTokenLimit`; HTTP `409 enrollment_token_limit` |
| Unknown, expired, or consumed enrollment token | HTTP `401 enrollment_invalid` without distinguishing the cause |
| Administrator status/revocation for missing enrollment ID | `404 not_found` |
| Revocation for already consumed enrollment | `409 enrollment_consumed` |
| Missing/revoked node credential | HTTP `401 unauthorized` |
| Storage failure during credential authentication | HTTP `500 internal_error`, never `401` |
| Unknown/trailing JSON or incomplete envelope | protocol `invalid_envelope` and close |
| Unsupported protocol version/type | protocol `incompatible_version` / `unsupported_message` and close |
| Wrong session, duplicate, skipped, or decreasing sequence | protocol error and close |
| Missing/late hello or heartbeat | bounded protocol error and close |
| Full queue or blocked socket write | close the slow connection; never grow memory or block indefinitely |
| Context cancellation during connect/handshake | return `context.Canceled`/`context.DeadlineExceeded` through `errors.Is` |

Peer errors contain stable bounded codes/messages only. Raw SQL, filesystem
paths, Authorization values, tokens, credentials, and payloads are never
returned or logged.

## 5. Good / Base / Bad Cases

- Good: an HTTPS enrollment token is consumed once, an independent credential
  is saved mode 0600, and the Agent creates one outbound WSS connection.
- Base: a loopback HTTP Control is accepted for local development and produces
  a loopback WS endpoint without putting secrets in the URL.
- Bad: two concurrent requests consume one token; exactly one creates a node.
- Bad: 256 live tokens exist; concurrent creation attempts all fail with the
  stable limit error until one token is consumed or expires.
- Bad: a peer sends `seq=1` twice, adds an unknown field, exceeds 64 KiB, stalls
  socket reads, or never sends hello; Control terminates the session within a
  configured bound.

## 6. Tests Required

- Storage: one-time atomic consumption, expiry, 256/257 admission, concurrent
  admission, and capacity released by consumption/expiry.
- Status: pending/registered/online/expired progression, retained consumption
  across another creation, no secrets in responses, and consume/revoke races.
- HTTP: stable status/code mapping, secret absence from URLs/errors, TTL bounds
  before duration conversion, and internal storage error mapped to 500.
- Agent: redirect refusal, HTTPS/loopback validation, strict/bounded response
  and config decoding, mode checks, cancellation, and reconnect backoff.
- Protocol: both decode and encode enforce frame/session limits; unknown and
  trailing JSON, incompatible versions, all forbidden message names, repeated
  and out-of-order sequences are rejected.
- Protocol/collector: canonical boot ID, missing fallback, malformed/non-ASCII/
  overlong rejection, and strict hello JSON are covered.
- Connection: credential rotation/revocation and register races, single-node
  replacement, hello/heartbeat timeout, queue bounds, cleanup, and a real
  deterministic slow transport such as `net.Pipe` exercising write deadlines.
- Migration: each supported prior control schema is backed up before mutation;
  v3→v4 proves the backup lacks the cleanup outbox.

## 7. Wrong vs Correct

### Wrong

```go
// Secret leaks into URLs/logs, redirects may forward it, and a channel-only
// test does not prove that a blocked socket obeys WriteTimeout.
endpoint := controlURL + "/agent/v1/enroll?token=" + token
client.Post(endpoint, "application/json", nil)
```

### Correct

```go
request.Header.Set("Authorization", "Bearer "+token)
client.CheckRedirect = func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}
```

Keep only a domain-separated digest in `control.db`, strictly decode a bounded
response, and test slow writes with an actually blocked transport.
