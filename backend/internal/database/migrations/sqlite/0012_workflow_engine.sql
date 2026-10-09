-- 0012_workflow_engine (sqlite) — P1.4: Workflow engine (исполняемые воркфлоу).
--
-- Ориентир OpenRIG workflow runtime: «the runtime is the scribe, not the gate».
-- Active-воркфлоу проецирует задачи по блокам; закрытие шага в одной транзакции
-- создаёт следующий.
--
-- engine_enabled=false (compat, по умолчанию) = воркфлоу остаётся «картинкой»
-- (редактор, без исполнения) — существующие воркфлоу работают как раньше.
--
-- workflow_instances — один запуск воркфлоу (state: running|waiting|completed|
-- failed|aborted). workflow_steps — append-only trail шагов (task_id — задача,
-- созданная для task/agent-блока; exit_kind — причина выхода; actor — роль).
--
-- TIMESTAMP — TEXT (RFC3339Nano, UTC), bool — INTEGER (0/1) — конвенции (ADR-002).

-- Fлаг исполняемости воркфлоу (compat: старые — как раньше).
ALTER TABLE workflows ADD COLUMN engine_enabled INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS workflow_instances (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id      INTEGER NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    state            TEXT NOT NULL DEFAULT 'running'
                     CHECK (state IN ('running', 'waiting', 'completed', 'failed', 'aborted')),
    root_objective   TEXT NOT NULL DEFAULT '',
    current_block_id INTEGER,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    finished_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_wf ON workflow_instances(workflow_id, state);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_state ON workflow_instances(state);

-- Шаг исполнения (append-only trail). block_id — логическая ссылка на
-- workflow_blocks (без FK: блок может быть удалён, trail сохраняется).
-- task_id — логическая ссылка на queue_tasks (nullable: control-блоки без задачи).
-- actor — role_id целевой роли (nullable).
CREATE TABLE IF NOT EXISTS workflow_steps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    block_id    INTEGER NOT NULL,
    task_id     INTEGER,
    state       TEXT NOT NULL DEFAULT 'pending'
                CHECK (state IN ('pending', 'active', 'done', 'failed', 'skipped', 'routed')),
    exit_kind   TEXT CHECK (exit_kind IN ('handoff', 'waiting', 'done', 'failed')),
    actor       INTEGER,
    result_note TEXT,
    entered_at  TEXT,
    exited_at   TEXT
);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_instance ON workflow_steps(instance_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_task ON workflow_steps(task_id);
CREATE INDEX IF NOT EXISTS idx_workflow_steps_state ON workflow_steps(instance_id, state);
