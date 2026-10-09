package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"daemon/internal/models"
)

// WakeRepo — P0.3: wake для заблокированной задачи (task_wakes).
type WakeRepo struct{ db *sql.DB }

const wakeColumns = `id, task_id, kind, ref, continuation, created_at, fired_at`

func (r *WakeRepo) Create(ctx context.Context, tx DBTX, w *models.TaskWake) error {
	w.CreatedAt = nowTime()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO task_wakes (task_id, kind, ref, continuation, created_at, fired_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		w.TaskID, w.Kind, w.Ref, w.Continuation,
		w.CreatedAt.Format(time.RFC3339Nano), wakeTimeStr(w.FiredAt))
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

// GetLatestByTask — самая свежая wake задачи (armed или fired) — для деталки.
func (r *WakeRepo) GetLatestByTask(ctx context.Context, tx DBTX, taskID int64) (*models.TaskWake, error) {
	return scanWake(tx.QueryRowContext(ctx,
		`SELECT `+wakeColumns+` FROM task_wakes WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, taskID))
}

// GetActiveByTask — арм. wake (fired_at IS NULL) — для close-existing при block + resolve.
func (r *WakeRepo) GetActiveByTask(ctx context.Context, tx DBTX, taskID int64) (*models.TaskWake, error) {
	return scanWake(tx.QueryRowContext(ctx,
		`SELECT `+wakeColumns+` FROM task_wakes WHERE task_id = ? AND fired_at IS NULL ORDER BY created_at DESC, id DESC LIMIT 1`, taskID))
}

// ListArmedTimers — арм. timer-wakes, чей срок наступил (ref = fire-at < now).
// Кандидаты; service проверит, что задача всё ещё blocked.
func (r *WakeRepo) ListArmedTimers(ctx context.Context, tx DBTX, now time.Time) ([]*models.TaskWake, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+wakeColumns+` FROM task_wakes
		 WHERE kind = 'timer' AND fired_at IS NULL AND ref IS NOT NULL AND ref < ?
		 ORDER BY ref ASC, id ASC LIMIT 200`, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWakes(rows)
}

// ListArmedByRef — арм. task-wakes на referenced-задачу (ref = task_id).
func (r *WakeRepo) ListArmedByRef(ctx context.Context, tx DBTX, ref string) ([]*models.TaskWake, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+wakeColumns+` FROM task_wakes
		 WHERE kind = 'task' AND ref = ? AND fired_at IS NULL
		 ORDER BY created_at ASC, id ASC LIMIT 200`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWakes(rows)
}

// MarkFired — закрытие wake (fired ИЛИ resolve) — идемпотентность.
func (r *WakeRepo) MarkFired(ctx context.Context, tx DBTX, id int64, at time.Time) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE task_wakes SET fired_at = ? WHERE id = ? AND fired_at IS NULL`,
		at.Format(time.RFC3339Nano), id)
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

func wakeTimeStr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func scanWake(s taskScanner) (*models.TaskWake, error) {
	var (
		w       models.TaskWake
		ref     sql.NullString
		ca      string
		firedAt sql.NullString
	)
	if err := s.Scan(&w.ID, &w.TaskID, &w.Kind, &ref, &w.Continuation, &ca, &firedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ref.Valid {
		w.Ref = ref.String
	}
	if firedAt.Valid {
		if tm, err := parseTime(firedAt.String); err == nil {
			w.FiredAt = &tm
		}
	}
	if tm, err := parseTime(ca); err == nil {
		w.CreatedAt = tm
	}
	return &w, nil
}

func scanWakes(rows *sql.Rows) ([]*models.TaskWake, error) {
	out := make([]*models.TaskWake, 0, 20)
	for rows.Next() {
		var (
			w       models.TaskWake
			ref     sql.NullString
			ca      string
			firedAt sql.NullString
		)
		if err := rows.Scan(&w.ID, &w.TaskID, &w.Kind, &ref, &w.Continuation, &ca, &firedAt); err != nil {
			return nil, err
		}
		if ref.Valid {
			w.Ref = ref.String
		}
		if firedAt.Valid {
			if tm, err := parseTime(firedAt.String); err == nil {
				w.FiredAt = &tm
			}
		}
		if tm, err := parseTime(ca); err == nil {
			w.CreatedAt = tm
		}
		out = append(out, &w)
	}
	return out, rows.Err()
}
