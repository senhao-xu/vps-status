CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    deleted_at INTEGER
);

CREATE TABLE node_credentials (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    credential_digest BLOB NOT NULL UNIQUE CHECK (length(credential_digest) = 32),
    created_at INTEGER NOT NULL,
    revoked_at INTEGER
);

CREATE INDEX node_credentials_active_idx
    ON node_credentials(node_id, revoked_at);

CREATE TABLE enrollment_tokens (
    id TEXT PRIMARY KEY,
    token_digest BLOB NOT NULL UNIQUE CHECK (length(token_digest) = 32),
    node_name TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER,
    consumed_node_id TEXT REFERENCES nodes(id),
    CHECK (expires_at > created_at),
    CHECK ((consumed_at IS NULL) = (consumed_node_id IS NULL))
);

CREATE INDEX enrollment_tokens_available_idx
    ON enrollment_tokens(expires_at, consumed_at);
