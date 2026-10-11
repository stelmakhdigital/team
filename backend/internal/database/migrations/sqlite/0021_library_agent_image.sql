-- 0021_library_agent_image (sqlite) — P3.12: новый тип library `agent_image`
-- (snapshot продуктивного состояния сессии) + флаг is_pinned (защита от prune).
--
-- sqlite: CHECK (type IN ...) не меняется in-place → REBUILD таблицы:
-- CREATE new (расширенный CHECK + is_pinned) → copy → drop old → rename.
-- Индексы восстанавливаются после rename. Данные копируются (id сохраняются).
-- (library_versions.item_id REFERENCES library_items(id): id сохраняются,
--  rebuild на свежей/старой БД на старте; FK-связь интактна.)

CREATE TABLE library_items_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    type            TEXT NOT NULL CHECK (type IN ('team', 'workflow', 'role', 'segment', 'agent_image')),
    name            TEXT NOT NULL,
    description     TEXT,
    group_name      TEXT NOT NULL DEFAULT 'general',
    version         TEXT NOT NULL DEFAULT '1.0.0',
    author          TEXT,
    is_public       INTEGER NOT NULL DEFAULT 0,
    downloads_count INTEGER NOT NULL DEFAULT 0,
    tags            TEXT NOT NULL DEFAULT '[]',
    thumbnail       TEXT,
    source_type     TEXT,
    source_id       INTEGER,
    spec            TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    is_starter      INTEGER NOT NULL DEFAULT 0,
    is_pinned       INTEGER NOT NULL DEFAULT 0,
    UNIQUE (type, name)
);
INSERT INTO library_items_new (
    id, type, name, description, group_name, version, author, is_public,
    downloads_count, tags, thumbnail, source_type, source_id, spec, created_at, updated_at, is_starter
)
SELECT id, type, name, description, group_name, version, author, is_public,
       downloads_count, tags, thumbnail, source_type, source_id, spec, created_at, updated_at, is_starter
FROM library_items;
DROP TABLE library_items;
ALTER TABLE library_items_new RENAME TO library_items;
CREATE INDEX IF NOT EXISTS idx_library_items_type ON library_items(type, group_name);
