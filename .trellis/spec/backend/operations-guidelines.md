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
  enrollment secret, retaining the original two-argument interface. It reads
  current saved defaults, has `Cache-Control: no-store`, and derives from the
  same embedded source as the standalone Release bootstrap.
- `POST /api/v1/admin/enrollments` returns distinct `install_command` and
  `enroll_command` fields plus the one-time token and expiry.
- Control accepts `--agent-release-base-url HTTPS_URL` and
  `--agent-release-version VERSION`.
- A tag release contains `vps-status-agent-linux-amd64`,
  `vps-status-agent-linux-arm64`, `bootstrap-agent.sh`, `install-agent.sh`,
  `uninstall-agent.sh`, `vps-status-agent.service`, the Control binary, and one
  `SHA256SUMS` covering all assets.

### 3. Contracts

- The bootstrap supports Linux amd64/arm64 with systemd, detects architecture,
  downloads one explicit version over HTTPS, verifies every consumed asset
  against `SHA256SUMS`, and only then executes the installer.
- Enrollment downloads the versioned Release `bootstrap-agent.sh` directly;
  the command passes Control URL, token, explicit version and HTTPS release
  base as four shell-quoted arguments. `internal/control/bootstrap-agent.sh`
  is the sole bootstrap source, embedded for the compatibility endpoint and
  copied unchanged into the release directory by `scripts/release.sh`.
- Shared saved address/version behavior is owned by
  [Installation Settings](./installation-guidelines.md). The UI's configured
  release flag describes local eligibility, never a GitHub existence check.
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
- Execute the bootstrap with mocked downloader/installer commands to verify
  successful architecture selection and that checksum failure never executes
  the systemd installer or prints the token.
- Run `sh -n` on release/install/uninstall scripts, build a release-shaped
  directory, and run `sha256sum -c SHA256SUMS` across every asset.
- Keep the tag workflow's published file set equal to the checksum file's
  release asset set whenever an asset name changes.

### 7. Wrong vs Correct

Wrong: build `dist/*` and upload only an Actions Artifact while the installer
points at GitHub Releases.

Correct: verify `SHA256SUMS`, retain the Actions Artifact for CI evidence, and
create or update the matching GitHub Release with the complete `dist/*` set.

## Scenario: Docker Agent and GHCR

### 1. Scope / Trigger

Use when changing Agent images, host mounts, container registration, GHCR
publication or the generated Docker command.

### 2. Signatures

- Image: `ghcr.io/senhao-xu/vps-status-agent:latest`.
- CLI: `--config`, `--proc-root`, `--sys-root`, `--rootfs`, `--os-release`.
- Compose override: optional `VPS_STATUS_AGENT_IMAGE`; no persisted token env.
- Enrollment: `enroll --control-url URL --token TOKEN --config /data/agent.json`.

### 3. Contracts

- `deploy/Dockerfile.agent` cross-compiles CGO-free Linux amd64/arm64 on the
  build platform. Preserve BuildKit automatic `TARGETOS`/`TARGETARCH`; defaults
  inside the build stage can silently turn an arm64 image into an amd64 binary.
- Distroless static nonroot has CA roots and UID/GID65532; `/data` must be
  explicitly copied mode0700 and owned by that user. Verify final image/volume
  permissions: source-directory permissions alone may not survive `COPY`.
- Enrollment runs once in a removable container and writes mode0600
  `/data/agent.json` into a persistent named volume. Runtime reuses the volume
  without enrollment token in its command or environment.
- Runtime uses host network/PID/UTS, all capabilities dropped,
  no-new-privileges and read-only root. Host `/proc`, `/sys`, `/` and resolved
  `/etc/os-release` bind to `/host/proc`, `/host/sys`, `/host/root` and
  `/host/os-release`, read-only. Root bind is nonrecursive; never mount the
  Docker socket or require privileged mode. CLI defaults in the image select
  those paths. Host permissions can still make individual metrics unavailable.
- `deploy/docker-compose.agent.yml` provides profile-gated enrollment and
  runtime sharing a volume; only start runtime after enrollment succeeds.
  Compose must support `bind.recursive: disabled` (CI pins5.5.0).
- `.github/workflows/agent-image.yml` tests then publishes multiarch GHCR
  images using job-scoped `GITHUB_TOKEN` packages:write. Record source/version,
  commit-derived and stable version tags; never use operator OAuth credentials
  in image layers. Binary Release assets remain a separate workflow.
- Publication success and anonymous access are separate checks: verify the
  manifest architectures and anonymous pull before advertising public access.

### 4. Validation & Error Matrix

| Condition | Expected result |
| --- | --- |
| Relative or empty explicit source flag | CLI exit2 before config/network |
| Unreadable selected kernel source | Partial unavailable report; no native fallback |
| Expired/consumed enrollment token |401; `&&` prevents runtime start |
| Wrong credential permissions | Agent rejects config |
| Docker lacks nonrecursive-bind support | Installation fails; no privileged fallback |
| Image published but package private | Anonymous pull fails; report visibility separately |

### 5. Good / Base / Bad Cases

- Good: transient nonroot enrollment writes one credential, runtime restarts as
  the same node and reports host capacity/identity.
- Base: native binary defaults remain the current Linux paths.
- Bad: container OS/root disk or an amd64 ELF is shipped under an arm64 manifest.

### 6. Tests Required

- Fixture and actual websocket tests prove report and hello source overrides,
  native defaults and explicitly missing-source behavior.
- Inspect both built ELF architectures; verify final-image and copied volume
  directory0700/file0600/owner65532, read-only host mounts, nonrecursive root,
  no capabilities/token at runtime, enrollment replay401 and restart continuity.
- Execute the generated command against isolated Control; verify selected-copy,
  one POST on method switching, polling, zh/en/theme/mobile and secret cleanup.
- Actionlint, Compose normalization, image builds, multiarch registry manifests
  and actual anonymous pulls. Publication is not an access test.

### 7. Wrong vs Correct

Wrong: pass `--token` to the persistent container or bind only `/proc` while
leaving hello OS facts and root statfs pointed at the image.

Correct: enroll in a removable container into a persistent private volume;
run without a token and select host sources for both hello and reports.

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
