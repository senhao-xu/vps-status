# Remaining administrator pages — 2026-10-14

## Delivered scope

Komari/monitor-inspired notifications, storage, publication, security and appearance page layouts. Notification lists/search/provider-specific native create dialogs and named rule scope; separate storage summaries/maintenance/cleanup with independent exact confirmations; public identity/search/group selection/count and name/group-only draft preview; security summary/password layout/stepped MFA/recovery dismissal; cohesive save bars. Node/TCP/install and auth/public APIs retain existing behavior. No backend or dependency changes.

Reference evidence: research/remaining-pages-comparison.md. Parent remains active for outstanding capacity/security work.

## Checks

- Pinned pnpm10.18.0 lint/typecheck/test/build pass: **170 tests /21 files**.
- Final embedded assets: `index-Dn6XEq6e.js`, `index-DO0_b-t9.css`.
- `GOCACHE=/tmp/vps-status-go-cache go test -tags webui ./...` passes with socket permission; initial sandbox run failed because httptest could not listen, not an application failure.
- Tagged Control build passes. Go emits a nonfatal read-only module-cache stat warning in sandbox; binary build completes successfully.
- Independent Trellis review fixed keyed provider-field reuse, refreshed public configuration/draft acknowledgement, and committed MFA state/retry after failed session refresh. No unresolved findings.
- `git diff --check` passes.

## Isolated browser evidence

Chromium at HTTPS localhost18448 proxies a Control at18088 with `/tmp/vps-status-pages-browser/data`. Real production data, credentials, notification/public settings and history were not modified. Test Webhook uses example.invalid and is disabled before rules exist; no external notification test/send is executed.

`/tmp/vps-status-pages-browser/check.mjs` passes with zero JavaScript errors:

1. Provider switches clear secret inputs in both directions; failed creation preserves draft; real channel create/disable; repeated submission sends one request; pending Escape blocked and focus restored.
2. Real node/group rules, conditional offline/CPU fields, readable scope identities/search and empty states.
3. Selection filtering retains hidden choices; failed save preserves draft; actual public API reflects two selected nodes and public root makes no admin request; pristine refreshed fields follow server values while edited title stays local.
4. Real clear/reset/checkpoint on isolated data; independent exact confirmation, trailing whitespace rejected, successful confirmation cleared.
5. All administrator pages at320px in both visual themes/modes, plus Chinese; native dialog focus containment/dismissal.
6. Real TOTP setup/invalid code/confirm/disable; failed session refresh retains committed MFA and recovery codes, retry succeeds; code dismissal clears display; no secret in browser storage.
7. Password mismatch/wrong current retain form; successful change revokes session and returns to login.

Test harness fixes during reruns concerned only selectors, localized accessible names, fixture reset and omission of read-only updated_at from test PUT. Product corrections were reviewed and rebuilt before final successful run.

`/tmp/vps-status-pages-browser/final-matrix.mjs` passes **80 combinations**: zh/en × Emerald/Classic × light/dark ×1280/320px × five populated settings pages. Final screenshots under `/tmp/vps-status-pages-browser/final-*-desktop.png` and `final-*-mobile.png`. A simulated settings401 unmounts the dashboard and returns to login; zero JavaScript errors. Deployment details appended after completion. Temporary artifacts and backups are intentionally untracked.

## Local deployment

Upgrade backup created via supported CLI and verified: `data/backups/control-pre-pages-20261014.tar.gz` (3 files, telemetry included); backup and copied prior deployment env are mode0600. Prior image `vps-status-control:local-c9130fb` remains available. Use source image dev metadata with `--agent-release-version v0.1.0`, existing deploy_control-data volume and loopback18080. New deployment verification pending.
