-- 0018_sessions_update (sqlite) — P2.11: send to session + verify (RBAC).
-- sessions.update (operator+): ввод текста в терминал живой сессии (dangerous,
-- влияет на работающий процесс) — viewer не получает (not %.read), как
-- watchdog.update/diagnostics.read. admin получает через AllPermissions + wildcard.
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('sessions.update', "send text into a live session terminal (verify delivery)");
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'sessions.update';
