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
