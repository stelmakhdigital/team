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
	Sessions       *SessionRepo
	SessionHistory *SessionHistoryRepo
	Watchdog       *WatchdogRepo
	// Slice 4 — Message Center
	Messages         *MessageRepo
	Chatrooms        *ChatroomRepo
	ChatroomMessages *ChatroomMessageRepo
	// Slice 5 — Workflows
	Workflows           *WorkflowRepo
	WorkflowBlocks      *WorkflowBlockRepo
	WorkflowConnections *WorkflowConnectionRepo
	// Slice 5b — Library + Audit
	Library         *LibraryRepo
	LibraryVersions *LibraryVersionRepo
	Audit           *AuditRepo
}

func NewStores(db *sql.DB) *Stores {
	return &Stores{
		Teams:               &TeamRepo{db: db},
		Segments:            &SegmentRepo{db: db},
		Roles:               &RoleRepo{db: db},
		Relatives:           &RelativeRepo{db: db},
		Tasks:               &TaskRepo{db: db},
		History:             &HistoryRepo{db: db},
		Sessions:            &SessionRepo{db: db},
		SessionHistory:      &SessionHistoryRepo{db: db},
		Watchdog:            &WatchdogRepo{db: db},
		Messages:            &MessageRepo{db: db},
		Chatrooms:           &ChatroomRepo{db: db},
		ChatroomMessages:    &ChatroomMessageRepo{db: db},
		Workflows:           &WorkflowRepo{db: db},
		WorkflowBlocks:      &WorkflowBlockRepo{db: db},
		WorkflowConnections: &WorkflowConnectionRepo{db: db},
		Library:             &LibraryRepo{db: db},
		LibraryVersions:     &LibraryVersionRepo{db: db},
		Audit:               &AuditRepo{db: db},
	}
}

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

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
