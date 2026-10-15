# Bounded TCP Probe Guidelines

## 1. Scope and hard boundary

Use this contract for TCP task configuration, `probe.sync`, `probe.result`,
Agent scheduling, current probe state, probe history, and task cleanup.
The only network action is TCP connect followed immediately by close. Do not
add HTTP, ICMP, TLS negotiation, payload writes, banner reads, shell, terminal,
file access, process execution, plugins, or generic tasks.

## 2. Shared protocol contract

`internal/protocol` owns `MaxProbeTasks=64`, interval 5–3600 seconds, target
validation, `ProbeTask`, versioned complete `ProbeSync`, bounded
`ProbeSyncStatus`, `ProbeResult`, and the failure taxonomy. Targets are a DNS
name or IP plus port 1–65535. Public, private, and loopback unicast are allowed;
unspecified and multicast literals/resolved addresses are rejected.

Control sends a full snapshot after hello and after committed configuration
changes. Agent independently validates every untrusted snapshot. More than 64
tasks rejects the snapshot; invalid tasks are omitted with bounded status and
are never silently clamped. Rejections echo a task ID only when it satisfies
the shared ID contract; malformed IDs become an empty bounded identifier.
Duplicate versions do not restart tasks, older versions are rejected, deletion
cancels the task, and reconnect starts with a fresh session-local scheduler and
full snapshot.

## 3. Agent execution

- One goroutine owns each accepted task, so a task has at most one in-flight
  check. Its timer resets after completion; missed ticks are skipped.
- All tasks share an eight-slot non-blocking semaphore. Saturation skips that
  occurrence rather than queueing catch-up work.
- DNS resolution and all dials share a three-second context deadline. Only the
  first three returned addresses are considered and attempted serially. Each
  dial has at most one second and the first success closes immediately.
- The success path calls only `DialContext` and `Close`; it never calls
  `Read`/`Write`. Cancellation emits no stale result.

## 4. Control admission and memory

`probe.result` is accepted only after envelope session/sequence checks, strict
JSON and taxonomy validation, ±5-minute scheduled-time validation, current
assignment lookup, strictly advancing schedule, per-task half-interval rate
guard, and a bounded five-second connection window. Unknown tasks are rejected
before touching maps. A violation sends `invalid_probe_result` and closes the
session.

Control installs a committed snapshot's version and assignment admission state
before exposing its queued `probe.sync` frame to the writer. This ordering is a
protocol invariant: an immediate Agent acknowledgement or result must never be
checked against the preceding snapshot.

`ProbeHistory` retains at most one latest result per assigned node/task and one
open minute bucket per observed series. It aggregates attempts, success,
failure, and successful latency sum/min/max/count. Telemetry errors never
close WSS or stop Agent scheduling/current state.

## 5. Persistence and APIs

Control schema v5 owns `probe_tasks`, `probe_assignments`, one monotonic config
version row, and task-ID-only cleanup jobs. Task create/update validates the
resulting per-node count in the same transaction; the 65th assignment returns
`ErrProbeTaskLimit`. `control.db` must not contain result, latency, attempts,
success/failure, or history columns.

Telemetry schema v3 owns `probe_1m`, `probe_10m`, and `probe_1h`. Only complete
minutes are queued to the single writer. Rollups are weighted/idempotent and
use the same non-overlapping 24h/7d/30d retention ladder. Clear history removes
host/probe rollups and open buckets but preserves traffic. Reset all recreates
the file and clears current aggregation. Node/task deletion uses idempotent
outbox cleanup; jobs remain until telemetry deletion succeeds.

Administrator routes are documented in `api/README.md`. Every mutation uses
the existing Session/CSRF boundary. History outage returns stable
`telemetry_unavailable`; configuration and current results stay available.

## 6. Required verification

- 65 tasks, interval endpoints/out-of-range, port 0, unspecified/multicast,
  private/public/loopback, duplicate assignments, and transaction rollback.
- Malicious/duplicate/out-of-order snapshots, deletion/reconnect, bounded
  rejection status, DNS >3/slow DNS, slow TCP, semaphore saturation,
  missed-tick skip, cancellation, and a connection double proving no reads or
  writes.
- Unknown task, replay, per-task/connection floods, scheduled skew, latency and
  error taxonomy, and no unbounded map/dimension growth.
- Minute edges, failure-only null latency, weighted averages, rollup
  idempotency, non-overlapping retention, clear/reset, node/task cleanup retry,
  degraded telemetry continuity, and schema inspection proving no probe result
  enters `control.db`.


## Scenario: auto-join future enrolled nodes

### 1. Scope / Trigger

Use for the monitor-style future-node inclusion checkbox, probe DTO/config storage and enrollment assignments. Agent connect-only protocol remains unchanged.

### 2. Signatures

- Control schema10: `probe_tasks.auto_join INTEGER NOT NULL DEFAULT 0 CHECK(auto_join IN (0,1))`.
- Storage `ProbeTask.AutoJoin bool`; JSON DTO/POST `auto_join bool`; PATCH optional `*bool`.
- `ErrProbeAutoJoinLimit` maps to409 `probe_auto_join_limit`.
- `PatchProbeTask(ctx,id,ProbeTaskPatch,now)` merges optional fields inside the storage transaction and returns saved task plus previous node IDs. `ChangeProbeAssignment(ctx,id,nodeID,add,now)` changes a single assignment under the same mutation owner.

### 3. Contracts

- Migrated tasks and omitted POST values defaultfalse; omitted PATCH preserves the previous flag. Toggle changes future-node policy, not existing assignments. Explicit node_ids still owns current assignment replacement.
- Create/update cap enabled auto-join definitions at protocol.MaxProbeTasks in their existing transaction, alongside per-node assignment limits. A rejected mutation changes neither task, assignments nor snapshot version.
- Omitted PATCH assignments are read/preserved inside the transaction, never copied back from a handler's earlier snapshot. Explicit node_ids (including []) still replaces the complete set. Single-assignment endpoints mutate only their requested node against committed state; cleanup/sync uses the transaction's previous/current identities.
- ConsumeEnrollment inserts every enabled task assignment inside the node/credential/token transaction and bumps probe version once if assignments exist. Failure rolls back token consumption, identity, credential, assignments and version. The first Agent hello reads this committed bounded set; no Agent upgrade or auto_join wire field is needed.
- Migration runner backs up schema9 before adding the column. Downgrade requires verified pre-migration control/key backup; older binaries reject schema10.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| 65th enabled auto-join definition |409 probe_auto_join_limit; atomic rollback |
| 65th assignment on an existing node | Existing409 probe_task_limit |
| Unknown/consumed/expired enrollment | Existing enrollment error; no automatic assignments |
| Assignment insert/version failure during enrollment | Entire enrollment transaction rolls back |
| Toggle off | Existing assignments stay; later nodes excluded |

### 5. Good / Base / Bad Cases

Good: a future registered node obtains enabled checks before its first hello. Base: an old task remains false after migration. Bad: implementing the checkbox only in browser memory or retroactively assigning every existing node when it is enabled.

### 6. Tests Required

Schema1–9 migration backups and legacy preservation;64/65 create/update/concurrent admission; old POST/default and PATCH omit/false; enrollment64/version-once/disable; rejected enrollment and injected assignment/version failure rollback; real API enrollment confirms new assignment; a gated-body PATCH/enrollment regression proves omitted node_ids preserves concurrent automatic assignments, while explicit [] clears; assignment endpoints preserve unrelated autojoined nodes.

### 7. Wrong vs Correct

Wrong: consume enrollment, commit, then separately insert automatic assignments. Correct: insert assignments and bump version before the same enrollment transaction commits.
