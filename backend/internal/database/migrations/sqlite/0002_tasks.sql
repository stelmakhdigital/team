-- 0002_tasks (sqlite) — slice 2: Tasks & History
-- projects/goal_id и session_id добавляются в следующих миграциях (API projects/sessions ещё не реализованы).
CREATE TABLE IF NOT EXISTS queue_tasks (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id           INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    parent_task_id    INTEGER REFERENCES queue_tasks(id) ON DELETE SET NULL,
    destination_role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    source_role_id    INTEGER REFERENCES roles(id) ON DELETE SET NULL,
    title             TEXT NOT NULL,
    body              TEXT NOT NULL DEFAULT '',
    body_context      TEXT NOT NULL DEFAULT '{}',
    state             TEXT NOT NULL DEFAULT 'pending'
                      CHECK (state IN ('pending', 'in_progress', 'done', 'blocked', 'canceled')),
    closure_reason    TEXT
                      CHECK (closure_reason IS NULL OR closure_reason IN
                           ('handed_off_to', 'blocked_on', 'denied', 'canceled', 'no_follow_on', 'escalation')),
    closure_target_id INTEGER REFERENCES queue_tasks(id),
    blocked_since     TEXT,
    park_timeout_secs INTEGER,
    priority          INTEGER NOT NULL DEFAULT 0,
    estimated_secs    INTEGER,
    actual_secs       INTEGER,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    started_at        TEXT,
    completed_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_queue_tasks_team_state ON queue_tasks(team_id, state);
CREATE INDEX IF NOT EXISTS idx_queue_tasks_destination ON queue_tasks(destination_role_id, state);
CREATE INDEX IF NOT EXISTS idx_queue_tasks_parent ON queue_tasks(parent_task_id);

CREATE TABLE IF NOT EXISTS history_status (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    queue_task_id     INTEGER NOT NULL REFERENCES queue_tasks(id) ON DELETE CASCADE,
    from_state        TEXT,
    to_state          TEXT NOT NULL,
    closure_reason    TEXT,
    closure_target_id INTEGER REFERENCES queue_tasks(id),
    actor_role_id     INTEGER REFERENCES roles(id),
    actor_type        TEXT NOT NULL DEFAULT 'role'
                      CHECK (actor_type IN ('role', 'daemon', 'watchdog', 'human')),
    comment           TEXT,
    metadata          TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_history_status_task ON history_status(queue_task_id, created_at DESC);
