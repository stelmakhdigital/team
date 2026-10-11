-- 0024_walks (postgres) — P3.14: Walk (paced-доставка контекста в сессию).
-- Timestamps — TEXT RFC3339Nano (project-конвенция, как sessions/messages).

CREATE TABLE IF NOT EXISTS walks (
    id             BIGSERIAL PRIMARY KEY,
    session_id     BIGINT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    description    TEXT,
    mode           TEXT NOT NULL CHECK (mode IN ('interval', 'manual')),
    interval_secs  INTEGER,
    status         TEXT NOT NULL DEFAULT 'active'
                   CHECK (status IN ('active', 'paused', 'completed', 'failed')),
    current_seq    INTEGER NOT NULL DEFAULT 1,
    last_sent_at   TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_walks_session ON walks(session_id);

CREATE TABLE IF NOT EXISTS walk_steps (
    id           BIGSERIAL PRIMARY KEY,
    walk_id      BIGINT NOT NULL REFERENCES walks(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    content      TEXT NOT NULL,
    state        TEXT NOT NULL DEFAULT 'pending'
                 CHECK (state IN ('pending', 'sent', 'delivered', 'staged', 'retained', 'failed', 'skipped')),
    send_status  TEXT,
    note         TEXT,
    sent_at      TEXT,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_walk_steps_walk ON walk_steps(walk_id, seq);
