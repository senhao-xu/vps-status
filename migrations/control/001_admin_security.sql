CREATE TABLE admins (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    totp_secret_cipher BLOB,
    totp_confirmed INTEGER NOT NULL DEFAULT 0 CHECK (totp_confirmed IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    token_digest BLOB NOT NULL UNIQUE CHECK (length(token_digest) = 32),
    csrf_digest BLOB NOT NULL CHECK (length(csrf_digest) = 32),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER,
    CHECK (expires_at > created_at)
);

CREATE INDEX sessions_admin_expires_idx ON sessions(admin_id, expires_at);

CREATE TABLE recovery_codes (
    admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    code_digest BLOB NOT NULL CHECK (length(code_digest) = 32),
    created_at INTEGER NOT NULL,
    consumed_at INTEGER,
    PRIMARY KEY (admin_id, code_digest)
);

CREATE INDEX recovery_codes_available_idx
    ON recovery_codes(admin_id, consumed_at);
