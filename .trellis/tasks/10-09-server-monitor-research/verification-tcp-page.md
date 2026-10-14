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

## Follow-up: local deployment updated

After the user explicitly requested "更新", deployed source commit
`c9130fba37587a24afb06be4fb71ca7f96c30e30` to `deploy-control-1` using image
`vps-status-control:local-c9130fb`. Binary metadata is `version=dev` with this
full commit; this source deployment does not replace the published Release.

- Original `deploy_control-data` volume and `127.0.0.1:18080` port retained.
- Ignored `deploy/.env` pins the local image/commit/port. Ignored
  `deploy/.env.local-compose.yml` sets build VERSION=dev and runtime
  `--agent-release-version v0.1.0`, so installation keeps using real published
  Agent assets rather than requesting an unpublished source-build version.
- Use both Compose files for this deployment:

  ```sh
  docker compose --env-file deploy/.env -p deploy \
    -f deploy/docker-compose.yml -f deploy/.env.local-compose.yml ps
  ```

- Online pre-update backup verified, including paired master key/telemetry:
  `data/backups/control-pre-tcp-c9130fb-20261014.tar.gz`, mode0600.
- Original environment backup:
  `data/backups/deploy-pre-tcp-c9130fb.env`, mode0600.
- Old image retained: `vps-status-control:pre-tcp-v0.1.0-4a05543`.
- Verified post-update snapshot:
  `data/backups/control-post-tcp-c9130fb-20261014.tar.gz`, mode0600.
- Snapshot comparison confirms schema9 unchanged, master key identical, and
  all administrator/node/credential/task/assignment/appearance/public-page/
  installation records identical.
- Docker healthy; healthz/readyz both `ok`; administrator setup remains true.
- Actual served JS/CSS match final verified artifacts byte for byte:
  `index-CfIi3mof.js` and `index-EL5OLx-e.css`.
- Served bootstrap retains GitHub source and v0.1.0 default; shell syntax passes.
- Real deployed public/admin-login pages load without JavaScript errors, at
  desktop/320px. Screenshots: `/tmp/vps-status-tcp-deployed-{public,admin}-{1280,320}.png`.
- Only Control recreated using `--no-deps`; no real task/credential mutations
  or administrator login changes were used for deployment verification.
- Rollback restores the saved `.env` and uses the original Compose file with
  the retained v0.1.0 image. Schema is unchanged, so image-only rollback is
  compatible; use the pre-update verified backup if data restoration is needed.
