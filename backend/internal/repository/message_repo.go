package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
)

// ---------- Messages ----------

type MessageRepo struct{ db *sql.DB }

// MessageFilter — фильтры GET /api/v1/messages (контракт 20 §4.1).
type MessageFilter struct {
	TeamID      *int64
	QueueTaskID *int64
	FromRoleID  *int64
	ToRoleID    *int64
	Type        *models.MessageType
	Limit       int
	Offset      int
}

// MessageRow — сообщение + имена ролей (JOIN) для контрактного вью.
type MessageRow struct {
	*models.Message
	FromRoleName *string
	ToRoleName   *string
}

func (r *MessageRepo) Create(ctx context.Context, tx DBTX, m *models.Message) error {
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	meta, err := marshalJSON(m.Metadata)
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	m.CreatedAt = ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (team_id, queue_task_id, from_role_id, to_role_id, type, body, metadata, is_read, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.TeamID, m.QueueTaskID, m.FromRoleID, m.ToRoleID,
		string(m.Type), m.Body, meta, m.IsRead, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	m.ID = id
	return nil
}

func (r *MessageRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*MessageRow, error) {
	return scanMessageRow(tx.QueryRowContext(ctx, messageSelect+` WHERE m.id = ?`, id))
}

func (r *MessageRepo) List(ctx context.Context, tx DBTX, f MessageFilter) ([]*MessageRow, int, error) {
	conds := []string{}
	args := []any{}
	if f.TeamID != nil {
		conds = append(conds, "m.team_id = ?")
		args = append(args, *f.TeamID)
	}
	if f.QueueTaskID != nil {
		conds = append(conds, "m.queue_task_id = ?")
		args = append(args, *f.QueueTaskID)
	}
	if f.FromRoleID != nil {
		conds = append(conds, "m.from_role_id = ?")
		args = append(args, *f.FromRoleID)
	}
	if f.ToRoleID != nil {
		conds = append(conds, "m.to_role_id = ?")
		args = append(args, *f.ToRoleID)
	}
	if f.Type != nil {
		conds = append(conds, "m.type = ?")
		args = append(args, string(*f.Type))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + joinAnd(conds)
	}

	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages m`+where, args...).Scan(&total); err != nil {
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
		messageSelect+where+` ORDER BY m.created_at DESC, m.id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*MessageRow, 0, limit)
	for rows.Next() {
		row, err := scanMessageRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

const messageSelect = `
	SELECT m.id, m.team_id, m.queue_task_id, m.from_role_id, m.to_role_id,
	       m.type, m.body, m.metadata, m.is_read, m.created_at,
	       fr.name AS from_role_name, tr.name AS to_role_name
	FROM messages m
	LEFT JOIN roles fr ON fr.id = m.from_role_id
	LEFT JOIN roles tr ON tr.id = m.to_role_id`

func scanMessageRow(s teamScanner) (*MessageRow, error) {
	var (
		m        models.Message
		taskID   sql.NullInt64
		fromID   sql.NullInt64
		toID     sql.NullInt64
		fromName sql.NullString
		toName   sql.NullString
		meta     string
		read     int
		ca       string
	)
	err := s.Scan(&m.ID, &m.TeamID, &taskID, &fromID, &toID,
		&m.Type, &m.Body, &meta, &read, &ca, &fromName, &toName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if taskID.Valid {
		v := taskID.Int64
		m.QueueTaskID = &v
	}
	if fromID.Valid {
		v := fromID.Int64
		m.FromRoleID = &v
	}
	if toID.Valid {
		v := toID.Int64
		m.ToRoleID = &v
	}
	m.Metadata = unmarshalConfig(meta)
	m.IsRead = read != 0
	if m.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse message created_at: %w", err)
	}
	row := &MessageRow{Message: &m}
	if fromName.Valid {
		row.FromRoleName = &fromName.String
	}
	if toName.Valid {
		row.ToRoleName = &toName.String
	}
	return row, nil
}

// joinAnd — склейка WHERE-условий (без импорта strings в 10 строк).
func joinAnd(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

// ---------- Chatrooms ----------

type ChatroomRepo struct{ db *sql.DB }

func (r *ChatroomRepo) Create(ctx context.Context, tx DBTX, c *models.Chatroom) error {
	if c.Config == nil {
		c.Config = map[string]any{}
	}
	cfg, err := marshalJSON(c.Config)
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	c.CreatedAt, c.UpdatedAt = ts, ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO chatrooms (team_id, segment_id, name, topic, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.TeamID, c.SegmentID, c.Name, c.Topic, cfg, tsStr, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = id
	return nil
}

func (r *ChatroomRepo) GetByID(ctx context.Context, tx DBTX, id int64) (*models.Chatroom, error) {
	return scanChatroom(tx.QueryRowContext(ctx, `
		SELECT id, team_id, segment_id, name, topic, config, created_at, updated_at
		FROM chatrooms WHERE id = ?`, id))
}

// List — chatrooms, опционально по команде (контракт 20 §4.3).
func (r *ChatroomRepo) List(ctx context.Context, tx DBTX, teamID *int64) ([]*models.Chatroom, error) {
	q := `SELECT id, team_id, segment_id, name, topic, config, created_at, updated_at
	      FROM chatrooms`
	args := []any{}
	if teamID != nil {
		q += ` WHERE team_id = ?`
		args = append(args, *teamID)
	}
	q += ` ORDER BY id`
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*models.Chatroom, 0)
	for rows.Next() {
		c, err := scanChatroom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanChatroom(s teamScanner) (*models.Chatroom, error) {
	var (
		c      models.Chatroom
		segID  sql.NullInt64
		topic  sql.NullString
		cfg    string
		ca, ua string
	)
	err := s.Scan(&c.ID, &c.TeamID, &segID, &c.Name, &topic, &cfg, &ca, &ua)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if segID.Valid {
		v := segID.Int64
		c.SegmentID = &v
	}
	if topic.Valid {
		c.Topic = &topic.String
	}
	c.Config = unmarshalConfig(cfg)
	if c.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse chatroom created_at: %w", err)
	}
	if c.UpdatedAt, err = parseTime(ua); err != nil {
		return nil, fmt.Errorf("parse chatroom updated_at: %w", err)
	}
	return &c, nil
}

// ---------- Chatroom messages ----------

type ChatroomMessageRepo struct{ db *sql.DB }

func (r *ChatroomMessageRepo) Create(ctx context.Context, tx DBTX, m *models.ChatroomMessage) error {
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	meta, err := marshalJSON(m.Metadata)
	if err != nil {
		return err
	}
	ts, tsStr := nowTime(), nowStr()
	m.CreatedAt = ts
	res, err := tx.ExecContext(ctx, `
		INSERT INTO chatroom_messages (chatroom_id, from_role_id, body, metadata, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		m.ChatroomID, m.FromRoleID, m.Body, meta, tsStr)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	m.ID = id
	return nil
}

// ChatroomMessageRow — сообщение чата + имя отправителя для вью.
type ChatroomMessageRow struct {
	*models.ChatroomMessage
	FromRoleName *string
}

const chatroomMessageSelect = `
	SELECT cm.id, cm.chatroom_id, cm.from_role_id, cm.body, cm.metadata, cm.created_at,
	       fr.name AS from_role_name
	FROM chatroom_messages cm
	LEFT JOIN roles fr ON fr.id = cm.from_role_id`

// List — сообщения чата (по возрастанию, как чат-лента) + total для has_more.
func (r *ChatroomMessageRepo) List(ctx context.Context, tx DBTX, chatroomID int64, limit, offset int) ([]*ChatroomMessageRow, int, error) {
	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chatroom_messages WHERE chatroom_id = ?`, chatroomID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := tx.QueryContext(ctx,
		chatroomMessageSelect+` WHERE cm.chatroom_id = ?
		 ORDER BY cm.created_at ASC, cm.id ASC LIMIT ? OFFSET ?`,
		chatroomID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]*ChatroomMessageRow, 0, limit)
	for rows.Next() {
		row, err := scanChatroomMessageRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// GetLast — последнее сообщение чата (для last_message вью).
func (r *ChatroomMessageRepo) GetLast(ctx context.Context, tx DBTX, chatroomID int64) (*ChatroomMessageRow, error) {
	return scanChatroomMessageRow(tx.QueryRowContext(ctx,
		chatroomMessageSelect+` WHERE cm.chatroom_id = ?
		 ORDER BY cm.created_at DESC, cm.id DESC LIMIT 1`, chatroomID))
}

func scanChatroomMessageRow(s teamScanner) (*ChatroomMessageRow, error) {
	var (
		m        models.ChatroomMessage
		fromID   sql.NullInt64
		fromName sql.NullString
		meta     string
		ca       string
	)
	err := s.Scan(&m.ID, &m.ChatroomID, &fromID, &m.Body, &meta, &ca, &fromName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if fromID.Valid {
		v := fromID.Int64
		m.FromRoleID = &v
	}
	m.Metadata = unmarshalConfig(meta)
	if m.CreatedAt, err = parseTime(ca); err != nil {
		return nil, fmt.Errorf("parse chatroom message created_at: %w", err)
	}
	row := &ChatroomMessageRow{ChatroomMessage: &m}
	if fromName.Valid {
		row.FromRoleName = &fromName.String
	}
	return row, nil
}

// time — используется для типов моделей (через models), здесь явно для ясности.
var _ = time.Now
