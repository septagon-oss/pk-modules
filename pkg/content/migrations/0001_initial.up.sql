-- 0001_initial.up.sql creates the content table with (tenant_id, kind, slug)
-- uniqueness and supporting indexes for tenant listing and publish queries.
CREATE TABLE IF NOT EXISTS content (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    body_format TEXT NOT NULL,
    author_id TEXT NOT NULL,
    published_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(tenant_id, kind, slug)
);
CREATE INDEX IF NOT EXISTS idx_content_tenant ON content(tenant_id);
CREATE INDEX IF NOT EXISTS idx_content_published ON content(published_at);
