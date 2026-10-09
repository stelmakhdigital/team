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
	ID            int64
	TeamID        int64
	Name          string
	Description   *string
	State         WorkflowState
	Config        map[string]any
	EngineEnabled bool // P1.4: исполняемый воркфлоу (false = «картинка», compat)
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ---------- P1.4: Workflow engine (исполняемые воркфлоу) ----------

// WorkflowInstanceState — состояние инстанса (запуска) воркфлоу.
type WorkflowInstanceState string

const (
	WfInstanceRunning   WorkflowInstanceState = "running"
	WfInstanceWaiting   WorkflowInstanceState = "waiting"
	WfInstanceCompleted WorkflowInstanceState = "completed"
	WfInstanceFailed    WorkflowInstanceState = "failed"
	WfInstanceAborted   WorkflowInstanceState = "aborted"
)

func (s WorkflowInstanceState) Valid() bool {
	switch s {
	case WfInstanceRunning, WfInstanceWaiting, WfInstanceCompleted, WfInstanceFailed, WfInstanceAborted:
		return true
	}
	return false
}

// TerminallyFinished — инстанс завершён (нельзя продлить).
func (s WorkflowInstanceState) Finished() bool {
	return s == WfInstanceCompleted || s == WfInstanceFailed || s == WfInstanceAborted
}

// WorkflowInstance — один запуск воркфлоу.
type WorkflowInstance struct {
	ID             int64
	WorkflowID     int64
	State          WorkflowInstanceState
	RootObjective  string
	CurrentBlockID *int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	FinishedAt     *time.Time
}

// WorkflowStepState — состояние шага (append-only trail).
type WorkflowStepState string

const (
	WfStepPending WorkflowStepState = "pending"
	WfStepActive  WorkflowStepState = "active"
	WfStepDone    WorkflowStepState = "done"
	WfStepFailed  WorkflowStepState = "failed"
	WfStepSkipped WorkflowStepState = "skipped"
	WfStepRouted  WorkflowStepState = "routed"
)

// WorkflowStepExitKind — причина выхода из шага (nullable).
type WorkflowStepExitKind string

const (
	WfExitHandoff WorkflowStepExitKind = "handoff"
	WfExitWaiting WorkflowStepExitKind = "waiting"
	WfExitDone    WorkflowStepExitKind = "done"
	WfExitFailed  WorkflowStepExitKind = "failed"
)

// WorkflowStep — шаг исполнения (append-only trail). TaskID — задача для
// task/agent-блока (nullable: control-блоки без задачи). Actor — целевая роль.
type WorkflowStep struct {
	ID         int64
	InstanceID int64
	BlockID    int64
	TaskID     *int64
	State      WorkflowStepState
	ExitKind   *WorkflowStepExitKind
	Actor      *int64
	ResultNote *string
	EnteredAt  *time.Time
	ExitedAt   *time.Time
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
