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
