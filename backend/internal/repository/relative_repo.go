package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"daemon/internal/models"
)

type RelativeRepo struct{ db *sql.DB }

func (r *RelativeRepo) Create(ctx context.Context, tx DBTX, rel *models.Relative) error {
	rel.CreatedAt = nowTime()
	if rel.Config == nil {
		rel.Config = map[string]any{}
	}
	cfg, err := marshalJSON(rel.Config)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO relatives (team_id, from_role_id, to_role_id, type, config, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		rel.TeamID, rel.FromRoleID, rel.ToRoleID, rel.Type, cfg, nowStr())
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	rel.ID = id
	return nil
}

func (r *RelativeRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Relative, error) {
	return scanRelative(tx.QueryRowContext(ctx, `
		SELECT id, team_id, from_role_id, to_role_id, type, config, created_at
		FROM relatives WHERE id = ?`, id))
}

func (r *RelativeRepo) ListByTeam(ctx context.Context, tx DBTX, teamID int64) ([]*models.Relative, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, team_id, from_role_id, to_role_id, type, config, created_at
		FROM relatives WHERE team_id = ?
		ORDER BY id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.Relative, 0)
	for rows.Next() {
		rel, err := scanRelative(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (r *RelativeRepo) Delete(ctx context.Context, tx DBTX, id int64) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM relatives WHERE id = ?`, id)
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

func (r *RelativeRepo) UpdateConfig(ctx context.Context, tx DBTX, id int64, cfg map[string]any) error {
	cfgJSON, err := marshalJSON(cfg)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE relatives SET config = ? WHERE id = ?`, cfgJSON, id)
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

func scanRelative(s teamScanner) (*models.Relative, error) {
	var (
		rel     models.Relative
		cfg, ca string
	)
	if err := s.Scan(&rel.ID, &rel.TeamID, &rel.FromRoleID, &rel.ToRoleID, &rel.Type, &cfg, &ca); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rel.Config = unmarshalConfig(cfg)
	var err error
	if rel.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse relative created_at: %w", err)
	}
	return &rel, nil
}
