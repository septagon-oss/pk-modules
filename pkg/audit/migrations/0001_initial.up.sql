-- 0001_initial.up.sql creates the audit_events table.
CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'info',
    details TEXT NOT NULL DEFAULT '',
    emitted_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_emitted ON audit_events(tenant_id, emitted_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_emitted ON audit_events(actor, emitted_at);
