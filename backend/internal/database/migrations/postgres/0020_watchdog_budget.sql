-- 0020_watchdog_budget (postgres) — P2.14: add 'budget' к watchdog_events.event_type.
-- Меняем существующий CHECK-констрейн.

ALTER TABLE watchdog_events DROP CONSTRAINT IF EXISTS watchdog_events_event_type_check;
ALTER TABLE watchdog_events ADD CONSTRAINT watchdog_events_event_type_check
    CHECK (event_type IN ('wake', 'refocus', 'alignment_checkpoint', 'stale', 'blocked', 'idle', 'drift', 'watchdog_job', 'diagnostics_fail', 'budget'));
