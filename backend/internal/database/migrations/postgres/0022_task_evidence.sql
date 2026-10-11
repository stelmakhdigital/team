-- 0022_task_evidence (postgres) — P3.13: Proof/evidence на задачах.
-- См. sqlite/0022_task_evidence.sql. TIMESTAMP — TEXT (RFC3339Nano, UTC).

CREATE TABLE IF NOT EXISTS task_evidence (
    id             BIGSERIAL PRIMARY KEY,
    task_id        BIGINT NOT NULL REFERENCES queue_tasks(id) ON DELETE CASCADE,
    etype          TEXT NOT NULL CHECK (etype IN ('screenshot', 'test', 'log', 'other')),
    name           TEXT NOT NULL,
    content_ref    TEXT,
    content_sha    TEXT,
    description    TEXT,
    status         TEXT NOT NULL DEFAULT 'submitted'
                   CHECK (status IN ('submitted', 'accepted', 'rejected', 'withdrawn')),
    verdict_reason TEXT,
    submitted_by   TEXT,
    judged_by      TEXT,
    judged_at      TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_evidence_task ON task_evidence(task_id);
CREATE INDEX IF NOT EXISTS idx_task_evidence_status ON task_evidence(status);
