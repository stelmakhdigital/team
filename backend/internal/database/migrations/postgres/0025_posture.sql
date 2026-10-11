-- 0025_posture (postgres) — P3.16: Mode/permission posture (delegated vs human-led).
-- teams.posture — база (default delegated); sessions.posture — override (NULL = наследует).
-- human_requests — паркованные чувствительные операции (approve/deny → replay).

ALTER TABLE teams ADD COLUMN posture TEXT NOT NULL DEFAULT 'delegated'
    CONSTRAINT teams_posture_check CHECK (posture IN ('delegated', 'human_led'));

ALTER TABLE sessions ADD COLUMN posture TEXT
    CONSTRAINT sessions_posture_check CHECK (posture IN ('delegated', 'human_led'));

CREATE TABLE IF NOT EXISTS human_requests (
    id               BIGSERIAL PRIMARY KEY,
    scope_type       TEXT NOT NULL CHECK (scope_type IN ('team', 'session', 'global')),
    scope_id         BIGINT,
    team_id          BIGINT,
    method           TEXT NOT NULL,
    path             TEXT NOT NULL,
    body             TEXT,
    auth_key         TEXT,
    operation        TEXT NOT NULL,
    reversible       BOOLEAN NOT NULL DEFAULT FALSE,
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'approved', 'denied', 'replayed', 'expired')),
    decision_reason  TEXT,
    decided_by       TEXT,
    decided_at       TEXT,
    replay_status    INTEGER,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_human_requests_status ON human_requests(status);
CREATE INDEX IF NOT EXISTS idx_human_requests_scope ON human_requests(scope_type, scope_id);
