# Backend Quality Guidelines

## Required checks

Run from the repository root:

```bash
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
```

After `web/` has been built into `internal/control/webui/dist`, also run:

```bash
go test -tags webui ./...
go build -tags webui ./cmd/control
```

The CI workflow in `.github/workflows/ci.yml` is the executable reference. The root `Makefile` owns common local targets.

## Testing pattern

- Keep unit tests beside packages. Use `t.Fatalf` with actual/wanted values and close resources with `t.Cleanup`, following `internal/control/webui/assets_test.go` and `internal/versioninfo/version_test.go`.
- Prefer table tests when one contract has multiple representative cases.
- Cross-package integration, failure injection, and capacity workloads belong under `tests/`.
- A build tag with two implementations must be tested both with and without the tag.
- Network/storage/security work needs cancellation, timeout, boundary, and race coverage.
- WebSocket slow-peer tests should use a real deterministic transport such as `net.Pipe` when possible, so write deadlines are exercised rather than inferred only from channel-capacity tests.

## Shared contracts

- Define protocol constants and message decoding once in `internal/protocol`.
- Define each validation rule at the untrusted entry boundary and independently repeat only the security backstop required by the design, such as Control and Agent TCP limits.
- If a stable value must change together across packages, give it one owner; do not duplicate literals.

## Scope and safety gates

- Agent changes may not add shell, file, terminal, process execution, inbound listeners, or generic RPC.
- Public API types must be separate allowlist DTOs; never serialize an internal node/storage model directly.
- Generated assets, binaries, databases, WAL/SHM, coverage, and dependencies remain ignored according to `.gitignore`.
- Do not silently add CGO or platform-specific dependencies; Linux amd64 and arm64 release builds must remain possible.

## Review checklist

- Error and cancellation paths are tested.
- Goroutines, channels, timers, response bodies, rows, and sockets have clear owners and close paths.
- Queues, frames, retries, concurrency, and memory growth are bounded.
- Linux procfs fixtures cover CPU guest double-counting, counter reset, interface changes, missing files, and streaming directory bounds.
- No secrets appear in source fixtures, logs, URLs, errors, or snapshots.
- A change to a schema/protocol/public DTO updates its migration/decoder/consumer tests in the same change.
