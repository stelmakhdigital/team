-- 0013_watchdog_jobs (sqlite) — P1.6: Watchdog-джобы (register, context-usage-threshold).
-- Джоб переживает рестарт (persisted); scheduler (watchdog-loop) проверяет на каждом скане.
--
-- policy v1: 'context-usage-threshold' — target-сессия жива и context_used_percentage
--   >= threshold_pct → fire (алерт + fire watchdog-wakes parked-задач). Прочие политики
--   — документированный follow-up (см. api-decisions.md §P1.6).
-- target_session_id: для context-threshold ОБЯЗАТЕЛЬНА (nullable FK; ON DELETE SET NULL).
-- threshold_pct: целое 1..100 — выбрано pct (не mb): у нас есть context_used_percentage.
-- fired_count/last_fired_at/last_fired_pct: для дедуп (повтор, если процент вырос >5 п.т.
--   с last_fired; иначе quiet skip, НЕ логируется — как у OpenRIG).
-- watchdog_job_events: история fire / dead-target (bounded 20/job); quiet skips не логируются.
-- TIMESTAMP — TEXT (RFC3339Nano, UTC); bool — INTEGER (0/1) (ADR-002).

CREATE TABLE IF NOT EXISTS watchdog_jobs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL,
    policy            TEXT NOT NULL DEFAULT 'context-usage-threshold'
                      CHECK (policy IN ('context-usage-threshold')),
    target_session_id INTEGER REFERENCES sessions(id) ON DELETE SET NULL,
    threshold_pct     INTEGER,
    message           TEXT NOT NULL DEFAULT '',
    enabled           INTEGER NOT NULL DEFAULT 1,
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    fired_count       INTEGER NOT NULL DEFAULT 0,
    last_fired_at     TEXT,
    last_fired_pct    REAL
);
CREATE INDEX IF NOT EXISTS idx_watchdog_jobs_enabled ON watchdog_jobs(enabled);
CREATE INDEX IF NOT EXISTS idx_watchdog_jobs_target ON watchdog_jobs(target_session_id);

CREATE TABLE IF NOT EXISTS watchdog_job_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id     INTEGER NOT NULL REFERENCES watchdog_jobs(id) ON DELETE CASCADE,
    outcome    TEXT NOT NULL CHECK (outcome IN ('fired', 'dead-target')),
    detail     TEXT NOT NULL DEFAULT '',
    pct        REAL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_watchdog_job_events_job ON watchdog_job_events(job_id, created_at);

-- RBAC: watchdog.update (operator+). viewer не получает (not %.read).
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('watchdog.update', 'register/stop/delete watchdog jobs');
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'watchdog.update';
