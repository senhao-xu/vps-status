# TCP page refinement verification

Date: 2026-10-14. User requested a TCP page update referencing Komari/monitor.
Evidence and scope: `research/tcp-page-comparison.md`.

## Result

- Compact summaries and searchable task/node views replace the always-visible
  creation form. Targets, intervals, assignments and per-node recent outcomes
  have explicit labels; waiting and unassigned do not imply success.
- Native add/edit/delete dialogs reuse existing authenticated CRUD APIs,
  contain focus, restore focus, prevent repeated writes and pending dismissal,
  retain failed forms, and refresh through the existing dashboard owner.
- Node picker searches names/groups and selects all shown or clears selection.
- History selects task/node/range explicitly. 7d/30d queries combine all
  non-overlapping retained tiers with one clock and cancellation signal;
  partial-tier failure produces a visible complete-history error.
- TCP charts use elapsed-time positions across mixed-resolution samples.
- Existing `updated_at` is retained in the frontend DTO. Since storage uses
  seconds, samples in/before the configuration-update second are excluded;
  same-second fresh samples conservatively wait until the next sample.
- No backend, schema, protocol, probe limits or dependency changes.

## Checks

- Pinned pnpm10.18.0 lint, strict typecheck, 164 tests /20 files and build pass.
- Final generated assets: `index-CfIi3mof.js`, `index-EL5OLx-e.css`.
- `go test -tags webui ./...` and tagged Control build pass. Integration tests
  were rerun outside the sandbox because it forbids httptest listening sockets;
  the initial socket-denied result was an environment restriction.
- Independent Trellis checker reviewed the complete change and final boundary
  fixes; no remaining findings. `git diff --check` passes.

## Browser evidence and limits

Temporary Control uses `/tmp/vps-status-tcp-browser/data-current` with three
test nodes. HTTPS test endpoint `https://localhost:18448` proxies to the
temporary Control; production at18080 and its credentials/data were untouched.

Real UI/API checks passed for create, PATCH edit, DELETE and parent refresh,
filtered node selection, independent unassigned task, task/node search,
native validation/pending/focus behavior, failed edit retention/retry,
explicit history node and actual empty telemetry history.

Browser-only response fixtures supplied current success/failure samples and
historical points to verify task assignment/update filters, node switching,
three-tier request coverage, accessible tables and telemetry503 behavior.
These are presentation tests, not evidence of real Agent latency/capacity.

All four Emerald/Classic light/dark combinations pass at320px for task/node
views and dialogs. Chinese320px, Tab/Escape/focus restoration also pass.
No page-level JavaScript errors.

- Script: `/tmp/vps-status-tcp-browser/check.mjs`.
- Results: `/tmp/vps-status-tcp-browser/result.log`.
- Desktop: `/tmp/vps-status-tcp-browser/tasks-desktop.png`.
- Mobile: `/tmp/vps-status-tcp-browser/tasks-mobile.png`.
- Chinese modal: `/tmp/vps-status-tcp-browser/dialog-zh-mobile.png`.

Retained parent work: Security/runtime-toolchain remediation and externally
constrained100-Agent/400-task capacity baseline remain open. This UI change
does not publish replacement v0.1.0 Release assets or update the live service.
