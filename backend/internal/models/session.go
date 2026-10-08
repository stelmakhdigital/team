package models

import "time"

// SessionState — состояние сессии (контракт 20 §3.3).
type SessionState string

const (
	SessionStarting SessionState = "starting"
	SessionRunning  SessionState = "running"
	SessionIdle     SessionState = "idle"
	SessionStopping SessionState = "stopping"
	SessionStopped  SessionState = "stopped"
	SessionFailed   SessionState = "failed"
)

func (s SessionState) Valid() bool {
	switch s {
	case SessionStarting, SessionRunning, SessionIdle, SessionStopping, SessionStopped, SessionFailed:
		return true
	}
	return false
}

// Активные (не терминальные) состояния.
func (s SessionState) Active() bool {
	return s == SessionStarting || s == SessionRunning || s == SessionIdle || s == SessionStopping
}

// Session — сессия агента (процесс/tmux/pi).
type Session struct {
	ID          int64
	TeamID      int64
	RoleID      int64
	QueueTaskID *int64
	RuntimeType string
	RuntimeRef  string
	State       SessionState
	WorkingDir  string
	TargetDir   string
	Command     string
	Config      map[string]any
	ExitCode    *int
	StartedAt   *time.Time
	StoppedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SessionHistoryEntry — переход состояния сессии.
type SessionHistoryEntry struct {
	ID          int64
	SessionID   int64
	FromState   *string
	ToState     string
	ActorType   string // role | daemon | watchdog | human
	ActorRoleID *int64
	Comment     *string
	Metadata    map[string]any
	CreatedAt   time.Time
}

// WatchdogEvent — событие/алерт watchdog (контракт 20 §3.4).
type WatchdogEvent struct {
	ID             int64
	TeamID         int64
	QueueTaskID    *int64
	SessionID      *int64
	RoleID         *int64
	EventType      string // wake|refocus|alignment_checkpoint|stale|blocked|idle|drift
	Severity       string // low|medium|high|critical
	Description    string
	ActionTaken    *string
	IsRead         bool
	RequiresAction bool
	CreatedAt      time.Time
}
