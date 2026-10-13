# Packaging and Operations Guidelines

## Release contract

- Production Control is one CGO-free binary built with the `webui` tag; the
  generated Vite directory is never tracked or hand-edited.
- Release Agents are CGO-free `linux/amd64` and `linux/arm64` binaries.
- Version and commit are injected through `internal/versioninfo`; release
  artifacts are accompanied by `SHA256SUMS` and built with `-trimpath` and an
  empty Go build ID.
- Production containers contain neither Node.js nor a package manager and run
  as a non-root user with a persistent `/data` directory.

## Agent operations

- Package installation may require root, but the Agent runtime must use the
  dedicated locked system user and must not acquire capabilities or writable
  host paths.
- Re-running the installer preserves an existing mode-0600, service-owned
  credential config and restarts the service with the replacement binary;
  first enrollment is required only when no config exists.
- Credential rotation uses a mode-0600 file and the Agent's atomic config
  replacement command. Never accept the new credential as a command-line
  value in the replacement workflow.
- Uninstall retains configuration by default; an explicit purge is destructive
  and must be named as such.

## Scenario: One-command Agent installation release

### 1. Scope / Trigger

Use this contract when changing enrollment commands, the public bootstrap
script, Agent release asset names, release mirrors, or the tag release
workflow. These pieces form one installation boundary and must change together.

### 2. Signatures

- `GET /install-agent.sh` returns the public bootstrap script without an
  enrollment secret.
- `POST /api/v1/admin/enrollments` returns distinct `install_command` and
  `enroll_command` fields plus the one-time token and expiry.
- Control accepts `--agent-release-base-url HTTPS_URL` and
  `--agent-release-version VERSION`.
- A tag release contains `vps-status-agent-linux-amd64`,
  `vps-status-agent-linux-arm64`, `install-agent.sh`,
  `uninstall-agent.sh`, `vps-status-agent.service`, the Control binary, and one
  `SHA256SUMS` covering all assets.

### 3. Contracts

- The bootstrap supports Linux amd64/arm64 with systemd, detects architecture,
  downloads one explicit version over HTTPS, verifies every consumed asset
  against `SHA256SUMS`, and only then executes the installer.
- The Release base URL has no credentials, query, or fragment. The version is
  a path-safe 1–64 byte identifier. Neither value may be interpolated into the
  shell script before normalization and shell quoting.
- The token remains a short-lived, single-use command argument; installers
  never print it. Enrollment still sends it only in the Authorization header.
- `.github/workflows/release.yml` must publish the complete verified `dist/`
  set to the GitHub Release matching the tag, not only to an Actions Artifact.

### 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Non-HTTPS Release base, credentials, query, or fragment | Control startup fails |
| Version contains a slash, shell syntax, or exceeds 64 bytes | Control startup fails |
| Unsupported OS/architecture or missing required command | Bootstrap exits before download/install |
| Missing, duplicate, malformed, or mismatched checksum | Bootstrap exits before installer execution |
| Enrollment token expired/used | Existing `401 enrollment_invalid` behavior |
| Tag workflow rerun | Existing Release assets are uploaded with `--clobber` |

### 5. Good / Base / Bad Cases

- Good: a tagged Control generates a command that downloads the same tag's
  amd64/arm64 Agent and verified systemd assets, then enrolls once.
- Base: an operator points a source build or mirror at an explicit published
  version with both release flags.
- Bad: the build creates correct files but the workflow only stores an Actions
  Artifact; the default `/releases/download/<tag>/...` installer then 404s.

### 6. Tests Required

- Unit-test Release URL/version injection rejection and shell syntax of the
  rendered public bootstrap.
- Contract-test that enrollment returns both command fields and never places
  the token in a URL query.
- Run `sh -n` on release/install/uninstall scripts, build a release-shaped
  directory, and run `sha256sum -c SHA256SUMS` across every asset.
- Keep the tag workflow's published file set equal to the checksum file's
  release asset set whenever an asset name changes.

### 7. Wrong vs Correct

Wrong: build `dist/*` and upload only an Actions Artifact while the installer
points at GitHub Releases.

Correct: verify `SHA256SUMS`, retain the Actions Artifact for CI evidence, and
create or update the matching GitHub Release with the complete `dist/*` set.

## Backup boundary

- Default backups include `control.db` and the paired `master.key`.
  `telemetry.db` is independent and opt-in.
- Use SQLite online snapshots rather than copying an active main file without
  its WAL. Archive members are allowlisted, checksummed, and restored only
  after SQLite `quick_check` and schema-marker verification.
- Restore refuses to overwrite an existing data directory. Operators stop
  Control and cut over to a newly verified directory.

## Capacity evidence

- The harness is bounded to 100 Agents, 64 assignments per Agent, and one hour.
- Successful evidence requires every Agent host series, every configured probe
  assignment series, the alert path, maintenance, writable storage, and zero
  dropped telemetry writes; a single sampled series is not sufficient.
- `--baseline` accepts only 100 Agents, four TCP assignments each, and at least
  two minutes. The release run is ten minutes on externally evidenced 2 vCPU / 
  2 GiB limits.
- Smoke output is useful regression evidence but must never be described as the
  resource-constrained baseline.
- CPU affinity, `GOMAXPROCS`, and `GOMEMLIMIT` are useful shape-test controls,
  but a Go soft memory limit is not an externally enforced 2 GiB ceiling. The
  release baseline requires recorded cgroup or VM CPU and memory constraints.
- Do not use `RLIMIT_AS` as the memory-capacity proxy for Go. It limits virtual
  address reservations, so the runtime can fail to reserve an arena while
  resident/working memory remains far below the nominal limit. Such a failure
  is an invalid harness constraint, not evidence of product OOM behavior.

## Required verification

Run the full matrix in `tests/acceptance.md`, dependency/container scans, Docker
Compose config validation, shell syntax checks, release checksum validation,
and one bounded load smoke. Keep the capacity checklist open until the exact
resource-constrained baseline has actually passed.
