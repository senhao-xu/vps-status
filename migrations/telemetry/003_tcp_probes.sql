CREATE TABLE probe_1m (
    node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    bucket_start INTEGER NOT NULL,
    attempts INTEGER NOT NULL CHECK(attempts > 0),
    successes INTEGER NOT NULL CHECK(successes >= 0 AND successes <= attempts),
    failures INTEGER NOT NULL CHECK(failures >= 0 AND failures <= attempts),
    latency_count INTEGER NOT NULL CHECK(latency_count = successes),
    latency_sum REAL,
    latency_min REAL,
    latency_max REAL,
    CHECK(successes + failures = attempts),
    PRIMARY KEY(node_id, task_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE probe_10m (
    node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    bucket_start INTEGER NOT NULL,
    attempts INTEGER NOT NULL CHECK(attempts > 0),
    successes INTEGER NOT NULL CHECK(successes >= 0 AND successes <= attempts),
    failures INTEGER NOT NULL CHECK(failures >= 0 AND failures <= attempts),
    latency_count INTEGER NOT NULL CHECK(latency_count = successes),
    latency_sum REAL,
    latency_min REAL,
    latency_max REAL,
    CHECK(successes + failures = attempts),
    PRIMARY KEY(node_id, task_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE probe_1h (
    node_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    bucket_start INTEGER NOT NULL,
    attempts INTEGER NOT NULL CHECK(attempts > 0),
    successes INTEGER NOT NULL CHECK(successes >= 0 AND successes <= attempts),
    failures INTEGER NOT NULL CHECK(failures >= 0 AND failures <= attempts),
    latency_count INTEGER NOT NULL CHECK(latency_count = successes),
    latency_sum REAL,
    latency_min REAL,
    latency_max REAL,
    CHECK(successes + failures = attempts),
    PRIMARY KEY(node_id, task_id, bucket_start)
) WITHOUT ROWID;
