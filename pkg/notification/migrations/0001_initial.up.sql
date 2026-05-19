-- 0001_initial.up.sql creates the notifications and
-- notification_subscriptions tables and supporting indexes.
CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    category TEXT,
    severity TEXT NOT NULL,
    data TEXT,
    read_at DATETIME,
    emitted_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, emitted_at);

CREATE TABLE IF NOT EXISTS notification_subscriptions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    category TEXT,
    channel TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notif_subs_user ON notification_subscriptions(user_id);
