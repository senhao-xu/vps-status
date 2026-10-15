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
  explicit node display membership. Initial private defaults are superseded
  by migration011 and the current enrollment behavior.
- `008_appearance.sql`: singleton built-in visual theme selection; Emerald
  is the initial default, and Classic is the other accepted theme.

- `009_installation.sql`: singleton shared node entry address and Agent release.
- `010_probe_auto_join.sql`: checked auto-join flag, default false for all
  existing tasks; preserves existing assignments. Before mutation, the store
  creates a mode-0600 backup. Rolling back to schema9 requires restoring the
  pre-upgrade database backup along with the matching binary.

- `011_homepage_defaults.sql`: enables anonymous homepage access only for site
  settings never saved (`updated_at=0`). Preserves explicitly saved access
  choices and all existing node memberships. New enrollments join the homepage
  through the enrollment transaction, within the existing 1,000-node display
  limit. The mode-0600 pre-migration backup retains schema10 access settings and
  membership; a binary rollback requires restoring that backup.

Agent reports, probe results, traffic counters, and historical samples never
belong in this migration stream. Cleanup jobs contain only a stable node ID
and bounded retry metadata, never telemetry values.
