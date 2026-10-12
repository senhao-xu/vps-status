ALTER TABLE host_1m RENAME TO host_1m_v1;
ALTER TABLE host_10m RENAME TO host_10m_v1;
ALTER TABLE host_1h RENAME TO host_1h_v1;

CREATE TABLE host_1m (
    node_id TEXT NOT NULL, bucket_start INTEGER NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    cpu_count INTEGER NOT NULL CHECK(cpu_count >= 0), load_count INTEGER NOT NULL CHECK(load_count >= 0),
    memory_count INTEGER NOT NULL CHECK(memory_count >= 0), disk_count INTEGER NOT NULL CHECK(disk_count >= 0),
    network_count INTEGER NOT NULL CHECK(network_count >= 0), uptime_count INTEGER NOT NULL CHECK(uptime_count >= 0),
    process_count INTEGER NOT NULL CHECK(process_count >= 0), connection_count INTEGER NOT NULL CHECK(connection_count >= 0),
    cpu_sum REAL, cpu_min REAL, cpu_max REAL,
    load1_sum REAL, load5_sum REAL, load15_sum REAL,
    memory_used_sum REAL, memory_total_last INTEGER, swap_used_sum REAL, swap_total_last INTEGER,
    disk_used_sum REAL, disk_total_last INTEGER,
    rx_rate_sum REAL, rx_rate_max INTEGER, tx_rate_sum REAL, tx_rate_max INTEGER,
    uptime_last INTEGER, process_sum REAL, tcp_sum REAL, udp_sum REAL,
    rx_total_last INTEGER, tx_total_last INTEGER,
    PRIMARY KEY(node_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE host_10m (
    node_id TEXT NOT NULL, bucket_start INTEGER NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    cpu_count INTEGER NOT NULL CHECK(cpu_count >= 0), load_count INTEGER NOT NULL CHECK(load_count >= 0),
    memory_count INTEGER NOT NULL CHECK(memory_count >= 0), disk_count INTEGER NOT NULL CHECK(disk_count >= 0),
    network_count INTEGER NOT NULL CHECK(network_count >= 0), uptime_count INTEGER NOT NULL CHECK(uptime_count >= 0),
    process_count INTEGER NOT NULL CHECK(process_count >= 0), connection_count INTEGER NOT NULL CHECK(connection_count >= 0),
    cpu_sum REAL, cpu_min REAL, cpu_max REAL,
    load1_sum REAL, load5_sum REAL, load15_sum REAL,
    memory_used_sum REAL, memory_total_last INTEGER, swap_used_sum REAL, swap_total_last INTEGER,
    disk_used_sum REAL, disk_total_last INTEGER,
    rx_rate_sum REAL, rx_rate_max INTEGER, tx_rate_sum REAL, tx_rate_max INTEGER,
    uptime_last INTEGER, process_sum REAL, tcp_sum REAL, udp_sum REAL,
    rx_total_last INTEGER, tx_total_last INTEGER,
    PRIMARY KEY(node_id, bucket_start)
) WITHOUT ROWID;

CREATE TABLE host_1h (
    node_id TEXT NOT NULL, bucket_start INTEGER NOT NULL,
    sample_count INTEGER NOT NULL CHECK(sample_count > 0),
    cpu_count INTEGER NOT NULL CHECK(cpu_count >= 0), load_count INTEGER NOT NULL CHECK(load_count >= 0),
    memory_count INTEGER NOT NULL CHECK(memory_count >= 0), disk_count INTEGER NOT NULL CHECK(disk_count >= 0),
    network_count INTEGER NOT NULL CHECK(network_count >= 0), uptime_count INTEGER NOT NULL CHECK(uptime_count >= 0),
    process_count INTEGER NOT NULL CHECK(process_count >= 0), connection_count INTEGER NOT NULL CHECK(connection_count >= 0),
    cpu_sum REAL, cpu_min REAL, cpu_max REAL,
    load1_sum REAL, load5_sum REAL, load15_sum REAL,
    memory_used_sum REAL, memory_total_last INTEGER, swap_used_sum REAL, swap_total_last INTEGER,
    disk_used_sum REAL, disk_total_last INTEGER,
    rx_rate_sum REAL, rx_rate_max INTEGER, tx_rate_sum REAL, tx_rate_max INTEGER,
    uptime_last INTEGER, process_sum REAL, tcp_sum REAL, udp_sum REAL,
    rx_total_last INTEGER, tx_total_last INTEGER,
    PRIMARY KEY(node_id, bucket_start)
) WITHOUT ROWID;

INSERT INTO host_1m SELECT node_id,bucket_start,sample_count,
sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,
cpu_sum,cpu_min,cpu_max,load1_sum,load5_sum,load15_sum,memory_used_sum,memory_total_last,swap_used_sum,swap_total_last,
disk_used_sum,disk_total_last,rx_rate_sum,rx_rate_max,tx_rate_sum,tx_rate_max,uptime_last,process_sum,tcp_sum,udp_sum,rx_total_last,tx_total_last FROM host_1m_v1;
INSERT INTO host_10m SELECT node_id,bucket_start,sample_count,
sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,
cpu_sum,cpu_min,cpu_max,load1_sum,load5_sum,load15_sum,memory_used_sum,memory_total_last,swap_used_sum,swap_total_last,
disk_used_sum,disk_total_last,rx_rate_sum,rx_rate_max,tx_rate_sum,tx_rate_max,uptime_last,process_sum,tcp_sum,udp_sum,rx_total_last,tx_total_last FROM host_10m_v1;
INSERT INTO host_1h SELECT node_id,bucket_start,sample_count,
sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,sample_count,
cpu_sum,cpu_min,cpu_max,load1_sum,load5_sum,load15_sum,memory_used_sum,memory_total_last,swap_used_sum,swap_total_last,
disk_used_sum,disk_total_last,rx_rate_sum,rx_rate_max,tx_rate_sum,tx_rate_max,uptime_last,process_sum,tcp_sum,udp_sum,rx_total_last,tx_total_last FROM host_1h_v1;

DROP TABLE host_1m_v1;
DROP TABLE host_10m_v1;
DROP TABLE host_1h_v1;
