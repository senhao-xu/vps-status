# TCP 探测页面对比与改版边界

Date: 2026-10-14. User: TCP 探测页面参考 Komari 和 monitor 改版。

## Reference evidence

- Komari Web `9d2c144fcbde53858e175ecd70287fa6428048ef`:
  `src/pages/admin/pingTask.tsx` uses a compact heading/add action, task/server
  tabs and a dialog for creating tasks. `pingTask_Task.tsx` uses columns for
  name, assigned servers, target, protocol, interval and actions.
- monitor `c532d54843b87f8001f8387a7acdf1d440b23f40`:
  `web-admin/src/components/Admin.tsx` Ping/PingByNode/PingForm use task/node
  view switches, compact target/interval/assignment rows, add/edit dialogs,
  node selection and explicit pending/deletion states.
- Existing VPS Status embeds ProbesPanel in AdminDashboard.tsx, always shows
  the creation form, lists only name/target/current failure rate, and silently
  chooses the first assigned node for history. Existing backend supports
  updating probe configuration; no API or protocol expansion is needed.

## Implementation boundary

- Extract the TCP feature into ProbesPanel and its task editor. Keep existing
  API decoders and parent snapshot reload ownership. No dependency additions.
- Compact heading, summary counts, search, task/node view, responsive rows
  showing target, interval, assignments and latest per-node outcomes.
- Add/edit with a native dialog, labeled fields, searchable node selection,
  select-all/clear, a synchronous pending guard and visible validation/errors.
  Retain existing host and port fields, 5–3600 seconds and port 1–65535.
- Show explicit node and time-range selectors for selected-task history;
  use existing supported resolutions and MetricChart's text-table alternative.
  Missing samples and unassigned tasks must not render zero as success.
- Editing/deleting refresh through the parent and retain appropriate task
  selection. Refresh is an explicit user action; no new live-result protocol.
- Preserve zh/en, Emerald/Classic light/dark, keyboard focus and 320px layout.
- Do not add ICMP/HTTP, drag sorting, automatic assignments, new endpoints,
  schema migrations or security/toolchain upgrades. Do not modify real tasks
  or administrator credentials for browser verification.

Validation: frontend lint/typecheck/tests/build; tagged Go tests/build; real
browser checks with isolated test data for CRUD, filter/view/history selection,
pending/error handling, keyboard dismissal/focus and theme/mobile coverage.
