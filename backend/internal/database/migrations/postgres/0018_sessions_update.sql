-- 0018_sessions_update (postgres) — P2.11: send to session + verify (RBAC).
-- sessions.update (operator+): ввод текста в терминал живой сессии (dangerous) —
-- viewer не получает (not %.read). admin — через AllPermissions + wildcard.
INSERT INTO permissions (name, description) VALUES
    ('sessions.update', 'send text into a live session terminal (verify delivery)')
    ON CONFLICT (name) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'sessions.update'
    ON CONFLICT (role_id, permission_id) DO NOTHING;
