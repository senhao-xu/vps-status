# Probe editor assignment concurrency repair

Date: 2026-10-25. User requested a fix for the P2 found while reviewing
`07a3233`: saving a stale probe editor could undo later automatic enrollment.

## Bug analysis

1. Root cause: cross-layer contract (B), with a coverage gap (D). The storage
   transaction correctly preserved omitted `node_ids`, but the browser always
   sent a full snapshot. An unrelated scalar edit therefore deliberately
   replaced assignments added after that snapshot.
2. Earlier prevention covered a gated request with omitted assignments and a
   refreshed editor selection. Neither exercised the actual browser request
   from a stale editor. Refresh alone also leaves a race before the write.
3. Prevention: edit sends only explicit assignment deltas; the existing storage
   mutation transaction merges them against committed state. Keep legacy full
   replacement explicit. Unit/API tests and a real browser flow must cover the
   same serialization and concurrent enrollment.
4. Related paths: site settings already omit membership, and single-assignment
   endpoints already mutate against committed state. No changes to those paths
   are needed. Document both the collection mutation contract and its request
   producer, rather than relying only on storage-layer tests.
5. Knowledge captured in backend TCP and frontend monitoring specs and the
   cross-layer guide. This application has no `src/templates/markdown/spec/`
   template tree to synchronize.

## Baseline evidence

- Temporary Go overlay reproduces a cached editor body replacing
  `[existing-node,new-node]` with `[existing-node]` after a name-only save.
  That legacy replacement body intentionally remains supported; the corrected
  regression must exercise the new editor submission contract.
- Real Chromium against the original embedded Control reproduces the same
  loss: open Gateway editor, enroll Auto-B, rename, save, observe Auto-B removed.
  Output: `BASELINE REPRODUCED: rename silently removed Auto-B`.
- Browser harness: `/tmp/vps-status-probe-fix/check.cjs`; every run creates a
  new temporary data directory and uses its own Control and HTTPS proxy.
  No production credentials, configuration or deployment are used.

## Verification

- Frontend lint, strict typecheck and all227tests pass. Four new frontend
  cases verify the actual dialog assignment-write helper: default/empty create,
  untouched stale edit, explicit changes across refresh/filtering and node
  deletion.
- New storage/API suites cover name-only and explicit-delta writes with gated
  concurrent enrollment, disabled future policy, idempotent/empty deltas,
  legacy complete replacement, invalid/mixed/duplicate/overlapping lists,
  missing additions, history cleanup and quota rollback of scalar settings,
  assignment set and configuration version.
- `go test ./...`, `go test -race ./...`, `go vet ./...`,
  `go test -tags webui ./...`, tagged Control build, Go formatting and
  `git diff --check` pass.
- Production frontend build: `index-DYF6cHKx.js` and unchanged
  `index-CQ15YFdW.css`; Control embedded from that build.
- Real Chromium against the repaired Control passes three regressions with
  actual UI writes and real enrollment, without manually refreshing the open
  editor: name-only save preserves unseen Auto-B; explicit remove/add retains
  unrelated unseen Auto-D; disabling automatic joining retains Auto-E enrolled
  before save and excludes Excluded-F enrolled afterwards. Captured edit bodies
  contain no `node_ids`; explicit selection sends only its requested deltas.
  No browser page errors. Output:
  `ALL PROBE ASSIGNMENT BROWSER REGRESSIONS PASSED`.

Independent `trellis-check` review found no issues and made no source changes;
its lint/typecheck passed. Final production source is identical to the
browser-tested build. Parent task remains active for its prior outstanding
work. Source remains uncommitted. The subsequent user-requested local Docker
startup is recorded below.

## Local Docker startup — 2026-10-25

User requested local startup and specified Docker. Existing project containers
and `deploy_control-data` were absent. Other Docker volumes contained no
VPS Status `control.db`; unrelated `monitor_control-data` contains `monitor.db`
and was left untouched. The most recent repository backup,
`data/backups/control-post-login-home-20261015.tar.gz`, was verified and restored
through the supported CLI into a new temporary directory. It contains schema11,
one administrator, no nodes or probe tasks, and the paired key/telemetry.

- Built `vps-status-control:local-probe-fix-d2f4cffd2e` using the existing
  Dockerfile and both Compose files. Metadata: versiondev,
  commit`2e121ceecd0c464cddb48215052b30a89ceaa3b5-dirty-d2f4cffd2e`, protocolv1;
  Agent installation remains pinned to v0.1.0.
- Backed up ignored deployment settings to
  `data/backups/deploy-pre-probe-fix-20261025.env` (0600), then updated only the
  local image/commit selection. Created a fresh `deploy_control-data`, copied
  the verified restored files with nonroot ownership and started
  `deploy-control-1` on `127.0.0.1:18080` via Compose. No existing volume was
  overwritten, and other containers were unchanged.
- Container healthy; `/healthz` and `/readyz` return200/ok;
  `/api/v1/admin/setup` confirms the existing administrator is configured.
  Every configuration table and master-key digest matches the restored backup.
  Served JS/CSS match the tested build byte for byte.
- Read-only real Chromium checks pass for homepage and administrator login in
  en/zh at1280/320px, without horizontal overflow or JavaScript page errors.
  No real administrator credentials or settings were changed by smoke tests.
- Created and verified the supported post-startup backup including telemetry:
  `data/backups/control-post-probe-fix-20261025.tar.gz` (0600).

Access: `http://localhost:18080/`, administration at `/admin`. The recovered
state is from the October15 backup; the missing prior Docker volume was not
available to recover any later changes.

## Source synchronization — 2026-10-25

Following the user's explicit commit/push request, the reviewed production/API/
frontend/spec/test repair is committed as `362ec9e` on
`codex/agent-docker-ghcr`. No production source changed during this commit-only
step. Subsequent full Go/race/vet/tagged runs with Go1.27.3 and the final273-test
frontend run also include this repair. Earlier uncommitted-state notes above
are historical checkpoints; the larger task remains active.
