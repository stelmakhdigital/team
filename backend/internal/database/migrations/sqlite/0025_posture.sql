-- 0025_posture (sqlite) — P3.16: Mode/permission posture (delegated vs human-led).
-- teams.posture — база (default delegated); sessions.posture — override (NULL = наследует).
-- human_requests — паркованные чувствительные операции (approve/deny → replay).

ALTER TABLE teams ADD COLUMN posture TEXT NOT NULL DEFAULT 'delegated'
    CHECK (posture IN ('delegated', 'human_led'));

ALTER TABLE sessions ADD COLUMN posture TEXT
    CHECK (posture IN ('delegated', 'human_led'));  -- NULL = наследует от команды

CREATE TABLE IF NOT EXISTS human_requests (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_type       TEXT NOT NULL CHECK (scope_type IN ('team', 'session', 'global')),
    scope_id         INTEGER,
    team_id          INTEGER,
    method           TEXT NOT NULL,
    path             TEXT NOT NULL,
    body             TEXT,                              -- body оригинала (bounded ≤ 64KB)
    auth_key         TEXT,                              -- API-ключ оригинала (replay-auth)
    operation        TEXT NOT NULL,                     -- "delete task", "config.set ..."
    reversible       INTEGER NOT NULL DEFAULT 0,        -- ADR-002: bool = INTEGER
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'denied', 'replayed', 'expired')),
    decision_reason  TEXT,
    decided_by       TEXT,
    decided_at       TEXT,
    replay_status    INTEGER,                           -- HTTP-статус replay (после approve)
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_human_requests_status ON human_requests(status);
CREATE INDEX IF NOT EXISTS idx_human_requests_scope ON human_requests(scope_type, scope_id);
