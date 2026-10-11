package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
)

// WalkRepo — P3.14: Walk (paced-доставка контекста в сессию).
type WalkRepo struct {
	db *sql.DB
}

const walkSelect = `SELECT id, session_id, name, description, mode, interval_secs,
	status, current_seq, last_sent_at, created_at, updated_at FROM walks`

const walkStepSelect = `SELECT id, walk_id, seq, content, state, send_status, note, sent_at, created_at
	FROM walk_steps`

// CreateWalk — вставить walk (id присваивается DB).
func (r *WalkRepo) CreateWalk(ctx context.Context, tx DBTX, w *models.Walk) error {
	now := nowTime()
	res, err := tx.ExecContext(ctx, `INSERT INTO walks
		(session_id, name, description, mode, interval_secs, status, current_seq, last_sent_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.SessionID, w.Name, w.Description, string(w.Mode), nullableInt(w.IntervalSecs),
		string(w.Status), w.CurrentSeq, nullableTime(w.LastSentAt),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert walk: %w", err)
	}
	w.ID, err = res.LastInsertId()
	if err != nil {
		return fmt.Errorf("walk id: %w", err)
	}
	return nil
}

// CreateSteps — вставить шаги (batch). ID присваиваются DB.
func (r *WalkRepo) CreateSteps(ctx context.Context, tx DBTX, steps []*models.WalkStep) error {
	now := nowTime().Format(time.RFC3339Nano)
	for _, s := range steps {
		s.WalkID = steps[0].WalkID
		res, err := tx.ExecContext(ctx, `INSERT INTO walk_steps
			(walk_id, seq, content, state, created_at) VALUES (?, ?, ?, 'pending', ?)`,
			s.WalkID, s.Seq, s.Content, now)
		if err != nil {
			return fmt.Errorf("insert walk step: %w", err)
		}
		s.ID, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("walk step id: %w", err)
		}
	}
	return nil
}

func scanWalk(s teamScanner) (*models.Walk, error) {
	var (
		w            models.Walk
		desc         sql.NullString
		intervalSecs sql.NullInt64
		lastSent     sql.NullString
		ca, ua       string
	)
	err := s.Scan(&w.ID, &w.SessionID, &w.Name, &desc, &w.Mode, &intervalSecs,
		&w.Status, &w.CurrentSeq, &lastSent, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if desc.Valid {
		v := desc.String
		w.Description = &v
	}
	if intervalSecs.Valid {
		v := int(intervalSecs.Int64)
		w.IntervalSecs = &v
	}
	if lastSent.Valid {
		if t, perr := parseTime(lastSent.String); perr == nil {
			w.LastSentAt = &t
		}
	}
	if w.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse walk created_at: %w", err)
	}
	if w.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse walk updated_at: %w", err)
	}
	return &w, nil
}

func (r *WalkRepo) GetWalk(ctx context.Context, tx DBTX, id int64) (*models.Walk, error) {
	return scanWalk(tx.QueryRowContext(ctx, walkSelect+` WHERE id = ?`, id))
}

func (r *WalkRepo) ListWalksBySession(ctx context.Context, tx DBTX, sessionID int64) ([]*models.Walk, error) {
	rows, err := tx.QueryContext(ctx, walkSelect+` WHERE session_id = ? ORDER BY id DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Walk
	for rows.Next() {
		w, err := scanWalk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListActiveIntervalWalks — active walks c mode=interval (для AdvanceDue, reaper).
func (r *WalkRepo) ListActiveIntervalWalks(ctx context.Context, tx DBTX) ([]*models.Walk, error) {
	rows, err := tx.QueryContext(ctx, walkSelect+
		` WHERE status = ? AND mode = ? ORDER BY id`, string(models.WalkActive), string(models.WalkModeInterval))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Walk
	for rows.Next() {
		w, err := scanWalk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpdateWalkStatus — сменить status (updated_at = now).
func (r *WalkRepo) UpdateWalkStatus(ctx context.Context, tx DBTX, id int64, status models.WalkStatus) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE walks SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), nowStr(), id)
	return err
}

// UpdateWalkAdvance — после advance: current_seq++, last_sent_at, status (updated_at = now).
func (r *WalkRepo) UpdateWalkAdvance(ctx context.Context, tx DBTX, id int64, currentSeq int, lastSentAt time.Time, status models.WalkStatus) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE walks SET current_seq = ?, last_sent_at = ?, status = ?, updated_at = ? WHERE id = ?`,
		currentSeq, lastSentAt.Format(time.RFC3339Nano), string(status), nowStr(), id)
	return err
}

// UpdateWalkSeq — сменить current_seq + status (без last_sent_at; skip).
func (r *WalkRepo) UpdateWalkSeq(ctx context.Context, tx DBTX, id int64, currentSeq int, status models.WalkStatus) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE walks SET current_seq = ?, status = ?, updated_at = ? WHERE id = ?`,
		currentSeq, string(status), nowStr(), id)
	return err
}

// UpdateStep — сменить state/send_status/note/sent_at шага.
func (r *WalkRepo) UpdateStep(ctx context.Context, tx DBTX, id int64, state models.WalkStepState, sendStatus *string, note *string, sentAt *time.Time) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE walk_steps SET state = ?, send_status = ?, note = ?, sent_at = ? WHERE id = ?`,
		string(state), sendStatus, note, nullableTime(sentAt), id)
	return err
}

func (r *WalkRepo) GetStepBySeq(ctx context.Context, tx DBTX, walkID int64, seq int) (*models.WalkStep, error) {
	return scanWalkStep(tx.QueryRowContext(ctx,
		walkStepSelect+` WHERE walk_id = ? AND seq = ?`, walkID, seq))
}

func (r *WalkRepo) ListStepsByWalk(ctx context.Context, tx DBTX, walkID int64) ([]*models.WalkStep, error) {
	rows, err := tx.QueryContext(ctx, walkStepSelect+` WHERE walk_id = ? ORDER BY seq`, walkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.WalkStep
	for rows.Next() {
		s, err := scanWalkStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountSteps — число шагов walk'а (max_seq).
func (r *WalkRepo) CountSteps(ctx context.Context, tx DBTX, walkID int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM walk_steps WHERE walk_id = ?`, walkID).Scan(&n)
	return n, err
}

func scanWalkStep(s teamScanner) (*models.WalkStep, error) {
	var (
		st     models.WalkStep
		sendSt sql.NullString
		note   sql.NullString
		sentAt sql.NullString
		ca     string
	)
	err := s.Scan(&st.ID, &st.WalkID, &st.Seq, &st.Content, &st.State, &sendSt, &note, &sentAt, &ca)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if sendSt.Valid {
		v := sendSt.String
		st.SendStatus = &v
	}
	if note.Valid {
		v := note.String
		st.Note = &v
	}
	if sentAt.Valid {
		if t, perr := parseTime(sentAt.String); perr == nil {
			st.SentAt = &t
		}
	}
	if st.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse walk step created_at: %w", err)
	}
	return &st, nil
}
