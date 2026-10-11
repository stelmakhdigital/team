-- 0023_messages_ack (postgres) — P3.15: mailbox-семантика (inbox/outbox).
-- ack_state на messages (pending/absorbed/denied/dropped) + атрибуция (acked_by/acked_at).
-- pg: ADD COLUMN + ADD CONSTRAINT (CHECK) in-place.

ALTER TABLE messages ADD COLUMN IF NOT EXISTS ack_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE messages ADD CONSTRAINT messages_ack_state_check
    CHECK (ack_state IN ('pending', 'absorbed', 'denied', 'dropped'));
ALTER TABLE messages ADD COLUMN IF NOT EXISTS acked_by TEXT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS acked_at TEXT;

CREATE INDEX IF NOT EXISTS idx_messages_to_role ON messages(to_role_id);
CREATE INDEX IF NOT EXISTS idx_messages_from_role ON messages(from_role_id);
CREATE INDEX IF NOT EXISTS idx_messages_ack_state ON messages(ack_state);

-- RBAC: messages.update (operator+; viewer не получает — только %.read).
INSERT INTO permissions (name, description) VALUES
    ('messages.update', 'ack входящие (absorb/deny/drop) — mailbox-семантика (P3.15)')
ON CONFLICT (name) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'messages.update'
ON CONFLICT DO NOTHING;
