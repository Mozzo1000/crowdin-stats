PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS projects (
    public_id           TEXT PRIMARY KEY,
    crowdin_project_id  TEXT NOT NULL,
    ciphertext          BLOB NOT NULL,
    nonce               BLOB NOT NULL,
    key_version         INTEGER NOT NULL DEFAULT 1, -- always 1 today; reserved for a future encryption key rotation (see SECURITY.md), not read back yet
    created_at          INTEGER NOT NULL,
    revoked             INTEGER NOT NULL DEFAULT 0,
    revoke_token_hash   TEXT
);

CREATE TABLE IF NOT EXISTS cache (
    key         TEXT PRIMARY KEY,
    svg         TEXT NOT NULL,
    cached_at   INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS rate_limits (
    bucket_key   TEXT PRIMARY KEY,
    count        INTEGER NOT NULL,
    window_start INTEGER NOT NULL
);
