package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"daemon/internal/models"
)

type RoleRepo struct{ db *sql.DB }

func (r *RoleRepo) Create(ctx context.Context, tx DBTX, role *models.Role) error {
	ts, tsStr := nowTime(), nowStr()
	role.CreatedAt, role.UpdatedAt = ts, ts
	if role.State == "" {
		role.State = models.RoleActive
	}
	if role.Config == nil {
		role.Config = map[string]any{}
	}
	cfg, err := marshalJSON(role.Config)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO roles (team_id, segment_id, name, address, agent_spec, profile, config, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		role.TeamID, role.SegmentID, role.Name, role.Address, role.AgentSpec, role.Profile,
		cfg, role.State, tsStr, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	role.ID = id
	return nil
}

func (r *RoleRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Role, error) {
	return scanRole(tx.QueryRowContext(ctx, `
		SELECT id, team_id, segment_id, name, address, agent_spec, profile, config, state, created_at, updated_at
		FROM roles WHERE id = ?`, id))
}

func (r *RoleRepo) GetBySegmentAndName(ctx context.Context, tx DBTX, segmentID int64, name string) (*models.Role, error) {
	return scanRole(tx.QueryRowContext(ctx, `
		SELECT id, team_id, segment_id, name, address, agent_spec, profile, config, state, created_at, updated_at
		FROM roles WHERE segment_id = ? AND name = ?`, segmentID, name))
}

func (r *RoleRepo) ListByTeam(ctx context.Context, tx DBTX, teamID int64) ([]*models.Role, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, team_id, segment_id, name, address, agent_spec, profile, config, state, created_at, updated_at
		FROM roles WHERE team_id = ?
		ORDER BY id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.Role, 0)
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (r *RoleRepo) CountByTeam(ctx context.Context, tx DBTX, teamID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles WHERE team_id = ?`, teamID).Scan(&n)
	return n, err
}

// UpdateMutable — обновляет agent_spec/profile/config (остальные поля неизменяемы в slice 1).
func (r *RoleRepo) UpdateMutable(ctx context.Context, tx DBTX, role *models.Role) error {
	return r.updateMutableFields(ctx, tx, role, role.Config)
}

// UpdateMutableLayout — обновляет только config (layout drag&drop).
func (r *RoleRepo) UpdateMutableLayout(ctx context.Context, tx DBTX, role *models.Role, cfg map[string]any) error {
	return r.updateMutableFields(ctx, tx, role, cfg)
}

func (r *RoleRepo) updateMutableFields(ctx context.Context, tx DBTX, role *models.Role, cfg map[string]any) error {
	cfgJSON, err := marshalJSON(cfg)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE roles
		SET agent_spec = ?, profile = ?, config = ?, updated_at = ?
		WHERE id = ?`,
		role.AgentSpec, role.Profile, cfgJSON, nowStr(), role.ID)
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

func scanRole(s teamScanner) (*models.Role, error) {
	var (
		role        models.Role
		cfg, ca, ua string
	)
	err := s.Scan(&role.ID, &role.TeamID, &role.SegmentID, &role.Name, &role.Address,
		&role.AgentSpec, &role.Profile, &cfg, &role.State, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	role.Config = unmarshalConfig(cfg)
	if role.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse role created_at: %w", err)
	}
	if role.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse role updated_at: %w", err)
	}
	return &role, nil
}
