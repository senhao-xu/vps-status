# Form focus outline repair — 2026-10-25

The installation screen's Agent release-version input used the shared 3px outline with 3px outward offset. Browser measurements reproduced a 6px outward extent with a 0px gap before the effective-version label; the baseline screenshot confirms that the outline covered that label.

The single shared CSS rule now uses `outline-offset: -3px` for focused inputs other than checkbox/radio, selects and textareas. Existing width, color, layout, button/link and checkbox/radio indicators are retained. No runtime dependency or API/data changes.

Validation passed:

- Frontend lint/typecheck, all 227 tests, production build.
- `go test -tags webui ./...` and tagged Control build.
- Independent Trellis check: no findings or follow-up source edits.
- Real embedded installation screen with isolated temporary data: 16 combinations of zh/en, Emerald/Classic, dark/light and 1280/320px. Mouse and Tab focus stay visible with 0px outward extent; no horizontal overflow or page errors.
- Browser fixtures using the production CSS verify default/password/email/url/number/search inputs, selects and textareas use the inset indicator while checkbox/radio/button/link retain visible outer indicators.
- Before/after screenshots visually inspected at `/tmp/vps-status-probe-fix/focus-before-1280.png`, `focus-after-1280.png` and `focus-after-320.png`. Harness/results: `focus-check.cjs` and `focus-results.json` in the same directory. All setup writes were confined to isolated fixture data; actual site configuration was not changed.

Docker deployment passed:

- Supported CLI pre/post backups created with telemetry, verified and copied to `data/backups/control-{pre,post}-focus-fix-20261025.tar.gz` with mode0600. Previous Compose environment preserved in `data/backups/deploy-pre-focus-fix-20261025.env`.
- Only `deploy-control-1` recreated using `vps-status-control:local-focus-00989f1fa2`; health is healthy. Same `deploy_control-data:/data`, nonroot user, loopback port18080 and Agent v0.1.0 pin.
- `/readyz`200; served `index-DisDoQ13.js` and `index-sio5oRUo.css` match the verified local production build byte-for-byte.
- Read-only live homepage/admin-login browser smoke passed in en/zh at1280/320px without page errors or overflow.
- All21 Control tables, database schema and master key match the verified pre-update backup. Comparison evidence: `/tmp/vps-status-probe-fix/focus-data-verification.json`.
- No source commit/push and no archive of the larger unfinished parent task.
