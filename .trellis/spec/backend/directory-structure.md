# Backend Directory Structure

## Runtime entrypoints

- `cmd/control/` owns flag parsing, process lifecycle, and assembly of Control dependencies. Keep business logic out of `main`.
- `cmd/agent/` owns Agent process flags and lifecycle. It must remain Linux-focused and must not expose an inbound server.
- Both entrypoints obtain build metadata from `internal/versioninfo`; do not duplicate version formatting. See `cmd/control/main.go`, `cmd/agent/main.go`, and `internal/versioninfo/version.go`.

## Internal ownership

- `internal/control/`: HTTP, Agent connection registry, alert orchestration, and application services.
- `internal/agent/`: Linux collection, outbound connection/reconnect logic, and bounded TCP scheduling.
- `internal/protocol/`: the single owner of Agent message types, protocol versions, validation helpers, and wire-level constants.
- `internal/security/`: password, token, session, TOTP, recovery-code, and encryption primitives.
- `internal/storage/control/`: only the low-growth configuration/identity database.
- `internal/storage/telemetry/`: only host/probe rollups, traffic ledger, retention, and disk safeguards.
- `internal/control/webui/`: embedded/fallback Web asset serving. `assets_built.go` and `assets_fallback.go` intentionally provide the same `assets` symbol under opposite build tags.

## Contract and support directories

- `api/` holds HTTP contract documents or generated-contract inputs, not handler implementations.
- `migrations/control/` and `migrations/telemetry/` are separate migration streams. Never combine them through SQLite `ATTACH`.
- `deploy/` holds Docker, systemd, and reverse-proxy artifacts.
- `tests/` is for cross-package integration, failure-injection, and capacity tests; package-local tests stay next to the Go source.

## Naming and dependency direction

- Package names are short lower-case nouns. Exported identifiers require Go documentation comments.
- Prefer dependency flow from entrypoint -> application package -> storage/security/protocol. Storage packages must not import HTTP handlers or UI code.
- Control and Agent may both import `internal/protocol`; they must not import each other's runtime package.
- Keep files cohesive by responsibility. Do not create a generic `utils` package; put a helper with the concept it owns.

## Current examples

- Shared stable concept: `internal/protocol.Version`.
- Shared presentation of build metadata: `versioninfo.String` with table-driven tests in `internal/versioninfo/version_test.go`.
- Build-specific implementation behind one package API: `internal/control/webui/assets_built.go` and `assets_fallback.go`.

## Avoid

- Business logic in `cmd/*/main.go`.
- Agent-specific types inside Control handlers or database models.
- Cross-imports between the two storage packages.
- A second protocol-version constant, asset server, or build-metadata formatter.
