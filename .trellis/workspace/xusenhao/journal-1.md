# Journal - xusenhao (Part 1)

> AI development session journal
> Started: 2026-10-09

---



## Session 1: Emerald themes and guided enrollment deployed

**Date**: 2026-10-13
**Task**: Emerald themes and guided enrollment deployed
**Branch**: `main`

### Summary

Committed and pushed theme management, guided node installation, localization and prior approved password changes; rebuilt local Control with original data volume and port preserved.

### Main Changes

- Code commit 249e8d6 pushed to origin/main; deploy-control-1 rebuilt with commit=249e8d6 on 127.0.0.1:18080; vps-node-panel untouched.
- Verified pre-upgrade configuration/master-key backup: data/backups/control-pre-emerald-20261013.tar.gz; retained old image vps-status-control:pre-emerald-20261013.

### Git Commits

| Hash | Message |
|------|---------|
| `249e8d6` | (see git log) |

### Testing

- [OK] Prior full Go tests/race/vet, frontend 72 tests/lint/typecheck/build and tagged embed checks passed; Docker rebuild and healthcheck passed.
- [OK] Actual deployed browser check passed: Emerald theme, existing administrator, production assets, 320px login layout and no page errors.

### Status

[OK] **Completed**

### Next Steps

- Keep main task active for the outstanding 100 Agent / 400 TCP capacity baseline. Local dev build needs an existing published Agent release version before one-command remote installation.


## Session 2: Public homepage routing deployed

**Date**: 2026-10-13
**Task**: Public homepage routing deployed
**Branch**: `main`

### Summary

Pushed public homepage correction to origin/main and deployed Control with original volume and 18080 port; verified production routes and administrator retention.

### Main Changes

- Control rebuilt with commit=f3989b1 and deployed to deploy-control-1 at 127.0.0.1:18080; deploy_control-data retained.
- Verified online control.db/master.key backup at data/backups/control-pre-public-homepage-f3989b1.tar.gz; rollback image retained as vps-status-control:pre-public-homepage-f3989b1.

### Git Commits

| Hash | Message |
|------|---------|
| `f3989b1` | (see git log) |

### Testing

- [OK] Prior frontend 85 tests/lint/typecheck/build and tagged Go tests/build passed. Deployed Control health and readiness passed.
- [OK] Actual Chromium verified public root without admin requests, top-right admin entry, existing administrator login, return link, production assets, /public alias, zh/en 320px layout and no page errors.

### Status

[OK] **Completed**

### Next Steps

- Public page remains disabled with zero published nodes; administrator must explicitly enable it and select publication allowlist. Main task remains active for outstanding 100 Agent/400 TCP capacity baseline.


## Session 3: Security password change deployed

**Date**: 2026-10-13
**Task**: Security password change deployed
**Branch**: `main`

### Summary

Implemented, pushed and deployed administrator password change in Security, with current-password verification, confirmation, atomic all-session revocation and localized re-login notice.

### Main Changes

- Control rebuilt and deployed with commit=ab20759 on original 127.0.0.1:18080 and deploy_control-data; no database migration or actual administrator password mutation.
- Verified backup data/backups/control-pre-password-ab20759.tar.gz; retained rollback image vps-status-control:pre-password-ab20759.

### Git Commits

| Hash | Message |
|------|---------|
| `ab20759` | (see git log) |

### Testing

- [OK] Frontend lint/typecheck/88 tests/build and full Go tests/race/vet/tagged tests/build passed; independent review fixed stale session-refresh response guard.
- [OK] Real temporary HTTPS Control browser covered password errors, confirmation mismatch, duplicate guard, all-session revocation, old/new/empty password login, SSE closure, delayed refresh and en/zh 320px themes.
- [OK] Deployed health/readiness, public homepage/admin entry and embedded Security password/MFA forms passed. Production browser mutations intercepted with fixtures; existing real administrator preserved.

### Status

[OK] **Completed**

### Next Steps

- Administrator can now use Security -> Change password. Main task remains active for outstanding 100 Agent/400 TCP baseline.


## Session 4: Unified node installation and admin layouts

**Date**: 2026-10-14
**Task**: Unified node installation and admin layouts
**Branch**: `main`

### Summary

Implemented shared installation settings, name-only node enrollment, pinned GitHub Release bootstrap and node/settings layouts; full tests and temporary browser acceptance passed. User authorized commit and push; no Release publication or real deployment.

### Main Changes

- Schema v9 installation defaults; authenticated settings API; token admission guarded by remote address and explicit release configuration.
- Canonical standalone bootstrap published by release workflow; checksum verification and compatibility endpoint preserved.
- Node overview, installation dialog and settings layout with localized guidance, focus and 320px themes.

### Git Commits

| Hash | Message |
|------|---------|
| `603c377` | (see git log) |

### Testing

- [OK] Frontend lint/typecheck, 143 tests, production build; full Go tests/race/vet/tagged embed; migration preservation and mocked bootstrap failures.
- [OK] Actual temporary-browser settings save/restart, duplicate submit guard, GitHub command, token exchange, focus and mobile layouts; release artifact checksums and 5-Agent/20-task smoke passed.

### Status

[OK] **Completed**

### Next Steps

- Publish a Release containing bootstrap-agent.sh before remote installation; deployment not performed. Main task remains active for the outstanding resource-constrained capacity baseline.


## Session 5: Publish v0.1.0 and deploy local Control

**Date**: 2026-10-14
**Task**: Publish v0.1.0 and deploy local Control
**Branch**: `main`

### Summary

Published and downloaded v0.1.0 assets; checksum and tag CI passed. Updated local deploy-control-1 at 127.0.0.1:18080 with original data volume; verified online backups, schema v8-to-v9 preservation, health and browser pages. Fixed broken Trivy Action references; actual Security scan fails on Go standard-library advisories and is disclosed in release notes. Capacity baseline remains open; active tasks retained.

### Git Commits

| Hash | Message |
|------|---------|
| `4a05543` | (see git log) |
| `fc7e7bd` | (see git log) |
| `ec156c9` | (see git log) |

### Status

[OK] **Completed**


## Session 6: Refine TCP probe page from Komari and monitor

**Date**: 2026-10-14
**Task**: Refine TCP probe page from Komari and monitor
**Branch**: `main`

### Summary

Implemented task/node views, search, summaries and native create/edit/delete dialogs with existing APIs. Added explicit task/node/range history, complete retained-tier queries, timestamp-scaled TCP charts and conservative same-second stale-sample filtering. Passed lint/typecheck, 164 frontend tests, build, tagged Go tests/build, independent review and isolated HTTPS Chromium CRUD/error/focus/theme320px checks. Synced frontend contracts/spec and verification evidence. Existing v0.1.0 deployment unchanged; parent remains active for security/capacity work.

### Git Commits

| Hash | Message |
|------|---------|
| `c0aa645` | (see git log) |

### Status

[OK] **Completed**


## Session 7: Deploy updated TCP page locally

**Date**: 2026-10-14
**Task**: Deploy updated TCP page locally
**Branch**: `main`

### Summary

Updated deploy-control-1 to local-c9130fb source image after verified pre-update backup. Retained deploy_control-data volume and 127.0.0.1:18080. Local ignored Compose override builds dev metadata and pins Agent release v0.1.0. Docker/healthz/readyz pass, served JS/CSS match verified TCP artifacts, deployed public/admin browser desktop320px checks pass. Snapshot comparison confirms schema9 and master key/admin/node/task/appearance/settings retained. Saved rollback image and original environment; parent security/capacity work remains open.

### Git Commits

| Hash | Message |
|------|---------|
| `c9130fb` | (see git log) |

### Status

[OK] **Completed**


## Session 8: Remaining administrator pages and local update

**Date**: 2026-10-14
**Task**: Remaining administrator pages and local update
**Branch**: `main`

### Summary

Refined notifications, storage, publication, security and appearance against Komari/monitor; independent review, 170 frontend tests, tagged Go tests/build and 80 browser layout combinations passed. Pushed main and deployed local-3480a64 on 18080 with published Agent v0.1.0 pinned. Verified backup/master key/all 21 config tables unchanged and deployed read-only browser checks. Parent remains active for capacity/security follow-up.

### Git Commits

| Hash | Message |
|------|---------|
| `3480a64` | (see git log) |

### Status

[OK] **Completed**


## Session 9: Latency checks, homepage settings and local deployment

**Date**: 2026-10-15
**Task**: Latency checks, homepage settings and local deployment
**Branch**: `main`

### Summary

Implemented latency detail dialogs, monitor-style TCP target/selection/auto-join, first-class homepage/site settings, node display controls and login presentation. Verified migrations/concurrency/safe DTO,223 frontend tests, Go full/race/vet/tagged checks and browser flows; deployed verified local snapshots with backups. User authorized commit and push. Parent task remains active for outstanding security/capacity work.

### Main Changes

- Control schemas10/11 preserve legacy task/site choices and merge concurrent enrollment/configuration updates atomically.
- Refined zh/en theme/mobile UI, homepage button and administrator login; updated source-backed specs and verification evidence.

### Git Commits

| Hash | Message |
|------|---------|
| `07a3233` | (see git log) |

### Testing

- [OK] Frontend lint/typecheck/223 tests/build; Go full/race/vet/tagged; isolated migration/API/browser and deployed health/assets/backup comparisons passed.

### Status

[OK] **Completed**

### Next Steps

- Complete outstanding parent security/capacity acceptance; keep parent task active.


## Session 10: Docker Agent and public GHCR image

**Date**: 2026-10-25
**Task**: Docker Agent and public GHCR image
**Branch**: `codex/agent-docker-ghcr`

### Summary

Implemented Docker Agent host monitoring and selectable installation; published public multiarch GHCR latest sha256:8784ceb33080620a106cba67d409ceeec08ad0282a7a156bff3ec7ce13511d02 via Actions40170241222. Verified anonymous amd64/arm64 pulls, exact UI command and Docker/Compose registration/host metrics/restart. Patched Go/crypto/scanner and cancellation race; all gates and independent reviews pass. Local18080 updated healthy; all21 config tables/schema/master key preserved. DraftPR1 attached. Parent remains active; unrelated probe assignment changes left dirty.

### Git Commits

| Hash | Message |
|------|---------|
| `d2fa9ee` | (see git log) |
| `3b7b3c0` | (see git log) |
| `a82dbab` | (see git log) |

### Status

[OK] **Completed**


## Session 11: Commit remaining probe repair and verification records

**Date**: 2026-10-25
**Task**: Commit remaining probe repair and verification records
**Branch**: `codex/agent-docker-ghcr`

### Summary

User explicitly requested committing and pushing all remaining changes. Committed the previously reviewed atomic probe-assignment delta repair and regression tests, synchronized installation/focus/Docker evidence and task plans, and collected prior session records on codex/agent-docker-ghcr. Production source is unchanged from the tested local tree; no deployment or data mutation. The larger parent remains active for unfinished acceptance work.

### Git Commits

| Hash | Message |
|------|---------|
| `362ec9e` | (see git log) |
| `d39bb79` | (see git log) |

### Testing

- [OK] Prior full Go/race/vet/tagged and frontend273 checks plus independent reviews cover the unchanged code; staged diff checks pass.

### Status

[OK] **Completed**

### Next Steps

- Keep the parent active for its outstanding capacity/security acceptance work.
