# monitor-style TCP configuration verification

Date:2026-10-15. User chose monitor-style TCP configuration after reviewing the original creation form. Source/scope: research/monitor-tcp-configuration.md.

## Implemented

Single host:port target including bracketed IPv6; default60s, default-all existing nodes and default-enabled future-node inclusion; group/search selection and select/deselect shown preserve hidden choices. Editor retains draft/explicit overrides while receiving refreshed automatic assignments. Compact no-node state and concise method description replace the large empty picker and protocol-internal explanation. Existing task/node views and latency detail/history remain.

Control schema10 adds checked auto_join defaultfalse for old tasks. DTO emits bool; POST omission defaultsfalse and optional PATCH preserves omitted fields. Enabled definitions and each node remain bounded64. Future enrollment atomically creates assignments and advances snapshot version before first Agent hello; Agent protocol and published v0.1.0 installation pin remain compatible.

Independent review found a real enrollment/PATCH assignment-loss race. Fixed with the storage-owned common mutation transaction: PatchProbeTask merges pointers against committed values and ChangeProbeAssignment changes only the specified node. Omitted node_ids cannot overwrite concurrent automatic assignments; explicit [] remains full clearing. Deterministic gated-body enrollment regression and assignment endpoint cases pass. Final independent review has no remaining findings.

## Gates

- Frontend lint, strict typecheck,210 tests and production build pass. Final assets index-Bkhr0Se3.js and index-Dv5VNDIC.css.
- Final backend source passes go test ./..., go test -race ./..., go vet ./..., go test -tags webui ./... and tagged Control build. Relevant suites include64/65 concurrent quotas, failure rollback, schema1–9 pre-mutation backups, legacy state preservation, optional PATCH and assignment concurrency.
- git diff --check passes. No runtime dependencies, detection modes, Agent message changes, public data changes or unrelated features added.

## Isolated migration and browser

Current deployed schema9 Control binary seeds a separate administrator, four nodes and one legacy task under /tmp/vps-monitor-config-browser/data. Supported CLI creates and verifies schema9.tar.gz. Final backend restores that snapshot into a new test directory and migrates it to10 before browser validation; real deployment data is never used as browser fixture.

Final script /tmp/vps-monitor-config-browser/check.mjs reports ALL MONITOR CONFIG BROWSER CHECKS PASSED:

1. Legacy target/interval/assignments preserved and auto_join=false after9->10.
2. Form has unified target, no separate host/port, defaults60/all/true; invalid URLs/bareIPv6/ports/paths/whitespace emit no mutation. Real bracketed IPv6 create round-trips host/port correctly.
3. Group named all is distinct from All groups; group/search select/deselect changes only shown IDs and preserves hidden choices.
4. Real later enrollment receives automatic task; legacy false task excludes it. New enrollment during an open edit arrives through parent refresh; explicit deselection and edited name survive. Turning policy off preserves chosen existing nodes and excludes the next enrollment.
5.409 quota feedback preserves draft; pending write blocks duplicate submit and Escape; success closes and restores focus.
6.32 en/zh × Emerald/Classic × light/dark ×1280/320 combinations pass create/edit/group/search, keyboard and horizontal-overflow checks. Native browser chrome traversal is permitted when document.hasFocus=false. Compact no-node state separately tested. Zero page errors.

Temporary evidence: result-final.log, create-zh-desktop.png, create-zh-mobile.png, create-empty-zh.png under /tmp/vps-monitor-config-browser/. Initial harness rerun corrected Escape delivery after Tab moved focus into browser chrome; no product changes were required for that harness issue.

## Deployment

Pre-update schema9 backup verified, including paired key and telemetry: data/backups/control-pre-monitor-config-20261015.tar.gz (0600). Prior ignored deployment env copied to data/backups/deploy-pre-monitor-config-20261015.env (0600), prior image retained as vps-status-control:pre-monitor-config-20261015. Source commit remains uncommitted/pending previous workflow confirmation; Docker uses dev metadata plus a dirty snapshot hash.

IMPORTANT rollback: schema10 cannot be opened by old schema9 binaries. Use the verified pre-update backup restored into a new data directory/volume, paired key and intended telemetry, with prior image/env; image-only downgrade against schema10 is invalid.


### Completed local Docker deployment

- Built through the existing Dockerfile and two Compose files. First attempt failed on a transient Docker Hub metadata timeout; unchanged build retry succeeded. Final image: vps-status-control:local-monitor-5dc92dd814.
- Binary metadata: version dev, commit927ce78193dae18ef36ac93e098bb0477e4fbaa8-dirty-5dc92dd814, protocolv1. No source commit/push/Release created.
- Only deploy-control-1 recreated with --no-deps. Container healthy, healthz/readyz ok, original deploy_control-data and127.0.0.1:18080 retained; Agent install pinv0.1.0 retained. Other containers left intact.
- Supported online post-backup verified: data/backups/control-post-monitor-config-20261015.tar.gz (0600), includes paired key/telemetry. Pre/post comparison confirms schema9->10, master key identical, every prior column/row across20 non-migration configuration tables identical, all migrated tasks auto_join=false, SQLite quick_check ok. Only expected migration metadata and new default column differ.
- Served index-Bkhr0Se3.js/index-Dv5VNDIC.css match verified frontend build byte for byte. Served JS includes unified target/automatic-join copy and auto_join DTO. Real deployed public/admin-login Chromium smoke passed at1280/320px, zero errors/overflow; existing administrator retained. No real credentials/task mutations used for smoke.
