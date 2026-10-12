# Telemetry Storage and Retention Guidelines

## 1. Scope / Trigger

Use this contract when changing the `telemetry.db` schema, validated-report
aggregation, host history queries, traffic accounting, retention, physical
reclaim, storage status, or degraded-history behavior. The purpose is to keep
configuration/authentication independent from high-growth telemetry and to
guarantee that 3-second reports never become append-only database rows.

TCP probe history follows the same writer, guard, rollup, retention, clear,
reset, and cleanup boundaries; its probe-specific contract is in
`tcp-probe-guidelines.md`. Remote commands, terminal, file, process execution,
and generic RPC remain forbidden regardless of storage availability.

## 2. Signatures

The storage boundary is:

```go
func Open(context.Context, string) (*telemetry.Store, error)
func NewService(*telemetry.Store, telemetry.Config) (*telemetry.Service, error)
func NewUnavailable(reason string) *telemetry.Service
func (*telemetry.Service) SubmitHost(telemetry.HostBucket) error
func (*telemetry.Service) SubmitTraffic(telemetry.TrafficDelta) error
func (*telemetry.Service) SubmitProbe(telemetry.ProbeBucket) error
func (*telemetry.Service) QueryHost(context.Context, string, telemetry.Resolution, time.Time, time.Time) ([]telemetry.HostPoint, error)
func (*telemetry.Service) QueryProbe(context.Context, string, string, telemetry.Resolution, time.Time, time.Time) ([]telemetry.ProbePoint, error)
func (*telemetry.Service) Traffic(context.Context, string) ([]telemetry.TrafficTotal, error)
func (*telemetry.Service) RunMaintenance(context.Context, time.Time) (telemetry.MaintenanceResult, error)
func (*telemetry.Service) ClearHistory(context.Context) error
func (*telemetry.Service) ResetAll(context.Context) error
func (*telemetry.Service) Checkpoint(context.Context) error
func (*telemetry.Service) IncrementalVacuum(context.Context, int) error
func (*telemetry.Service) FullVacuum(context.Context) error
func (*telemetry.Service) Stats(context.Context) (telemetry.StorageStats, error)
func (*telemetry.Service) DeleteNode(context.Context, string) error
func (*telemetry.Service) DeleteProbeTask(context.Context, string) error
```

Control owns `NewHostHistory`, `Observe`, `Seal`, `ClearBuckets`, and `Reset`.
Only an already validated, current-session `protocol.Report` reaches `Observe`.

Schema v1 owns `host_1m`, `host_10m`, `host_1h`, `traffic_ledger`,
`maintenance_metadata`, and its own `schema_migrations`; schema v2 adds
per-area valid counts and nullable metric values; schema v3 adds completed
`probe_1m`, `probe_10m`, and `probe_1h` aggregates. `control.db` may contain a
low-growth `telemetry_cleanup_jobs` outbox with node IDs and retry metadata,
but no telemetry value, sample, counter, bucket, handle, or cross-file key.

## 3. Contracts

- One serialized writer consumes a bounded queue. A separate read-only pool
  serves history/status reads. Queue saturation drops history work and changes
  observable state; it never blocks Agent report processing.
- Open minute buckets live only in memory. `HostBucket` stores total reports
  plus independent valid counts for CPU, load, memory, disk, network, uptime,
  processes, and connections. Unavailable areas do not update their sum/min/
  max/last fields. Query values are JSON `null` when the corresponding valid
  count is zero. A bucket is submitted only after its minute completes.
- Traffic baselines are in memory. Linux boot ID is carried only in the WSS
  hello/session and baseline, never persisted or exposed. Different valid boot
  IDs establish a zero-delta baseline even if new uptime is larger; the same
  boot ID continues monotonic counters across replacement sessions. If either
  side lacks boot ID, conservative uptime fallback applies.
- A report whose network or uptime area is unavailable cannot replace a
  traffic baseline. A collection timestamp that does not advance the last
  accepted baseline is ignored for traffic accounting, including across an
  Agent reconnect, so a stale counter cannot be reset and counted again.
- Rollups are weighted and idempotent: `1m -> 10m -> 1h`. Default tiers are
  non-overlapping: 1m through 24h, 10m from 24h through 7d, and 1h from 7d
  through 30d. Only 90 or 365 days may explicitly extend the hourly cutoff.
  Each metric uses its own summed valid count, and last values come from the
  newest source bucket where that metric area is valid.
- Node deletion inserts a cleanup job in the same `control.db` transaction as
  soft deletion and credential revocation. Startup and a bounded background
  pass retry synchronous idempotent telemetry deletion; only success removes
  the job. Telemetry outage, queue saturation, or a crash leaves it durable.
- `ClearHistory` removes rollup rows and open in-memory buckets but preserves
  traffic rows/baselines. `ResetAll` physically recreates `telemetry.db`, then
  clears both open buckets and traffic baselines. Both require exact CSRF-
  protected confirmations at the HTTP boundary.
- `auto_vacuum=INCREMENTAL` is persisted before schema creation. Ordinary
  maintenance uses chunked deletes, passive/truncate checkpoints, and bounded
  incremental vacuum. Full `VACUUM` is explicit and requires at least twice
  the telemetry footprint plus the configured free-space reserve.
- File budget counts main/WAL/SHM together. Low disk and file budget recovery
  use hysteresis. While paused, queues do not grow, reads/live state/auth/config
  remain available, and `State.reason` exposes only stable categories.
- Control opens `control.db` first. Telemetry open/migration/configuration
  failure creates `NewUnavailable("open_failed")` and cannot prevent login,
  node configuration, WSS live state, or the storage-status API from starting.

Administrator routes are:

```text
GET  /api/v1/admin/telemetry/storage
GET  /api/v1/admin/telemetry/host?node_id=&resolution=1m|10m|1h&from=&to=
GET  /api/v1/admin/telemetry/traffic?node_id=
POST /api/v1/admin/telemetry/{clear-history,reset-all,checkpoint,incremental-vacuum,full-vacuum}
```

## 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| telemetry open/read-only/corrupt | Control starts; state is unavailable; history endpoints return stable `503 telemetry_unavailable` |
| file total reaches budget | writes pause with `file_budget`; resume below 90% |
| free space reaches floor | writes pause with `low_disk`; resume only after the recovery margin |
| queue is full | `ErrQueueFull`, dropped counter increments, history pauses; report session continues |
| SQLite busy/write timeout | affected batch is dropped, state becomes `write_failed`, later health/guard check may recover |
| unknown resolution or invalid window | `400 invalid_resolution` / `400 invalid_input` |
| clear/reset confirmation mismatch | `400 confirmation_required`; no mutation |
| maintenance request cancelled | caller receives `context.Canceled` or `DeadlineExceeded`; operations use the caller context |
| traffic addition exceeds signed SQLite range | transaction fails; no partial lifetime/month update |
| node telemetry deletion fails or Control exits mid-cleanup | durable job remains and is retried on startup/background pass |

## 5. Good / Base / Bad Cases

- Good: twenty validated reports update one open memory bucket; the completed
  minute produces one `host_1m` row and never changes `control.db`.
- Base: the first report after restart writes zero traffic deltas, then later
  monotonic counters add to lifetime and the current UTC month.
- Bad: a reboot decreases uptime while counters increase. The interval must
  still contribute zero because the old and new kernel counters are unrelated.
- Bad: a reboot occurs during a long disconnect and new uptime exceeds the old
  baseline. A changed boot ID must still force a zero-delta baseline.
- Bad: history is cleared while an open bucket exists. The bucket must also be
  discarded or it will silently restore pre-clear data on the next seal.
- Bad: deleting an old telemetry file while its WAL/read pool is open. Reset
  must checkpoint, close both pools, remove sidecars, recreate, and then swap.

## 6. Tests Required

- Migration: mode 0600, independent schema version, incremental auto-vacuum,
  v1→v2 backup/copy semantics, expected tables, no raw-report table, and
  open/read-only failure behavior.
- Aggregation: multiple seconds remain memory-only; minute seal is weighted;
  new node, month switch, counter reset, reboot, and post-reset baseline.
- Writer: bounded queue/full/drop, cancellation, SQLite busy timeout, batch
  atomicity, concurrent/race run, and read queries while writes are paused.
- Retention: promotion before deletion, two consecutive runs produce the same
  rows, cancellation/interruption is restart-safe, and tiers do not overlap.
- Maintenance: clear preserves ledger, reset recreates the file and schema,
  WAL truncation, incremental/full vacuum safety, and durable restart-safe
  orphan node cleanup.
- Guard/API: both thresholds and recovery hysteresis, exact confirmation/CSRF,
  main/WAL/SHM/oldest/freelist fields, and degraded login/live availability.
- Boundary: enumerate `control.db` tables and reject names/columns representing
  reports, samples, traffic, probe results, rates, CPU, memory, or network data.

## 7. Wrong vs Correct

### Wrong

```go
// Persists every 3-second report and couples live availability to history.
controlDB.Exec(`INSERT INTO reports(node_id, payload) VALUES (?, ?)`, id, report)
if err := telemetry.Write(report); err != nil { return protocolFailure(err) }
```

### Correct

```go
live.acceptReport(nodeID, sessionID, report, now)
history.Observe(nodeID, report) // memory minute bucket + non-blocking bounded queue
// Queue/disk/SQLite failure changes telemetry.State only; the Agent stays live.
```
