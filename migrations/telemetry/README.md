# Telemetry database migrations

This stream is embedded and applied only by `internal/storage/telemetry`.
It is physically independent from `control.db`; no migration may use
`ATTACH`, foreign keys to Control tables, or raw Agent-report rows.

Schema v1 stores completed host rollup buckets, a compact lifetime/month
traffic ledger, and maintenance metadata. Schema v2 adds per-metric valid
sample counts and nullable values so an unavailable collector area is not
fabricated as zero. Schema v3 adds only completed TCP probe minute, ten-minute,
and hourly aggregates; individual probe results never enter SQLite.
