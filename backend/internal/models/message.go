package models

import "time"

// MessageType — тип сообщения (контракт 20 §4).
type MessageType string

const (
	MsgDirect    MessageType = "direct"    // конкретному получателю
	MsgBroadcast MessageType = "broadcast" // всем в team
	MsgSegment   MessageType = "segment"   // всем в segment
	MsgSystem    MessageType = "system"    // системное уведомление (серверное)
	MsgWatchdog  MessageType = "watchdog"  // от watchdog (серверное)
)

// PublicTypes — типы, доступные из публичного API (system/watchdog создаёт только daemon).
var PublicTypes = []MessageType{MsgDirect, MsgBroadcast, MsgSegment}

func (t MessageType) Valid() bool {
	switch t {
	case MsgDirect, MsgBroadcast, MsgSegment, MsgSystem, MsgWatchdog:
		return true
	}
	return false
}

func (t MessageType) Public() bool {
	switch t {
	case MsgDirect, MsgBroadcast, MsgSegment:
		return true
	}
	return false
}

// Message — сообщение между ролями (без обязательств, в отличие от задач).
type Message struct {
	ID          int64
	TeamID      int64
	QueueTaskID *int64
	FromRoleID  *int64 // NULL = отправлено оператором («You»)
	ToRoleID    *int64
	Type        MessageType
	Body        string
	Metadata    map[string]any
	IsRead      bool
	CreatedAt   time.Time
	// P3.15: mailbox-семантика (inbox/outbox) — подтверждение получения.
	// orthogonal к IsRead: read = прочитано в ленте; ack_state = mailbox-подтверждение.
	AckState AckState
	AckedBy  *string
	AckedAt  *time.Time
}

// AckState — состояние mailbox-подтверждения входящего (P3.15).
type AckState string

const (
	AckPending  AckState = "pending"  // неподтверждённое входящее (undelivered)
	AckAbsorbed AckState = "absorbed" // принято/обработано
	AckDenied   AckState = "denied"   // отказ
	AckDropped  AckState = "dropped"  // сброшено
)

func (s AckState) Valid() bool {
	switch s {
	case AckPending, AckAbsorbed, AckDenied, AckDropped:
		return true
	}
	return false
}

// AckAction — действие судьи по входящему (absorb/deny/drop).
type AckAction string

const (
	AckAbsorb AckAction = "absorb"
	AckDeny   AckAction = "deny"
	AckDrop   AckAction = "drop"
)

func (a AckAction) Valid() bool {
	switch a {
	case AckAbsorb, AckDeny, AckDrop:
		return true
	}
	return false
}

// State — итоговое AckState после действия.
func (a AckAction) State() AckState {
	switch a {
	case AckAbsorb:
		return AckAbsorbed
	case AckDeny:
		return AckDenied
	case AckDrop:
		return AckDropped
	}
	return AckPending
}

// Chatroom — общий чат команды или сегмента.
type Chatroom struct {
	ID        int64
	TeamID    int64
	SegmentID *int64 // NULL = чат на уровне команды
	Name      string
	Topic     *string
	Config    map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChatroomMessage — сообщение в общем чате.
type ChatroomMessage struct {
	ID         int64
	ChatroomID int64
	FromRoleID *int64 // NULL = отправлено оператором («You»)
	Body       string
	Metadata   map[string]any
	CreatedAt  time.Time
}
