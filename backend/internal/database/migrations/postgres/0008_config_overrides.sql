-- 0008_config_overrides (postgres) — U4: подсистема конфигурации
-- Слой "override" (явные значения из PUT /api/v1/config/{key}) + permission config.read.

CREATE TABLE IF NOT EXISTS config_overrides (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Permission: config.read (GET /api/v1/config; admin+operator+viewer)
INSERT INTO permissions (name, description) VALUES
    ('config.read', 'read daemon configuration (settings page)')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE p.name = 'config.read'
ON CONFLICT DO NOTHING;

-- U4: config.update теперь выдан и operator (admin+operator)
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'config.update'
ON CONFLICT DO NOTHING;

UPDATE security_roles SET description = 'read/write, config update' WHERE name = 'operator';
