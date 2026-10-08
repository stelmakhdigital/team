package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"daemon/internal/models"
)

// ---------- Library ----------

type LibraryRepo struct{ db *sql.DB }

const libraryItemSelect = `
	SELECT id, type, name, description, group_name, version, author, is_public,
	       downloads_count, tags, thumbnail, source_type, source_id, spec, created_at, updated_at
	FROM library_items`

func (r *LibraryRepo) Create(ctx context.Context, tx DBTX, it *models.LibraryItem) error {
	tags, err := json.Marshal(orEmptySlice(it.Tags))
	if err != nil {
		return err
	}
	spec, err := marshalJSON(orEmptyMap(it.Spec))
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	if it.Version == "" {
		it.Version = "1.0.0"
	}
	if it.Group == "" {
		it.Group = "general"
	}
	it.CreatedAt, it.UpdatedAt = ts, ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO library_items (type, name, description, group_name, version, author,
			is_public, tags, thumbnail, source_type, source_id, spec, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(it.Type), it.Name, it.Description, it.Group, it.Version, it.Author,
		it.IsPublic, string(tags), it.Thumbnail, it.SourceType, it.SourceID, spec, tsStr, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	it.ID = id
	return nil
}

func (r *LibraryRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.LibraryItem, error) {
	return scanLibraryItem(tx.QueryRowContext(ctx, libraryItemSelect+` WHERE id = ?`, id))
}

func (r *LibraryRepo) GetByTypeAndName(ctx context.Context, tx DBTX, t models.LibraryItemType, name string) (*models.LibraryItem, error) {
	return scanLibraryItem(tx.QueryRowContext(ctx, libraryItemSelect+` WHERE type = ? AND name = ?`, string(t), name))
}

// LibraryFilter — фильтры GET /api/v1/library (контракт 20 §5.1).
type LibraryFilter struct {
	Type   *models.LibraryItemType
	Group  *string
	Search *string
	Limit  int
	Offset int
}

// List — items + total.
func (r *LibraryRepo) List(ctx context.Context, tx DBTX, f LibraryFilter) ([]*models.LibraryItem, int, error) {
	conds := []string{}
	args := []any{}
	if f.Type != nil {
		conds = append(conds, "type = ?")
		args = append(args, string(*f.Type))
	}
	if f.Group != nil {
		conds = append(conds, "group_name = ?")
		args = append(args, *f.Group)
	}
	if f.Search != nil && *f.Search != "" {
		conds = append(conds, "(name LIKE ? OR description LIKE ?)")
		like := "%" + *f.Search + "%"
		args = append(args, like, like)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAnd(conds)
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_items`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	rows, err := tx.QueryContext(ctx, libraryItemSelect+where+` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*models.LibraryItem, 0, limit)
	for rows.Next() {
		it, err := scanLibraryItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

// IncrementDownloads — downloads_count +1.
func (r *LibraryRepo) IncrementDownloads(ctx context.Context, tx DBTX, id int64) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE library_items SET downloads_count = downloads_count + 1, updated_at = ? WHERE id = ?`,
		nowStr(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GroupCounts — counts по группам (контракт: groups).
func (r *LibraryRepo) GroupCounts(ctx context.Context, tx DBTX, f LibraryFilter) (map[string]int, error) {
	conds := []string{}
	args := []any{}
	if f.Type != nil {
		conds = append(conds, "type = ?")
		args = append(args, string(*f.Type))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAnd(conds)
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT group_name, COUNT(*) FROM library_items`+where+` GROUP BY group_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		out[name] = count
	}
	return out, rows.Err()
}

func scanLibraryItem(s teamScanner) (*models.LibraryItem, error) {
	var (
		it      models.LibraryItem
		desc    sql.NullString
		author  sql.NullString
		pub     int
		tags    string
		thumb   sql.NullString
		srcType sql.NullString
		srcID   sql.NullInt64
		spec    string
		ca, ua  string
	)
	err := s.Scan(&it.ID, &it.Type, &it.Name, &desc, &it.Group, &it.Version, &author, &pub,
		&it.DownloadsCount, &tags, &thumb, &srcType, &srcID, &spec, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if desc.Valid {
		it.Description = &desc.String
	}
	if author.Valid {
		it.Author = &author.String
	}
	it.IsPublic = pub != 0
	if err := json.Unmarshal([]byte(tags), &it.Tags); err != nil || it.Tags == nil {
		it.Tags = []string{}
	}
	if thumb.Valid {
		it.Thumbnail = &thumb.String
	}
	if srcType.Valid {
		it.SourceType = &srcType.String
	}
	if srcID.Valid {
		v := srcID.Int64
		it.SourceID = &v
	}
	it.Spec = unmarshalConfig(spec)
	if it.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse library item created_at: %w", err)
	}
	if it.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse library item updated_at: %w", err)
	}
	return &it, nil
}

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// ---------- Library versions ----------

type LibraryVersionRepo struct{ db *sql.DB }

func (r *LibraryVersionRepo) Create(ctx context.Context, tx DBTX, v *models.LibraryVersion) error {
	ts, tsStr := nowTime(), nowStr()
	v.CreatedAt = ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO library_versions (item_id, version, changes, author, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		v.ItemID, v.Version, v.Changes, v.Author, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	v.ID = id
	return nil
}

func (r *LibraryVersionRepo) ListByItem(ctx context.Context, tx DBTX, itemID int64) ([]*models.LibraryVersion, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, item_id, version, changes, author, created_at
		FROM library_versions WHERE item_id = ? ORDER BY created_at DESC, id DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.LibraryVersion, 0)
	for rows.Next() {
		var v models.LibraryVersion
		var changes, author sql.NullString
		var ca string
		if err := rows.Scan(&v.ID, &v.ItemID, &v.Version, &changes, &author, &ca); err != nil {
			return nil, err
		}
		if changes.Valid {
			v.Changes = &changes.String
		}
		if author.Valid {
			v.Author = &author.String
		}
		if v.CreatedAt, err = parseTime(ca); err != nil {
			return nil, fmt.Errorf("parse library version created_at: %w", err)
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

// ---------- Audit log ----------

type AuditRepo struct{ db *sql.DB }

func (r *AuditRepo) Create(ctx context.Context, tx DBTX, e *models.AuditEntry) error {
	details, err := marshalJSON(orEmptyMap(e.Details))
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	e.CreatedAt = ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO audit_log (user_id, user_name, api_key_id, action, resource, details, ip_address, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.UserName, e.APIKeyID, e.Action, e.Resource, details, e.IPAddress, e.UserAgent, tsStr)
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

// AuditFilter — фильтры GET /api/v1/audit (контракт 20 §6.3).
type AuditFilter struct {
	UserID    *int64
	Action    *string
	Resource  *string
	StartTime *string // RFC3339
	EndTime   *string // RFC3339
	Limit     int
	Offset    int
}

func (r *AuditRepo) List(ctx context.Context, tx DBTX, f AuditFilter) ([]*models.AuditEntry, int, error) {
	conds := []string{}
	args := []any{}
	if f.UserID != nil {
		conds = append(conds, "user_id = ?")
		args = append(args, *f.UserID)
	}
	if f.Action != nil {
		conds = append(conds, "action = ?")
		args = append(args, *f.Action)
	}
	if f.Resource != nil {
		conds = append(conds, "resource = ?")
		args = append(args, *f.Resource)
	}
	if f.StartTime != nil {
		conds = append(conds, "created_at >= ?")
		args = append(args, *f.StartTime)
	}
	if f.EndTime != nil {
		conds = append(conds, "created_at <= ?")
		args = append(args, *f.EndTime)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAnd(conds)
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, user_id, user_name, api_key_id, action, resource, details, ip_address, user_agent, created_at
		FROM audit_log`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*models.AuditEntry, 0, limit)
	for rows.Next() {
		var (
			e       models.AuditEntry
			uid     sql.NullInt64
			uname   sql.NullString
			keyID   sql.NullInt64
			res     sql.NullString
			details string
			ip      sql.NullString
			ua      sql.NullString
			ca      string
		)
		if err := rows.Scan(&e.ID, &uid, &uname, &keyID, &e.Action, &res, &details, &ip, &ua, &ca); err != nil {
			return nil, 0, err
		}
		if uid.Valid {
			v := uid.Int64
			e.UserID = &v
		}
		if uname.Valid {
			e.UserName = &uname.String
		}
		if keyID.Valid {
			v := keyID.Int64
			e.APIKeyID = &v
		}
		if res.Valid {
			e.Resource = &res.String
		}
		e.Details = unmarshalConfig(details)
		if ip.Valid {
			e.IPAddress = &ip.String
		}
		if ua.Valid {
			e.UserAgent = &ua.String
		}
		if e.CreatedAt, err = parseTime(ca); err != nil {
			return nil, 0, fmt.Errorf("parse audit created_at: %w", err)
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}
