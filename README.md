# VPS Status

VPS Status is a self-hosted, read-only Linux server monitoring project. The repository contains the Control and Agent, bounded Agent v1 protocol, Linux host collection, live/history monitoring, traffic accounting, connect-only TCP probes, live alerts, an administrator UI, and an allowlisted public status page.

Control keeps low-growth identities and configuration in `control.db` while completed host/probe rollups and the compact traffic ledger live in a physically separate `telemetry.db`. Raw three-second reports and individual TCP results stay in memory. The Linux Agent collects host metrics every three seconds and executes at most 64 assigned TCP tasks with bounded DNS, deadlines, and global concurrency. TCP checks only connect and immediately close; they never send payloads or read banners. Remote commands, Web Terminal, file management, and generic task execution are not supported.

## Requirements

- Go 1.24 or newer
- Node.js 22 or newer
- pnpm 10

## Development

Run the Go checks without installing any web dependencies:

```bash
make test-go
make build-go
```

After installing the web dependencies, run all checks:

```bash
pnpm --dir web install
make test
make build
```

`make build` writes `bin/control` and `bin/agent`. The Control build uses the React assets generated into `internal/control/webui/dist`. A normal Go build without the `webui` tag embeds a small fallback page, so Go tests and local backend work do not depend on Node.js.

Both binaries expose build metadata:

```bash
go run ./cmd/control --version
go run ./cmd/agent --version
```

An administrator can create a one-time installation command through
`POST /api/v1/admin/enrollments`. The command enrolls the Agent and writes its
node credential to a mode-0600 config. A configured Agent runs with:

```bash
go run ./cmd/agent --config /etc/vps-status/agent.json
```

See [api/README.md](api/README.md) for enrollment and Agent v1 contracts.

Release builds can inject `VERSION` and `COMMIT`:

```bash
make build VERSION=v0.1.0 COMMIT="$(git rev-parse --short HEAD)"
```

The release script builds the embedded-Web Control, both Linux Agent
architectures, and `SHA256SUMS`:

```bash
scripts/release.sh v0.1.0 "$(git rev-parse HEAD)" dist
```

## Running Control

```bash
go run ./cmd/control --data-dir ./data --listen :8080
```

The administrator API uses `Secure` cookies. Terminate TLS at a reverse proxy before using it from a browser. If both TOTP and recovery codes are lost, an operator with direct access to the Control data directory can reset MFA and revoke every session:

```bash
go run ./cmd/control admin reset-mfa --data-dir ./data
```

The administrator UI is served at `/`. The unauthenticated `/public` page is
disabled by default; enable it and explicitly select published nodes in the
Public page section. Its API is a separate server-side allowlist projection
that does not expose host identity, credentials, probe targets, or history.

Deployment, non-root systemd installation, TLS reverse proxy, credential
rotation, backup/verified restore, and Docker Compose instructions are in
[deploy/README.md](deploy/README.md). The security boundary and scan workflow
are recorded in [deploy/SECURITY.md](deploy/SECURITY.md); the PRD regression
map and bounded 100/400 capacity command are in
[tests/acceptance.md](tests/acceptance.md) and [tests/README.md](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
