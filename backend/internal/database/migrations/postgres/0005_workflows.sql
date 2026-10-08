-- 0005_workflows (postgres) — slice 5: Workflow Editor
-- Примечание: timestamps и JSON хранятся как TEXT для единообразия с sqlite-диалектом (ADR-002).
CREATE TABLE IF NOT EXISTS workflows (
    id             BIGSERIAL PRIMARY KEY,
    team_id        BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    description    TEXT,
    state          TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'active', 'archived')),
    config         TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE (team_id, name)
);
CREATE INDEX IF NOT EXISTS idx_workflows_team ON workflows(team_id, state);

CREATE TABLE IF NOT EXISTS workflow_blocks (
    id             BIGSERIAL PRIMARY KEY,
    workflow_id    BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    type           TEXT NOT NULL
                   CHECK (type IN ('task', 'decision', 'parallel', 'loop', 'agent', 'manual')),
    position_x     INTEGER NOT NULL DEFAULT 0,
    position_y     INTEGER NOT NULL DEFAULT 0,
    label          TEXT,
    config         TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workflow_blocks_wf ON workflow_blocks(workflow_id);

CREATE TABLE IF NOT EXISTS workflow_connections (
    id             BIGSERIAL PRIMARY KEY,
    workflow_id    BIGINT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    from_block_id  BIGINT NOT NULL REFERENCES workflow_blocks(id) ON DELETE CASCADE,
    to_block_id    BIGINT NOT NULL REFERENCES workflow_blocks(id) ON DELETE CASCADE,
    condition      TEXT,
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_workflow_connections_wf ON workflow_connections(workflow_id);
