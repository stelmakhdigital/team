-- 0007_security (postgres) — slice 6: RBAC (roles/permissions/users/api_keys), secrets, chatroom reads
CREATE TABLE IF NOT EXISTS security_roles (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);

CREATE TABLE IF NOT EXISTS permissions (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       BIGINT NOT NULL REFERENCES security_roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT,
    password_hash TEXT,
    role_id       BIGINT REFERENCES security_roles(id),
    created_at    TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
    id          BIGSERIAL PRIMARY KEY,
    key_hash    TEXT NOT NULL UNIQUE,   -- sha256 hex (в БД открытый ключ не храним)
    name        TEXT NOT NULL,
    user_id     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    permissions TEXT NOT NULL DEFAULT '[]',  -- доп. permissions (JSON-массив), поверх роли
    expires_at  TIMESTAMPTZ,
    revoked     INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);

CREATE TABLE IF NOT EXISTS secrets (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    value       TEXT NOT NULL,          -- encrypted (AES-256-GCM)
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS chatroom_reads (
    user_id       BIGINT NOT NULL,
    chatroom_id   BIGINT NOT NULL REFERENCES chatrooms(id) ON DELETE CASCADE,
    last_read_id  BIGINT NOT NULL DEFAULT 0,  -- id последнего прочитанного сообщения
    last_read_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, chatroom_id)
);

-- Seed: роли (не путать с agent roles)
INSERT INTO security_roles (name, description) VALUES
    ('admin', 'full access'),
    ('operator', 'read/write, no config update'),
    ('viewer', 'read-only')
ON CONFLICT (name) DO NOTHING;

-- Seed: permissions
INSERT INTO permissions (name, description) VALUES
    ('teams.read', 'read teams/segments/roles/relatives'),
    ('teams.create', 'create teams/segments/roles/relatives'),
    ('teams.update', 'modify teams/segments/roles/relatives (incl. layout)'),
    ('teams.delete', 'archive teams, delete relatives'),
    ('tasks.read', 'read tasks and history'),
    ('tasks.create', 'create tasks'),
    ('tasks.update', 'task state transitions and handoff'),
    ('sessions.read', 'read sessions, transcripts, watchdog alerts'),
    ('sessions.create', 'start sessions'),
    ('sessions.stop', 'stop sessions'),
    ('messages.read', 'read messages and chatrooms'),
    ('messages.create', 'send messages (direct/broadcast/segment, chatrooms)'),
    ('chatrooms.read', 'read chatrooms and their messages'),
    ('chatrooms.create', 'post to chatrooms'),
    ('workflows.read', 'read workflows/blocks/connections'),
    ('workflows.create', 'create workflows/blocks/connections'),
    ('workflows.update', 'update workflow blocks/connections'),
    ('library.read', 'read library items'),
    ('library.create', 'save library items'),
    ('library.apply', 'apply library items'),
    ('dashboard.read', 'dashboard summary/tasks/sessions/alerts/metrics, WS events'),
    ('audit.read', 'read audit log'),
    ('config.update', 'update role config (agent settings)')
ON CONFLICT (name) DO NOTHING;

-- Seed: role_permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name <> 'config.update'
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'viewer' AND p.name LIKE '%.read'
ON CONFLICT DO NOTHING;
