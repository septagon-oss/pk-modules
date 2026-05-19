-- 0001_initial.up.sql creates the api_keys table.
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    hash TEXT NOT NULL,
    scopes TEXT NOT NULL,
    last_used_at DATETIME,
    revoked_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
