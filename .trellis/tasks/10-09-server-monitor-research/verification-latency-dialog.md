# 延迟检测与详情弹窗验收

Date: 2026-10-15. Scope and official source evidence: `research/latency-dialog-comparison.md`.

## Delivered behavior

- Administrator navigation and management copy now use 延迟检测 / Latency checks; TCP connection latency remains explicit in method metadata, task descriptions and latency chart title.
- Task names, View details actions and per-node result buttons open a native detail dialog instead of a permanent bottom history section. Detail metadata and latest results derive from the refreshed task; waiting/unassigned are explicit.
- Task-only entry requires a node choice; node entry carries the source node ID. Modal task/node/range controls use retained 24h/7d/30d history and accessible chart tables. Historical buckets retain existing across-configuration semantics and have a concise note.
- No eager history outside an open valid detail selection. Closing/switching/refresh/config-update unmounts keyed history and aborts old requests. Removed assignments permanently invalidate selection; deleted task closes detail and restores the stable Add trigger fallback.
- Reused shared SettingsDialog lifecycle for probe details/editor/deletion; existing mutation pending/error/401 behavior and TCP bounds remain. No backend/schema/protocol/dependency changes.

## Checks

- Pinned pnpm 10.18.0 via cached binary: lint, strict TypeScript, all 21 files / 173 Vitest tests and production build pass. Independent Trellis checker repeated lint/typecheck/tests with no findings or further source edits.
- `go test -tags webui ./...` and `go build -tags webui -o /tmp/vps-status-latency-browser/control-final ./cmd/control` pass after the final frontend build.
- Built assets: `index-BF1liiqV.js`, `index-eVmUwi9f.css`. `git diff --check` passes.
- Default sandbox execution could not start (os error2). Read/write/build/browser checks ran using the tool's approved elevated execution mode. No application defect caused the initial environment failure.

## Browser evidence and limits

Isolated Control: `127.0.0.1:18089`, test data `/tmp/vps-status-latency-browser/data`. Temporary HTTPS frontend/proxy: `https://localhost:18449`. Production data, administrator credentials/settings and deployed service were not changed.

`/tmp/vps-status-latency-browser/check.mjs` completes with ALL LATENCY BROWSER CHECKS PASSED:

1. Explicit task detail entry; no initial/eager telemetry queries; choosing a node; real empty-history response; Escape and trigger focus restoration.
2. Browser response fixtures for successful/failed latest results and retained chart points; node/task/range switching, all three 30d retention queries, accessible tables, complete-history 503 error/retry, unassigned guidance and close cancellation of blocked history requests.
3. Source-node identity when opening from node results. Actual isolated create, PATCH edit and DELETE through UI, with parent refresh and updated target.
4. Real assignment removal while viewing details cancels the selected history and clears the selection; reassignment does not restore it. External task deletion followed by modal refresh closes it and restores Add button focus.
5. 32 combinations: en/zh × Emerald/Classic × light/dark × 1280/320px. Both task/node entries and dialogs have no page/dialog horizontal overflow. Tab focus remains within native modal when the document has focus; browser chrome traversal is allowed. Escape works in every combination.
6. Telemetry401 unmounts dashboard/detail and returns to login. Zero page-level JavaScript errors.

Blocked-request network assertions directly verify close cancellation; node/range/task/config cancellation also has independent code review of keyed unmount and AbortController cleanup. Fixture chart/current outcomes are presentation tests, not evidence of real Agent latency or capacity.

Artifacts (temporary, intentionally untracked): result.log, details-desktop.png, details-zh-mobile.png, check.mjs under `/tmp/vps-status-latency-browser/`. Initial harness reruns fixed selector ambiguity, select label matching and normal Chromium chrome focus traversal; no product source changes were needed.

## Delivery state

All changes remain reviewable in the workspace; commit awaits the one-shot confirmation required by `.trellis/workflow.md` Phase3.4. No push or Release publication performed. Local Docker deployment was subsequently requested and completed as recorded below. Parent task remains active for its pre-existing unfinished capacity/security acceptance criteria.

## Proposed commit (awaiting confirmation)

1. `feat: add latency check details dialog`

   - `.trellis/spec/frontend/monitoring-ui-guidelines.md`
   - `.trellis/tasks/10-09-server-monitor-research/check.jsonl`
   - `.trellis/tasks/10-09-server-monitor-research/design.md`
   - `.trellis/tasks/10-09-server-monitor-research/implement.jsonl`
   - `.trellis/tasks/10-09-server-monitor-research/implement.md`
   - `.trellis/tasks/10-09-server-monitor-research/prd.md`
   - `.trellis/tasks/10-09-server-monitor-research/research/latency-dialog-comparison.md`
   - `.trellis/tasks/10-09-server-monitor-research/verification-latency-dialog.md`
   - `web/src/features/admin/ProbeDetailDialog.tsx`
   - `web/src/features/admin/ProbeResult.tsx`
   - `web/src/features/admin/ProbeTaskDialog.tsx`
   - `web/src/features/admin/ProbesPanel.test.tsx`
   - `web/src/features/admin/ProbesPanel.tsx`
   - `web/src/features/admin/SettingsDialog.tsx`
   - `web/src/i18n.tsx`
   - `web/src/styles.css`

Unrecognized dirty files: none. Initial working tree was clean; all listed changes belong to this increment. No push or deployment included.


## Requested local Docker update — 2026-10-15

User explicitly requested updating the local Docker service. Rebuilt and recreated only `deploy-control-1` using the existing two Compose files and `--no-deps`; unrelated containers were retained.

- Image: `vps-status-control:local-latency-94e40b101c`.
- Binary metadata: version `dev`, commit `927ce78193dae18ef36ac93e098bb0477e4fbaa8-dirty-94e40b101c`. This accurately identifies the uncommitted tested source snapshot; the update did not create or imply a source commit.
- Original volume `deploy_control-data`, bind `127.0.0.1:18080`, and runtime `--agent-release-version v0.1.0` retained. Ignored `deploy/.env` selects the new image; the existing ignored override retains dev version metadata and Agent pin.
- Pre/post online backups include paired master key and telemetry, are verified by the supported CLI and copied to `data/backups/control-{pre,post}-latency-20261015.tar.gz` with mode0600. Prior environment: `data/backups/deploy-pre-latency-20261015.env` (0600). Backup files were staged under `/data` because Docker cp did not expose the tmpfs `/tmp` backup.
- Retained rollback image: `vps-status-control:pre-latency-20261015` (same content as prior `local-3480a64`). Restore the saved deployment environment and recreate Control with the same two Compose files for image-only rollback; no schema changes were introduced.
- Container healthy; healthz and readyz return ok. Served `index-BF1liiqV.js` and `index-eVmUwi9f.css` match the verified build byte for byte; the served JS contains the new zh/en latency wording.
- Offline pre/post backup comparison: master key identical, all21 configuration tables identical, SQLite quick_check passes. No real tasks, administrator credentials or settings were changed for validation.
- Read-only Chromium public/admin-login checks pass at1280/320px, with no horizontal overflow or page errors. Existing administrator setup retained.

## Subsequent monitor configuration revision

The user later chose monitor-style TCP configuration, including persistent future-node assignments and schema10. See verification-monitor-tcp-config.md for the newer behavior, checks, Docker image and migration-aware rollback; the previous image-only rollback note applies solely to the earlier schema9 UI update.
