-- 0009_task_closure_required (postgres) — P0.2: дедлайн закрытия задачи
-- tasks.closure_required_at выставляется при переходе в in_progress
-- (= now + tasks.closure_required_secs) и пересчитывается при повторном входе.
-- TIMESTAMP храним как TEXT (RFC3339Nano, UTC) — единая конвенция всех
-- временных полей queue_tasks (created_at/updated_at/started_at/...).

ALTER TABLE queue_tasks ADD COLUMN closure_required_at TEXT;

-- индекс для bounded-среза просроченных (state + наличие дедлайна)
CREATE INDEX IF NOT EXISTS idx_queue_tasks_closure_due ON queue_tasks(state, closure_required_at);
