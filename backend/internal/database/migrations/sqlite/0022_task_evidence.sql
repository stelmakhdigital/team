-- 0022_task_evidence (sqlite) — P3.13: Proof/evidence на задачах.
-- Артефакты-доказательства (screenshot/test/log/other), привязанные к задаче,
-- с typed метаданными (etype, content_sha = candidate-sha) + verdict (judge).
--
-- «Closure ≠ acceptance»: task.state (done/canceled) ≠ acceptance (verdict).
-- acceptance вычисляется по evidence (есть accepted → accepted; …) — см. service.
--
-- Timestamps — TEXT (RFC3339Nano, UTC) — как во всех таблицах проекта.

CREATE TABLE task_evidence (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id        INTEGER NOT NULL REFERENCES queue_tasks(id) ON DELETE CASCADE,
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
