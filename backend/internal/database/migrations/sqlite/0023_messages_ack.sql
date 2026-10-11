-- 0023_messages_ack (sqlite) — P3.15: mailbox-семантика (inbox/outbox).
-- ack_state на messages (pending/absorbed/denied/dropped) + атрибуция (acked_by/acked_at).
--
-- sqlite: CHECK не добавляется in-place → REBUILD таблицы (как 0021):
-- CREATE messages_new (все колонки + ack_state с CHECK + acked_by/acked_at)
--   → copy (существующие → 'pending', NULL, NULL) → drop old → rename.
-- Индексы восстанавливаются после rename. Данные копируются (id сохраняются).
-- (messages: FK-связи НАРУЖУ (teams/queue_tasks/roles); таблицы на messages.id
--  не ссылаются — rebuild безопасен; id + FK-значения сохраняются.)

CREATE TABLE messages_new (
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
    created_at     TEXT NOT NULL,
    ack_state      TEXT NOT NULL DEFAULT 'pending'
                   CHECK (ack_state IN ('pending', 'absorbed', 'denied', 'dropped')),
    acked_by       TEXT,
    acked_at       TEXT
);
INSERT INTO messages_new (
    id, team_id, queue_task_id, from_role_id, to_role_id,
    type, body, metadata, is_read, created_at, ack_state, acked_by, acked_at
)
SELECT id, team_id, queue_task_id, from_role_id, to_role_id,
       type, body, metadata, is_read, created_at, 'pending', NULL, NULL
FROM messages;
DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;
CREATE INDEX IF NOT EXISTS idx_messages_to_role ON messages(to_role_id);
CREATE INDEX IF NOT EXISTS idx_messages_from_role ON messages(from_role_id);
CREATE INDEX IF NOT EXISTS idx_messages_ack_state ON messages(ack_state);

-- RBAC: messages.update (operator+; viewer не получает — только %.read).
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('messages.update', 'ack входящие (absorb/deny/drop) — mailbox-семантика (P3.15)');
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'messages.update';
