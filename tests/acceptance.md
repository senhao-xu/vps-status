# PRD acceptance regression map

This map ties each acceptance item to an executable owner. Commands are run
from the repository root; package tests contain the detailed negative and
failure-injection assertions.

| PRD acceptance item | Primary evidence |
|---|---|
| Single administrator, password KDF, login/logout/session/rate limit | `go test ./internal/security ./internal/storage/control ./internal/control -count=1` (`TestAdminAuthenticationMFAAndSessionLifecycle`, limiter/session tests) |
| TOTP, recovery codes, local reset | same command plus `go test ./cmd/control -count=1` |
| One-time enrollment, digest-only credentials, revoke/rotate | `go test ./internal/agent ./internal/storage/control ./internal/control -count=1` |
| Shared installation defaults, dev guard, versioned GitHub bootstrap and checksum failure paths | `go test ./internal/storage/control ./internal/control -count=1`; `sh -n internal/control/bootstrap-agent.sh`; release `sha256sum -c SHA256SUMS` |
| Non-root Linux amd64/arm64 Agent, WSS, prohibited capability absence | CGO-free cross-build commands below, protocol allowlist tests, and `deploy/systemd/vps-status-agent.service` review |
| Three-second live state; no raw SQLite rows | `go test ./internal/control ./internal/storage/telemetry -count=1` (`TestHostHistoryKeepsRawReportsInMemoryUntilMinuteCompletes` and schema boundary tests) |
| 1m/10m/1h retention and ~2,856 non-overlapping buckets | `go test ./internal/storage/telemetry -count=1` retention/rollup tests |
| control.db does not grow with telemetry | storage schema-boundary tests in both storage packages |
| Clear history preserves config/ledger; reset is separate | telemetry maintenance and CSRF/confirmation API tests |
| Telemetry failure preserves auth/config/live/alerts | telemetry degradation and alert-manager tests |
| Storage sizes/oldest/freelist, chunked reclaim | telemetry stats/maintenance tests and administrator API tests |
| 65th TCP task and invalid interval rejected on both sides | `go test ./internal/protocol ./internal/agent ./internal/storage/control ./internal/control -count=1` |
| TCP connect-only/address/banner boundary | Agent probe tests using recording connections and invalid target cases |
| Alert state/delivery/retry/storm prevention | alert, notification, persistence, restart, timeout, 429/5xx tests in `internal/control` and `internal/storage/control` |
| Public allowlist and private-field negative assertions | `go test ./internal/control ./internal/storage/control -run 'Public' -count=1` plus Web public decoder/render tests |
| Embedded Control, two Agent architectures, Docker without Node runtime | `scripts/release.sh`, checksum validation, tagged Go test/build, Dockerfile stage inspection and image scan |
| 100 online Agents / 400 TCP assignments on 2 vCPU / 2 GiB | `go run ./tests/load --baseline --agents 100 --tasks-per-agent 4 --duration 10m --output load-100x400.json` under an externally verified cgroup/VM limit; success checks every host/assignment series and zero dropped telemetry writes and is not replaceable by smoke evidence |

Full release gate:

```bash
test -z "$(gofmt -l .)"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
corepack pnpm@10.18.0 --dir web lint
corepack pnpm@10.18.0 --dir web typecheck
corepack pnpm@10.18.0 --dir web test
corepack pnpm@10.18.0 --dir web build
go test -tags webui ./... -count=1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/agent
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/agent
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags webui ./cmd/control
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags webui ./cmd/control
docker compose -f deploy/docker-compose.yml config
sh -n scripts/release.sh deploy/systemd/install-agent.sh deploy/systemd/uninstall-agent.sh
```

The load command's JSON records only application observations. A valid baseline
report also includes the external CPU/memory constraint, kernel/cgroup details,
wall time, and scanner/release revision. Do not mark the capacity criterion
complete from an unconstrained local smoke run.

## Recorded M8 execution status

On 2026-10-12 the full Go, race, and tagged-WebUI suites passed after the prior
Web lint/typecheck/test/build and cross-architecture build gates. The bounded
5×4 smoke also passed every application assertion with zero telemetry drops.
A 100×4, 123.306-second shape run passed with two-CPU affinity,
`GOMAXPROCS=2`, and `GOMEMLIMIT=1536MiB`, but it is not the acceptance baseline:
cgroup v2 was read-only, so an external 2 GiB ceiling was not enforced or
proven. A subsequent 100×4 sustained run passed for 603.344807018 seconds with
the same CPU affinity and Go runtime settings: all 100 Agents and 400 probe
assignments were represented, all application assertions passed, and telemetry
dropped zero writes. It still recorded `baseline_claim=false` because the host
could not enforce/prove the required memory ceiling. An `RLIMIT_AS=2 GiB`
attempt is excluded from capacity conclusions: Go failed a virtual-address
reservation while resident use was far below the limit, so this was not a
product OOM. The final table row therefore remains pending until a writable
cgroup or VM/Docker limit provides external 2-vCPU/2-GiB evidence.
