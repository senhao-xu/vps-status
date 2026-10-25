# Installation-settings action feedback — 2026-10-25

The user screenshot highlights EnrollmentDialog's “前往安装设置” action: its borderless resting style gets only the generic pale-green hover fill in Emerald light mode. The baseline real-dialog screenshot reproduces this.

Changed only `.settings-link` CSS: a 1px theme-link-colored resting border with horizontal padding, and a darker accent fill with white text for enabled hover and keyboard focus. The existing 3px focus indicator remains visible. State changes keep the same border width and padding; all previous source fixes are preserved.

Validation passed:

- Frontend lint/typecheck, all227 tests and production build; tagged Go tests/build.
- Independent Trellis review: no findings or additional edits.
- Real embedded add-node dialog with isolated temporary data:16 combinations of zh/en, Emerald/Classic, dark/light and1280/320px. Checked resting/hover/Tab states, no hover layout shift, visible keyboard outline, navigation to installation settings through Enter/click, no page errors/overflow or mutation requests.
- Computed hover/focus text contrast exceeds4.5:1 in every theme (minimum5.38:1 in Classic dark).
- Visual inspection of before/after and320px screenshots. Harness/results/screenshots under `/tmp/vps-status-probe-fix/settings-action-*`; representative new hover image: `settings-action-after-hover-1280.png`.
- The real site was not used for fixture setup or setting changes.

Docker deployment passed:

- Supported CLI backups with telemetry created and verified before/after the update: `data/backups/control-{pre,post}-settings-action-20261025.tar.gz`, mode0600. Previous Compose environment saved as `data/backups/deploy-pre-settings-action-20261025.env`.
- Only `deploy-control-1` recreated with `vps-status-control:local-settings-action-0ade51bc67`; healthy, nonroot user, same `deploy_control-data:/data`, loopback18080 and Agent v0.1.0 pin.
- `/readyz`200. Served `index-ByAMsPKG.js` and `index-o0c6uOO_.css` match the browser-tested local production build byte-for-byte.
- Read-only deployed homepage/admin-login smoke passed for en/zh and1280/320px.
- All21 Control tables, schema and master key match the pre-update backup. Evidence: `/tmp/vps-status-probe-fix/settings-action-data-verification.json`.
- No source commit/push; the larger unfinished parent task remains active.
