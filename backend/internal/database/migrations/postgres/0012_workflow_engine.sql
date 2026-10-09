-- 0012_workflow_engine (postgres) — P1.4: Workflow engine (исполняемые воркфлоу).
-- См. sqlite/0012_workflow_engine.sql. TIMESTAMP — TEXT, bool — INTEGER (0/1) (ADR-002).

ALTER TABLE workflows ADD COLUMN engine_enabled INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS workflow_instances (
    id               BIGSERIAL PRIMARY KEY,
    workflow_id      BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    state            TEXT NOT NULL DEFAULT 'running'
                     CHECK (state IN ('running', 'waiting', 'completed', 'failed', 'aborted')),
    root_objective   TEXT NOT NULL DEFAULT '',
    current_block_id BIGINT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    finished_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_wf ON workflow_instances(workflow_id, state);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_state ON workflow_instances(state);

CREATE TABLE IF NOT EXISTS workflow_steps (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    block_id    BIGINT NOT NULL,
    task_id     BIGINT,
    state       TEXT NOT NULL DEFAULT 'pending'
                CHECK (state IN ('pending', 'active', 'done', 'failed', 'skipped', 'routed')),
    exit_kind   TEXT CHECK (exit_kind IN ('handoff', 'waiting', 'done', 'failed')),
    actor       BIGINT,
    result_note TEXT,
    entered_at  TEXT,
    exited_at   TEXT
);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_instance ON workflow_steps(instance_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_task ON workflow_steps(task_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_state ON workflow_steps(instance_id, state);
