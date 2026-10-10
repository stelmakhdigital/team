-- 0015_diagnostics (postgres) — P1.8: Health/Doctor-панель (RBAC).
-- diagnostics.read (operator+): health/doctor-поверхность раскрывает операционные
-- детали (статус БД, tmux, живость процессов, расхождения) — viewer не получает
-- (not %.read), как watchdog.update. admin получает через AllPermissions (Go).
INSERT INTO permissions (name, description) VALUES
    ('diagnostics.read', 'read diagnostics (daemon/DB/tmux/sessions/logs/git health + divergence)')
    ON CONFLICT (name) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'diagnostics.read'
    ON CONFLICT (role_id, permission_id) DO NOTHING;
