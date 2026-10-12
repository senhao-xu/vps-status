# Research: Competitor telemetry storage and retention

- Query: How do monitor-probe/monitor, Komari, Beszel, and Nezha persist configuration, live reports, historical host metrics, probe results, and traffic counters; how do they control retention, WAL growth, reclaim deleted space, and react to low disk space? What storage boundary best keeps this project's configuration database small?
- Scope: mixed (official repository source plus official project/runtime references)
- Date: 2026-10-09

## Findings

### Executive comparison

| Project | Configuration vs telemetry | Exact/live samples | Persisted history and probes | Retention / downsampling | File-size safeguards |
|---|---|---|---|---|---|
| monitor-probe/monitor | One SQLite file contains settings, nodes, credentials, traffic ledger, host metrics, probe definitions, probe results, sessions, and hourly rollups. | Every report updates in-memory live state; one aggregate host row is written per minute. | Host minutes and TCP probe results have parallel hourly tables. Traffic totals are a compact, one-row-per-node ledger. | Minute detail is kept at most 7 days; complete hours are folded to hourly rows; total history defaults to 30 days and is configurable from 1 to 365 days. | WAL auto-checkpoint 256 pages, 1 MiB journal limit, explicit checkpoint, per-node/batched pruning, size/free-page reporting, and manual `VACUUM`. No application-level low-disk write pause was found. |
| Komari | Strongest physical separation in this set: `komari.db` is the main/configuration database and the metric store defaults to `metrics.db` (or MySQL/PostgreSQL). Ping results also use the metric store. | Exact samples are never persisted: newest 1 minute is directly addressable in memory and the remaining 9 minutes are losslessly byte-encoded in memory. Reports and ping results are queued and batch-written every 3 seconds, but the persistent writes are rollups. | Normalized series/dictionaries plus 1m/5m/1h/1d rollups; host, traffic deltas/counters, and ping latency/loss share the metric system. | Store ladder: 1m for 600 min, 5m for 3000 min, 1h for 600 h, then 1d terminal. Each metric has its own ultimate retention; built-ins currently default to only 1 day, so the effective default is 1m for 10h then 5m through 1d. Compaction every 5m; retention cleanup hourly. | SQLite uses one writer plus read pool, WAL, 4,000-page auto-checkpoint and 4 MiB journal cap. Storage size includes DB/WAL/SHM; explicit reclaim checkpoints, vacuums, optimizes, and checkpoints again. No automatic low-free-disk pause was found. |
| Beszel | No dedicated metric database is opened by Beszel. Configuration/auth collections and `system_stats`, `container_stats`, and `network_monitor_stats` are all collections on the same embedded PocketBase app/database. | The system path persists one `1m` record; live UI state is also maintained by the running system manager. | Host, container, and network-monitor probe records use the same five record types. Each coarser record is created by averaging the preceding tier. | 1m for 1h; 10m for 12h; 20m for 1d; 120m for 7d; 480m for 30d. Cleanup hourly. Alert history is trimmed to 200 per user once it exceeds 250. | The inspected Beszel code has no app-owned WAL limit, vacuum/compaction routine, or low-disk guard; those details are left to PocketBase/SQLite. The aggressive 30-day ladder is its main size control. |
| Nezha | Main configuration stays in `data/sqlite.db`; an optional, separately located VictoriaMetrics-backed TSDB holds server metrics and service-probe metrics. Separation is not absolute: hourly traffic-delta rows remain in SQLite, and service history falls back to SQLite when TSDB is disabled/unavailable. | Current server state lives in memory. With TSDB enabled, server reports become TSDB samples; storage-level dedup defaults to 30 seconds. | TSDB stores 17 host series and service delay/status series. A 512-row buffer flushes every 5 seconds. SQLite stores hourly traffic deltas needed by traffic alert periods. | TSDB retention defaults to 30 days. No Nezha-owned multi-resolution downsampling ladder was found; VictoriaMetrics manages its storage internally. SQLite service fallback uses 15-minute summaries, but no explicit age cleanup for that table was found in the inspected path. | TSDB reserves 1 GiB free disk by default, limits VictoriaMetrics caches to 256 MiB, pauses/discards new history writes while storage is read-only/full, and keeps dashboard/query/alert operation alive. SQLite `VACUUM` plus TSDB flush runs weekly. |

### 1. monitor-probe/monitor

#### Physical layout and write path

`Db::open` opens one path, and the same schema declares low-frequency configuration/state (`setting`, `node`, `session`, `ping_task`, `ping_node`) together with high-growth telemetry (`metric`, `ping_record`, `metric_hour`, `ping_hour`) and the compact traffic ledger (`traffic`). This is a deliberately tuned single-file design, not configuration/telemetry isolation ([`src/db.rs:34-207`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L34-L207), [`src/db.rs:969-986`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L969-L986)).

The report handler keeps the newest report on the connected agent entry, accumulates all reports during a minute, and writes only the completed minute row. Thus a 3-second Agent report interval does not imply a SQLite insert every 3 seconds ([`src/agent_ws.rs:753-805`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/agent_ws.rs#L753-L805)). Probe results are buffered for a minute and inserted as one transaction, reducing page/commit overhead ([`src/db.rs:2120-2148`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L2120-L2148)).

Traffic is intentionally not reconstructed from expiring metric history. The `traffic` table stores raw-counter baselines plus lifetime, month, and day counters in one row per node; it is booked about once per minute and survives Agent/Control restarts ([`src/db.rs:115-129`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L115-L129), [`src/db.rs:1410-1424`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L1410-L1424)).

#### Retention and reclaim

- Minute detail is capped at 7 days.
- Overall retention defaults to 30 days and is clamped to 1-365 days.
- A source comment estimates that 100 nodes with four probes need about 145 MiB for one year of hourly data versus about 6.6 GiB if minute rows were kept for the year ([`src/db.rs:772-788`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L772-L788), [`src/db.rs:2281-2289`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L2281-L2289)).
- Complete hours are folded transactionally into `metric_hour` and `ping_hour`; averages are weighted and probe rows preserve answered/lost counts, latency median, and bounds ([`src/db.rs:1704-1733`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L1704-L1733), [`src/db.rs:1756-1804`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L1756-L1804)).
- Pruning is deliberately incremental: minute rows are removed at most one node-day per statement, locks are released between chunks, and hourly rows are removed per node. Traffic totals are exempt ([`src/db.rs:1807-1872`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L1807-L1872)).
- Housekeeping runs once at startup and then hourly; it rolls up before pruning so a lagging rollup loses disk headroom rather than history ([`src/main.rs:760-804`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/main.rs#L760-L804)).

SQLite is in WAL mode with `synchronous=NORMAL`, a 256-page auto-checkpoint, and a 1 MiB journal-size limit. A chart read triggers a best-effort truncate checkpoint if the WAL exceeds 4 MiB. The data page reports main-file, WAL, freelist, oldest timestamp, and per-table rows. Explicit `VACUUM` reclaims pages and is followed by a truncate checkpoint ([`src/db.rs:34-45`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L34-L45), [`src/db.rs:993-1018`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L993-L1018), [`src/db.rs:2291-2336`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L2291-L2336), [`src/db.rs:2359-2381`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L2359-L2381)).

### 2. Komari

#### Physical layout and write path

Komari defaults the main GORM/SQLite database to `./data/komari.db`, while its always-enabled metric store defaults to `./data/metrics.db` and can instead target MySQL or PostgreSQL. Its compatibility layer states that normal runtime reads/writes for host, GPU, and ping history have fully moved out of the legacy main-database tables into the metric store ([`cmd/root.go:38-40`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/cmd/root.go#L38-L40), [`internal/metricstore/config.go:25-40`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/config.go#L25-L40), [`database/records/records.go:11-16`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/database/records/records.go#L11-L16)).

Exact samples are memory-only. One minute is kept in an ordinary searchable slice, the prior nine minutes are losslessly byte-encoded, and only minute summaries are eventually persisted. Store comments explicitly state that exact samples are never persisted ([`pkg/metric/raw_points.go:13-18`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/raw_points.go#L13-L18), [`pkg/metric/raw_points.go:149-175`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/raw_points.go#L149-L175), [`pkg/metric/store.go:60-76`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/store.go#L60-L76), [`pkg/metric/migrations.go:9-14`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/migrations.go#L9-L14)).

Agent reports and ping records share a bounded queue (512 entries) and a 3-second batch flush. Ping records are mapped to latency and loss metric points tagged by task ID, so probe history obeys the same rollup/retention machinery as host metrics ([`internal/metricstore/report_batcher.go:39-46`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/report_batcher.go#L39-L46), [`internal/metricstore/report_batcher.go:206-239`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/report_batcher.go#L206-L239), [`internal/metricstore/ping_records.go:29-62`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/ping_records.go#L29-L62)). Network cumulative counters and reset-aware traffic deltas are metric series, not a separate permanent ledger in the main database ([`internal/metricstore/report_mapping.go:14-31`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/report_mapping.go#L14-L31)).

#### Retention and reclaim

The default store ladder is:

- exact: 10 minutes, memory only;
- 1 minute: 600 minutes (10 hours);
- 5 minutes: 3,000 minutes (50 hours);
- 1 hour: 600 hours (25 days);
- 1 day: terminal tier, nominally 100 years so the metric definition supplies the real final cutoff.

These values and the configurable first three tier windows are defined in [`internal/metricstore/config.go:11-40`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/config.go#L11-L40) and [`internal/metricstore/config.go:184-196`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/config.go#L184-L196). However, built-in metric definitions currently default to a one-day final retention. The per-metric policy caps/omits coarser tiers that do not extend retention, making the practical fresh-install default 1m through 10h and 5m through 1d, unless the metric retention is changed ([`internal/metricstore/definitions.go:11-12`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/definitions.go#L11-L12), [`internal/metricstore/definitions.go:60-85`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/definitions.go#L60-L85), [`pkg/metric/rollup.go:79-102`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/rollup.go#L79-L102)).

The server seals minute/coarse buckets every 5 minutes and runs retention deletion hourly. Cleanup groups metrics with the same tier/cutoff into shared deletes inside one transaction ([`internal/server/runtime.go:180-192`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/server/runtime.go#L180-L192), [`internal/server/runtime.go:212-241`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/server/runtime.go#L212-L241), [`pkg/metric/store.go:1241-1323`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/store.go#L1241-L1323)).

For SQLite, Komari deliberately serializes writes on one primary connection and opens a separate WAL read pool. Defaults are a 4,000-page WAL auto-checkpoint and 4 MiB journal cap. Explicit storage inspection includes DB/WAL/SHM, while reclaim deletes orphaned dictionary rows, checkpoints, vacuums, optimizes, and checkpoints again ([`internal/metricstore/config.go:87-107`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/internal/metricstore/config.go#L87-L107), [`pkg/metric/store.go:250-337`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/store.go#L250-L337), [`pkg/metric/maintenance.go:47-75`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/maintenance.go#L47-L75), [`pkg/metric/maintenance.go:94-133`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/pkg/metric/maintenance.go#L94-L133)).

### 3. Beszel

#### Physical layout and write path

Beszel embeds one PocketBase application and uses a single application data directory (`beszel_data`). It does not open a second metrics database. Host, container, and network-monitor statistics are PocketBase collections accessed through the same `core.App`/`app.DB()` interface as systems, users, alerts, and other configuration records ([`internal/cmd/hub/hub.go:37-65`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/cmd/hub/hub.go#L37-L65), [`internal/hub/hub.go:27-53`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/hub/hub.go#L27-L53), [Docker Compose volume](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/supplemental/docker/hub/docker-compose.yml#L4-L12)).

Each system update writes a `system_stats` record of type `1m` and, when present, one `container_stats` record. Network-monitor probes write one `1m` record per changed monitor with min/max/sum and success/total counts ([`internal/hub/systems/system.go:250-293`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/hub/systems/system.go#L250-L293), [`internal/hub/systems/system.go:528-569`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/hub/systems/system.go#L528-L569)).

#### Retention and reclaim

Every 10 minutes Beszel builds longer records by averaging:

- 1m -> 10m;
- 10m -> 20m;
- 20m -> 120m;
- 120m -> 480m.

Host and container collections follow this chain per system; network-monitor history follows it per monitor ([`internal/records/records.go:41-70`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/records/records.go#L41-L70), [`internal/records/records.go:71-165`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/records/records.go#L71-L165), [`internal/records/records.go:168-226`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/records/records.go#L168-L226)).

An hourly job deletes 1m after 1h, 10m after 12h, 20m after 1d, 120m after 7d, and 480m after 30d from all three statistics collections. It also trims each user's alert history to 200 when the count exceeds 250 ([`internal/hub/hub.go:142-148`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/hub/hub.go#L142-L148), [`internal/records/records_deletion.go:40-91`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/records/records_deletion.go#L40-L91)).

No Beszel-owned `PRAGMA wal_checkpoint`, journal-size cap, `VACUUM`, incremental-vacuum, maximum database size, or low-free-space guard was found in this repository snapshot. PocketBase may apply its own SQLite behavior, but that is a versioned dependency concern rather than a Beszel storage contract. Therefore Beszel demonstrates a compact retention ladder, not physical isolation of configuration from history.

### 4. Nezha

#### Physical layout and write path

Nezha's dashboard defaults its main GORM/SQLite file to `data/sqlite.db`. An optional `tsdb.data_path` enables a physically separate VictoriaMetrics storage directory; its config includes retention, minimum free disk, maximum cache memory, write buffer, and flush interval ([`cmd/dashboard/main.go:119-143`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/cmd/dashboard/main.go#L119-L143), [`model/config.go:136-144`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/model/config.go#L136-L144), [`service/singleton/singleton.go:80-100`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/singleton.go#L80-L100)).

TSDB defaults are 30-day retention, 1 GiB reserved free disk, 256 MiB maximum VictoriaMetrics cache allocation, 30-second deduplication, 512 buffered rows, and a 5-second buffer flush. VictoriaMetrics also receives a 5-second internal data flush interval ([`pkg/tsdb/config.go:5-34`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/config.go#L5-L34), [`pkg/tsdb/tsdb.go:41-53`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/tsdb.go#L41-L53), [`pkg/tsdb/writer.go:13-64`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/writer.go#L13-L64)). It persists 17 server series, including rates and cumulative transfers, and two service-probe series (delay and status) ([`pkg/tsdb/writer.go:92-150`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/writer.go#L92-L150), [`pkg/tsdb/writer.go:152-216`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/writer.go#L152-L216)).

If no TSDB path is configured, server historical charts are unavailable, while service-monitor history is auto-migrated and stored in main SQLite. When TSDB initializes, Nezha drops that legacy `service_histories` table without migration. If TSDB startup fails for disk-full reasons, it prepares the SQLite service-history fallback and continues ([`service/singleton/tsdb.go:16-73`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/tsdb.go#L16-L73), [`cmd/dashboard/controller/server.go:545-556`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/cmd/dashboard/controller/server.go#L545-L556), [`service/singleton/servicesentinel.go:106-129`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/servicesentinel.go#L106-L129)).

Separation is incomplete because hourly traffic deltas remain `Transfer` rows in the main SQLite database. Their retention is demand-driven by traffic alert cycles: rows not needed by any applicable traffic rule are deleted; with no such rule all transfer rows are deleted ([`service/singleton/singleton.go:114-151`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/singleton.go#L114-L151), [`service/singleton/singleton.go:154-207`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/singleton.go#L154-L207)).

#### Disk behavior

The TSDB asks VictoriaMetrics to become read-only below the configured free-space floor. Nezha then discards new buffered/history writes, does not grow the in-memory buffer, and keeps existing queries, dashboard, and alerts running. A runtime disk-full panic similarly disables further TSDB writes until restart rather than taking down the Control process ([`pkg/tsdb/tsdb.go:121-168`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/tsdb.go#L121-L168), [`pkg/tsdb/disk_guard_test.go:41-109`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/pkg/tsdb/disk_guard_test.go#L41-L109)). Weekly maintenance runs SQLite `VACUUM` and a TSDB flush ([`service/singleton/servicesentinel.go:231-240`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/servicesentinel.go#L231-L240), [`service/singleton/singleton.go:209-228`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/singleton.go#L209-L228)).

### Recommendation for this project

The requested property should be a hard physical boundary, not only separate tables. Use two SQLite files plus an in-memory live layer:

```text
Control process
├── memory
│   ├── latest host report / online state
│   ├── latest TCP result
│   └── open aggregation buckets
├── control.db                 small, durable, backed up by default
│   ├── administrator/session
│   ├── node metadata + credential digests
│   ├── probe definitions + node assignments
│   ├── alert rules/state + notification configuration
│   └── public-page configuration
└── telemetry.db               bounded, optional history backup
    ├── host rollup buckets
    ├── TCP probe rollup buckets
    └── compact lifetime/month traffic ledger
```

Key contracts:

1. Do not persist 3-second raw reports. Update the live state and current minute accumulator in memory; write a completed 1-minute bucket in batches.
2. Persist probe definitions in `control.db`, but persist latency/loss samples and rollups only in `telemetry.db`.
3. Keep lifetime/current-month traffic accounting as a compact durable ledger in `telemetry.db`, separate from expiring rollup tables. This makes the requested physical boundary strict: no Agent report, probe result, or derived traffic value can grow `control.db`. Traffic thresholds must read the ledger rather than reconstruct totals from expiring history.
4. Never require a cross-file foreign key or atomic transaction. Telemetry uses stable node/task IDs as logical dimensions; node/task deletion queues idempotent history cleanup. The Control must remain usable if `telemetry.db` is missing, corrupt, read-only, cleared, or temporarily closed.
5. Default retention should prioritize a small file: 1-minute buckets for 24 hours, 10-minute buckets through 7 days, and 1-hour buckets through 30 days. A 90/365-day hourly option can be explicit. Both host and TCP histories use the same ladder. If tiers do not overlap after promotion, this is about 2,856 retained host buckets per node and the same upper bound per node/probe series before gaps.
6. `Clear history` should delete/recreate only the rollup tables in `telemetry.db` and preserve its compact traffic ledger. A separate, explicitly destructive `Reset all telemetry` operation may recreate the entire file. Configuration, node identities, credential digests, and rules in `control.db` survive either operation. Default backups include `control.db`; telemetry/history backup is a separate option.
7. Bound SQLite sidecars independently: WAL mode, one telemetry writer, a read-only pool if needed, `wal_autocheckpoint`, `journal_size_limit`, and explicit truncate checkpoints after large maintenance. Show main/WAL/SHM bytes and oldest retained bucket in the admin UI.
8. Expiry deletion must be time-keyed and chunked by tier/node so it does not hold the only writer for seconds. Deleting rows does not shrink a SQLite file: use `auto_vacuum=INCREMENTAL` from database creation plus controlled `incremental_vacuum`, or a scheduled/manual full `VACUUM` when free-page ratio and free disk make it safe.
9. Add a Nezha-style disk guard. At a configurable minimum free-space threshold or maximum telemetry-file budget, stop accepting historical buckets and clear pending history buffers, but continue live state, configuration, authentication, online/offline detection, and alert evaluation. Emit a durable operator alert and resume only with hysteresis.

This is closer to Komari's physical split, monitor's memory/minute/hour and traffic-ledger model, Beszel's aggressive default retention, and Nezha's graceful low-disk degradation than to any one competitor unchanged.

## Files Found

- `monitor/src/db.rs` — single-file schema, traffic ledger, rollup/prune logic, WAL/checkpoint/statistics/vacuum.
- `monitor/src/agent_ws.rs` — in-memory live report handling, per-minute host aggregation, traffic booking.
- `monitor/src/main.rs` — hourly rollup/prune scheduling.
- `komari/cmd/root.go` — main `komari.db` default.
- `komari/database/records/records.go` — runtime telemetry delegation away from legacy main-DB tables.
- `komari/internal/metricstore/config.go` — `metrics.db`, backend selection, and rollup ladder.
- `komari/internal/metricstore/definitions.go` — per-metric retention and one-day built-in default.
- `komari/internal/metricstore/report_batcher.go` — report/ping queues and 3-second batching.
- `komari/internal/metricstore/ping_records.go` — probe latency/loss mapping into metric storage.
- `komari/internal/server/runtime.go` — 5-minute compaction and hourly retention schedules.
- `komari/pkg/metric/raw_points.go` — 10-minute memory-only exact-sample window.
- `komari/pkg/metric/store.go` — normalized storage, cleanup grouping, SQLite pools/tuning.
- `komari/pkg/metric/maintenance.go` — size inspection and backend-specific physical reclaim.
- `beszel/internal/cmd/hub/hub.go` — embedded PocketBase/data-directory initialization.
- `beszel/internal/hub/systems/system.go` — 1-minute host/container/network-monitor persistence.
- `beszel/internal/records/records.go` — multi-stage averaging/rollups.
- `beszel/internal/records/records_deletion.go` — tier expiry and bounded alert history.
- `nezha/cmd/dashboard/main.go` — main SQLite path and TSDB initialization order.
- `nezha/model/config.go` — external TSDB configuration boundary.
- `nezha/pkg/tsdb/config.go` — retention, memory, disk, dedup, and buffering defaults.
- `nezha/pkg/tsdb/tsdb.go` — VictoriaMetrics open options and low-disk degradation.
- `nezha/pkg/tsdb/writer.go` — buffered host/service metric series writes.
- `nezha/service/singleton/tsdb.go` — enablement and SQLite fallback behavior.
- `nezha/service/singleton/singleton.go` — main-SQLite traffic rows, demand-driven cleanup, and vacuum.

## External References

- monitor-probe/monitor inspected commit: [`d8d8283`](https://github.com/monitor-probe/monitor/tree/d8d8283ee9a637b3896f7e89808f8610a4462076).
- Komari inspected commit: [`e48aa1e`](https://github.com/komari-monitor/komari/tree/e48aa1e76931132fb5dc93ce53763dd59df83d9f).
- Beszel inspected commit: [`bc2278e`](https://github.com/henrygd/beszel/tree/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3); PocketBase dependency is v0.40.4 in that snapshot.
- Nezha inspected commit: [`9881c47`](https://github.com/nezhahq/nezha/tree/9881c47f0cc92256ba203919da5a25fab79a6bad); VictoriaMetrics dependency is v1.151.0 in that snapshot.
- SQLite WAL: <https://sqlite.org/wal.html>.
- SQLite auto-vacuum / incremental vacuum: <https://sqlite.org/pragma.html#pragma_auto_vacuum> and <https://sqlite.org/pragma.html#pragma_incremental_vacuum>.

## Related Specs

- `.trellis/spec/backend/database-guidelines.md` — currently a placeholder; no project-specific ORM, migration, transaction, naming, WAL, or retention contract exists yet.
- `.trellis/spec/backend/index.md` — requires actual project conventions to replace placeholders once the design is chosen.
- `.trellis/tasks/10-09-server-monitor-research/research/monitor-komari-deep-dive.md` — prior architecture/security comparison of monitor and Komari.
- `.trellis/tasks/10-09-server-monitor-research/research/competitor-notes.md` — broader product comparison and MVP boundary.

## Caveats / Not Found

- Findings are source snapshots, not guarantees about later releases. Commit hashes above are required when reproducing the conclusions.
- No workload benchmark was run. The 145 MiB/6.6 GiB figures are monitor's own source comments, not measurements of this project.
- Beszel does not expose its SQLite tuning in application code. PocketBase v0.40.4 may manage WAL/checkpoint/backup behavior internally, but no Beszel-level contract for WAL caps, vacuum, or low-disk degradation was found.
- Komari's nominal rollup ladder is easy to misread: built-in metric definitions currently default to one day, so a fresh install does not retain 25 days merely because the 1-hour store tier is configured for 600 hours.
- Nezha's separation is optional and partial. Main-SQLite traffic deltas and SQLite service fallback mean it should not be copied as proof that configuration storage can never grow from telemetry.
- Nezha delegates compaction and persistence layout to embedded VictoriaMetrics. No Nezha-owned fixed multi-resolution downsampling tiers comparable to monitor/Komari/Beszel were found.
- SQLite `auto_vacuum=INCREMENTAL` must be chosen correctly when the telemetry database is created (or applied with the documented rebuild procedure); merely setting it later on an established database does not immediately reclaim existing pages.
