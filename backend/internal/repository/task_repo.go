package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"daemon/internal/models"
)

type TaskRepo struct{ db *sql.DB }

type TaskFilter struct {
	TeamID          *int64
	State           *models.TaskState
	DestinationRole *int64
	ParentTaskID    *int64
	ActiveOnly      bool // pending|in_progress|blocked
	States          []models.TaskState
	Limit           int
	Offset          int
}

const taskColumns = `id, team_id, parent_task_id, destination_role_id, source_role_id,
	title, body, body_context, state, closure_reason, closure_target_id,
	blocked_since, park_timeout_secs, priority, estimated_secs, actual_secs,
	created_at, updated_at, started_at, completed_at`

func (r *TaskRepo) Create(ctx context.Context, tx DBTX, t *models.Task) error {
	ts, tsStr := nowTime(), nowStr()
	t.CreatedAt, t.UpdatedAt = ts, ts
	if t.State == "" {
		t.State = models.TaskPending
	}
	if t.BodyContext == nil {
		t.BodyContext = map[string]any{}
	}
	bc, err := marshalJSON(t.BodyContext)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO queue_tasks (team_id, parent_task_id, destination_role_id, source_role_id,
			title, body, body_context, state, closure_reason, closure_target_id,
			blocked_since, park_timeout_secs, priority, estimated_secs, actual_secs,
			created_at, updated_at, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TeamID, t.ParentTaskID, t.DestinationRoleID, t.SourceRoleID,
		t.Title, t.Body, bc, t.State, t.ClosureReason, t.ClosureTargetID,
		t.BlockedSince, t.ParkTimeoutSecs, t.Priority, t.EstimatedSecs, t.ActualSecs,
		tsStr, tsStr, t.StartedAt, t.CompletedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	t.ID = id
	return nil
}

func (r *TaskRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Task, error) {
	return scanTask(tx.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM queue_tasks WHERE id = ?`, id))
}

func (r *TaskRepo) List(ctx context.Context, tx DBTX, f TaskFilter) ([]*models.Task, int, error) {
	conds := []string{}
	args := []any{}
	addCond := func(cond string, a ...any) {
		conds = append(conds, cond)
		args = append(args, a...)
	}
	if f.TeamID != nil {
		addCond("team_id = ?", *f.TeamID)
	}
	if f.DestinationRole != nil {
		addCond("destination_role_id = ?", *f.DestinationRole)
	}
	if f.ParentTaskID != nil {
		addCond("parent_task_id = ?", *f.ParentTaskID)
	}
	if f.State != nil {
		addCond("state = ?", string(*f.State))
	} else if f.ActiveOnly {
		addCond("state IN ('pending', 'in_progress', 'blocked')")
	} else if len(f.States) > 0 {
		ph := make([]string, len(f.States))
		for i, s := range f.States {
			ph[i] = "?"
			args = append(args, string(s))
		}
		addCond(fmt.Sprintf("state IN (%s)", joinPh(ph)))
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM queue_tasks`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + taskColumns + ` FROM queue_tasks` + where +
		` ORDER BY priority DESC, updated_at DESC, id LIMIT ? OFFSET ?`
	rows, err := tx.QueryContext(ctx, query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*models.Task, 0, limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// UpdateState — обновляет state + служебные поля (closure, blocked_since, started/completed).
func (r *TaskRepo) UpdateState(ctx context.Context, tx DBTX, id int64, t *models.Task) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE queue_tasks
		SET state = ?, closure_reason = ?, closure_target_id = ?, blocked_since = ?,
			updated_at = ?, started_at = COALESCE(started_at, ?), completed_at = ?
		WHERE id = ?`,
		t.State, t.ClosureReason, t.ClosureTargetID, t.BlockedSince,
		nowStr(), t.StartedAt, t.CompletedAt, id)
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

func (r *TaskRepo) CountOpenByParent(ctx context.Context, tx DBTX, parentID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queue_tasks
		 WHERE parent_task_id = ? AND state IN ('pending', 'in_progress', 'blocked')`, parentID).Scan(&n)
	return n, err
}

func (r *TaskRepo) CountByStates(ctx context.Context, tx DBTX, teamID *int64, states ...models.TaskState) (int, error) {
	if len(states) == 0 {
		return 0, nil
	}
	ph := make([]string, len(states))
	args := make([]any, 0, len(states)+1)
	for i, s := range states {
		ph[i] = "?"
		args = append(args, string(s))
	}
	if teamID != nil {
		args = append(args, *teamID)
		return countQuery(ctx, tx, `SELECT COUNT(*) FROM queue_tasks WHERE state IN (`+joinPh(ph)+`) AND team_id = ?`, args...)
	}
	return countQuery(ctx, tx, `SELECT COUNT(*) FROM queue_tasks WHERE state IN (`+joinPh(ph)+`)`, args...)
}

func (r *TaskRepo) CountDoneSince(ctx context.Context, tx DBTX, since time.Time) (int, error) {
	return countQuery(ctx, tx,
		`SELECT COUNT(*) FROM queue_tasks WHERE state = 'done' AND completed_at >= ?`,
		since.Format(time.RFC3339Nano))
}

type HistoryRepo struct{ db *sql.DB }

func (r *HistoryRepo) Create(ctx context.Context, tx DBTX, h *models.HistoryEntry) error {
	h.CreatedAt = nowTime()
	if h.Metadata == nil {
		h.Metadata = map[string]any{}
	}
	md, err := marshalJSON(h.Metadata)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO history_status (queue_task_id, from_state, to_state, closure_reason,
			closure_target_id, actor_role_id, actor_type, comment, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.QueueTaskID, h.FromState, h.ToState, h.ClosureReason, h.ClosureTargetID,
		h.ActorRoleID, h.ActorType, h.Comment, md, h.CreatedAt.Format(time.RFC3339Nano))
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

func (r *HistoryRepo) ListByTask(ctx context.Context, tx DBTX, taskID int64, limit, offset int) ([]*models.HistoryEntry, int, error) {
	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM history_status WHERE queue_task_id = ?`, taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, queue_task_id, from_state, to_state, closure_reason, closure_target_id,
			actor_role_id, actor_type, comment, metadata, created_at
		FROM history_status
		WHERE queue_task_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*models.HistoryEntry, 0, limit)
	for rows.Next() {
		var (
			h         models.HistoryEntry
			md        string
			fromState sql.NullString
			closure   sql.NullString
			targetID  sql.NullInt64
			actorRole sql.NullInt64
			comment   sql.NullString
			createdAt string
		)
		if err := rows.Scan(&h.ID, &h.QueueTaskID, &fromState, &h.ToState, &closure,
			&targetID, &actorRole, &h.ActorType, &comment, &md, &createdAt); err != nil {
			return nil, 0, err
		}
		if fromState.Valid {
			v := fromState.String
			h.FromState = &v
		}
		if closure.Valid {
			v := closure.String
			h.ClosureReason = &v
		}
		if targetID.Valid {
			h.ClosureTargetID = &targetID.Int64
		}
		if actorRole.Valid {
			h.ActorRoleID = &actorRole.Int64
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
		if h.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, 0, fmt.Errorf("parse history created_at: %w", err)
		}
		out = append(out, &h)
	}
	return out, total, rows.Err()
}

type taskScanner interface{ Scan(dest ...any) error }

func scanTask(s taskScanner) (*models.Task, error) {
	var (
		t          models.Task
		parentID   sql.NullInt64
		sourceRole sql.NullInt64
		closure    sql.NullString
		targetID   sql.NullInt64
		bodyCtx    string
		blockedAt  sql.NullString
		parkTO     sql.NullInt64
		estSecs    sql.NullInt64
		actSecs    sql.NullInt64
		startedAt  sql.NullString
		completed  sql.NullString
		ca, ua     string
	)
	err := s.Scan(&t.ID, &t.TeamID, &parentID, &t.DestinationRoleID, &sourceRole,
		&t.Title, &t.Body, &bodyCtx, &t.State, &closure, &targetID,
		&blockedAt, &parkTO, &t.Priority, &estSecs, &actSecs, &ca, &ua, &startedAt, &completed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if parentID.Valid {
		t.ParentTaskID = &parentID.Int64
	}
	if sourceRole.Valid {
		t.SourceRoleID = &sourceRole.Int64
	}
	if closure.Valid {
		c := models.ClosureReason(closure.String)
		t.ClosureReason = &c
	}
	if targetID.Valid {
		t.ClosureTargetID = &targetID.Int64
	}
	if blockedAt.Valid {
		if tm, err := parseTime(blockedAt.String); err == nil {
			t.BlockedSince = &tm
		}
	}
	if parkTO.Valid {
		v := int(parkTO.Int64)
		t.ParkTimeoutSecs = &v
	}
	if estSecs.Valid {
		v := int(estSecs.Int64)
		t.EstimatedSecs = &v
	}
	if actSecs.Valid {
		v := int(actSecs.Int64)
		t.ActualSecs = &v
	}
	if startedAt.Valid {
		if tm, err := parseTime(startedAt.String); err == nil {
			t.StartedAt = &tm
		}
	}
	if completed.Valid {
		if tm, err := parseTime(completed.String); err == nil {
			t.CompletedAt = &tm
		}
	}
	var bc map[string]any
	if err := json.Unmarshal([]byte(bodyCtx), &bc); err == nil && bc != nil {
		t.BodyContext = bc
	} else {
		t.BodyContext = map[string]any{}
	}
	if t.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse task created_at: %w", err)
	}
	if t.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse task updated_at: %w", err)
	}
	return &t, nil
}

func joinPh(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func countQuery(ctx context.Context, tx DBTX, query string, args ...any) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}
