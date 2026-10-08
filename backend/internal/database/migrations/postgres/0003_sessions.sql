-- 0003_sessions (postgres) — slice 3: Sessions & Runtime
CREATE TABLE IF NOT EXISTS sessions (
    id             BIGSERIAL PRIMARY KEY,
    team_id        BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role_id        BIGINT NOT NULL REFERENCES roles(id) ON DELETE SET NULL,
    queue_task_id  BIGINT REFERENCES queue_tasks(id) ON DELETE SET NULL,
    runtime_type   TEXT NOT NULL DEFAULT 'process'
                   CHECK (runtime_type IN ('container', 'process', 'tmux', 'pi', 'k8s', 'other')),
    runtime_ref    TEXT,
    state          TEXT NOT NULL DEFAULT 'starting'
                   CHECK (state IN ('starting', 'running', 'idle', 'stopping', 'stopped', 'failed')),
    working_dir    TEXT,
    target_dir     TEXT,
    command        TEXT,
    config         TEXT NOT NULL DEFAULT '{}',
    exit_code      INTEGER,
    started_at     TEXT,
    stopped_at     TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_team_state ON sessions(team_id, state);
CREATE INDEX IF NOT EXISTS idx_sessions_role ON sessions(role_id, state);
CREATE INDEX IF NOT EXISTS idx_sessions_task ON sessions(queue_task_id);

CREATE TABLE IF NOT EXISTS session_history (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    from_state      TEXT,
    to_state        TEXT NOT NULL,
    actor_type      TEXT NOT NULL DEFAULT 'daemon'
                    CHECK (actor_type IN ('role', 'daemon', 'watchdog', 'human')),
    actor_role_id   BIGINT REFERENCES roles(id),
    comment         TEXT,
    metadata        TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_history_session ON session_history(session_id, created_at ASC);

CREATE TABLE IF NOT EXISTS watchdog_events (
    id               BIGSERIAL PRIMARY KEY,
    team_id          BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    queue_task_id    BIGINT REFERENCES queue_tasks(id) ON DELETE SET NULL,
    session_id       BIGINT REFERENCES sessions(id) ON DELETE SET NULL,
    role_id          BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    event_type       TEXT NOT NULL
                     CHECK (event_type IN ('wake', 'refocus', 'alignment_checkpoint', 'stale', 'blocked', 'idle', 'drift')),
    severity         TEXT NOT NULL DEFAULT 'medium'
                     CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    description      TEXT NOT NULL,
    action_taken     TEXT,
    is_read          INTEGER NOT NULL DEFAULT 0,
    requires_action  INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_watchdog_events_team ON watchdog_events(team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_watchdog_events_task ON watchdog_events(queue_task_id, created_at DESC);
