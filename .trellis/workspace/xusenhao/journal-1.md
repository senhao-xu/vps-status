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
