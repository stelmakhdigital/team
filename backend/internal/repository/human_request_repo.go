package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
)

// HumanRequestRepo — P3.16: паркованные чувствительные операции.
type HumanRequestRepo struct {
	db *sql.DB
}

const humanRequestSelect = `SELECT id, scope_type, scope_id, team_id, method, path, body, auth_key,
	operation, reversible, status, decision_reason, decided_by, decided_at, replay_status, created_at, updated_at
	FROM human_requests`

// Create — вставить human_request (id присваивается DB).
func (r *HumanRequestRepo) Create(ctx context.Context, tx DBTX, hr *models.HumanRequest) error {
	now := nowTime()
	res, err := tx.ExecContext(ctx, `INSERT INTO human_requests
		(scope_type, scope_id, team_id, method, path, body, auth_key, operation, reversible, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		hr.ScopeType, nullableInt64(hr.ScopeID), nullableInt64(hr.TeamID), hr.Method, hr.Path,
		hr.Body, hr.AuthKey, hr.Operation, boolToInt(hr.Reversible),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert human_request: %w", err)
	}
	hr.ID, err = res.LastInsertId()
	if err != nil {
		return fmt.Errorf("human_request id: %w", err)
	}
	return nil
}

func scanHumanRequest(s teamScanner) (*models.HumanRequest, error) {
	var (
		hr         models.HumanRequest
		scopeID    sql.NullInt64
		teamID     sql.NullInt64
		body       sql.NullString
		authKey    sql.NullString
		reversible int
		status     string
		decReason  sql.NullString
		decidedBy  sql.NullString
		decidedAt  sql.NullString
		replaySt   sql.NullInt64
		ca, ua     string
	)
	err := s.Scan(&hr.ID, &hr.ScopeType, &scopeID, &teamID, &hr.Method, &hr.Path, &body, &authKey,
		&hr.Operation, &reversible, &status, &decReason, &decidedBy, &decidedAt, &replaySt, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if scopeID.Valid {
		v := scopeID.Int64
		hr.ScopeID = &v
	}
	if teamID.Valid {
		v := teamID.Int64
		hr.TeamID = &v
	}
	if body.Valid {
		v := body.String
		hr.Body = &v
	}
	if authKey.Valid {
		v := authKey.String
		hr.AuthKey = &v
	}
	hr.Reversible = reversible != 0
	hr.Status = models.HumanRequestStatus(status)
	if decReason.Valid {
		v := decReason.String
		hr.DecisionReason = &v
	}
	if decidedBy.Valid {
		v := decidedBy.String
		hr.DecidedBy = &v
	}
	if decidedAt.Valid {
		if t, perr := parseTime(decidedAt.String); perr == nil {
			hr.DecidedAt = &t
		}
	}
	if replaySt.Valid {
		v := int(replaySt.Int64)
		hr.ReplayStatus = &v
	}
	var err2 error
	if hr.CreatedAt, err2 = parseTime(ca); err2 != nil {
		return nil, fmt.Errorf("parse human_request created_at: %w", err2)
	}
	if hr.UpdatedAt, err2 = parseTime(ua); err2 != nil {
		return nil, fmt.Errorf("parse human_request updated_at: %w", err2)
	}
	return &hr, nil
}

func (r *HumanRequestRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.HumanRequest, error) {
	return scanHumanRequest(tx.QueryRowContext(ctx, humanRequestSelect+` WHERE id = ?`, id))
}

// ListPending — pending-запросы (для Attention + дедуп), новые сверху, bounded.
func (r *HumanRequestRepo) ListPending(ctx context.Context, tx DBTX, limit int) ([]*models.HumanRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := tx.QueryContext(ctx, humanRequestSelect+` WHERE status = 'pending' ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.HumanRequest
	for rows.Next() {
		hr, err := scanHumanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, hr)
	}
	return out, rows.Err()
}

// ListPendingByOperation — дедуп: pending-запрос на ту же операцию (метод+path).
func (r *HumanRequestRepo) ListPendingByOperation(ctx context.Context, tx DBTX, method, path string) ([]*models.HumanRequest, error) {
	rows, err := tx.QueryContext(ctx, humanRequestSelect+
		` WHERE status = 'pending' AND method = ? AND path = ? ORDER BY id DESC`, method, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.HumanRequest
	for rows.Next() {
		hr, err := scanHumanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, hr)
	}
	return out, rows.Err()
}

// SetDecision — approve/deny: status + decided_by/at + reason (updated_at = now).
func (r *HumanRequestRepo) SetDecision(ctx context.Context, tx DBTX, id int64, status models.HumanRequestStatus, reason, decidedBy *string, decidedAt time.Time) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE human_requests SET status = ?, decision_reason = ?, decided_by = ?, decided_at = ?, updated_at = ? WHERE id = ?`,
		string(status), reason, decidedBy, decidedAt.Format(time.RFC3339Nano), nowStr(), id)
	return err
}

// MarkReplayed — one-time: approved → replayed (только если сейчас approved).
// Возвращает, удалось ли (affected rows).
func (r *HumanRequestRepo) MarkReplayed(ctx context.Context, tx DBTX, id int64) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE human_requests SET status = 'replayed', updated_at = ? WHERE id = ? AND status = 'approved'`,
		nowStr(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetReplayStatus — после replay: HTTP-статус replay (не меняет status).
func (r *HumanRequestRepo) SetReplayStatus(ctx context.Context, tx DBTX, id int64, status int) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE human_requests SET replay_status = ?, updated_at = ? WHERE id = ?`, status, nowStr(), id)
	return err
}
