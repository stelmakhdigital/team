-- 0006_library_audit (postgres) — slice 5b: Library + Audit log
-- Примечание: timestamps и JSON хранятся как TEXT для единообразия с sqlite-диалектом (ADR-002).
CREATE TABLE IF NOT EXISTS library_items (
    id             BIGSERIAL PRIMARY KEY,
    type           TEXT NOT NULL CHECK (type IN ('team', 'workflow', 'role', 'segment')),
    name           TEXT NOT NULL,
    description    TEXT,
    group_name     TEXT NOT NULL DEFAULT 'general',
    version        TEXT NOT NULL DEFAULT '1.0.0',
    author         TEXT,
    is_public      INTEGER NOT NULL DEFAULT 0,
    downloads_count INTEGER NOT NULL DEFAULT 0,
    tags           TEXT NOT NULL DEFAULT '[]',
    thumbnail      TEXT,
    source_type    TEXT,
    source_id      INTEGER,
    spec           TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE (type, name)
);
CREATE INDEX IF NOT EXISTS idx_library_items_type ON library_items(type, group_name);

CREATE TABLE IF NOT EXISTS library_versions (
    id             BIGSERIAL PRIMARY KEY,
    item_id        BIGINT NOT NULL REFERENCES library_items(id) ON DELETE CASCADE,
    version        TEXT NOT NULL,
    changes        TEXT,
    author         TEXT,
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_library_versions_item ON library_versions(item_id, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_log (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT,
    user_name      TEXT,
    api_key_id     BIGINT,
    action         TEXT NOT NULL,
    resource       TEXT,
    details        TEXT NOT NULL DEFAULT '{}',
    ip_address     TEXT,
    user_agent     TEXT,
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(created_at DESC);
