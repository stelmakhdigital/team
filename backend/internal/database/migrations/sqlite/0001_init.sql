-- 0001_init (sqlite) — slice 1: Team Builder
CREATE TABLE IF NOT EXISTS teams (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    spec_path   TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL DEFAULT 'active'
                CHECK (state IN ('active', 'archived', 'stopped')),
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS segments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id     INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE(team_id, name)
);
CREATE INDEX IF NOT EXISTS idx_segments_team ON segments(team_id);

CREATE TABLE IF NOT EXISTS roles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    segment_id INTEGER NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    address    TEXT NOT NULL,
    agent_spec TEXT NOT NULL DEFAULT '',
    profile    TEXT NOT NULL DEFAULT '',
    config     TEXT NOT NULL DEFAULT '{}',
    state      TEXT NOT NULL DEFAULT 'active'
               CHECK (state IN ('active', 'inactive', 'blocked')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(team_id, segment_id, name),
    UNIQUE(address)
);
CREATE INDEX IF NOT EXISTS idx_roles_team ON roles(team_id);
CREATE INDEX IF NOT EXISTS idx_roles_segment ON roles(segment_id);

CREATE TABLE IF NOT EXISTS relatives (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id      INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    from_role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    to_role_id   INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    type         TEXT NOT NULL
                 CHECK (type IN ('delegates_to', 'spawned_by', 'can_observe', 'collaborates_with')),
    config       TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    UNIQUE(from_role_id, to_role_id, type)
);
CREATE INDEX IF NOT EXISTS idx_relatives_team ON relatives(team_id);
