package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
)

// ---------- P1.4: Workflow engine — instances ----------

type WorkflowInstanceRepo struct{ db *sql.DB }

const wfInstanceSelect = `
	SELECT id, workflow_id, state, root_objective, current_block_id, created_at, updated_at, finished_at
	FROM workflow_instances`

func (r *WorkflowInstanceRepo) Create(ctx context.Context, tx DBTX, i *models.WorkflowInstance) error {
	if i.State == "" {
		i.State = models.WfInstanceRunning
	}
	ts, tsStr := nowTime(), nowStr()
	i.CreatedAt, i.UpdatedAt = ts, ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_instances (workflow_id, state, root_objective, current_block_id, created_at, updated_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.WorkflowID, string(i.State), i.RootObjective, nullableInt64(i.CurrentBlockID), tsStr, tsStr, nullableTime(i.FinishedAt))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	i.ID = id
	return nil
}

func (r *WorkflowInstanceRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.WorkflowInstance, error) {
	return scanWfInstance(tx.QueryRowContext(ctx, wfInstanceSelect+` WHERE id = ?`, id))
}

// ListByWorkflow — инстансы workflow (новые сверху).
func (r *WorkflowInstanceRepo) ListByWorkflow(ctx context.Context, tx DBTX, workflowID int64) ([]*models.WorkflowInstance, error) {
	rows, err := tx.QueryContext(ctx, wfInstanceSelect+` WHERE workflow_id = ? ORDER BY id DESC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*models.WorkflowInstance, 0)
	for rows.Next() {
		i, err := scanWfInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ListByStates — инстансы в заданных состояниях (для Attention workflow_attention).
func (r *WorkflowInstanceRepo) ListByStates(ctx context.Context, tx DBTX, states ...models.WorkflowInstanceState) ([]*models.WorkflowInstance, error) {
	if len(states) == 0 {
		return []*models.WorkflowInstance{}, nil
	}
	conds := make([]string, 0, len(states))
	args := make([]any, 0, len(states))
	for _, s := range states {
		conds = append(conds, "state = ?")
		args = append(args, string(s))
	}
	rows, err := tx.QueryContext(ctx, wfInstanceSelect+` WHERE `+joinAnd(conds)+` ORDER BY updated_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*models.WorkflowInstance, 0)
	for rows.Next() {
		i, err := scanWfInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// Update — меняет state / current_block_id / finished_at (+ updated_at).
func (r *WorkflowInstanceRepo) Update(ctx context.Context, tx DBTX, i *models.WorkflowInstance) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE workflow_instances SET state = ?, current_block_id = ?, finished_at = ?, updated_at = ?
		WHERE id = ?`,
		string(i.State), nullableInt64(i.CurrentBlockID), nullableTime(i.FinishedAt), nowStr(), i.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	i.UpdatedAt = nowTime()
	return nil
}

func scanWfInstance(s teamScanner) (*models.WorkflowInstance, error) {
	var (
		i      models.WorkflowInstance
		cur    sql.NullInt64
		ca, ua string
		fin    sql.NullString
	)
	err := s.Scan(&i.ID, &i.WorkflowID, &i.State, &i.RootObjective, &cur, &ca, &ua, &fin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if cur.Valid {
		i.CurrentBlockID = &cur.Int64
	}
	if fin.Valid && fin.String != "" {
		t, err := parseTime(fin.String)
		if err != nil {
			return nil, fmt.Errorf("parse instance finished_at: %w", err)
		}
		i.FinishedAt = &t
	}
	if i.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse instance created_at: %w", err)
	}
	if i.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse instance updated_at: %w", err)
	}
	return &i, nil
}

// ---------- P1.4: Workflow engine — steps (append-only trail) ----------

type WorkflowStepRepo struct{ db *sql.DB }

const wfStepSelect = `
	SELECT id, instance_id, block_id, task_id, state, exit_kind, actor, result_note, entered_at, exited_at
	FROM workflow_steps`

func (r *WorkflowStepRepo) Create(ctx context.Context, tx DBTX, st *models.WorkflowStep) error {
	if st.State == "" {
		st.State = models.WfStepPending
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_steps (instance_id, block_id, task_id, state, exit_kind, actor, result_note, entered_at, exited_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		st.InstanceID, st.BlockID, nullableInt64(st.TaskID), string(st.State), nullableStrPtr(st.ExitKind),
		nullableInt64(st.Actor), nullableStr(st.ResultNote), nullableTime(st.EnteredAt), nullableTime(st.ExitedAt))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	st.ID = id
	return nil
}

func (r *WorkflowStepRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.WorkflowStep, error) {
	return scanWfStep(tx.QueryRowContext(ctx, wfStepSelect+` WHERE id = ?`, id))
}

// GetActiveByTask — активный шаг для задачи (для projection: task_id → step).
// Nil + nil, если активного шага нет (не workflow-задача / уже закрыт).
func (r *WorkflowStepRepo) GetActiveByTask(ctx context.Context, tx DBTX, taskID int64) (*models.WorkflowStep, error) {
	return scanWfStep(tx.QueryRowContext(ctx,
		wfStepSelect+` WHERE task_id = ? AND state = 'active' ORDER BY id DESC LIMIT 1`, taskID))
}

// ListByInstance — trail шагов инстанса (ASC, append-only).
func (r *WorkflowStepRepo) ListByInstance(ctx context.Context, tx DBTX, instanceID int64) ([]*models.WorkflowStep, error) {
	rows, err := tx.QueryContext(ctx, wfStepSelect+` WHERE instance_id = ? ORDER BY id ASC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWfStepRows(rows)
}

// ListActiveByInstance — активные шаги инстанса (для abort/route: текущий шаг).
func (r *WorkflowStepRepo) ListActiveByInstance(ctx context.Context, tx DBTX, instanceID int64) ([]*models.WorkflowStep, error) {
	rows, err := tx.QueryContext(ctx, wfStepSelect+` WHERE instance_id = ? AND state = 'active' ORDER BY id ASC`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWfStepRows(rows)
}

// LastFailedByInstance — последний failed-шаг (для resume). Nil, если нет.
func (r *WorkflowStepRepo) LastFailedByInstance(ctx context.Context, tx DBTX, instanceID int64) (*models.WorkflowStep, error) {
	return scanWfStep(tx.QueryRowContext(ctx,
		wfStepSelect+` WHERE instance_id = ? AND state = 'failed' ORDER BY id DESC LIMIT 1`, instanceID))
}

// Update — меняет state/exit_kind/actor/result_note/exited_at активного шага.
func (r *WorkflowStepRepo) Update(ctx context.Context, tx DBTX, st *models.WorkflowStep) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE workflow_steps SET state = ?, exit_kind = ?, actor = ?, result_note = ?, exited_at = ?
		WHERE id = ?`,
		string(st.State), nullableStrPtr(st.ExitKind), nullableInt64(st.Actor), nullableStr(st.ResultNote),
		nullableTime(st.ExitedAt), st.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanWfStepRows(rows *sql.Rows) ([]*models.WorkflowStep, error) {
	out := make([]*models.WorkflowStep, 0)
	for rows.Next() {
		st, err := scanWfStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func scanWfStep(s teamScanner) (*models.WorkflowStep, error) {
	var (
		st models.WorkflowStep
		ta sql.NullInt64
		ak sql.NullString
		ac sql.NullInt64
		nt sql.NullString
		en sql.NullString
		ex sql.NullString
	)
	err := s.Scan(&st.ID, &st.InstanceID, &st.BlockID, &ta, &st.State, &ak, &ac, &nt, &en, &ex)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ta.Valid {
		st.TaskID = &ta.Int64
	}
	if ak.Valid {
		k := models.WorkflowStepExitKind(ak.String)
		st.ExitKind = &k
	}
	if ac.Valid {
		st.Actor = &ac.Int64
	}
	if nt.Valid {
		st.ResultNote = &nt.String
	}
	st.EnteredAt = parseNullableTime(en)
	st.ExitedAt = parseNullableTime(ex)
	return &st, nil
}

// ---------- helpers ----------

// nullableTime — *time.Time → any (nil = NULL).
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}

// nullableStr — *string → any (nil = NULL).
func nullableStr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullableStrPtr — *WorkflowStepExitKind → any (nil = NULL).
func nullableStrPtr(k *models.WorkflowStepExitKind) any {
	if k == nil {
		return nil
	}
	return string(*k)
}

// parseNullableTime — sql.NullString → *time.Time (empty/invalid = nil).
func parseNullableTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil
	}
	return &t
}
