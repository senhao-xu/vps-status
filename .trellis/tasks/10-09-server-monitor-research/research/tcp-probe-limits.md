# Research: Competitor TCP probe task limits and anti-abuse controls

- Query: What per-node/per-Agent probe limits are actually enforced by monitor-probe/monitor, Komari, Beszel, and Nezha, and where are they enforced?
- Scope: mixed (official source repositories, fixed commits)
- Date: 2026-10-09

## Findings

### Executive comparison

| Project | Hard tasks per node | Enforced interval | TCP timeout | Concurrency / dispatch | DNS address limit | Address classes | TCP payload / banner |
|---|---:|---|---|---|---|---|---|
| monitor | **64** | **5–3600 s** | 900 ms per attempted address | One serial loop per task; at most 64; Hub also admits at most 128 results per 5 s/session | **3** addresses | No public/private/loopback filter found | Connect then close; no write/read |
| Beszel | **No cap found** | **1–3600 s at Hub DB/API schema**; Agent maps `<1` to 30 s | 3 s total TCP probe budget | One serial schedule per task, but no global task/concurrency cap found | No numeric cap; tries resolver results within shared 3 s budget | No public/private/loopback filter found | Connect then close; no write/read |
| Komari | **No cap found** | Positive integer seconds; create rejects only `0`, scheduler skips `<=0`; no maximum found | 3 s per attempt; a successful high-latency sample can cause up to 3 retries | Hub fans out tasks with goroutines; Agent starts a goroutine for every received task; no global limiter found | First resolved address only | No public/private/loopback filter found | Connect then close; no write/read |
| Nezha | **No cap found** | `0` becomes 30 s; any positive `uint64` is accepted; no maximum found | 10 s TCP dial | Dashboard schedules every service; Agent starts a goroutine for every received task; no probe-specific limiter found | First resolved address only | No public/private/loopback filter found | Connect then close; no write/read |

Only `monitor` implements the complete abuse-control pattern of a server-side per-node assignment cap plus an independent Agent-side backstop. The other three products rely mainly on administrator trust, probe timeout, and/or a global Agent opt-out rather than a per-node task quota.

### 1. monitor-probe/monitor

Inspected commits:

- Control: [`d8d8283ee9a637b3896f7e89808f8610a4462076`](https://github.com/monitor-probe/monitor/tree/d8d8283ee9a637b3896f7e89808f8610a4462076)
- Agent: [`c85572170630b4d8558a7f0f8c48a2aeee078a23`](https://github.com/monitor-probe/agent/tree/c85572170630b4d8558a7f0f8c48a2aeee078a23)

Hard task cap and interval:

- Control defines `MAX_PROBES_PER_NODE = 64` and `MIN_PROBE_INTERVAL = 5` seconds. It checks the resulting database assignments in the write transaction and refuses an edit that would leave any node above 64; this is server enforcement, not a UI hint ([`src/db.rs:1905-1921`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L1905-L1921), [`src/db.rs:1979-2015`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/db.rs#L1979-L2015)).
- The API refuses intervals outside 5–3600 seconds and validates an explicit nonzero TCP port ([`src/api.rs:1310-1321`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/api.rs#L1310-L1321), [`src/api.rs:1323-1348`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/api.rs#L1323-L1348)).
- The Agent independently truncates an untrusted Hub list to 64 and clamps every received interval to 5–3600 seconds. Therefore a compromised Hub cannot bypass these two bounds by talking directly to the Agent ([`src/main.rs:413-449`](https://github.com/monitor-probe/agent/blob/c85572170630b4d8558a7f0f8c48a2aeee078a23/src/main.rs#L413-L449)).

Timeout, DNS, and concurrency:

- Each task owns one loop and runs its checks serially. Missed ticks use `Delay`, preventing a stalled check from producing a burst ([`src/main.rs:447-478`](https://github.com/monitor-probe/agent/blob/c85572170630b4d8558a7f0f8c48a2aeee078a23/src/main.rs#L447-L478)).
- DNS resolution has a 900 ms deadline. A resolved name contributes at most three addresses; connections are attempted serially with a 900 ms deadline each ([`src/main.rs:482-532`](https://github.com/monitor-probe/agent/blob/c85572170630b4d8558a7f0f8c48a2aeee078a23/src/main.rs#L482-L532), [`src/main.rs:535-551`](https://github.com/monitor-probe/agent/blob/c85572170630b4d8558a7f0f8c48a2aeee078a23/src/main.rs#L535-L551)).
- Control rate-limits accepted probe results to 128 per 5-second session window (`2 × 64`), in addition to controlling task assignment ([`src/agent_ws.rs:31-39`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/agent_ws.rs#L31-L39), [`src/agent_ws.rs:240-256`](https://github.com/monitor-probe/monitor/blob/d8d8283ee9a637b3896f7e89808f8610a4462076/src/agent_ws.rs#L240-L256)).

Target behavior:

- Control checks only `host:port` syntax and a nonzero `uint16` port; no rejection of RFC1918, loopback, link-local, or other non-public destinations is present in that validation path.
- TCP probing uses `TcpStream::connect` and immediately drops the returned stream. It sends no application payload and reads no banner ([`src/main.rs:544-551`](https://github.com/monitor-probe/agent/blob/c85572170630b4d8558a7f0f8c48a2aeee078a23/src/main.rs#L544-L551)).

### 2. Beszel

Inspected commit: [`bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3`](https://github.com/henrygd/beszel/tree/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3).

Task count and interval:

- No per-system task-count maximum was found. The Agent's full sync builds a map for every supplied configuration and starts every new task; it validates only that the task ID is nonempty ([`agent/network_monitor.go:35-71`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor.go#L35-L71), [`agent/network_monitor.go:103-133`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor.go#L103-L133)).
- PocketBase's `network_monitors.interval` field enforces an integer range of 1–3600 seconds, so normal API writes receive a server-side schema check ([`internal/migrations/0_collections_snapshot_0_20_0.go:2025-2036`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/migrations/0_collections_snapshot_0_20_0.go#L2025-L2036)). The web form repeats the same 1–3600 validation ([`monitor-dialog.tsx:53-68`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/site/src/components/network-monitors-table/monitor-dialog.tsx#L53-L68)).
- The Agent does not repeat the upper bound. If handed an interval below one second through the sync protocol, it substitutes 30 seconds; otherwise it uses the received interval ([`agent/network_monitor_schedule.go:10-24`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor_schedule.go#L10-L24)).

Timeout, DNS, and concurrency:

- Each monitor has its own scheduling goroutine. Checks for the same monitor are serialized and share an in-flight run, but there is no global semaphore or maximum monitor count in the inspected manager/scheduler ([`agent/network_monitor_task.go:63-119`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor_task.go#L63-L119), [`agent/network_monitor_schedule.go:27-53`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor_schedule.go#L27-L53)).
- TCP resolution and all address attempts share one 3-second context deadline. All resolver results may be tried; there is no numeric address cap, but the remaining deadline is divided among remaining addresses ([`agent/network_monitor_probe.go:43-79`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor_probe.go#L43-L79)). HTTP probes use a 10-second client timeout ([`internal/entities/monitor/monitor.go:5-7`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/internal/entities/monitor/monitor.go#L5-L7), [`agent/network_monitor.go:23-28`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor.go#L23-L28)).

Target behavior:

- No destination-address class filter was found before `LookupHost`/`DialContext`; private and loopback targets are therefore reachable from the Agent.
- TCP connects and closes without a write or read, so it neither sends a payload nor collects a banner ([`agent/network_monitor_probe.go:68-76`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor_probe.go#L68-L76)). HTTP monitoring is different: it sends a GET request and checks the status ([`agent/network_monitor_probe.go:118-137`](https://github.com/henrygd/beszel/blob/bc2278e7e9820825ce990fb2fe7fca4b9fe9a1c3/agent/network_monitor_probe.go#L118-L137)).

### 3. Komari

Inspected commits:

- Control: [`e48aa1e76931132fb5dc93ce53763dd59df83d9f`](https://github.com/komari-monitor/komari/tree/e48aa1e76931132fb5dc93ce53763dd59df83d9f)
- Agent: [`828afafe7fb5b1b58c1232094e1e46320719016e`](https://github.com/komari-monitor/komari-agent/tree/828afafe7fb5b1b58c1232094e1e46320719016e)
- Web UI: [`9d2c144fcbde53858e175ecd70287fa6428048ef`](https://github.com/komari-monitor/komari-web/tree/9d2c144fcbde53858e175ecd70287fa6428048ef)

Task count and interval:

- No per-node or global task-count limit was found. `AddPingTask` persists the supplied clients and interval directly, and `GetPingTasksByClient` returns every matching task ([`database/tasks/ping.go:15-47`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/database/tasks/ping.go#L15-L47), [`database/tasks/ping.go:97-114`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/database/tasks/ping.go#L97-L114)).
- The create RPC requires a nonzero interval but has no minimum beyond that and no maximum; edit writes the supplied interval without an equivalent nonzero check ([`web/rpc/jsonrpc/admin.ping.go:42-62`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/web/rpc/jsonrpc/admin.ping.go#L42-L62), [`web/rpc/jsonrpc/admin.ping.go:79-95`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/web/rpc/jsonrpc/admin.ping.go#L79-L95)). The scheduler ignores `<=0` and otherwise schedules integer-second intervals, making 1 second the fastest effective positive value without an intentional anti-abuse floor ([`utils/pingSchedule.go:33-53`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/utils/pingSchedule.go#L33-L53)).
- The UI's numeric inputs specify neither `min` nor `max`, so there is no additional UI-only task or interval quota ([`src/pages/admin/pingTask.tsx:183-192`](https://github.com/komari-monitor/komari-web/blob/9d2c144fcbde53858e175ecd70287fa6428048ef/src/pages/admin/pingTask.tsx#L183-L192), [`src/pages/admin/pingTask_Task.tsx:409-419`](https://github.com/komari-monitor/komari-web/blob/9d2c144fcbde53858e175ecd70287fa6428048ef/src/pages/admin/pingTask_Task.tsx#L409-L419)).

Timeout, DNS, and concurrency:

- On every schedule, Control starts a goroutine per task and dispatches it to every selected client ([`utils/pingSchedule.go:42-70`](https://github.com/komari-monitor/komari/blob/e48aa1e76931132fb5dc93ce53763dd59df83d9f/utils/pingSchedule.go#L42-L70)). The Agent likewise starts a new goroutine for every incoming ping request, with no probe semaphore visible in that path ([`server/websocket.go:384-391`](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/server/websocket.go#L384-L391)).
- TCP uses a 3-second `DialTimeout`. DNS resolves to a slice but deliberately selects only the first address ([`server/task.go:130-142`](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/server/task.go#L130-L142), [`server/task.go:177-200`](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/server/task.go#L177-L200), [`server/task.go:250-275`](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/server/task.go#L250-L275)). A successful result over 1000 ms can cause as many as three additional measurements, so one dispatched task may make up to four connections ([`server/task.go:275-299`](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/server/task.go#L275-L299)).

Target behavior:

- No destination-address class filter was found. DNS names and direct IPs, including private and loopback values, reach `DialTimeout`.
- TCP only opens and closes the connection; it sends no data and reads no banner ([`server/task.go:193-200`](https://github.com/komari-monitor/komari-agent/blob/828afafe7fb5b1b58c1232094e1e46320719016e/server/task.go#L193-L200)). Komari also supports HTTP GET and ICMP, which have broader network behavior than its TCP check.

### 4. Nezha

Inspected commits:

- Dashboard: [`9881c47f0cc92256ba203919da5a25fab79a6bad`](https://github.com/nezhahq/nezha/tree/9881c47f0cc92256ba203919da5a25fab79a6bad)
- Agent: [`05ae9b9b17661ca1d5441e8c17b02e69a76173af`](https://github.com/nezhahq/agent/tree/05ae9b9b17661ca1d5441e8c17b02e69a76173af)

Task count and interval:

- No per-server service-monitor count limit was found in create/update, scheduling, or dispatch. Each saved service receives its own cron entry, and each tick is sent to every selected/covered server ([`service/singleton/servicesentinel.go:447-494`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/service/singleton/servicesentinel.go#L447-L494), [`cmd/dashboard/rpc/service_dispatch.go:12-60`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/cmd/dashboard/rpc/service_dispatch.go#L12-L60)).
- `ServiceForm.Duration` is an unbounded `uint64` in the API model. `Duration == 0` is mutated to the 30-second default; any positive duration becomes `@every <n>s`. No explicit minimum or maximum check was found ([`model/service_api.go:5-20`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/model/service_api.go#L5-L20), [`model/service.go:320-327`](https://github.com/nezhahq/nezha/blob/9881c47f0cc92256ba203919da5a25fab79a6bad/model/service.go#L320-L327)). Thus the effective minimum positive interval is one second, but it is not an explicit anti-abuse floor.

Timeout, DNS, and concurrency:

- Agent dispatch starts a goroutine for each ordinary received task; no probe-specific task count or semaphore is present in this dispatch path ([`cmd/agent/task_dispatch.go:47-68`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/cmd/agent/task_dispatch.go#L47-L68)).
- TCP resolves and uses the first IP, then dials with a 10-second timeout ([`cmd/agent/main.go:681-708`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/cmd/agent/main.go#L681-L708), [`cmd/agent/main.go:1067-1079`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/cmd/agent/main.go#L1067-L1079)). The DNS lookup itself uses `context.Background()` and has no explicit per-probe deadline in this function.
- Agent has a coarse local safety switch, `disable_send_query`, that rejects all TCP/ICMP/HTTP checks. It is an all-or-nothing opt-out, not a count/rate limit ([`model/config.go:46`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/model/config.go#L46), [`cmd/agent/main.go:681-685`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/cmd/agent/main.go#L681-L685)).

Target behavior:

- No private, loopback, link-local, or other address-class rejection was found between service target input and `net.DialTimeout`.
- TCP connects and immediately closes without payload or banner handling ([`cmd/agent/main.go:697-707`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/cmd/agent/main.go#L697-L707)). HTTP monitoring is materially broader: it issues a GET and drains the response body ([`cmd/agent/main.go:742-777`](https://github.com/nezhahq/agent/blob/05ae9b9b17661ca1d5441e8c17b02e69a76173af/cmd/agent/main.go#L742-L777)).

### Implications for this project

- A four-task-per-node hard limit is substantially stricter than the only inspected competitor with a real cap (`monitor`: 64). Komari, Beszel, and Nezha do not provide evidence for choosing four as a compatibility or industry-standard value.
- Competitor evidence supports enforcing the same bound at both Control and Agent. Control provides a clear configuration error; Agent remains safe if Control is buggy or compromised.
- A count limit alone is insufficient. Worst-case outbound connection rate is approximately `tasks × addresses × retries ÷ interval`; therefore interval floor, DNS-address cap, retry policy, timeout, and missed-tick behavior must be designed together.
- The narrowest demonstrated TCP behavior is connect-and-close. Banner reads and arbitrary payloads are unnecessary for latency/availability and would broaden both abuse potential and protocol complexity.

## Files Found

- `monitor/src/db.rs` — server-side per-node quota and minimum interval.
- `monitor/src/api.rs` — target and interval API validation.
- `monitor/src/agent_ws.rs` — inbound result-rate admission.
- `monitor-agent/src/main.rs` — Agent-side cap, interval clamp, DNS cap, and TCP handshake.
- `beszel/internal/migrations/0_collections_snapshot_0_20_0.go` — persisted server-side interval constraints.
- `beszel/agent/network_monitor*.go` — monitor lifecycle, scheduling, timeout, address iteration, and probe behavior.
- `beszel/internal/site/src/components/network-monitors-table/monitor-dialog.tsx` — UI interval validation.
- `komari/database/tasks/ping.go` — task persistence and per-client lookup without count quota.
- `komari/utils/pingSchedule.go` — scheduling and fan-out behavior.
- `komari/web/rpc/jsonrpc/admin.ping.go` — create/edit validation boundary.
- `komari-agent/server/task.go` — DNS selection, timeouts, retries, and protocol behavior.
- `komari-agent/server/websocket.go` — per-request goroutine dispatch.
- `komari-web/src/pages/admin/pingTask*.tsx` — UI task forms and interval inputs.
- `nezha/model/service.go` and `model/service_api.go` — service interval representation/defaulting.
- `nezha/service/singleton/servicesentinel.go` and `cmd/dashboard/rpc/service_dispatch.go` — per-service cron and per-server dispatch.
- `nezha-agent/cmd/agent/task_dispatch.go` and `main.go` — unbounded ordinary-task goroutine dispatch and TCP implementation.

## External References

All external references above use immutable Git commit URLs and line anchors. Repository versions are the commits listed per project; no behavior was inferred from screenshots or marketing documentation.

## Related Specs

- `.trellis/tasks/10-09-server-monitor-research/prd.md` — current candidate MVP requirement for bounded TCP probes.
- `.trellis/tasks/10-09-server-monitor-research/research/monitor-komari-deep-dive.md` — broader Control/Agent security boundary comparison.
- `.trellis/tasks/10-09-server-monitor-research/research/competitor-notes.md` — product-level competitor overview.

## Caveats / Not Found

- “No cap found” means exhaustive text search plus inspection of create/update, scheduling/sync, and Agent dispatch paths at the fixed commits. It is not a claim that an undocumented deployment proxy, fork, or future release cannot add a quota.
- Komari's frontend is a separate repository; it was inspected because server-side source alone cannot establish whether a UI-only constraint exists.
- Nezha Dashboard and Agent were at different repository commits because they are independently versioned projects.
- Address-class conclusions concern the probe execution paths. Some projects have SSRF filtering elsewhere (for example notifications or plugins), but those unrelated filters do not constrain Agent TCP probes.
