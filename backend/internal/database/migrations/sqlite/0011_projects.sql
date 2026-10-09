-- 0011_projects (sqlite) — P1.0: Projects (мультипроектность)
-- Сущность «проект»: рабочая директория (existing/new), git, привязка к
-- командам/задачам/сессиям. «Где работать» — свойство работы.
--
-- kind: 'existing' — каталог уже существует (path задан);
--       'new'      — daemon создаёт каталог (parent + name) + git init.
-- git:   автоопределение (.git есть) для existing; результат git init для new.
-- archived: DELETE /projects/{id} = архив (идемпотентно); архивный проект
--           нельзя использовать для новых команд/сессий (409).
-- path:  immutable после создания (PATCH path → 409, см. api-decisions.md §P1.0).
--
-- TIMESTAMP храним как TEXT (RFC3339Nano, UTC), bool как INTEGER (0/1) — конвенции.

CREATE TABLE IF NOT EXISTS projects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
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

-- P1.0: привязка команд и задач к проекту (nullable — команды/задачи без
-- project_id работают как раньше; миграционная совместимость).
-- NOTE: FK не прописываем в ALTER (SQLite не поддерживает ADD COLUMN REFERENCES);
--       связь логическая, на уровне сервиса.
ALTER TABLE teams ADD COLUMN project_id INTEGER;
ALTER TABLE queue_tasks ADD COLUMN project_id INTEGER;

CREATE INDEX IF NOT EXISTS idx_teams_project ON teams(project_id);
CREATE INDEX IF NOT EXISTS idx_queue_tasks_project ON queue_tasks(project_id);

-- P1.0: привязка сессий к проекту (результат разрешения working_dir).
ALTER TABLE sessions ADD COLUMN project_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id);

-- P1.0: permissions (RBAC): read — viewer; create/update — operator+.
-- См. 0007 (модель ролей) + 0008 (паттерн добавления permissions).
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('projects.read', 'read projects and their summary'),
    ('projects.create', 'create/archive projects'),
    ('projects.update', 'modify projects (name/description); archive');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'admin' AND p.name IN ('projects.read', 'projects.create', 'projects.update');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name IN ('projects.read', 'projects.create', 'projects.update');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'viewer' AND p.name = 'projects.read';
