-- 0014_watchdog_events_job (sqlite) — P1.6: add 'watchdog_job' к watchdog_events.event_type.
-- sqlite: CHECK-констрейн не меняется in-place → пересоздаём таблицу (копируем данные
-- + индексы). watchdog_events не referenced другими таблицами → DROP безопасен.

CREATE TABLE watchdog_events_new (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id          INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    queue_task_id    INTEGER REFERENCES queue_tasks(id) ON DELETE SET NULL,
    session_id       INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
    role_id          INTEGER REFERENCES roles(id) ON DELETE SET NULL,
    event_type       TEXT NOT NULL
                     CHECK (event_type IN ('wake', 'refocus', 'alignment_checkpoint', 'stale', 'blocked', 'idle', 'drift', 'watchdog_job')),
    severity         TEXT NOT NULL DEFAULT 'medium'
                     CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    description      TEXT NOT NULL,
    action_taken     TEXT,
    is_read          INTEGER NOT NULL DEFAULT 0,
    requires_action  INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL
);
INSERT INTO watchdog_events_new
    (id, team_id, queue_task_id, session_id, role_id, event_type, severity,
     description, action_taken, is_read, requires_action, created_at)
    SELECT id, team_id, queue_task_id, session_id, role_id, event_type, severity,
           description, action_taken, is_read, requires_action, created_at
    FROM watchdog_events;
DROP TABLE watchdog_events;
ALTER TABLE watchdog_events_new RENAME TO watchdog_events;
CREATE INDEX IF NOT EXISTS idx_watchdog_events_team ON watchdog_events(team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_watchdog_events_task ON watchdog_events(queue_task_id, created_at DESC);
