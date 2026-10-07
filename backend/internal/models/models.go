package models

import "time"

// TeamState — состояние команды.
type TeamState string

const (
	TeamActive   TeamState = "active"
	TeamArchived TeamState = "archived"
	TeamStopped  TeamState = "stopped"
)

func (s TeamState) Valid() bool {
	switch s {
	case TeamActive, TeamArchived, TeamStopped:
		return true
	}
	return false
}

type Team struct {
	ID          int64
	Name        string
	Description string
	SpecPath    string
	State       TeamState
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Segment — логическая группа ролей внутри команды.
type Segment struct {
	ID          int64
	TeamID      int64
	Name        string
	Description string
	Config      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RoleState — состояние роли.
type RoleState string

const (
	RoleActive   RoleState = "active"
	RoleInactive RoleState = "inactive"
	RoleBlocked  RoleState = "blocked"
)

func (s RoleState) Valid() bool {
	switch s {
	case RoleActive, RoleInactive, RoleBlocked:
		return true
	}
	return false
}

// Role — конкретный агент (seat) в сегменте.
type Role struct {
	ID        int64
	TeamID    int64
	SegmentID int64
	Name      string
	Address   string // уникально: "team:segment.role"
	AgentSpec string
	Profile   string
	Config    map[string]any
	State     RoleState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RelativeType — тип связи между ролями.
type RelativeType string

const (
	RelDelegatesTo  RelativeType = "delegates_to"
	RelSpawnedBy    RelativeType = "spawned_by"
	RelCanObserve   RelativeType = "can_observe"
	RelCollaborates RelativeType = "collaborates_with"
)

func (t RelativeType) Valid() bool {
	switch t {
	case RelDelegatesTo, RelSpawnedBy, RelCanObserve, RelCollaborates:
		return true
	}
	return false
}

// Relative — направленный край между ролями.
type Relative struct {
	ID         int64
	TeamID     int64
	FromRoleID int64
	ToRoleID   int64
	Type       RelativeType
	Config     map[string]any
	CreatedAt  time.Time
}
