-- 0007_security (sqlite) — slice 6: RBAC (roles/permissions/users/api_keys), secrets, chatroom reads
CREATE TABLE IF NOT EXISTS security_roles (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);

CREATE TABLE IF NOT EXISTS permissions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    description TEXT
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       INTEGER NOT NULL REFERENCES security_roles(id) ON DELETE CASCADE,
    permission_id INTEGER NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT,
    password_hash TEXT,
    role_id       INTEGER REFERENCES security_roles(id),
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash    TEXT NOT NULL UNIQUE,   -- sha256 hex (в БД открытый ключ не храним)
    name        TEXT NOT NULL,
    user_id     INTEGER REFERENCES users(id) ON DELETE SET NULL,
    permissions TEXT NOT NULL DEFAULT '[]',  -- доп. permissions (JSON-массив), поверх роли
    expires_at  TEXT,
    revoked     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);

CREATE TABLE IF NOT EXISTS secrets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    value       TEXT NOT NULL,          -- encrypted (AES-256-GCM)
    description TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chatroom_reads (
    user_id      INTEGER NOT NULL,
    chatroom_id  INTEGER NOT NULL REFERENCES chatrooms(id) ON DELETE CASCADE,
    last_read_id INTEGER NOT NULL DEFAULT 0,  -- id последнего прочитанного сообщения
    last_read_at TEXT NOT NULL,
    PRIMARY KEY (user_id, chatroom_id)
);

-- Seed: роли (не путать с agent roles)
INSERT OR IGNORE INTO security_roles (name, description) VALUES
    ('admin', 'full access'),
    ('operator', 'read/write, no config update'),
    ('viewer', 'read-only');

-- Seed: permissions
INSERT OR IGNORE INTO permissions (name, description) VALUES
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
    ('config.update', 'update role config (agent settings)');

-- Seed: role_permissions
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'admin';

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name <> 'config.update';

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'viewer' AND p.name LIKE '%.read';
