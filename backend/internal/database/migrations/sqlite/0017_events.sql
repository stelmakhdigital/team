-- 0017_events (sqlite) — P2.9: Pulse feed (единая лента событий).
-- events — real-time-события шины (task./session./message./alert./workflow.),
-- ОТДЕЛЬНО от audit_log (действия API: user/api_key/resource). См. api-decisions §P2.9.
-- Bounded: держать ≤5000 событий ИЛИ ≤7 дней (что раньше); auto-cleanup при записи.
-- НЕ хранить session.output (live-транскрипт — поток, не событие).
-- TIMESTAMP — TEXT (RFC3339Nano, UTC); entity_id 0 — без сущности (ADR-002).

CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    type        TEXT NOT NULL,
    team_id     INTEGER,
    role_id     INTEGER,
    entity_kind TEXT NOT NULL,
    entity_id   INTEGER NOT NULL DEFAULT 0,
    summary     TEXT NOT NULL DEFAULT '',
    payload     TEXT,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_team_id ON events(team_id);
