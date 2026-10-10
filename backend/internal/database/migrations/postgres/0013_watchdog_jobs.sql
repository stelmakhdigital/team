-- 0013_watchdog_jobs (postgres) — P1.6: Watchdog-джобы. См. sqlite/0013_watchdog_jobs.sql.
-- TIMESTAMP — TEXT, bool — INTEGER (0/1) (ADR-002).

CREATE TABLE IF NOT EXISTS watchdog_jobs (
    id                BIGSERIAL PRIMARY KEY,
    name              TEXT NOT NULL,
    policy            TEXT NOT NULL DEFAULT 'context-usage-threshold'
                      CHECK (policy IN ('context-usage-threshold')),
    target_session_id BIGINT REFERENCES sessions(id) ON DELETE SET NULL,
    threshold_pct     INTEGER,
    message           TEXT NOT NULL DEFAULT '',
    enabled           INTEGER NOT NULL DEFAULT 1,
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    fired_count       INTEGER NOT NULL DEFAULT 0,
    last_fired_at     TEXT,
    last_fired_pct    DOUBLE PRECISION
);
CREATE INDEX IF NOT EXISTS idx_watchdog_jobs_enabled ON watchdog_jobs(enabled);
CREATE INDEX IF NOT EXISTS idx_watchdog_jobs_target ON watchdog_jobs(target_session_id);

CREATE TABLE IF NOT EXISTS watchdog_job_events (
    id         BIGSERIAL PRIMARY KEY,
    job_id     BIGINT NOT NULL REFERENCES watchdog_jobs(id) ON DELETE CASCADE,
    outcome    TEXT NOT NULL CHECK (outcome IN ('fired', 'dead-target')),
    detail     TEXT NOT NULL DEFAULT '',
    pct        DOUBLE PRECISION,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_watchdog_job_events_job ON watchdog_job_events(job_id, created_at);

-- RBAC: watchdog.update (operator+).
INSERT INTO permissions (name, description) VALUES
    ('watchdog.update', 'register/stop/delete watchdog jobs')
    ON CONFLICT (name) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM security_roles r CROSS JOIN permissions p
WHERE r.name = 'operator' AND p.name = 'watchdog.update'
    ON CONFLICT (role_id, permission_id) DO NOTHING;
