# 延迟检测与查看详情弹窗

Date: 2026-10-15. User requests renaming TCP probing as latency checks and adding a viewing modal; provided Komari and monitor as references.

## Source evidence

Read-only fresh shallow checkouts under `/tmp/vps-latency-*`:

- Komari official frontend `9d2c144fcbde53858e175ecd70287fa6428048ef` (radix): [pingTask.tsx](https://github.com/komari-monitor/komari-web/blob/9d2c144fcbde53858e175ecd70287fa6428048ef/src/pages/admin/pingTask.tsx), `pingTask_Task.tsx`, `pingTask_Server.tsx`. Compact page header and task/server tabs; task rows expose target, method, interval and assignments. Create/edit use dialogs. This evidence supports retaining task/node views and explicit TCP method metadata; it does not imply Komari has this exact details modal.
- monitor `c532d54843b87f8001f8387a7acdf1d440b23f40`: [Admin.tsx](https://github.com/monitor-probe/monitor/blob/c532d54843b87f8001f8387a7acdf1d440b23f40/web-admin/src/components/Admin.tsx), `Ping`, `PingByNode`, `PingForm`. Uses 延迟监控 naming, compact targets/intervals/assignments, task/node switch and dialog forms with bounded intervals. No automatic assignment/sorting is imported.
- monitor default theme `4493b5f3bbfd9ef94675de0b2e11681d865a1428`: [NodeDetail.tsx](https://github.com/monitor-probe/monitor-theme-default/blob/4493b5f3bbfd9ef94675de0b2e11681d865a1428/src/components/NodeDetail.tsx). Network latency tab with time-window controls; changes clear previous chart data, empty results differ from request failures. This is a detail page, not a modal. Reuse that state clarity in this project's native modal.

## Adaptation boundary

The current project already has native add/edit/delete dialogs, correct latest-sample freshness filtering, tiered 24h/7d/30d history and accessible chart tables. Its bottom history section requires scrolling, and the TCP-probe product name does not match the requested latency-check wording.

Rename presentation only to 延迟检测 / Latency checks, retain TCP connect method and existing safety limits. Task names, explicit view actions and per-node result entries open one native detail dialog. It displays task metadata, current outcomes, explicit assigned-node/range choices and existing retained-history curves. No history request before a valid selection or after close; switching/cancellation/401 uses existing APIs. Current result freshness still uses latestProbeResult; historical buckets keep their existing task-history semantics across configuration edits. No backend/schema/protocol/dependency changes. Reuse shared dialog focus behavior, preserve theme/localization and 320px layout.
