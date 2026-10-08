package models

import "time"

// LibraryItemType — тип элемента библиотеки (контракт 20 §5).
type LibraryItemType string

const (
	LibTeam     LibraryItemType = "team"
	LibWorkflow LibraryItemType = "workflow"
	LibRole     LibraryItemType = "role"
	LibSegment  LibraryItemType = "segment"
)

func (t LibraryItemType) Valid() bool {
	switch t {
	case LibTeam, LibWorkflow, LibRole, LibSegment:
		return true
	}
	return false
}

// LibraryItem — элемент библиотеки с полным spec (JSON).
type LibraryItem struct {
	ID             int64
	Type           LibraryItemType
	Name           string
	Description    *string
	Group          string
	Version        string
	Author         *string
	IsPublic       bool
	DownloadsCount int
	Tags           []string
	Thumbnail      *string
	SourceType     *string // "team", "workflow", ...
	SourceID       *int64
	Spec           map[string]any
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LibraryVersion — версия элемента (append-only).
type LibraryVersion struct {
	ID        int64
	ItemID    int64
	Version   string
	Changes   *string
	Author    *string
	CreatedAt time.Time
}

// AuditEntry — запись audit log (контракт 20 §6.3).
type AuditEntry struct {
	ID        int64
	UserID    *int64
	UserName  *string
	APIKeyID  *int64
	Action    string
	Resource  *string
	Details   map[string]any
	IPAddress *string
	UserAgent *string
	CreatedAt time.Time
}
