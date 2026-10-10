-- 0017_events (postgres) — P2.9: Pulse feed. См. sqlite/0017_events.sql.
-- TIMESTAMP — TEXT (RFC3339Nano, UTC); entity_id 0 — без сущности.

CREATE TABLE IF NOT EXISTS events (
    id          BIGSERIAL PRIMARY KEY,
    type        TEXT NOT NULL,
    team_id     BIGINT,
    role_id     BIGINT,
    entity_kind TEXT NOT NULL,
    entity_id   BIGINT NOT NULL DEFAULT 0,
    summary     TEXT NOT NULL DEFAULT '',
    payload     TEXT,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_team_id ON events(team_id);
