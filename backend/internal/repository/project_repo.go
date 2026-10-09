package repository

import (
	"context"
	"database/sql"
	"fmt"

	"daemon/internal/models"
)

// ProjectRepo — репозиторий проектов (P1.0).
type ProjectRepo struct{ db *sql.DB }

// ProjectFilter — фильтры списка проектов.
type ProjectFilter struct {
	ArchivedOnly bool   // только архивные (по умолчанию — нет)
	IncludeArch  bool   // включать архивные в список (по умолчанию — только активные)
	Name         string // подстрока (ILIKE/LIKE)
}

const projectCols = `id, name, path, kind, git, description, archived, created_at, updated_at`

func (r *ProjectRepo) Create(ctx context.Context, tx DBTX, p *models.Project) error {
	ts, tsStr := nowTime(), nowStr()
	p.CreatedAt, p.UpdatedAt = ts, ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO projects (name, path, kind, git, description, archived, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Path, string(p.Kind), p.Git, p.Description, p.Archived, tsStr, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	p.ID = id
	return nil
}

func (r *ProjectRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Project, error) {
	return scanProject(tx.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

func (r *ProjectRepo) GetByPath(ctx context.Context, tx DBTX, path string) (*models.Project, error) {
	return scanProject(tx.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE path = ?`, path))
}

func (r *ProjectRepo) GetByName(ctx context.Context, tx DBTX, name string) (*models.Project, error) {
	return scanProject(tx.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE name = ?`, name))
}

func (r *ProjectRepo) List(ctx context.Context, tx DBTX, f ProjectFilter, limit, offset int) ([]*models.Project, int, error) {
	where := `1=1`
	var args []any
	if f.ArchivedOnly {
		where = `archived = 1`
	} else if !f.IncludeArch {
		where = `archived = 0`
	}
	if f.Name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+f.Name+"%")
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qargs := append([]any{}, args...)
	qargs = append(qargs, limit, offset)
	rows, err := tx.QueryContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE `+where+` ORDER BY id LIMIT ? OFFSET ?`, qargs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*models.Project, 0, limit)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// Archive — DELETE /projects/{id} = архив (идемпотентно).
func (r *ProjectRepo) Archive(ctx context.Context, tx DBTX, id int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE projects SET archived = 1, updated_at = ? WHERE id = ?`, timeNow(), id)
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

// UpdateMeta — name/description (path менять нельзя → 409 на уровне сервиса).
func (r *ProjectRepo) UpdateMeta(ctx context.Context, tx DBTX, id int64, name, description string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		name, description, timeNow(), id)
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

// Счётчики для сводки GET /projects/{id}.
func (r *ProjectRepo) CountTeams(ctx context.Context, tx DBTX, projectID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM teams WHERE project_id = ?`, projectID).Scan(&n)
	return n, err
}

func (r *ProjectRepo) CountTasks(ctx context.Context, tx DBTX, projectID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM queue_tasks WHERE project_id = ?`, projectID).Scan(&n)
	return n, err
}

// CountActiveSessions — активные (не терминальные) сессии проекта.
func (r *ProjectRepo) CountActiveSessions(ctx context.Context, tx DBTX, projectID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE project_id = ? AND state IN ('starting','running','idle','stopping')`,
		projectID).Scan(&n)
	return n, err
}

type projectScanner interface{ Scan(dest ...any) error }

func scanProject(s projectScanner) (*models.Project, error) {
	var (
		p         models.Project
		kind      string
		git, arch int
		ca, ua    string
	)
	if err := s.Scan(&p.ID, &p.Name, &p.Path, &kind, &git, &p.Description, &arch, &ca, &ua); err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Kind = models.ProjectKind(kind)
	p.Git = git != 0
	p.Archived = arch != 0
	var err error
	if p.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse project created_at: %w", err)
	}
	if p.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse project updated_at: %w", err)
	}
	return &p, nil
}
