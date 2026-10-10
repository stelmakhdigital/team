package models

import (
	"database/sql"
	"time"
)

// Event — P2.9: real-time-событие шины (для единой ленты Pulse).
// Отличается от AuditEntry: events = события real-time шины (task./session./
// message./alert./workflow.), audit = действия API (user/api_key/resource).
type Event struct {
	ID         int64
	Type       string
	TeamID     sql.NullInt64
	RoleID     sql.NullInt64
	EntityKind string
	EntityID   int64
	Summary    string
	Payload    string
	CreatedAt  time.Time
}

// EnrichedEvent — Event + решённые имена (team/role/entity) для ответа GET /events.
type EnrichedEvent struct {
	ID          int64   `json:"id"`
	Type        string  `json:"type"`
	TeamID      *int64  `json:"team_id"`
	TeamName    *string `json:"team_name"`
	RoleID      *int64  `json:"role_id"`
	RoleName    *string `json:"role_name"`
	EntityKind  string  `json:"entity_kind"`
	EntityID    int64   `json:"entity_id"`
	EntityTitle *string `json:"entity_title"`
	Summary     string  `json:"summary"`
	CreatedAt   string  `json:"created_at"`
}
