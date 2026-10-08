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
