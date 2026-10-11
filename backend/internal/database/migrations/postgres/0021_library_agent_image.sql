-- 0021_library_agent_image (postgres) — P3.12: новый тип library `agent_image`
-- + флаг is_pinned (защита от prune).
-- pg: меняется существующий CHECK-констрейн type (DROP/ADD) + ADD COLUMN.

ALTER TABLE library_items DROP CONSTRAINT IF EXISTS library_items_type_check;
ALTER TABLE library_items ADD CONSTRAINT library_items_type_check
    CHECK (type IN ('team', 'workflow', 'role', 'segment', 'agent_image'));

ALTER TABLE library_items ADD COLUMN IF NOT EXISTS is_pinned INTEGER NOT NULL DEFAULT 0;
