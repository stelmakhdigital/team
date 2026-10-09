-- 0011_projects (postgres) — P1.0: Projects (мультипроектность)
-- См. sqlite/0011_projects.sql. TIMESTAMP — TEXT (RFC3339Nano, UTC), bool — INTEGER (0/1).

CREATE TABLE IF NOT EXISTS projects (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    path        TEXT NOT NULL UNIQUE,
    kind        TEXT NOT NULL CHECK (kind IN ('existing', 'new')),
    git         INTEGER NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    archived    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_projects_archived ON projects(archived);

-- Привязка команд/задач/сессий к проекту (nullable; миграционная совместимость).
ALTER TABLE teams ADD COLUMN project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE queue_tasks ADD COLUMN project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE sessions ADD COLUMN project_id BIGINT REFERENCES projects(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_teams_project ON teams(project_id);
CREATE INDEX IF NOT EXISTS idx_queue_tasks_project ON queue_tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id);

-- P1.0: permissions (RBAC): read — viewer; create/update — operator+.
INSERT INTO permissions (name, description) VALUES
    ('projects.read', 'read projects and their summary'),
    ('projects.create', 'create/archive projects'),
    ('projects.update', 'modify projects (name/description); archive')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'admin' AND p.name IN ('projects.read', 'projects.create', 'projects.update')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name IN ('projects.read', 'projects.create', 'projects.update')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'viewer' AND p.name = 'projects.read'
ON CONFLICT DO NOTHING;
