package models

import "time"

// WorkflowState — состояние workflow (контракт 20 §2.1).
type WorkflowState string

const (
	WorkflowDraft    WorkflowState = "draft"
	WorkflowActive   WorkflowState = "active"
	WorkflowArchived WorkflowState = "archived"
)

func (s WorkflowState) Valid() bool {
	switch s {
	case WorkflowDraft, WorkflowActive, WorkflowArchived:
		return true
	}
	return false
}

// WorkflowBlockType — тип блока (контракт 20 §2.1).
type WorkflowBlockType string

const (
	BlockTask     WorkflowBlockType = "task"
	BlockDecision WorkflowBlockType = "decision"
	BlockParallel WorkflowBlockType = "parallel"
	BlockLoop     WorkflowBlockType = "loop"
	BlockAgent    WorkflowBlockType = "agent"
	BlockManual   WorkflowBlockType = "manual"
)

func (t WorkflowBlockType) Valid() bool {
	switch t {
	case BlockTask, BlockDecision, BlockParallel, BlockLoop, BlockAgent, BlockManual:
		return true
	}
	return false
}

// Workflow — сценарий автоматизации для команды.
type Workflow struct {
	ID          int64
	TeamID      int64
	Name        string
	Description *string
	State       WorkflowState
	Config      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WorkflowBlock — узел графа workflow.
type WorkflowBlock struct {
	ID         int64
	WorkflowID int64
	Type       WorkflowBlockType
	PositionX  int
	PositionY  int
	Label      *string
	Config     map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WorkflowConnection — directed edge между блоками (condition — для decision).
type WorkflowConnection struct {
	ID          int64
	WorkflowID  int64
	FromBlockID int64
	ToBlockID   int64
	Condition   *string
	CreatedAt   time.Time
}
