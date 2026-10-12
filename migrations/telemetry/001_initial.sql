CREATE TABLE host_1m (
    node_id TEXT NOT NULL,
    bucket_start INTEGER NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    cpu_sum REAL NOT NULL, cpu_min REAL NOT NULL, cpu_max REAL NOT NULL,
    load1_sum REAL NOT NULL, load5_sum REAL NOT NULL, load15_sum REAL NOT NULL,
    memory_used_sum REAL NOT NULL, memory_total_last INTEGER NOT NULL,
    swap_used_sum REAL NOT NULL, swap_total_last INTEGER NOT NULL,
    disk_used_sum REAL NOT NULL, disk_total_last INTEGER NOT NULL,
    rx_rate_sum REAL NOT NULL, rx_rate_max INTEGER NOT NULL,
    tx_rate_sum REAL NOT NULL, tx_rate_max INTEGER NOT NULL,
    uptime_last INTEGER NOT NULL, process_sum REAL NOT NULL,
    tcp_sum REAL NOT NULL, udp_sum REAL NOT NULL,
    rx_total_last INTEGER NOT NULL, tx_total_last INTEGER NOT NULL,
    PRIMARY KEY(node_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE host_10m (
    node_id TEXT NOT NULL,
    bucket_start INTEGER NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    cpu_sum REAL NOT NULL, cpu_min REAL NOT NULL, cpu_max REAL NOT NULL,
    load1_sum REAL NOT NULL, load5_sum REAL NOT NULL, load15_sum REAL NOT NULL,
    memory_used_sum REAL NOT NULL, memory_total_last INTEGER NOT NULL,
    swap_used_sum REAL NOT NULL, swap_total_last INTEGER NOT NULL,
    disk_used_sum REAL NOT NULL, disk_total_last INTEGER NOT NULL,
    rx_rate_sum REAL NOT NULL, rx_rate_max INTEGER NOT NULL,
    tx_rate_sum REAL NOT NULL, tx_rate_max INTEGER NOT NULL,
    uptime_last INTEGER NOT NULL, process_sum REAL NOT NULL,
    tcp_sum REAL NOT NULL, udp_sum REAL NOT NULL,
    rx_total_last INTEGER NOT NULL, tx_total_last INTEGER NOT NULL,
    PRIMARY KEY(node_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE host_1h (
    node_id TEXT NOT NULL,
    bucket_start INTEGER NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    cpu_sum REAL NOT NULL, cpu_min REAL NOT NULL, cpu_max REAL NOT NULL,
    load1_sum REAL NOT NULL, load5_sum REAL NOT NULL, load15_sum REAL NOT NULL,
    memory_used_sum REAL NOT NULL, memory_total_last INTEGER NOT NULL,
    swap_used_sum REAL NOT NULL, swap_total_last INTEGER NOT NULL,
    disk_used_sum REAL NOT NULL, disk_total_last INTEGER NOT NULL,
    rx_rate_sum REAL NOT NULL, rx_rate_max INTEGER NOT NULL,
    tx_rate_sum REAL NOT NULL, tx_rate_max INTEGER NOT NULL,
    uptime_last INTEGER NOT NULL, process_sum REAL NOT NULL,
    tcp_sum REAL NOT NULL, udp_sum REAL NOT NULL,
    rx_total_last INTEGER NOT NULL, tx_total_last INTEGER NOT NULL,
    PRIMARY KEY(node_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE traffic_ledger (
    node_id TEXT NOT NULL,
    period TEXT NOT NULL,
    receive_bytes INTEGER NOT NULL CHECK(receive_bytes >= 0),
    transmit_bytes INTEGER NOT NULL CHECK(transmit_bytes >= 0),
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, period)
) WITHOUT ROWID;

CREATE TABLE maintenance_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;
