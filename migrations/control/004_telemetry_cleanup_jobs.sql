CREATE TABLE telemetry_cleanup_jobs (
    node_id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
    last_attempt_at INTEGER
) WITHOUT ROWID;

CREATE INDEX telemetry_cleanup_jobs_created_idx ON telemetry_cleanup_jobs(created_at, node_id);
