package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"daemon/internal/models"
)

func errorsIs(err, target error) bool { return errors.Is(err, target) }

type SegmentRepo struct{ db *sql.DB }

func (r *SegmentRepo) Create(ctx context.Context, tx DBTX, s *models.Segment) error {
	ts, tsStr := nowTime(), nowStr()
	s.CreatedAt, s.UpdatedAt = ts, ts
	if s.Config == nil {
		s.Config = map[string]any{}
	}
	cfg, err := marshalJSON(s.Config)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO segments (team_id, name, description, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.TeamID, s.Name, s.Description, cfg, tsStr, tsStr)
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

func (r *SegmentRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Segment, error) {
	return scanSegment(tx.QueryRowContext(ctx, `
		SELECT id, team_id, name, description, config, created_at, updated_at
		FROM segments WHERE id = ?`, id))
}

func (r *SegmentRepo) GetByTeamAndName(ctx context.Context, tx DBTX, teamID int64, name string) (*models.Segment, error) {
	return scanSegment(tx.QueryRowContext(ctx, `
		SELECT id, team_id, name, description, config, created_at, updated_at
		FROM segments WHERE team_id = ? AND name = ?`, teamID, name))
}

func (r *SegmentRepo) ListByTeam(ctx context.Context, tx DBTX, teamID int64) ([]*models.Segment, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, team_id, name, description, config, created_at, updated_at
		FROM segments WHERE team_id = ?
		ORDER BY id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.Segment, 0)
	for rows.Next() {
		s, err := scanSegment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SegmentRepo) CountRoles(ctx context.Context, tx DBTX, segmentID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles WHERE segment_id = ?`, segmentID).Scan(&n)
	return n, err
}

func (r *SegmentRepo) UpdateConfig(ctx context.Context, tx DBTX, id int64, cfg map[string]any) error {
	cfgJSON, err := marshalJSON(cfg)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE segments SET config = ?, updated_at = ? WHERE id = ?`, cfgJSON, timeNow(), id)
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

func scanSegment(s teamScanner) (*models.Segment, error) {
	var seg models.Segment
	var cfg, ca, ua string
	if err := s.Scan(&seg.ID, &seg.TeamID, &seg.Name, &seg.Description, &cfg, &ca, &ua); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	seg.Config = unmarshalConfig(cfg)
	var err error
	if seg.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse segment created_at: %w", err)
	}
	if seg.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse segment updated_at: %w", err)
	}
	return &seg, nil
}
