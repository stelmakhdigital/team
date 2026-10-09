-- 0010_task_wakes (sqlite) — P0.3: wake для заблокированной задачи (block with wake)
-- У блока есть причина-триггер (wake) + текст «как продолжить».
-- Разблокировка: по срабатыванию wake (timer/task) или ручным resolve.
--
-- kind: 'timer' | 'task'. Вид 'watchdog' ЗАПЛАНИРОВАН (P1.6), в коде пока не
--       реализуется (см. api-decisions.md §P0.3).
-- ref:  kind=task  → task_id (string) referenced-задачи;
--       kind=timer → fire-at (RFC3339Nano, = created_at + after).
-- fired_at: закрытие wake (fired ИЛИ resolve) — идемпотентность (не срабатывает повторно).
-- TIMESTAMP храним как TEXT (RFC3339Nano, UTC) — единая конвенция.

CREATE TABLE IF NOT EXISTS task_wakes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      INTEGER NOT NULL REFERENCES queue_tasks(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    ref          TEXT,
    continuation TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    fired_at     TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_wakes_task ON task_wakes(task_id);
-- арм. timer-wakes (fired_at IS NULL) + их fire-at — для watchdog-loop
CREATE INDEX IF NOT EXISTS idx_task_wakes_kind_fired ON task_wakes(kind, fired_at);
-- task-wakes по referenced task — для триггера закрытия
CREATE INDEX IF NOT EXISTS idx_task_wakes_ref ON task_wakes(ref);
