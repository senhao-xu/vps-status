# Security review and operating boundary

Review date: 2026-10-11. This document records the M8 review surface and the
checks that must remain in release CI.

## Permanent Agent boundary

The Agent is outbound-only and its complete protocol allowlist remains
`hello`, `report`, `probe.sync`, `probe.result`, `ping`, `pong`, and `error`.
There is no remote command, terminal, file/process/container API, plugin RPC,
or generic task execution message. TCP probes validate task count, interval,
address class, DNS result count, concurrency, timeout, and result rate on both
sides; a successful probe sends and reads no application bytes.

The systemd service uses a dedicated locked user, no new privileges, a
read-only system view, no devices, and only Internet/Unix address families.
It does not need root at runtime or access container-runtime sockets.

## Secrets and logs

- Enrollment/node/session/CSRF/TOTP/recovery/master/notification secrets are
  never normal log fields. Notification APIs never read stored secrets back.
- Node credentials and recovery values are stored only as digests. Reversible
  notification/TOTP values use the deployment master key.
- Top-level terminal errors pass through the centralized redactor, including
  Authorization Bearer values, secret query parameters, URL userinfo, and
  Telegram bot URL tokens. Tests assert sentinel secrets are absent.
- Full protocol frames, reports, webhook bodies, database rows, and Agent
  configuration records must not be added to logs during debugging.

## HTTP and browser boundary

- Mutating administrator endpoints require an authenticated SameSite=Strict,
  Secure, HttpOnly session plus a matching CSRF header/digest. Login/setup are
  bounded by independent fixed-window limiters.
- Requests carrying a foreign `Origin` or `Sec-Fetch-Site: cross-site` are
  rejected. Origin comparison includes the externally forwarded scheme and
  host. Control intentionally emits no permissive CORS header.
- CSP, frame denial, no-referrer, no-sniff, and API no-store headers are set by
  one outer middleware. Reverse proxies must preserve `Host`.
- The only unauthenticated JSON endpoint is `GET /api/v1/public/status`; its
  DTO is constructed from an explicit field allowlist and negative tests cover
  hostname, IP-like facts, OS/kernel/architecture, credentials, probe targets,
  notification data, IDs, and history. Setup status and Agent endpoints have
  separate fixed contracts; unknown `/api/` paths return JSON 404.
- `/healthz` and `/readyz` disclose only `ok` and no database/runtime details.

## WSS and resource bounds

- Agent connection attempts are rate limited per remote address before
  credential lookup and WebSocket upgrade.
- Authorization headers are the only credential transport. Origin-bearing
  browser upgrades are rejected. Frames, JSON shape, sequence, session ID,
  hello time, heartbeat, reads/writes, report frequency, queues, and one active
  connection per node are bounded.
- Live SSE subscribers, telemetry writes, alert evaluations, notification
  delivery/outbox, response bodies, database reads, and cleanup batches have
  hard bounds. Slow consumers are disconnected rather than allowed to block
  report ingestion.

## Supply chain and container

- Go and pnpm locks are committed; production builds use `-trimpath`, no CGO,
  explicit version/commit metadata, and checksum manifests.
- `.github/workflows/security.yml` runs `govulncheck`, production `pnpm audit`,
  Trivy filesystem/config/secret scanning, builds the production image, and
  scans it for high/critical vulnerabilities and secrets.
- The Docker build uses pinned major/minor toolchain images. The final image is
  distroless, contains no Node.js/package manager/shell, runs non-root, and the
  Compose service is read-only with all capabilities dropped.
- Release operators still review scanner advisories for reachability and pin
  image/action digests in environments whose policy requires immutable third-
  party references.

## Release review commands

```bash
test -z "$(gofmt -l .)"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
corepack pnpm@10.18.0 --dir web audit --prod
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
docker compose -f deploy/docker-compose.yml config
docker build -f deploy/Dockerfile -t vps-status-control:review .
```

Scanner commands require current vulnerability databases and network access;
CI is the reproducible enforcement point when an offline workstation cannot
fetch them.
