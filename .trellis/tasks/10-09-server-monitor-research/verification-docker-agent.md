# Docker Agent / GHCR verification (2026-10-25)

## Implementation

- Source branch `codex/agent-docker-ghcr`, initial implementation commit `d2fa9ee`.
- Draft PR: https://github.com/senhao-xu/vps-status/pull/1 (attached to this task).
- Linux amd64/arm64 Agent image, GHCR workflow, transient registration and
  hardened persistent runtime, Compose and selectable systemd/Docker UI.
- Same explicit host source paths drive hello and reports. No protocol/schema
  addition; includes the prerequisite existing four-field enrollment decoder fix
  and explicit local-test installation entry. The previously separate probe repair is subsequently committed as `362ec9e`.

## Completed local evidence

- Frontend lint/typecheck/build and273 tests pass. Assets:
  `index-D7crBSUT.css`, `index-CyYJCRXj.js`.
- Go full tests/race/vet/tagged tests and CGO-free Linux amd64/arm64 builds pass
  before compiler repair; Darwin arm64 stub compiles.
- Actual Docker AND Compose enrollment/report/restart checks pass against fresh
  Control18895/TLS18896, no actual18080 site mutation. Host hostname, OS, kernel,
  memory/root-disk capacity/network counters match host; no unavailable metrics.
  Config owner65532/file0600/directory0700, read-only/nonrecursive mounts, no
  capabilities/privileged/runtime token, replay401 and same-node restart pass.
  Evidence `/tmp/vps-status-docker-agent/container-check.json`; reproducible
  harness `container-check.cjs` accepts `VPS_STATUS_AGENT_CHECK_IMAGE`.
- Mocked real-component browser and actual fresh Control browser BOTH pass16
  zh/en × Emerald/Classic × light/dark ×1280/320 variants. Verify selected copy,
  keyboard radio choice, single POST on method switching, no horizontal overflow,
  secret cleanup and polling. Actual generated Docker command runs successfully
  with only registry image replaced by local packaging image and pull skipped;
  online and same-node restart pass. No real site settings/credentials/nodes used.
  Evidence `/tmp/vps-status-docker-ui/{evidence,ui-evidence}.json` and
  `real-check.cjs`; screenshot has all commands masked.
- Actionlint1.7.7, shell syntax, Compose5.5 normalization, explicit image ELF
  architecture checks and nonroot data-volume copy-up permissions pass.
- Independent Trellis review: `verification-docker-review.md`, no actionable
  findings.5 Agent/20 TCP assignment/15s bounded smoke passes all host/probe
  series, alert/maintenance/writable telemetry and zero drops; not a resource
  constrained capacity baseline.

## GitHub publication / security

- Existing CI workflow ID381773391 dispatches branch-local publication input
  before default-branch merge. First run40168941924 was canceled before push
  after PR Security40168984819 revealed inherited Go1.24 standard-library CVEs.
- Security repair commit `3b7b3c0`: Go1.27.3 toolchain/build images,
  compatible govulncheck1.9, only x/crypto0.55 and required x/sys0.47.
  These dependencies require minimumGo1.25. Module verification and old/new
  Argon2id default/test hash compatibility pass. Full Go/race/vet/tagged and
  actual final-image Docker/Compose enrollment/host/restart pass again.
- Trivy0.70: source and both Agent image architectures have0HIGH/CRITICAL
  vulnerabilities/misconfigurations and0secrets, with no suppressions.
  Govulncheck:0reachable/0imported-package vulnerabilities;3 advisories remain
  in required but unused modules (not an all-module-zero claim).
- GitHub Security run40169655300 passes dependencies and Control/Agent image
  scan jobs. Publication run40169653139 stopped on a preexisting cancellation
  race: SetReadDeadline sees a connection already closed by canceled context;
  the error must preserve context.Canceled. Fixed and verified below.
- Final source `a82dbabe64981ac3ae32763fb7a8535f8f974220` normalizes failed
  session I/O cancellation errors. Deterministic real TCP regression reproduces
  five boundaries before the fix; original/new tests pass100race repetitions,
  independent reviewer20repetitions and fullGo/race/vet/tagged checks.
- Final workflow run40170241222 succeeds (validate + publish). PR CI and Security
  also succeed. Post-push workflow verifies actual Linux amd64+arm64 manifest
  and reports package visibility `public`.
- Published image: `ghcr.io/senhao-xu/vps-status-agent:latest`, immutable index
  `sha256:8784ceb33080620a106cba67d409ceeec08ad0282a7a156bff3ec7ce13511d02`.
  SHA tag is `sha-a82dbabe64981ac3ae32763fb7a8535f8f974220`.
- Fresh empty Docker config anonymously pulls amd64 AND arm64 successfully;
  local registry-manifest read also confirms both platforms. The actual arm64
  registry binary is AArch64 ELF, Go1.27.3, CGO0 (extracted without executing).
  Native published `--version` records the expected finalsource and protocolv1.
- Exact UI-generated Docker command now runs with the real GHCR image and
  actual pull, with no local image replacement. It registers/comes online and
  restarts as the same node against freshly built isolated Control. Final actual
  GHCR-image Docker AND Compose harness also passes all host facts/capacity,
  network counters, permissions, single-use token, hardening/mounts and restart
  checks. Temporary resources are cleaned; actual18080 data is untouched.

## Actual local Control update

- Before image switch, created and verified telemetry-inclusive online backup:
  `data/backups/control-pre-agent-docker-20261025.tar.gz`; preserved original
  deployment env as `deploy-pre-agent-docker-20261025.env`.
- Previous running image `vps-status-control:local-install-2b97ac1716`, real data
  volume `deploy_control-data`, loopback18080 and runtime Agent releasev0.1.0.
- Immediately before switching, made another verified telemetry-inclusive
  snapshot `control-pre-agent-docker-switch-20261025.tar.gz`.
- Running image now `vps-status-control:local-agent-docker-3b7b3c0`, versiondev,
  commit3b7b3c0-dirty. Recreated only Control with original compose override,
  data volume, localhost18080 and runtime Agent releasev0.1.0 retained.
- Healthy/readiness/version/new JS+CSS checks pass. Read-only actual homepage
  and administrator login page smoke passes zh/en ×1280/320 with no overflow
  or JS errors. Post-switch verified backup:
  `control-post-agent-docker-20261025.tar.gz`.
- SQLite quick_check, all21 Control tables and complete schema match the
  immediate pre-switch snapshot; paired master.key bytes are unchanged.

The larger parent task remains active for its outstanding capacity/security work.

## Durable local evidence

Sanitized JSON reports and reusable fixture harnesses are preserved under
`data/verification/agent-docker-20261025/` (ignored, private permissions),
including registry, published-container, browser, deployed-read-only and Trivy
reports. No actual site credential or enrollment secret is in this evidence.

## Remaining-change commit synchronization

The user explicitly requested committing and pushing the remaining changes on
2026-10-25. Probe assignment repair is committed as `362ec9e`; installation/
focus/Docker evidence and task records are collected with it on the existing
PR branch. No code, container, site data or published Agent image was changed
by this Git-only follow-up. GHCR continues to identify its verified Agent
source `a82dbab`; the additional production change concerns Control probe
assignment editing. The larger parent task remains active.
