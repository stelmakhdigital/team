package repository

import (
	"context"
	"database/sql"
	"fmt"

	"daemon/internal/models"
)

type TeamRepo struct{ db *sql.DB }

func (r *TeamRepo) Create(ctx context.Context, tx DBTX, t *models.Team) error {
	ts, tsStr := nowTime(), nowStr()
	t.CreatedAt, t.UpdatedAt = ts, ts
	if t.State == "" {
		t.State = models.TeamActive
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO teams (name, spec_path, description, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		t.Name, t.SpecPath, t.Description, t.State, tsStr, tsStr)
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

func (r *TeamRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Team, error) {
	return scanTeam(tx.QueryRowContext(ctx, `
		SELECT id, name, spec_path, description, state, created_at, updated_at
		FROM teams WHERE id = ?`, id))
}

func (r *TeamRepo) GetByName(ctx context.Context, tx DBTX, name string) (*models.Team, error) {
	return scanTeam(tx.QueryRowContext(ctx, `
		SELECT id, name, spec_path, description, state, created_at, updated_at
		FROM teams WHERE name = ?`, name))
}

func (r *TeamRepo) List(ctx context.Context, tx DBTX, limit, offset int) ([]*models.Team, int, error) {
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM teams`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, spec_path, description, state, created_at, updated_at
		FROM teams
		ORDER BY id
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	teams := make([]*models.Team, 0, limit)
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, 0, err
		}
		teams = append(teams, t)
	}
	return teams, total, rows.Err()
}

func (r *TeamRepo) UpdateState(ctx context.Context, tx DBTX, id int64, state models.TeamState) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE teams SET state = ?, updated_at = ? WHERE id = ?`, state, timeNow(), id)
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

func (r *TeamRepo) UpdateMeta(ctx context.Context, tx DBTX, id int64, name, description string) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE teams SET name = ?, description = ?, updated_at = ? WHERE id = ?`, name, description, timeNow(), id)
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

type teamScanner interface{ Scan(dest ...any) error }

func scanTeam(s teamScanner) (*models.Team, error) {
	var (
		t      models.Team
		ca, ua string
	)
	if err := s.Scan(&t.ID, &t.Name, &t.SpecPath, &t.Description, &t.State, &ca, &ua); err != nil {
		if errorsIs(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var err error
	if t.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse team created_at: %w", err)
	}
	if t.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse team updated_at: %w", err)
	}
	return &t, nil
}
