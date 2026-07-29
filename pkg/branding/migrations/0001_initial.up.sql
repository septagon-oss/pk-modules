-- 0001_initial.up.sql creates the branding_profiles table.
CREATE TABLE IF NOT EXISTS branding_profiles (
    tenant_id          TEXT PRIMARY KEY,
    display_name       TEXT NOT NULL,
    logo_data          BLOB,
    logo_content_type  TEXT NOT NULL DEFAULT '',
    logo_alt           TEXT NOT NULL DEFAULT '',
    primary_color      TEXT NOT NULL DEFAULT '',
    font_key           TEXT NOT NULL DEFAULT '',
    setup_completed_at DATETIME,
    created_at         DATETIME NOT NULL,
    updated_at         DATETIME NOT NULL
);
