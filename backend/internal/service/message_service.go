package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// roleLister — часть RoleRepo, нужна MessageService (сверх roleStore).
type roleLister interface {
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Role, error)
	ListByTeam(ctx context.Context, tx repository.DBTX, teamID int64) ([]*models.Role, error)
	ListBySegment(ctx context.Context, tx repository.DBTX, segmentID int64) ([]*models.Role, error)
}

// MessageService — Message Center: сообщения между ролями + chatrooms (контракт 20 §4).
type MessageService struct {
	db       *sql.DB
	teams    teamStore
	roles    roleLister
	tasks    taskStore
	segments segmentStore
	msgs     *repository.MessageRepo
	rooms    *repository.ChatroomRepo
	roomMsgs *repository.ChatroomMessageRepo
	// Bus — real-time события (message.sent); nil = события не публикуются.
	Bus *EventBus
}

func NewMessageService(db *sql.DB, s *repository.Stores) *MessageService {
	return &MessageService{
		db: db, teams: s.Teams, roles: s.Roles, tasks: s.Tasks, segments: s.Segments,
		msgs: s.Messages, rooms: s.Chatrooms, roomMsgs: s.ChatroomMessages,
	}
}

// ---------- Views (контракт 20 §4) ----------

// MessageView — сообщение с именами ролей (контракт: Message).
type MessageView struct {
	ID            int64   `json:"id"`
	TeamID        int64   `json:"team_id"`
	QueueTaskID   *int64  `json:"queue_task_id,omitempty"`
	FromRoleID    *int64  `json:"from_role_id,omitempty"`
	FromRoleName  *string `json:"from_role_name,omitempty"`
	ToRoleID      *int64  `json:"to_role_id,omitempty"`
	ToRoleName    *string `json:"to_role_name,omitempty"`
	Type          string  `json:"type"`
	Body          string  `json:"body"`
	IsRead        bool    `json:"is_read"`
	CreatedAt     string  `json:"created_at"`
	IsMine        bool    `json:"is_mine"`
	RequiresReply bool    `json:"requires_reply"`
}

type chatroomLastMessage struct {
	Body         string `json:"body"`
	FromRoleName string `json:"from_role_name"`
	CreatedAt    string `json:"created_at"`
}

// ChatroomView — чатroom с last_message/counts (контракт: Chatroom).
type ChatroomView struct {
	ID           int64                `json:"id"`
	TeamID       int64                `json:"team_id"`
	SegmentID    *int64               `json:"segment_id,omitempty"`
	Name         string               `json:"name"`
	Topic        *string              `json:"topic,omitempty"`
	LastMessage  *chatroomLastMessage `json:"last_message,omitempty"`
	UnreadCount  int                  `json:"unread_count"`
	MembersCount int                  `json:"members_count"`
}

// ChatroomMessageView — сообщение чата (контракт: ChatroomMessage).
type ChatroomMessageView struct {
	ID           int64  `json:"id"`
	ChatroomID   int64  `json:"chatroom_id"`
	FromRoleID   *int64 `json:"from_role_id,omitempty"`
	FromRoleName string `json:"from_role_name"` // оператор без роли → "You"
	Body         string `json:"body"`
	CreatedAt    string `json:"created_at"`
	IsMine       bool   `json:"is_mine"`
}

// senderName — NULL-отправитель = оператор («You», как в mock-фронте).
func senderName(roleName *string) string {
	if roleName != nil && *roleName != "" {
		return *roleName
	}
	return "You"
}

func (s *MessageService) messageView(row *repository.MessageRow) *MessageView {
	m := row.Message
	fromName := row.FromRoleName
	if m.FromRoleID == nil {
		you := "You" // оператор: консистентно с chatrooms (наблюдение 1, answer_backend.md)
		fromName = &you
	}
	return &MessageView{
		ID: m.ID, TeamID: m.TeamID, QueueTaskID: m.QueueTaskID,
		FromRoleID: m.FromRoleID, FromRoleName: fromName,
		ToRoleID: m.ToRoleID, ToRoleName: row.ToRoleName,
		Type: string(m.Type), Body: m.Body, IsRead: m.IsRead,
		CreatedAt: m.CreatedAt.Format(rfc3339),
		IsMine:    m.FromRoleID == nil, RequiresReply: false,
	}
}

// ---------- Messages ----------

type SendMessageRequest struct {
	TeamID      int64          `json:"team_id"`
	QueueTaskID *int64         `json:"queue_task_id"`
	FromRoleID  *int64         `json:"from_role_id"` // опционально (расширение контракта: агент-отправитель)
	ToRoleID    *int64         `json:"to_role_id"`
	Type        string         `json:"type"`
	Body        string         `json:"body"`
	Metadata    map[string]any `json:"metadata"`
}

type SendMessageResult struct {
	ID          int64   `json:"id"`
	Status      string  `json:"status"`
	DeliveredTo []int64 `json:"delivered_to"`
}

// SendMessage — POST /api/v1/messages (контракт 20 §4.2).
// Доставка: direct → to_role_id; broadcast → все роли команды;
// segment → все роли сегмента to_role_id (контракт без segment_id — см. api-decisions).
func (s *MessageService) SendMessage(ctx context.Context, req SendMessageRequest) (*SendMessageResult, error) {
	team, err := s.requireActiveTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}

	t := models.MessageType(req.Type)
	if !t.Public() {
		return nil, NewValidation(fmt.Sprintf("invalid message type %q (allowed: direct|broadcast|segment)", req.Type))
	}
	if req.Body == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "body", Reason: "required"}})
	}

	var fromRole, toRole *models.Role
	if req.FromRoleID != nil {
		fromRole, err = s.requireRoleInTeam(ctx, *req.FromRoleID, team.ID)
		if err != nil {
			return nil, err
		}
	}
	if req.ToRoleID != nil {
		toRole, err = s.requireRoleInTeam(ctx, *req.ToRoleID, team.ID)
		if err != nil {
			return nil, err
		}
	}

	var delivered []int64
	switch t {
	case models.MsgDirect:
		if toRole == nil {
			return nil, NewValidation("invalid request", []FieldError{{Field: "to_role_id", Reason: "required for direct message"}})
		}
		delivered = []int64{toRole.ID}
	case models.MsgBroadcast:
		delivered, err = s.roleIDsByTeam(ctx, team.ID)
		if err != nil {
			return nil, err
		}
	case models.MsgSegment:
		if toRole == nil {
			return nil, NewValidation("invalid request", []FieldError{{Field: "to_role_id", Reason: "required for segment message (defines the segment)"}})
		}
		roles, err := s.roles.ListBySegment(ctx, s.db, toRole.SegmentID)
		if err != nil {
			return nil, err
		}
		delivered = make([]int64, 0, len(roles))
		for _, r := range roles {
			delivered = append(delivered, r.ID)
		}
	}

	if req.QueueTaskID != nil {
		task, err := s.tasks.GetByID(ctx, s.db, *req.QueueTaskID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, notFound("task")
			}
			return nil, err
		}
		if task.TeamID != team.ID {
			return nil, NewValidation(fmt.Sprintf("task %d belongs to another team", task.ID))
		}
	}

	msg := &models.Message{
		TeamID: team.ID, QueueTaskID: req.QueueTaskID,
		FromRoleID: req.FromRoleID, ToRoleID: req.ToRoleID,
		Type: t, Body: req.Body, Metadata: req.Metadata,
	}
	if err := s.msgs.Create(ctx, s.db, msg); err != nil {
		return nil, err
	}

	channels := []string{fmt.Sprintf("team:%d", team.ID)}
	if msg.QueueTaskID != nil {
		channels = append(channels, fmt.Sprintf("task:%d", *msg.QueueTaskID))
	}
	s.Bus.Publish(newEvent("message.sent", map[string]any{
		"message_id":     msg.ID,
		"team_id":        team.ID,
		"from_role_name": senderName(nilOrName(fromRole)),
		"to_role_name":   nilOrName(toRole),
		"body":           msg.Body,
		"type":           string(t),
	}, channels...))

	if delivered == nil {
		delivered = []int64{}
	}
	return &SendMessageResult{ID: msg.ID, Status: "sent", DeliveredTo: delivered}, nil
}

func nilOrName(r *models.Role) *string {
	if r == nil {
		return nil
	}
	return &r.Name
}

// ListMessages — GET /api/v1/messages (контракт 20 §4.1).
func (s *MessageService) ListMessages(ctx context.Context, f repository.MessageFilter) ([]*MessageView, int, error) {
	rows, total, err := s.msgs.List(ctx, s.db, f)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*MessageView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.messageView(row))
	}
	return out, total, nil
}

func (s *MessageService) roleIDsByTeam(ctx context.Context, teamID int64) ([]int64, error) {
	roles, err := s.roles.ListByTeam(ctx, s.db, teamID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// ---------- Chatrooms ----------

// ListChatrooms — GET /api/v1/chatrooms (контракт 20 §4.3).
// unread_count — реальный для user (slice 6, DB-ключ); без user → 0.
// members_count = роли команды/сегмента.
func (s *MessageService) ListChatrooms(ctx context.Context, teamID *int64, userID *int64) ([]*ChatroomView, error) {
	rooms, err := s.rooms.List(ctx, s.db, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]*ChatroomView, 0, len(rooms))
	for _, room := range rooms {
		view := &ChatroomView{
			ID: room.ID, TeamID: room.TeamID, SegmentID: room.SegmentID,
			Name: room.Name, Topic: room.Topic,
			UnreadCount: 0,
		}
		if room.SegmentID != nil {
			if n, err := s.segments.CountRoles(ctx, s.db, *room.SegmentID); err == nil {
				view.MembersCount = n
			}
		} else if n, err := s.roles.ListByTeam(ctx, s.db, room.TeamID); err == nil {
			view.MembersCount = len(n)
		}
		if last, err := s.roomMsgs.GetLast(ctx, s.db, room.ID); err == nil && last != nil {
			cm := last.ChatroomMessage
			view.LastMessage = &chatroomLastMessage{
				Body:         cm.Body,
				FromRoleName: senderName(last.FromRoleName),
				CreatedAt:    cm.CreatedAt.Format(rfc3339),
			}
		}
		// Slice 6: unread_count по user (RBAC): сообщения с created_at > last_read_at
		// (нет read-записи → все сообщения непрочитаны).
		if userID != nil {
			if n, err := s.unreadCount(ctx, *userID, room.ID); err == nil {
				view.UnreadCount = n
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// unreadCount — непрочитанные сообщения чата для user (id > last_read_id).
func (s *MessageService) unreadCount(ctx context.Context, userID, chatroomID int64) (int, error) {
	var lastReadID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT last_read_id FROM chatroom_reads WHERE user_id = ? AND chatroom_id = ?`,
		userID, chatroomID).Scan(&lastReadID)
	var n int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chatroom_messages WHERE chatroom_id = ? AND id > ?`,
		chatroomID, lastReadID.Int64).Scan(&n)
	return n, err
}

// markChatroomRead — user прочитал чат (last_read_id = max(id) сообщений).
func (s *MessageService) markChatroomRead(ctx context.Context, userID, chatroomID int64) error {
	var lastID sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(id) FROM chatroom_messages WHERE chatroom_id = ?`, chatroomID).Scan(&lastID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chatroom_reads WHERE user_id = ? AND chatroom_id = ?`,
		userID, chatroomID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO chatroom_reads (user_id, chatroom_id, last_read_id, last_read_at) VALUES (?, ?, ?, ?)`,
			userID, chatroomID, lastID.Int64, now)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE chatroom_reads SET last_read_id = ?, last_read_at = ? WHERE user_id = ? AND chatroom_id = ?`,
		lastID.Int64, now, userID, chatroomID)
	return err
}

// GetChatroomMessages — GET /api/v1/chatrooms/{id}/messages (контракт 20 §4.3).
// Slice 6: для аутентифицированного user чтение помечает чат прочитанным.
func (s *MessageService) GetChatroomMessages(ctx context.Context, chatroomID int64, limit, offset int, userID *int64) ([]*ChatroomMessageView, int, error) {
	if _, err := s.rooms.GetByID(ctx, s.db, chatroomID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, 0, notFound("chatroom")
		}
		return nil, 0, err
	}
	rows, total, err := s.roomMsgs.List(ctx, s.db, chatroomID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if userID != nil {
		_ = s.markChatroomRead(ctx, *userID, chatroomID)
	}
	if err != nil {
		return nil, 0, err
	}
	out := make([]*ChatroomMessageView, 0, len(rows))
	for _, row := range rows {
		cm := row.ChatroomMessage
		out = append(out, &ChatroomMessageView{
			ID: cm.ID, ChatroomID: cm.ChatroomID, FromRoleID: cm.FromRoleID,
			FromRoleName: senderName(row.FromRoleName), Body: cm.Body,
			CreatedAt: cm.CreatedAt.Format(rfc3339),
			IsMine:    cm.FromRoleID == nil,
		})
	}
	return out, total, nil
}

// SendChatroomMessage — POST /api/v1/chatrooms/{id}/messages (контракт 20 §4.3).
func (s *MessageService) SendChatroomMessage(ctx context.Context, chatroomID int64, req struct {
	Body       string `json:"body"`
	FromRoleID *int64 `json:"from_role_id"` // опционально (расширение контракта)
}) (*ChatroomMessageView, error) {
	room, err := s.rooms.GetByID(ctx, s.db, chatroomID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("chatroom")
		}
		return nil, err
	}
	if req.Body == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "body", Reason: "required"}})
	}
	var fromRole *models.Role
	if req.FromRoleID != nil {
		fromRole, err = s.requireRoleInTeam(ctx, *req.FromRoleID, room.TeamID)
		if err != nil {
			return nil, err
		}
	}

	cm := &models.ChatroomMessage{ChatroomID: room.ID, FromRoleID: req.FromRoleID, Body: req.Body}
	if err := s.roomMsgs.Create(ctx, s.db, cm); err != nil {
		return nil, err
	}
	s.Bus.Publish(newEvent("message.sent", map[string]any{
		"message_id":     cm.ID,
		"chatroom_id":    room.ID,
		"team_id":        room.TeamID,
		"from_role_name": senderName(nilOrName(fromRole)),
		"body":           cm.Body,
		"type":           "chatroom",
	}, fmt.Sprintf("team:%d", room.TeamID), fmt.Sprintf("chatroom:%d", room.ID)))

	view := &ChatroomMessageView{
		ID: cm.ID, ChatroomID: cm.ChatroomID, FromRoleID: cm.FromRoleID,
		FromRoleName: senderName(nilOrName(fromRole)), Body: cm.Body,
		CreatedAt: cm.CreatedAt.Format(rfc3339), IsMine: cm.FromRoleID == nil,
	}
	return view, nil
}

// ---------- Helpers ----------

func (s *MessageService) requireActiveTeam(ctx context.Context, teamID int64) (*models.Team, error) {
	team, err := s.teams.GetByID(ctx, s.db, teamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("team")
		}
		return nil, err
	}
	if team.State != models.TeamActive {
		return nil, NewConflict(fmt.Sprintf("team %q is %s", team.Name, team.State))
	}
	return team, nil
}

func (s *MessageService) requireRoleInTeam(ctx context.Context, roleID, teamID int64) (*models.Role, error) {
	role, err := s.roles.GetByID(ctx, s.db, roleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	if role.TeamID != teamID {
		return nil, NewValidation(fmt.Sprintf("role %d does not belong to team %d", roleID, teamID))
	}
	return role, nil
}
