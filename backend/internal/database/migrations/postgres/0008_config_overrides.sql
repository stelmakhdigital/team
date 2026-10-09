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

-- NB: config.update остаётся admin-only (не выдаём operator): эта же permission
-- защищает PATCH /roles/{id}/config (slice 6) — выдача operator изменила бы RBAC slice 6.
-- U4 daemon-config write = admin-only.

