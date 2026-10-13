# Control database migrations

SQL files in this directory are embedded by `embed.go` and applied in numeric
order by `internal/storage/control`. Migrations are forward-only and have an
independent schema version from the telemetry database.

- `001_admin_security.sql`: singleton administrator, sessions, and recovery codes.
- `002_nodes_and_enrollment.sql`: node metadata, credential digests, and one-time enrollment state.
- `003_node_metadata.sql`: low-growth display grouping and static host identity facts.
- `004_telemetry_cleanup_jobs.sql`: durable node-ID-only retry jobs for cross-file telemetry cleanup.
- `005_tcp_probes.sql`: TCP task definitions, assignments, monotonic snapshot
  version, and task-ID-only telemetry cleanup jobs.
- `006_alerts.sql`: encrypted notification configuration, bounded alert rules
  and current state, plus a low-growth transition/test delivery outbox.
- `007_public_status.sql`: singleton public-page presentation settings and an
  explicit node publication allowlist. Nodes remain private until inserted.
- `008_appearance.sql`: singleton built-in visual theme selection; Emerald
  is the initial default, and Classic is the other accepted theme.

Agent reports, probe results, traffic counters, and historical samples never
belong in this migration stream. Cleanup jobs contain only a stable node ID
and bounded retry metadata, never telemetry values.
