-- 0014_watchdog_events_job (postgres) — P1.6: add 'watchdog_job' к watchdog_events.event_type.
-- Меняем существующий CHECK-констрейн (стандартное имя pg: <table>_<column>_check).

ALTER TABLE watchdog_events DROP CONSTRAINT IF EXISTS watchdog_events_event_type_check;
ALTER TABLE watchdog_events ADD CONSTRAINT watchdog_events_event_type_check
    CHECK (event_type IN ('wake', 'refocus', 'alignment_checkpoint', 'stale', 'blocked', 'idle', 'drift', 'watchdog_job'));
