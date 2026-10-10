package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"daemon/internal/models"
)

// P1.6 — Watchdog-джобы (register, context-usage-threshold).

const jobColumns = `id, name, policy, target_session_id, threshold_pct, message,
    enabled, created_by, created_at, fired_count, last_fired_at, last_fired_pct`

// WatchdogJobRepo — CRUD регистрируемых watchdog-джобов.
type WatchdogJobRepo struct{ db *sql.DB }

// RegisterWatchdogJob — вставка нового джоба (id на выходе).
func (r *WatchdogJobRepo) Create(ctx context.Context, tx DBTX, j *models.WatchdogJob) error {
	j.CreatedAt = nowTime()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO watchdog_jobs (name, policy, target_session_id, threshold_pct, message,
			enabled, created_by, created_at, fired_count, last_fired_at, last_fired_pct)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		j.Name, j.Policy, nullableInt64(j.TargetSessionID), nullableInt(j.ThresholdPct),
		j.Message, boolToInt(j.Enabled), j.CreatedBy, j.CreatedAt.Format(time.RFC3339Nano),
		nullableTime(j.LastFiredAt), nullableFloat64(j.LastFiredPct))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	j.ID = id
	return nil
}

// GetByID — джоб по id (ErrNotFound если нет).
func (r *WatchdogJobRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.WatchdogJob, error) {
	return scanJob(tx.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM watchdog_jobs WHERE id = ?`, id))
}

// ListJobs — джобы (enabled только, если !all; limit; sort id desc).
func (r *WatchdogJobRepo) List(ctx context.Context, tx DBTX, all bool, limit int) ([]*models.WatchdogJob, error) {
	q := `SELECT ` + jobColumns + ` FROM watchdog_jobs`
	if !all {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY id DESC LIMIT ?`
	rows, err := tx.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanJobs(rows)
}

// SetEnabled — stop / enable джоба.
func (r *WatchdogJobRepo) SetEnabled(ctx context.Context, tx DBTX, id int64, enabled bool) error {
	res, err := tx.ExecContext(ctx, `UPDATE watchdog_jobs SET enabled = ? WHERE id = ?`,
		boolToInt(enabled), id)
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

// RecordFire — fired_count++, last_fired_at/pct (после fire).
func (r *WatchdogJobRepo) RecordFire(ctx context.Context, tx DBTX, id int64, at time.Time, pct *float64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE watchdog_jobs SET fired_count = fired_count + 1, last_fired_at = ?, last_fired_pct = ? WHERE id = ?`,
		at.Format(time.RFC3339Nano), nullableFloat64(pct), id)
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

// Delete — удаление джоба (idempotent no-op если уже нет).
func (r *WatchdogJobRepo) Delete(ctx context.Context, tx DBTX, id int64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM watchdog_jobs WHERE id = ?`, id)
	return err
}

func scanJob(s taskScanner) (*models.WatchdogJob, error) {
	var (
		j            models.WatchdogJob
		targetID     sql.NullInt64
		thresholdPct sql.NullInt32
		enabled      int
		ca           string
		lastFiredAt  sql.NullString
		lastFiredPct sql.NullFloat64
	)
	if err := s.Scan(&j.ID, &j.Name, &j.Policy, &targetID, &thresholdPct, &j.Message,
		&enabled, &j.CreatedBy, &ca, &j.FiredCount, &lastFiredAt, &lastFiredPct); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if targetID.Valid {
		v := targetID.Int64
		j.TargetSessionID = &v
	}
	if thresholdPct.Valid {
		v := int(thresholdPct.Int32)
		j.ThresholdPct = &v
	}
	j.Enabled = enabled != 0
	if tm, err := parseTime(ca); err == nil {
		j.CreatedAt = tm
	}
	if lastFiredAt.Valid {
		if tm, err := parseTime(lastFiredAt.String); err == nil {
			j.LastFiredAt = &tm
		}
	}
	if lastFiredPct.Valid {
		v := lastFiredPct.Float64
		j.LastFiredPct = &v
	}
	return &j, nil
}

func scanJobs(rows *sql.Rows) ([]*models.WatchdogJob, error) {
	out := make([]*models.WatchdogJob, 0, 20)
	for rows.Next() {
		var (
			j            models.WatchdogJob
			targetID     sql.NullInt64
			thresholdPct sql.NullInt32
			enabled      int
			ca           string
			lastFiredAt  sql.NullString
			lastFiredPct sql.NullFloat64
		)
		if err := rows.Scan(&j.ID, &j.Name, &j.Policy, &targetID, &thresholdPct, &j.Message,
			&enabled, &j.CreatedBy, &ca, &j.FiredCount, &lastFiredAt, &lastFiredPct); err != nil {
			return nil, err
		}
		if targetID.Valid {
			v := targetID.Int64
			j.TargetSessionID = &v
		}
		if thresholdPct.Valid {
			v := int(thresholdPct.Int32)
			j.ThresholdPct = &v
		}
		j.Enabled = enabled != 0
		if tm, err := parseTime(ca); err == nil {
			j.CreatedAt = tm
		}
		if lastFiredAt.Valid {
			if tm, err := parseTime(lastFiredAt.String); err == nil {
				j.LastFiredAt = &tm
			}
		}
		if lastFiredPct.Valid {
			v := lastFiredPct.Float64
			j.LastFiredPct = &v
		}
		out = append(out, &j)
	}
	return out, rows.Err()
}

// WatchdogJobEventRepo — история fire / dead-target (bounded 20/job).
type WatchdogJobEventRepo struct{ db *sql.DB }

const jobEventColumns = `id, job_id, outcome, detail, pct, created_at`

// const jobEventCap — bounded история (последние N на джоб).
const jobEventCap = 20

func (r *WatchdogJobEventRepo) Create(ctx context.Context, tx DBTX, e *models.WatchdogJobEvent) error {
	e.CreatedAt = nowTime()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO watchdog_job_events (job_id, outcome, detail, pct, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		e.JobID, e.Outcome, e.Detail, nullableFloat64(e.Pct), e.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	e.ID = id
	// bounded: оставить последние jobEventCap на джоб
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM watchdog_job_events WHERE job_id = ? AND id NOT IN (
			SELECT id FROM watchdog_job_events WHERE job_id = ? ORDER BY id DESC LIMIT ?)`,
		e.JobID, e.JobID, jobEventCap)
	return nil
}

// ListByJob — последние события джоба (desc, cap).
func (r *WatchdogJobEventRepo) ListByJob(ctx context.Context, tx DBTX, jobID int64, limit int) ([]*models.WatchdogJobEvent, error) {
	if limit <= 0 {
		limit = jobEventCap
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT `+jobEventColumns+` FROM watchdog_job_events WHERE job_id = ? ORDER BY id DESC LIMIT ?`,
		jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*models.WatchdogJobEvent, 0, limit)
	for rows.Next() {
		var (
			e   models.WatchdogJobEvent
			pct sql.NullFloat64
			ca  string
		)
		if err := rows.Scan(&e.ID, &e.JobID, &e.Outcome, &e.Detail, &pct, &ca); err != nil {
			return nil, err
		}
		if pct.Valid {
			v := pct.Float64
			e.Pct = &v
		}
		if tm, err := parseTime(ca); err == nil {
			e.CreatedAt = tm
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
