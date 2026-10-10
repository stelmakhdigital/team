package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// DBTX — *sql.DB или *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Stores — конретные репозитории над одним *sql.DB.
type Stores struct {
	Teams          *TeamRepo
	Segments       *SegmentRepo
	Roles          *RoleRepo
	Relatives      *RelativeRepo
	Tasks          *TaskRepo
	History        *HistoryRepo
	Wakes          *WakeRepo // P0.3: block with wake
	Sessions       *SessionRepo
	SessionHistory *SessionHistoryRepo
	Watchdog       *WatchdogRepo
	// P1.6 — Watchdog-джобы (register, context-usage-threshold)
	WatchdogJobs      *WatchdogJobRepo
	WatchdogJobEvents *WatchdogJobEventRepo
	// Slice 4 — Message Center
	Messages         *MessageRepo
	Chatrooms        *ChatroomRepo
	ChatroomMessages *ChatroomMessageRepo
	// Slice 5 — Workflows
	Workflows           *WorkflowRepo
	WorkflowBlocks      *WorkflowBlockRepo
	WorkflowConnections *WorkflowConnectionRepo
	// P1.4 — Workflow engine (исполняемые воркфлоу)
	WorkflowInstances *WorkflowInstanceRepo
	WorkflowSteps     *WorkflowStepRepo
	// Slice 5b — Library + Audit
	Library         *LibraryRepo
	LibraryVersions *LibraryVersionRepo
	Audit           *AuditRepo
	// U4 — Config (overrides)
	ConfigOverrides *ConfigRepo
	// P1.0 — Projects (мультипроектность)
	Projects *ProjectRepo
	// P2.9 — Pulse feed (events: real-time-события шины)
	Events *EventRepo
}

func NewStores(db *sql.DB) *Stores {
	return &Stores{
		Teams:               &TeamRepo{db: db},
		Segments:            &SegmentRepo{db: db},
		Roles:               &RoleRepo{db: db},
		Relatives:           &RelativeRepo{db: db},
		Tasks:               &TaskRepo{db: db},
		History:             &HistoryRepo{db: db},
		Wakes:               &WakeRepo{db: db},
		Sessions:            &SessionRepo{db: db},
		SessionHistory:      &SessionHistoryRepo{db: db},
		Watchdog:            &WatchdogRepo{db: db},
		WatchdogJobs:        &WatchdogJobRepo{db: db},
		WatchdogJobEvents:   &WatchdogJobEventRepo{db: db},
		Messages:            &MessageRepo{db: db},
		Chatrooms:           &ChatroomRepo{db: db},
		ChatroomMessages:    &ChatroomMessageRepo{db: db},
		Workflows:           &WorkflowRepo{db: db},
		WorkflowBlocks:      &WorkflowBlockRepo{db: db},
		WorkflowConnections: &WorkflowConnectionRepo{db: db},
		WorkflowInstances:   &WorkflowInstanceRepo{db: db},
		WorkflowSteps:       &WorkflowStepRepo{db: db},
		Library:             &LibraryRepo{db: db},
		LibraryVersions:     &LibraryVersionRepo{db: db},
		Audit:               &AuditRepo{db: db},
		ConfigOverrides:     &ConfigRepo{db: db},
		Projects:            &ProjectRepo{db: db},
		Events:              NewEventRepo(db),
	}
}

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// nullableInt64 — *int64 → any (nil = NULL) для nullable FK-колонн (P1.0).
func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// nullableInt — *int → any (nil = NULL) (P1.6: threshold_pct).
func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// boolToInt — bool → int (0/1) (ADR-002: bool — INTEGER).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullableFloat64 — *float64 → any (nil = NULL) (P1.6: last_fired_pct).
func nullableFloat64(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// IsUniqueViolation — определение unique-violation для sqlite/postgres.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") || // sqlite
		strings.Contains(s, "23505") || // postgres SQLSTATE
		strings.Contains(s, "duplicate key value")
}

func nowTime() time.Time { return time.Now().UTC() }
func nowStr() string     { return nowTime().Format(time.RFC3339Nano) }
func timeNow() string    { return nowStr() } // alias
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func marshalJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalConfig(s string) map[string]any {
	var m map[string]any
	if s == "" {
		return map[string]any{}
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}
