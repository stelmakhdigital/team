package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
)

// EvidenceRepo — P3.13: артефакты-доказательства задачи (task_evidence).
type EvidenceRepo struct{ db *sql.DB }

const evidenceColumns = `id, task_id, etype, name, content_ref, content_sha, description,
    status, verdict_reason, submitted_by, judged_by, judged_at, created_at, updated_at`

func (r *EvidenceRepo) Create(ctx context.Context, tx DBTX, e *models.TaskEvidence) error {
	now := nowTime()
	e.CreatedAt = now
	e.UpdatedAt = now
	res, err := tx.ExecContext(ctx, `
		INSERT INTO task_evidence (task_id, etype, name, content_ref, content_sha, description,
			status, verdict_reason, submitted_by, judged_by, judged_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TaskID, string(e.Type), e.Name, e.ContentRef, e.ContentSHA, e.Description,
		string(e.Status), e.VerdictReason, e.SubmittedBy, e.JudgedBy,
		evidenceTimeStr(e.JudgedAt), e.CreatedAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	e.ID = id
	return nil
}

// GetByID — артефакт по id (nil → ErrNotFound).
func (r *EvidenceRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.TaskEvidence, error) {
	return scanEvidence(tx.QueryRowContext(ctx,
		`SELECT `+evidenceColumns+` FROM task_evidence WHERE id = ?`, id))
}

// GetByTask — артефакт по id, но только если он принадлежит задаче taskID (иначе
// ErrNotFound) — защита от cross-task verdict.
func (r *EvidenceRepo) GetByTask(ctx context.Context, tx DBTX, taskID, id int64) (*models.TaskEvidence, error) {
	return scanEvidence(tx.QueryRowContext(ctx,
		`SELECT `+evidenceColumns+` FROM task_evidence WHERE id = ? AND task_id = ?`, id, taskID))
}

// ListByTask — bounded-список артефактов задачи (новые сверху).
func (r *EvidenceRepo) ListByTask(ctx context.Context, tx DBTX, taskID int64, limit, offset int) ([]*models.TaskEvidence, int, error) {
	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_evidence WHERE task_id = ?`, taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT `+evidenceColumns+` FROM task_evidence WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		taskID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, err := scanEvidences(rows)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// SetVerdict — применить вердикт судьи: status + verdict_reason + judged_by + judged_at.
// Возвращает обновлённый артефакт. 0 строк → ErrNotFound.
func (r *EvidenceRepo) SetVerdict(ctx context.Context, tx DBTX, id int64, status models.EvidenceStatus, reason *string, judgedBy string, at time.Time) (*models.TaskEvidence, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE task_evidence
		SET status = ?, verdict_reason = ?, judged_by = ?, judged_at = ?, updated_at = ?
		WHERE id = ?`,
		string(status), reason, judgedBy, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, tx, id)
}

// StatusSummaryByTasks — агрегированные счётчики статусов по списку задач (одним
// запросом) — для readiness/acceptance в TaskView без N+1.
// Возвращает map[taskID]map[status]count.
func (r *EvidenceRepo) StatusSummaryByTasks(ctx context.Context, tx DBTX, taskIDs []int64) (map[int64]map[string]int, error) {
	out := make(map[int64]map[string]int, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	// IN (?) — bounded (задачи, переданные в viewsFor; до 500).
	placeholders := make([]string, len(taskIDs))
	args := make([]any, 0, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	q := fmt.Sprintf(`SELECT task_id, status, COUNT(*) FROM task_evidence WHERE task_id IN (%s) GROUP BY task_id, status`,
		joinStrings(placeholders, ", "))
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			taskID int64
			status string
			count  int
		)
		if err := rows.Scan(&taskID, &status, &count); err != nil {
			return nil, err
		}
		m, ok := out[taskID]
		if !ok {
			m = make(map[string]int)
			out[taskID] = m
		}
		m[status] = count
	}
	return out, rows.Err()
}

func evidenceTimeStr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func scanEvidence(s taskScanner) (*models.TaskEvidence, error) {
	var (
		e         models.TaskEvidence
		ref       sql.NullString
		sha       sql.NullString
		desc      sql.NullString
		reason    sql.NullString
		judgedBy  sql.NullString
		judgedAt  sql.NullString
		submitted sql.NullString
		ca        string
		ua        string
	)
	if err := s.Scan(&e.ID, &e.TaskID, &e.Type, &e.Name, &ref, &sha, &desc,
		&e.Status, &reason, &submitted, &judgedBy, &judgedAt, &ca, &ua); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.ContentRef = nullStr(ref)
	e.ContentSHA = nullStr(sha)
	e.Description = nullStr(desc)
	e.VerdictReason = nullStr(reason)
	e.JudgedBy = nullStr(judgedBy)
	if submitted.Valid {
		e.SubmittedBy = submitted.String
	}
	if judgedAt.Valid {
		if tm, err := parseTime(judgedAt.String); err == nil {
			e.JudgedAt = &tm
		}
	}
	if tm, err := parseTime(ca); err == nil {
		e.CreatedAt = tm
	}
	if tm, err := parseTime(ua); err == nil {
		e.UpdatedAt = tm
	}
	return &e, nil
}

func scanEvidences(rows *sql.Rows) ([]*models.TaskEvidence, error) {
	out := make([]*models.TaskEvidence, 0, 20)
	for rows.Next() {
		var (
			e         models.TaskEvidence
			ref       sql.NullString
			sha       sql.NullString
			desc      sql.NullString
			reason    sql.NullString
			judgedBy  sql.NullString
			judgedAt  sql.NullString
			submitted sql.NullString
			ca        string
			ua        string
		)
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Type, &e.Name, &ref, &sha, &desc,
			&e.Status, &reason, &submitted, &judgedBy, &judgedAt, &ca, &ua); err != nil {
			return nil, err
		}
		e.ContentRef = nullStr(ref)
		e.ContentSHA = nullStr(sha)
		e.Description = nullStr(desc)
		e.VerdictReason = nullStr(reason)
		e.JudgedBy = nullStr(judgedBy)
		if submitted.Valid {
			e.SubmittedBy = submitted.String
		}
		if judgedAt.Valid {
			if tm, err := parseTime(judgedAt.String); err == nil {
				e.JudgedAt = &tm
			}
		}
		if tm, err := parseTime(ca); err == nil {
			e.CreatedAt = tm
		}
		if tm, err := parseTime(ua); err == nil {
			e.UpdatedAt = tm
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// nullStr — sql.NullString → *string (nil если пусто).
func nullStr(ns sql.NullString) *string {
	if ns.Valid && ns.String != "" {
		v := ns.String
		return &v
	}
	return nil
}

// joinStrings — concat (локальный; не тянем strings только ради одного join).
func joinStrings(items []string, sep string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
