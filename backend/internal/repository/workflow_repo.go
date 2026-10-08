package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"daemon/internal/models"
)

// ---------- Workflows ----------

type WorkflowRepo struct{ db *sql.DB }

const workflowSelect = `
	SELECT id, team_id, name, description, state, config, created_at, updated_at
	FROM workflows`

func (r *WorkflowRepo) Create(ctx context.Context, tx DBTX, w *models.Workflow) error {
	if w.Config == nil {
		w.Config = map[string]any{}
	}
	if w.State == "" {
		w.State = models.WorkflowDraft
	}
	cfg, err := marshalJSON(w.Config)
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	w.CreatedAt, w.UpdatedAt = ts, ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO workflows (team_id, name, description, state, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		w.TeamID, w.Name, w.Description, string(w.State), cfg, tsStr, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	w.ID = id
	return nil
}

func (r *WorkflowRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Workflow, error) {
	return scanWorkflow(tx.QueryRowContext(ctx, workflowSelect+` WHERE id = ?`, id))
}

// WorkflowFilter — фильтры GET /api/v1/workflows (контракт 20 §2.0).
type WorkflowFilter struct {
	TeamID *int64
	State  *models.WorkflowState
}

// List — workflows + total.
func (r *WorkflowRepo) List(ctx context.Context, tx DBTX, f WorkflowFilter) ([]*models.Workflow, int, error) {
	conds := []string{}
	args := []any{}
	if f.TeamID != nil {
		conds = append(conds, "team_id = ?")
		args = append(args, *f.TeamID)
	}
	if f.State != nil {
		conds = append(conds, "state = ?")
		args = append(args, string(*f.State))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAnd(conds)
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflows`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, workflowSelect+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*models.Workflow, 0, total)
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, w)
	}
	return out, total, rows.Err()
}

func (r *WorkflowRepo) UpdateState(ctx context.Context, tx DBTX, id int64, state models.WorkflowState) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE workflows SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), nowStr(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanWorkflow(s teamScanner) (*models.Workflow, error) {
	var (
		w      models.Workflow
		desc   sql.NullString
		cfg    string
		ca, ua string
	)
	err := s.Scan(&w.ID, &w.TeamID, &w.Name, &desc, &w.State, &cfg, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if desc.Valid {
		w.Description = &desc.String
	}
	w.Config = unmarshalConfig(cfg)
	if w.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse workflow created_at: %w", err)
	}
	if w.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse workflow updated_at: %w", err)
	}
	return &w, nil
}

// ---------- Workflow blocks ----------

type WorkflowBlockRepo struct{ db *sql.DB }

const workflowBlockSelect = `
	SELECT id, workflow_id, type, position_x, position_y, label, config, created_at, updated_at
	FROM workflow_blocks`

func (r *WorkflowBlockRepo) Create(ctx context.Context, tx DBTX, b *models.WorkflowBlock) error {
	if b.Config == nil {
		b.Config = map[string]any{}
	}
	cfg, err := marshalJSON(b.Config)
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	b.CreatedAt, b.UpdatedAt = ts, ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_blocks (workflow_id, type, position_x, position_y, label, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.WorkflowID, string(b.Type), b.PositionX, b.PositionY, b.Label, cfg, tsStr, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	b.ID = id
	return nil
}

func (r *WorkflowBlockRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.WorkflowBlock, error) {
	return scanWorkflowBlock(tx.QueryRowContext(ctx, workflowBlockSelect+` WHERE id = ?`, id))
}

func (r *WorkflowBlockRepo) ListByWorkflow(ctx context.Context, tx DBTX, workflowID int64) ([]*models.WorkflowBlock, error) {
	rows, err := tx.QueryContext(ctx, workflowBlockSelect+` WHERE workflow_id = ? ORDER BY id`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.WorkflowBlock, 0)
	for rows.Next() {
		b, err := scanWorkflowBlock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Update — drag&drop / config (контракт 20 §2.5). Возвращает старое и новое значение
// для response.changes.
func (r *WorkflowBlockRepo) Update(ctx context.Context, tx DBTX, b *models.WorkflowBlock) (*models.WorkflowBlock, error) {
	old, err := r.GetByID(ctx, tx, b.ID)
	if err != nil {
		return nil, err
	}
	if b.Config == nil {
		b.Config = old.Config
	}
	if b.Label == nil {
		b.Label = old.Label
	}
	cfg, err := marshalJSON(b.Config)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE workflow_blocks SET position_x = ?, position_y = ?, label = ?, config = ?, updated_at = ?
		WHERE id = ?`,
		b.PositionX, b.PositionY, b.Label, cfg, nowStr(), b.ID)
	if err != nil {
		return nil, err
	}
	b.UpdatedAt = nowTime()
	return old, err
}

func scanWorkflowBlock(s teamScanner) (*models.WorkflowBlock, error) {
	var (
		b   models.WorkflowBlock
		lbl sql.NullString
		cfg string
		ca  string
		ua  string
	)
	err := s.Scan(&b.ID, &b.WorkflowID, &b.Type, &b.PositionX, &b.PositionY, &lbl, &cfg, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if lbl.Valid {
		b.Label = &lbl.String
	}
	b.Config = unmarshalConfig(cfg)
	if b.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse block created_at: %w", err)
	}
	if b.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse block updated_at: %w", err)
	}
	return &b, nil
}

// ---------- Workflow connections ----------

type WorkflowConnectionRepo struct{ db *sql.DB }

func (r *WorkflowConnectionRepo) Create(ctx context.Context, tx DBTX, c *models.WorkflowConnection) error {
	ts, tsStr := nowTime(), nowStr()
	c.CreatedAt = ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_connections (workflow_id, from_block_id, to_block_id, condition, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		c.WorkflowID, c.FromBlockID, c.ToBlockID, c.Condition, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = id
	return nil
}

func (r *WorkflowConnectionRepo) ListByWorkflow(ctx context.Context, tx DBTX, workflowID int64) ([]*models.WorkflowConnection, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, workflow_id, from_block_id, to_block_id, condition, created_at
		FROM workflow_connections WHERE workflow_id = ? ORDER BY id`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.WorkflowConnection, 0)
	for rows.Next() {
		var c models.WorkflowConnection
		var cond sql.NullString
		var ca string
		if err := rows.Scan(&c.ID, &c.WorkflowID, &c.FromBlockID, &c.ToBlockID, &cond, &ca); err != nil {
			return nil, err
		}
		if cond.Valid {
			c.Condition = &cond.String
		}
		if c.CreatedAt, err = parseTime(ca); err != nil {
			return nil, fmt.Errorf("parse connection created_at: %w", err)
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}
