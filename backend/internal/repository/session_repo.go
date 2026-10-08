package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
)

// ---------- Sessions ----------

type SessionRepo struct{ db *sql.DB }

type SessionFilter struct {
	TeamID *int64
	RoleID *int64
	State  *models.SessionState
	Active bool // starting|running|idle|stopping
	Limit  int
	Offset int
}

const sessionColumns = `id, team_id, role_id, queue_task_id, runtime_type, runtime_ref,
	state, working_dir, target_dir, command, config, exit_code,
	started_at, stopped_at, created_at, updated_at`

func (r *SessionRepo) Create(ctx context.Context, tx DBTX, s *models.Session) error {
	now := nowTime()
	s.CreatedAt, s.UpdatedAt = now, now
	if s.State == "" {
		s.State = models.SessionStarting
	}
	if s.Config == nil {
		s.Config = map[string]any{}
	}
	cfg, err := marshalJSON(s.Config)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (team_id, role_id, queue_task_id, runtime_type, runtime_ref,
			state, working_dir, target_dir, command, config, exit_code,
			started_at, stopped_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.TeamID, s.RoleID, s.QueueTaskID, s.RuntimeType, s.RuntimeRef,
		s.State, s.WorkingDir, s.TargetDir, s.Command, cfg, s.ExitCode,
		s.StartedAt, s.StoppedAt,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	s.ID = id
	return nil
}

func (r *SessionRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Session, error) {
	return scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id))
}

func (r *SessionRepo) List(ctx context.Context, tx DBTX, f SessionFilter) ([]*models.Session, int, error) {
	conds := []string{}
	args := []any{}
	if f.TeamID != nil {
		conds = append(conds, "team_id = ?")
		args = append(args, *f.TeamID)
	}
	if f.RoleID != nil {
		conds = append(conds, "role_id = ?")
		args = append(args, *f.RoleID)
	}
	if f.State != nil {
		conds = append(conds, "state = ?")
		args = append(args, string(*f.State))
	} else if f.Active {
		conds = append(conds, "state IN ('starting', 'running', 'idle', 'stopping')")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAll(conds)
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT `+sessionColumns+` FROM sessions`+where+`
		ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*models.Session, 0, limit)
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

func (r *SessionRepo) UpdateState(ctx context.Context, tx DBTX, id int64, s *models.Session) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE sessions SET state = ?, runtime_ref = ?, exit_code = ?,
			started_at = COALESCE(started_at, ?), stopped_at = ?, updated_at = ?
		WHERE id = ?`,
		s.State, s.RuntimeRef, s.ExitCode, s.StartedAt, s.StoppedAt, nowStr(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SessionRepo) ListByState(ctx context.Context, tx DBTX, state models.SessionState, limit int) ([]*models.Session, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE state = ? ORDER BY id LIMIT ?`,
		string(state), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SessionRepo) CountByStates(ctx context.Context, tx DBTX, states ...models.SessionState) (int, error) {
	if len(states) == 0 {
		return 0, nil
	}
	ph := make([]string, len(states))
	args := make([]any, len(states))
	for i, s := range states {
		ph[i] = "?"
		args[i] = string(s)
	}
	return countQuery(ctx, tx, `SELECT COUNT(*) FROM sessions WHERE state IN (`+joinPh(ph)+`)`, args...)
}

type sessionScanner interface{ Scan(dest ...any) error }

func scanSession(s sessionScanner) (*models.Session, error) {
	var (
		x         models.Session
		taskID    sql.NullInt64
		ref       sql.NullString
		wd, td, c sql.NullString
		cfgStr    string
		exit      sql.NullInt64
		started   sql.NullString
		stopped   sql.NullString
		ca, ua    string
	)
	err := s.Scan(&x.ID, &x.TeamID, &x.RoleID, &taskID, &x.RuntimeType, &ref,
		&x.State, &wd, &td, &c, &cfgStr, &exit, &started, &stopped, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if taskID.Valid {
		x.QueueTaskID = &taskID.Int64
	}
	if ref.Valid {
		x.RuntimeRef = ref.String
	}
	if wd.Valid {
		x.WorkingDir = wd.String
	}
	if td.Valid {
		x.TargetDir = td.String
	}
	if c.Valid {
		x.Command = c.String
	}
	if exit.Valid {
		v := int(exit.Int64)
		x.ExitCode = &v
	}
	if started.Valid {
		if t, err := parseTime(started.String); err == nil {
			x.StartedAt = &t
		}
	}
	if stopped.Valid {
		if t, err := parseTime(stopped.String); err == nil {
			x.StoppedAt = &t
		}
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(cfgStr), &cfg); err == nil && cfg != nil {
		x.Config = cfg
	} else {
		x.Config = map[string]any{}
	}
	if x.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse session created_at: %w", err)
	}
	if x.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse session updated_at: %w", err)
	}
	return &x, nil
}

// ---------- Session history ----------

type SessionHistoryRepo struct{ db *sql.DB }

func (r *SessionHistoryRepo) Create(ctx context.Context, tx DBTX, h *models.SessionHistoryEntry) error {
	h.CreatedAt = nowTime()
	if h.ActorType == "" {
		h.ActorType = "daemon"
	}
	if h.Metadata == nil {
		h.Metadata = map[string]any{}
	}
	md, err := marshalJSON(h.Metadata)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO session_history (session_id, from_state, to_state, actor_type,
			actor_role_id, comment, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		h.SessionID, h.FromState, h.ToState, h.ActorType, h.ActorRoleID, h.Comment, md,
		h.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	h.ID = id
	return nil
}

func (r *SessionHistoryRepo) ListBySession(ctx context.Context, tx DBTX, sessionID int64, limit, offset int) ([]*models.SessionHistoryEntry, int, error) {
	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_history WHERE session_id = ?`, sessionID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, session_id, from_state, to_state, actor_type, actor_role_id, comment, metadata, created_at
		FROM session_history
		WHERE session_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT ? OFFSET ?`, sessionID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*models.SessionHistoryEntry, 0, limit)
	for rows.Next() {
		var (
			h       models.SessionHistoryEntry
			from    sql.NullString
			actor   sql.NullInt64
			comment sql.NullString
			md      string
			created string
		)
		if err := rows.Scan(&h.ID, &h.SessionID, &from, &h.ToState, &h.ActorType, &actor, &comment, &md, &created); err != nil {
			return nil, 0, err
		}
		if from.Valid {
			v := from.String
			h.FromState = &v
		}
		if actor.Valid {
			h.ActorRoleID = &actor.Int64
		}
		if comment.Valid {
			v := comment.String
			h.Comment = &v
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(md), &m); err == nil && m != nil {
			h.Metadata = m
		} else {
			h.Metadata = map[string]any{}
		}
		if h.CreatedAt, err = parseTime(created); err != nil {
			return nil, 0, fmt.Errorf("parse session_history created_at: %w", err)
		}
		out = append(out, &h)
	}
	return out, total, rows.Err()
}

// ---------- Watchdog events ----------

type WatchdogRepo struct{ db *sql.DB }

func (r *WatchdogRepo) Create(ctx context.Context, tx DBTX, e *models.WatchdogEvent) error {
	e.CreatedAt = nowTime()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO watchdog_events (team_id, queue_task_id, session_id, role_id,
			event_type, severity, description, action_taken, is_read, requires_action, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TeamID, e.QueueTaskID, e.SessionID, e.RoleID,
		e.EventType, e.Severity, e.Description, e.ActionTaken,
		e.IsRead, e.RequiresAction, e.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	e.ID = id
	return nil
}

type AlertFilter struct {
	TeamID *int64
	Limit  int
}

func (r *WatchdogRepo) List(ctx context.Context, tx DBTX, f AlertFilter) ([]*models.WatchdogEvent, int, error) {
	conds := []string{}
	args := []any{}
	if f.TeamID != nil {
		conds = append(conds, "team_id = ?")
		args = append(args, *f.TeamID)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAll(conds)
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM watchdog_events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, team_id, queue_task_id, session_id, role_id, event_type, severity,
			description, action_taken, is_read, requires_action, created_at
		FROM watchdog_events`+where+`
		ORDER BY created_at DESC, id DESC LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*models.WatchdogEvent, 0, limit)
	for rows.Next() {
		var (
			e    models.WatchdogEvent
			task sql.NullInt64
			sess sql.NullInt64
			role sql.NullInt64
			act  sql.NullString
			read int
			reqA int
			ca   string
		)
		if err := rows.Scan(&e.ID, &e.TeamID, &task, &sess, &role, &e.EventType, &e.Severity,
			&e.Description, &act, &read, &reqA, &ca); err != nil {
			return nil, 0, err
		}
		if task.Valid {
			e.QueueTaskID = &task.Int64
		}
		if sess.Valid {
			e.SessionID = &sess.Int64
		}
		if role.Valid {
			e.RoleID = &role.Int64
		}
		if act.Valid {
			v := act.String
			e.ActionTaken = &v
		}
		e.IsRead = read != 0
		e.RequiresAction = reqA != 0
		if e.CreatedAt, err = parseTime(ca); err != nil {
			return nil, 0, fmt.Errorf("parse watchdog created_at: %w", err)
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}

// ExistsUnread — есть ли непрочитанный алерт того же типа для сущности (dedup).
func (r *WatchdogRepo) ExistsUnread(ctx context.Context, tx DBTX, eventType string, queueTaskID, sessionID *int64) (bool, error) {
	q := `SELECT COUNT(*) FROM watchdog_events WHERE event_type = ? AND is_read = 0 AND (queue_task_id = ? OR session_id = ?)`
	var n int
	err := tx.QueryRowContext(ctx, q, eventType, queueTaskID, sessionID).Scan(&n)
	return n > 0, err
}

func (r *WatchdogRepo) CountBySeverity(ctx context.Context, tx DBTX, severity string) (int, error) {
	return countQuery(ctx, tx, `SELECT COUNT(*) FROM watchdog_events WHERE severity = ?`, severity)
}

func (r *WatchdogRepo) CountAll(ctx context.Context, tx DBTX) (int, error) {
	return countQuery(ctx, tx, `SELECT COUNT(*) FROM watchdog_events`)
}

func (r *WatchdogRepo) MarkRead(ctx context.Context, tx DBTX, id int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE watchdog_events SET is_read = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func joinAll(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}
