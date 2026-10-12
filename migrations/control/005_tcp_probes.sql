CREATE TABLE probe_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
    interval_seconds INTEGER NOT NULL CHECK(interval_seconds BETWEEN 5 AND 3600),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE probe_assignments (
    task_id TEXT NOT NULL REFERENCES probe_tasks(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(task_id, node_id)
) WITHOUT ROWID;

CREATE INDEX probe_assignments_node_idx ON probe_assignments(node_id, task_id);

CREATE TABLE probe_config_state (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    version INTEGER NOT NULL CHECK(version > 0)
);

INSERT INTO probe_config_state(singleton, version) VALUES(1, 1);

CREATE TABLE telemetry_probe_cleanup_jobs (
    task_id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
    last_attempt_at INTEGER
) WITHOUT ROWID;

CREATE INDEX telemetry_probe_cleanup_jobs_created_idx
    ON telemetry_probe_cleanup_jobs(created_at, task_id);
