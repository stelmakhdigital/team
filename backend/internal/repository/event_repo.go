package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// P2.9: EventRepo — bounded-хранилище real-time-событий (Pulse feed).
// Отдельно от AuditRepo (events = события шины, audit = действия API).

const (
	// eventMaxRows — держать ≤N последних событий (bounded).
	eventMaxRows = 5000
	// eventMaxAge — держать события не старше 7 дней (bounded).
	eventMaxAge = 7 * 24 * time.Hour
)

type EventRepo struct{ db *sql.DB }

func NewEventRepo(db *sql.DB) *EventRepo { return &EventRepo{db: db} }

// EventListFilter — фильтры GET /events.
type EventListFilter struct {
	Type       *string
	TeamID     *int64
	RoleID     *int64
	EntityKind *string
	EntityID   *int64
	Limit      int
	Offset     int
}

// Record — запись события + bounded-cleanup (≤5000 ИЛИ ≤7 дней).
func (r *EventRepo) Record(ctx context.Context, tx DBTX, ev *EventIn) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events (type, team_id, role_id, entity_kind, entity_id, summary, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.Type, ev.TeamID, ev.RoleID, ev.EntityKind, ev.EntityID, ev.Summary, ev.Payload, ev.CreatedAtStr)
	if err != nil {
		return err
	}
	return r.Prune(ctx, tx)
}

// EventIn — входные данные для записи (nullable-поля как *T / sql.Null*).
type EventIn struct {
	Type         string
	TeamID       any // nil = NULL
	RoleID       any // nil = NULL
	EntityKind   string
	EntityID     int64
	Summary      string
	Payload      string
	CreatedAtStr string
}

// Prune — bounded-cleanup: удалить старше 7 дней + оставить ≤5000 последних.
func (r *EventRepo) Prune(ctx context.Context, tx DBTX) error {
	cutoff := nowTime().Add(-eventMaxAge).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, cutoff); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		return err
	}
	if count <= eventMaxRows {
		return nil
	}
	// удалить самые старые (count - eventMaxRows): оставить последние 5000
	_, err := tx.ExecContext(ctx,
		`DELETE FROM events WHERE id NOT IN (SELECT id FROM events ORDER BY created_at DESC, id DESC LIMIT ?)`,
		eventMaxRows)
	return err
}

// List — события по фильтрам (created_at DESC) + total.
func (r *EventRepo) List(ctx context.Context, tx DBTX, f EventListFilter) ([]*EventOut, int, error) {
	conds := []string{}
	args := []any{}
	if f.Type != nil {
		conds = append(conds, "type = ?")
		args = append(args, *f.Type)
	}
	if f.TeamID != nil {
		conds = append(conds, "team_id = ?")
		args = append(args, *f.TeamID)
	}
	if f.RoleID != nil {
		conds = append(conds, "role_id = ?")
		args = append(args, *f.RoleID)
	}
	if f.EntityKind != nil {
		conds = append(conds, "entity_kind = ?")
		args = append(args, *f.EntityKind)
	}
	if f.EntityID != nil {
		conds = append(conds, "entity_id = ?")
		args = append(args, *f.EntityID)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAnd(conds)
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id, type, team_id, role_id, entity_kind, entity_id, summary, payload, created_at
		 FROM events`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*EventOut, 0, limit)
	for rows.Next() {
		var (
			e       EventOut
			teamID  sql.NullInt64
			roleID  sql.NullInt64
			payload sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.Type, &teamID, &roleID, &e.EntityKind, &e.EntityID, &e.Summary, &payload, &e.CreatedAtStr); err != nil {
			return nil, 0, err
		}
		if teamID.Valid {
			v := teamID.Int64
			e.TeamID = &v
		}
		if roleID.Valid {
			v := roleID.Int64
			e.RoleID = &v
		}
		if payload.Valid {
			v := payload.String
			e.Payload = &v
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}

// EventOut — событие из БД (nullable-поля как *T).
type EventOut struct {
	ID           int64
	Type         string
	TeamID       *int64
	RoleID       *int64
	EntityKind   string
	EntityID     int64
	Summary      string
	Payload      *string
	CreatedAtStr string
}

// --- batch-lookups для enrichment (bounded: 1 запрос на тип) ---

// TeamNames — id → name для списка id (IN).
func (r *EventRepo) TeamNames(ctx context.Context, tx DBTX, ids []int64) (map[int64]string, error) {
	return nameMap(ctx, tx, "teams", "id", "name", ids)
}

// RoleNames — id → name для списка id.
func (r *EventRepo) RoleNames(ctx context.Context, tx DBTX, ids []int64) (map[int64]string, error) {
	return nameMap(ctx, tx, "roles", "id", "name", ids)
}

// TaskTitles — id → title для списка id (таблица queue_tasks).
func (r *EventRepo) TaskTitles(ctx context.Context, tx DBTX, ids []int64) (map[int64]string, error) {
	return nameMap(ctx, tx, "queue_tasks", "id", "title", ids)
}

// nameMap — SELECT id, col FROM table WHERE id IN (...).
func nameMap(ctx context.Context, tx DBTX, table, idCol, valCol string, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	query := `SELECT ` + idCol + `, ` + valCol + ` FROM ` + table + ` WHERE ` + idCol + ` IN (` + strings.Join(ph, ", ") + `)`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
