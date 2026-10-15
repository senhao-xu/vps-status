# Linux Collection and Live Monitoring Guidelines

## 1. Scope / Trigger

Use this contract when changing Linux host collection, the `report` Agent
message, Control's current-state memory, online/offline semantics, node
metadata APIs, or administrator SSE. It protects the hard boundary that
3-second samples are live data and never enter `control.db`.

This scope does not include telemetry rollups, durable traffic accounting,
TCP probes, alerts, public DTOs, container metrics, or remote execution.

## 2. Signatures

The shared report owner is `internal/protocol`:

```go
type Report struct {
    CollectedAtUnixMilli int64
    CPU                  CPUStats
    Load                 LoadStats
    Memory               MemoryStats
    Disk                 DiskStats
    Network              NetworkStats
    UptimeSeconds        uint64
    ProcessCount         uint64
    Connections          ConnectionStats
    Unavailable          []MetricArea
}

func ValidateReport(Report) error
func ValidateReportAt(Report, time.Time) error
func DecodeReport(json.RawMessage, time.Time) (Report, error)
```

The runtime boundaries are:

```go
type Collector interface { Collect(time.Time) (protocol.Report, error) }
func NewCollector() Collector
func CollectHostFacts() HostFacts

func NewLiveStore() *LiveStore
func (*LiveStore) Snapshot(nodeID string) LiveSnapshot
func (*LiveStore) Subscribe() (uint64, <-chan LiveEvent, error)
func (*LiveStore) Unsubscribe(uint64)

func (*Store) ListNodes(context.Context) ([]Node, error)
func (*Store) NodeByID(context.Context, string) (Node, error)
type NodePatch struct {
    Name, Group *string
    Hidden, HomepageVisible *bool
}
func (*Store) PatchNode(context.Context, string, NodePatch, time.Time) (Node, error)
func (*Store) UpdateNode(context.Context, string, string, string, bool, time.Time) error // legacy wrapper
func (*Store) UpdateNodeFacts(context.Context, string, NodeFacts, time.Time) error
func (*Store) DeleteNode(context.Context, string, time.Time) error
```

Control schema v3 adds only `group_name`, `hidden`, `hostname`, `os`,
`kernel`, `architecture`, and `virtualization` to `nodes`.

## 3. Contracts

### Linux Agent

- The production collector is Linux-only and reads bounded `/proc`, `/sys`,
  and `statfs` data without executing commands or opening an inbound socket.
- It reports CPU, 1/5/15 load, memory/Swap, root filesystem, aggregate
  non-loopback network rates/counters, uptime, process count, and TCP/UDP
  socket-entry counts.
- `ServerHello.report_interval_millis` defaults to 3000 and must be within
  1000–60000 before conversion to `time.Duration`.
- The Agent resets its report timer only after collection and transmission
  complete; delayed work skips missed ticks instead of replaying them and
  tripping Control's report-rate guard.
- Missing or unreadable sources produce a sorted, unique `unavailable` enum
  from `cpu`, `load`, `memory`, `disk`, `network`, `uptime`, `processes`, and
  `connections`. Raw errors and paths never cross the wire.
- Low-frequency facts also read bounded Linux boot ID. It is normalized to a
  canonical lowercase UUID for `ClientHello` only and is omitted on any read
  or validation failure.
- CPU totals exclude `guest`/`guest_nice` because Linux already includes them
  in `user`/`nice`. Process directories are counted in fixed-size batches.
  Counter decreases and newly appearing interfaces contribute zero rate for
  that interval rather than underflowing.

### Control live state

- Control strictly decodes one canonical `protocol.Report`, rejects unknown or
  trailing JSON, invalid relations, NaN/Inf, excessive values, timestamps
  outside ±5 minutes, and reports arriving faster than half the negotiated
  interval.
- One latest report exists per node in `LiveStore`; it is copied on write,
  snapshot, and fanout. A new Agent session clears the prior session's report.
- A node becomes online after a valid report from its current session. It is
  stale after four missed report intervals and offline immediately when the
  connection closes. No timer writes a database row.
- SSE has at most 32 subscribers with 16 queued events each. Slow subscribers
  are evicted without blocking report processing. Streams require an active
  administrator session, close at session expiry, and revalidate revocation on
  the 15-second keepalive. Ready, event, and keepalive writes each reset a
  bounded write deadline longer than that keepalive, so the HTTP server's
  ordinary write timeout is not the stream lifetime and blocked clients exit.

### Administrator HTTP and persistence

- `GET /api/v1/admin/nodes` returns `{ "nodes": [...] }`; detail uses
  `GET /api/v1/admin/nodes/{id}`. Explicit DTOs combine low-growth metadata
  with a copied live snapshot. Each admin node emits homepage_visible derived
  from public_nodes; it is independent of hidden and is never added to the
  status DTO or private live event payload.
- `PATCH /api/v1/admin/nodes/{id}` accepts optional `name`, `group`, and
  `hidden`, and `homepage_visible`; name is non-empty and name/group are at most
  128 bytes. It requires CSRF and merges supplied fields in one storage-owned
  transaction, including display membership. Omitted fields retain current
  committed values. An enable beyond the1000display cap returns409
  homepage_node_limit and rolls back metadata too. `DELETE` soft-deletes the node, revokes credentials atomically,
  disconnects it, removes current memory state, and atomically records a
  durable node-ID-only telemetry cleanup job.
- `GET /api/v1/admin/live` is authenticated `text/event-stream` with bounded
  `connected`, `report`, `stale`, `disconnected`, and `deleted` events.
- Static hello facts may update `control.db` only when changed. Reports,
  rates, cumulative counters, samples, and SSE events never do.
- Boot ID is not a static node fact: it remains in the current WSS session and
  in-memory traffic baseline only, and is absent from node/live/public DTOs.

## 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Non-Linux runtime attempts to run/enroll | Agent exits with a Linux-only error |
| One kernel source is missing/denied | Report succeeds with that bounded `unavailable` area |
| Collector output violates protocol bounds | Session collection fails and reconnect uses bounded backoff |
| Unknown/trailing report JSON or invalid numeric relation | protocol `invalid_report`, then close |
| Report is too frequent | protocol `report_rate`, then close |
| Report belongs to a replaced session | protocol `session_mismatch`, then close |
| Four intervals pass without a report | current session remains connected but node becomes offline/stale |
| Node name/group invalid | HTTP `400 invalid_input` |
| Homepage display cap on manual enable | HTTP `409 homepage_node_limit`; entire node update unchanged |
| Node missing/deleted | HTTP `404 not_found` |
| 33rd live subscriber | HTTP `503 live_subscriber_limit` |
| SSE session expires/revokes or client cancels | stream closes and subscription is released |

## 5. Good / Base / Bad Cases

- Good: every 3 seconds a Linux Agent sends one valid report; Control replaces
  one in-memory snapshot and broadcasts without changing SQLite.
- Base: an unprivileged Agent cannot read socket tables; all other metrics are
  still sent and `connections` is listed as unavailable.
- Bad: CPU guest time is summed twice, a reset counter underflows, or a new
  interface inherits another interface's baseline. Fixture tests must fail.
- Bad: a replacement WSS session is shown online using the old session's last
  report. `setConnected` must clear it until the replacement reports.
- Bad: an SSE writer is slow. Its bounded queue is removed; Agent ingestion
  continues.

## 6. Tests Required

- Protocol: strict JSON, timestamp skew, NaN/Inf, byte/count/rate bounds,
  memory/disk relations, unique known unavailable areas, and typed envelope.
- Collector fixtures: kernel CPU field variants, guest exclusion, old meminfo
  fallback, missing files, disk failure, counter reset, interface add/remove,
  socket/process bounds, virtualization facts, and boot-ID normalization/
  rejection fixtures.
- Agent: negotiated interval range before duration conversion, typed report
  sequence, write failure/cancellation, Linux amd64/arm64 CGO-free builds.
- Control WSS: static facts, report acceptance, rate rejection, four-interval
  stale transition, disconnect/replacement isolation, and race checks.
- Live/SSE: copy isolation, subscriber/queue limits, authentication,
  cancellation, session lifetime, and DTO secret-negative assertions.
- Storage/API: v2→v3 and v3→v4 pre-migration backups, node list/detail/update/delete,
  credential revocation, and schema assertions proving no report/telemetry
  table or value exists in `control.db`.

## 7. Wrong vs Correct

### Wrong

```go
// A 3-second sample crosses the configuration database boundary.
store.Exec(`INSERT INTO node_reports(node_id, payload) VALUES (?, ?)`, nodeID, report)
```

### Correct

```go
// Replace one copied live value; Milestone 4 consumes validated reports into
// in-memory minute buckets and writes only completed rollups to telemetry.db.
live.acceptReport(nodeID, sessionID, report, receivedAt)
```

The physical database split is the feature: convenience is not a reason to
persist a report, counter, rate, or SSE event in `control.db`.
