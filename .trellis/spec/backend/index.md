# Backend Development Guidelines

These guidelines describe the current Go foundation and the approved Control/Agent architecture. The repository is early-stage; rules backed only by the reviewed design are labeled as boundaries rather than presented as existing implementation patterns.

## Guides

| Guide | Owns |
|---|---|
| [Directory Structure](./directory-structure.md) | Go package and runtime boundaries |
| [Database Guidelines](./database-guidelines.md) | `control.db` / `telemetry.db`, migrations, and SQLite constraints |
| [Agent Enrollment and Protocol](./agent-protocol-guidelines.md) | Enrollment, node credentials, WSS framing, sequencing, and connection limits |
| [Linux Collection and Live Monitoring](./live-monitoring-guidelines.md) | Report schema, Linux collector, live memory, node API, and SSE contracts |
| [Telemetry Storage and Retention](./telemetry-storage-guidelines.md) | Minute aggregation, traffic ledger, rollups, maintenance, and disk degradation |
| [Bounded TCP Probes](./tcp-probe-guidelines.md) | Probe configuration, full sync, connect-only scheduling, admission, and rollups |
| [Alerts and Notifications](./alert-guidelines.md) | Live alert state, encrypted channels, durable delivery, and retry bounds |
| [Public Status Boundary](./public-status-guidelines.md) | Publication allowlist, public DTO, and private-field exclusion |
| [Error Handling](./error-handling.md) | CLI, HTTP, background, and degraded-mode failures |
| [Logging Guidelines](./logging-guidelines.md) | Operational logging and secret redaction |
| [Quality Guidelines](./quality-guidelines.md) | Formatting, tests, race checks, builds, and scope gates |
| [Packaging and Operations](./operations-guidelines.md) | Releases, deployment, backups, security scanning, and capacity evidence |

## Pre-Development Checklist

- Read the task PRD/design and identify whether the change belongs to Control, Agent, protocol, security, or one storage layer.
- Read [Directory Structure](./directory-structure.md) for every Go change.
- Read [Database Guidelines](./database-guidelines.md) before adding a table, migration, query, retention job, or persistent state.
- Read [Agent Enrollment and Protocol](./agent-protocol-guidelines.md) before changing enrollment, node credentials, Agent config, WSS, or Agent messages.
- Read [Linux Collection and Live Monitoring](./live-monitoring-guidelines.md) before changing host metrics, reports, online state, node APIs, or SSE.
- Read [Telemetry Storage and Retention](./telemetry-storage-guidelines.md) before changing history aggregation, telemetry queries, traffic accounting, retention, vacuum, or disk guards.
- Read [Bounded TCP Probes](./tcp-probe-guidelines.md) before changing probe tasks, assignments, protocol payloads, scheduling, result admission, or probe history.
- Read [Alerts and Notifications](./alert-guidelines.md) before changing live thresholds, channel secrets, alert state, outbox delivery, webhook/Telegram clients, or alert APIs.
- Read [Public Status Boundary](./public-status-guidelines.md) before changing public-page configuration, unauthenticated APIs, or public UI data.
- Read [Error Handling](./error-handling.md) and [Logging Guidelines](./logging-guidelines.md) before adding an API, background worker, network connection, or credential path.
- Read [Quality Guidelines](./quality-guidelines.md) before changing build tags, shared constants, or cross-platform code.
- Search for the existing owner of a concept before adding a second constant or decoder.

## Quality Check

- Run `gofmt`, `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- If the Web assets or `webui` boundary changed, build the frontend first and run `go test -tags webui ./...`.
- Verify no Agent sample, probe result, or traffic value enters `control.db`.
- Verify protocol additions cannot express shell, terminal, file, or generic process execution.
- Verify logs and errors contain no credentials, recovery codes, notification secrets, or Authorization values.

Reference foundation files: `go.mod`, `cmd/control/main.go`, `cmd/agent/main.go`, `internal/protocol/version.go`, `internal/control/webui/`, and the active task's `design.md`.
