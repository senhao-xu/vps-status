# Local installation without a saved address — 2026-10-25

The user correctly noted that a blank address at the current localhost deployment disables command creation. The default remote workflow intentionally rejects local origins, while the API and Agent already support explicit loopback HTTP for local development.

Changed the owning frontend resolver and add-node dialog to expose an unchecked “Local testing only / 仅在本机测试” choice when saved defaults are blank and the current origin is valid loopback. Explicit selection sends the canonical local address through the existing `control_url` field. Clear help explains that the Agent must run on the same computer as the browser. Remote settings, saved-address priority, HTTPS, release eligibility, mutation guards and secret lifecycle remain unchanged.

Integration uncovered a separate blocker: an actual pre-fix Agent consumes a token from Control but rejects the response because its strict decoder declares two fields while the documented server response contains four. Added the already-documented `node_name` and `websocket_path` string fields to the Agent decoder. Unknown fields, malformed field types, trailing content, response bounds and redirect refusal remain enforced. Existing two-field compatibility remains. No server wire/schema changes.

Validation passed:

- Pre-fix UI baseline: no local choice, blank localhost settings block creation. Pre-fix actual Agent binary: registration fails with `enrollment response is invalid`, no config saved, despite consumed token.
- Frontend lint/typecheck, all268 tests and production build. Includes explicit opt-in, canonical IPv4 aliases/IPv6/mapped-loopback, invalid addresses, saved precedence and no invalid-saved fallback.
- Go full tests, race, vet, tagged tests, tagged Control build and actual Agent build.
- New real Control-Agent integration regression under `tests/enrollment`: independent credential, associated node/name/status, mode0600 private config round-trip, replay rejection and single-node guarantee. Test went red before the fix and green after it.
- Isolated real HTTP localhost browser/Agent harness:16 combinations of zh/en, Emerald/Classic, dark/light,1280/320px. Default blocked, Tab/Space opt-in, reversible checkbox, same-origin canonical request, no double submit, disabled controls during request, empty settings unchanged, no secret in browser preferences, memory-only choice resets after reopening. Actual temporary Agent CLI enrolls and connects; dialog reaches online. Duplicate token returns401. Release guard and real saved-remote priority remain.
- Before/after screenshots visually inspected. Harness and evidence under `/tmp/vps-status-probe-fix/local-install-*`; both Control data and Agent config are temporary isolated fixtures. No real Agent installation/service or actual-site setting mutation.

Independent full-scope Trellis review passed: no source findings or modifications. Corrected README wording to specify the browser's computer, which is important when viewing a remote panel through an SSH tunnel.

Local Agent binary built at `data/bin/vps-status-agent-local-linux-amd64`, CGO0 Linux amd64, versiondev/commit2e121ceecd0c464cddb48215052b30a89ceaa3b5-dirty-2b97ac1716. Agent source fix must be included in a future published Agent release; no existing release asset is modified. The local artifact is provided separately while preserving the current deployment's release pin. Download availability or contents of existing published v0.1.0 assets have not been verified/updated by this work.

Local Docker deployment passed:

- Supported CLI pre/post backups with telemetry created, verified and copied to `data/backups/control-{pre,post}-local-install-20261025.tar.gz`, mode0600. Previous Compose environment saved as `data/backups/deploy-pre-local-install-20261025.env`.
- Only `deploy-control-1` recreated with `vps-status-control:local-install-2b97ac1716`; healthy and nonroot. Preserved `deploy_control-data:/data`, loopback18080 and Agent v0.1.0 release pin.
- `/readyz`200; served `index-D-yAPX7V.js` and `index-o0c6uOO_.css` match the verified local build byte-for-byte. Read-only deployed homepage/login smoke passes en/zh and1280/320px.
- All21 Control tables, schema and master key match the pre-update backup. Evidence: `/tmp/vps-status-probe-fix/local-install-data-verification.json`.
- Local Agent checksum saved beside the binary as `.sha256`. No real-site Agent installation, credentials/settings changes, release publication or source commit/push. Larger parent task remains active.
