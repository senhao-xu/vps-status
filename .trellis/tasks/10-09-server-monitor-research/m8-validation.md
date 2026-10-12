# Milestone 8 validation record

Date: 2026-10-11 through 2026-10-12 (Asia/Shanghai)

## Passed in this workspace

- `test -z "$(gofmt -l .)"`
- `go vet ./...`
- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go test -tags webui ./... -count=1`
- Non-listening package tests: protocol, security, both storage packages,
  backup, version metadata, Web fallback, and both command packages.
- Selective Control tests for origin/security headers, WSS connection limiter,
  public DTO negative boundary, alert state, telemetry-independent alerts, and
  registry-wide shutdown.
- Race tests for protocol, security, both storage packages, backup, command
  packages, and selective Control live/history/public/alert tests.
- Web lint, strict typecheck, 36 Vitest tests, and production Vite build.
- `go test -tags webui ./... -run '^$'` (all tagged packages compiled).
- CGO-free Linux amd64/arm64 builds for Control and Agent. Native binaries
  printed `version=dev commit=unknown protocol=v1`; `go version -m` confirmed
  `CGO_ENABLED=0`, target OS/architecture, and Control's `webui` tag.
- `scripts/release.sh v0.0.0 deadbeef ...` produced embedded-Web Control,
  amd64/arm64 Agents, valid `SHA256SUMS`, and injected metadata. A second build
  with the same inputs produced an identical checksum manifest. Strict release
  input validation rejects values that could alter linker flags, and native
  artifacts must print the exact version, commit, and protocol contract.
- `docker compose -f deploy/docker-compose.yml config` and shell syntax checks
  for release/install/uninstall scripts.
- Backup tests exercised default and telemetry-inclusive creation, verification,
  atomic restore into a new path, overwrite refusal, manifest-member coverage,
  future/incomplete-schema rejection, and master-key authentication of stored
  ciphertext. The final path audit additionally verifies that an existing
  archive is preserved, source data members and restore targets cannot be
  symlinks, parent/nested traversal and tar link members are rejected, and
  archive/restore publication uses Linux `RENAME_NOREPLACE` so a target that
  appears during verification cannot be replaced.
- The nginx example now separates Agent WebSocket upgrades from the unbuffered
  administrator SSE stream. Proxied same-origin checks cover both external
  scheme and host.
- A loopback-enabled 5-Agent × 4-assignment × 15-second smoke passed with all
  5 Agents online, all host/probe series present, the alert state firing,
  maintenance successful, telemetry writable, and zero dropped writes. Its
  observed storage/runtime values were 53,248-byte main database, 135,992-byte
  WAL, 71,575,104-byte heap allocation, and 89,878,560-byte Go system memory.
  The harness correctly recorded `baseline_claim=false`.
- A larger 100-Agent × 4-assignment shape test ran for 123.306 seconds with
  `taskset -c 0,1`, `GOMAXPROCS=2`, and `GOMEMLIMIT=1536MiB`. All 100 Agents
  were online; every host/probe series was present; alert firing, maintenance,
  writable telemetry, and zero dropped writes all passed. The observed main
  database was 155,648 bytes, WAL 1,054,752 bytes, heap allocation 7,083,184
  bytes, and Go system memory 156,893,232 bytes; the process reported two
  visible CPUs. It correctly recorded `baseline_claim=false` because this was
  not the externally constrained release baseline.
- A sustained 100-Agent × 4-assignment shape test then ran for
  603.344807018 seconds using:

  ```bash
  timeout 780s env GOMAXPROCS=2 GOMEMLIMIT=1536MiB taskset -c 0,1 \
    /tmp/vps-status-load --agents 100 --tasks-per-agent 4 --duration 10m \
    --output /tmp/vps-status-load-100x4-10m.json
  ```

  It passed with all 100 Agents online, all 400 assignments represented,
  every host/probe series present, alert state `firing`, maintenance successful,
  telemetry writable, and zero dropped writes. The observed main database was
  610,304 bytes, WAL 1,153,632 bytes, heap allocation 6,958,352 bytes, and Go
  system memory 156,893,232 bytes; the process reported two visible CPUs. It
  correctly recorded `baseline_claim=false`: CPU affinity was exercised, but
  an externally enforced 2 GiB memory ceiling was still unavailable.

All Go cache and release outputs used `/tmp`; generated Vite assets remain
ignored and are not source artifacts.

## Environment-limited checks (not claimed passed)

- Initial restricted-sandbox attempts could not bind localhost, but the main
  session subsequently ran and passed the complete Go, race, tagged-WebUI, and
  load-smoke commands in a loopback-enabled environment. The successful
  results above supersede those initial environment failures.
- The exact 100-Agent/400-assignment baseline was not run under externally
  evidenced 2 vCPU/2 GiB limits. CPU affinity, `GOMAXPROCS`, and the Go soft
  memory limit exercised the desired shape, but cgroup v2 was mounted read-only,
  so no 2 GiB child cgroup/VM memory ceiling could be enforced or proven. Its
  checklist remains open.
- An attempted `prlimit --as=2147483648` run is not capacity or OOM evidence.
  `RLIMIT_AS` counts the Go runtime's reserved virtual address space rather
  than resident/working memory; the runtime failed while reserving a 4 MiB
  virtual block even though only about 124,649,472 bytes were in use. This
  demonstrates that `RLIMIT_AS` is an invalid proxy for the required 2 GiB
  cgroup/VM memory limit, not that the product exhausted 2 GiB.
- `pnpm audit` could not reach registry.npmjs.org (`EAI_AGAIN`). `govulncheck`
  and Trivy are not installed locally. The Docker socket is inaccessible, so
  the image could not be built/scanned here. The scheduled/PR security workflow
  contains dependency, filesystem/config/secret, image-build, and image-scan
  enforcement for a networked runner.

## Required follow-up

Run the 100×4 workload for the documented 10 minutes under an externally
enforced and recorded 2-vCPU/2-GiB child cgroup or VM. Archive its JSON plus the
cgroup/VM configuration before checking the sole remaining capacity item.
Networked CI must also run the dependency and container scanners described
above; the implementation/full-regression checklist does not substitute for a
clean, current vulnerability database result.
