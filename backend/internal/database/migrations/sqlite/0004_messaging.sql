-- 0004_messaging (sqlite) — slice 4: Message Center
CREATE TABLE IF NOT EXISTS chatrooms (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id        INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    segment_id     INTEGER REFERENCES segments(id) ON DELETE SET NULL,
    name           TEXT NOT NULL,
    topic          TEXT,
    config         TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE (team_id, segment_id, name)
);
CREATE INDEX IF NOT EXISTS idx_chatrooms_team ON chatrooms(team_id);

CREATE TABLE IF NOT EXISTS chatroom_messages (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    chatroom_id    INTEGER NOT NULL REFERENCES chatrooms(id) ON DELETE CASCADE,
    from_role_id   INTEGER REFERENCES roles(id) ON DELETE SET NULL,
    body           TEXT NOT NULL,
    metadata       TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chatroom_messages_chatroom ON chatroom_messages(chatroom_id, created_at ASC, id ASC);

CREATE TABLE IF NOT EXISTS messages (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id        INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    queue_task_id  INTEGER REFERENCES queue_tasks(id) ON DELETE SET NULL,
    from_role_id   INTEGER REFERENCES roles(id) ON DELETE SET NULL,
    to_role_id     INTEGER REFERENCES roles(id) ON DELETE SET NULL,
    type           TEXT NOT NULL DEFAULT 'direct'
                   CHECK (type IN ('direct', 'broadcast', 'segment', 'system', 'watchdog')),
    body           TEXT NOT NULL,
    metadata       TEXT NOT NULL DEFAULT '{}',
    is_read        INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_team ON messages(team_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(queue_task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_to_role ON messages(to_role_id, is_read, created_at DESC);
