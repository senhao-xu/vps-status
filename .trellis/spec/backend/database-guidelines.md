# Database Guidelines

## Hard storage boundary

The reviewed design requires two physical SQLite files:

- `control.db`: administrator/session state, node metadata and credential digests, enrollment tokens, probe definitions/assignments, alert and notification configuration, and public-page configuration.
- `telemetry.db`: host rollups, TCP probe rollups, traffic accounting, and telemetry maintenance metadata.

Agent reports, probe results, rates, cumulative traffic values, and historical buckets must never be written to `control.db`. The boundary is defined in the active task's `prd.md` and `design.md` and is more important than convenience or cross-table joins.

## Driver and query pattern

- `internal/storage/control` uses `database/sql` with the pure-Go `modernc.org/sqlite` driver. Keep explicit SQL and transaction ownership in the storage package; do not introduce an ORM, query generator, CGO driver, or second backend without an explicit design change.
- `control.db` uses one open/idle connection so its connection-local PRAGMAs are deterministic for the low-write single-admin workload. Telemetry will define its own writer/read-pool policy in `internal/storage/telemetry`; it must not reuse the control store handle.
- Each database owns its own connection setup and PRAGMAs under its storage package. Current control settings are WAL, `synchronous=NORMAL`, a 5-second busy timeout, foreign keys, a 256-page WAL auto-checkpoint, and a 4 MiB journal-size limit.
- Session creation first takes SQLite's write lock with a conditional administrator update and verifies that the MFA state used during authentication is still current. It then removes expired/revoked rows and enforces at most 32 active sessions per administrator by evicting the oldest rows in the same transaction. TOTP confirmation validates and retains the current active session and revokes other active sessions atomically with the MFA state change.
- Enrollment-token creation prunes expired rows, counts live rows, and inserts in one storage transaction. Consumed rows remain until their original expiry so installation status can link to the stable node ID. `control.db` permits at most 256 globally unconsumed, unexpired enrollment tokens and returns `ErrEnrollmentTokenLimit` before a 257th active row can be inserted.
- Control schema v3 keeps administrator-owned node name/group/hidden fields and low-frequency hello facts (hostname, OS, kernel, architecture, virtualization). `UpdateNodeFacts` writes only when those facts change. Schema v4 adds a node-ID-only telemetry cleanup outbox. Schema v5 adds only TCP definitions/assignments, a snapshot version, and task-ID-only cleanup jobs. These outboxes are not telemetry storage. Live reports, probe results/latency, boot IDs, rates, cumulative counters, and online transitions never update control rows.
- Control schema v6 adds encrypted notification channel configuration, bounded alert rules/current state, and a 4,096-row transition/test delivery outbox. Alert event payloads are bounded state-transition records, never raw reports or history. Completed rows are pruned; every table and assignment set has an application hard limit.
- Control schema v7 adds singleton `public_page` presentation settings and an
  explicit `public_nodes` allowlist. Absence from `public_nodes` means private;
  neither table contains host facts, reports, traffic, probe results, or history.
- Control schema v8 adds singleton `appearance` with a checked built-in
  `default_theme` enum. Its initial value is Emerald; see
  [Built-in Appearance](./appearance-guidelines.md).

## Migrations

- Keep control migrations under `migrations/control/` and telemetry migrations under `migrations/telemetry/`.
- Each stream has an independent monotonic schema version and runs before its package becomes available.
- `internal/storage/control.Open` checkpoints/closes an existing database and creates a mode-0600 backup before the first schema mutation, then applies each embedded migration and its version record in one transaction. It rejects databases newer than the running binary. Do not promise destructive down migrations.
- Migrations and retention operations must be restart-safe and idempotent where interruption is possible.
- Never use `ATTACH` to create a cross-file foreign key or atomic dependency.

## SQLite operating contract

- Enable WAL, a busy timeout, and `synchronous=NORMAL` through the owning package.
- `telemetry.db` uses one serialized writer, bounded batch queues, bounded WAL/checkpoint behavior, chunked expiry, and controlled page reclamation.
- Select `auto_vacuum=INCREMENTAL` when telemetry storage is first created; setting it later does not reclaim an existing layout.
- A telemetry open/write/space failure degrades history only. Authentication, configuration, in-memory current state, and real-time alert evaluation continue.

## Schema and query shape

- Use stable node/task identifiers across files as logical dimensions, not foreign keys.
- Use compact typed columns for fixed host and probe rollups rather than EAV or per-row JSON.
- Time-bucket keys begin with the dimension and bucket timestamp needed by reads and chunked retention.
- Roll up complete windows idempotently before deleting finer rows: 1m -> 10m -> 1h.
- Deleting a node/task schedules idempotent telemetry cleanup; it cannot require a cross-file transaction.
  Node deletion and cleanup-job insertion are one control transaction. The
  external idempotent deletion is retried on startup and in bounded batches;
  only success removes the job.

## Verification

- Tests must open both databases in separate temporary paths and assert that telemetry writes do not change control tables.
- Failure tests cover read-only telemetry, interrupted rollup/cleanup, WAL growth, counter reset, and disk-guard recovery.
- Schema changes require migration tests from every supported previous schema version.
- Schema v3 requires an explicit v2→v3 backup test that proves the backup does not contain v3 columns.
- Schema v4 requires an explicit v3→v4 backup test that proves the backup does not contain the cleanup table.
- Schema v5 requires an explicit v4→v5 backup test that proves the backup does not contain probe configuration or task-cleanup tables.
- Schema v6 requires an explicit v5→v6 backup test that proves the backup does not contain notification, alert, or delivery tables.
- Schema v7 requires an explicit v6→v7 backup test that proves the backup does
  not contain `public_page` or `public_nodes`.
- Schema v8 requires a v7-to-v8 backup test proving the mode-0600 backup lacks
  `appearance`; older binaries still reject newer schemas.
- Enrollment-token tests must cover concurrent admission at the 256-row boundary and prove that consumption and expiry release capacity.
