package models

import "time"

// TaskState — состояние задачи (queue_tasks).
type TaskState string

const (
	TaskPending    TaskState = "pending"
	TaskInProgress TaskState = "in_progress"
	TaskDone       TaskState = "done"
	TaskBlocked    TaskState = "blocked"
	TaskCanceled   TaskState = "canceled"
)

func (s TaskState) Valid() bool {
	switch s {
	case TaskPending, TaskInProgress, TaskDone, TaskBlocked, TaskCanceled:
		return true
	}
	return false
}

// Терминальные состояния: переходы из них невозможны.
func (s TaskState) Terminal() bool { return s == TaskDone || s == TaskCanceled }

// AllowedTransitions — допустимые переходы состояний.
var AllowedTransitions = map[TaskState][]TaskState{
	TaskPending:    {TaskInProgress, TaskDone, TaskBlocked, TaskCanceled},
	TaskInProgress: {TaskDone, TaskBlocked, TaskCanceled},
	TaskBlocked:    {TaskPending, TaskInProgress, TaskDone, TaskCanceled},
}

func CanTransition(from, to TaskState) bool {
	if from.Terminal() {
		return false
	}
	for _, t := range AllowedTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// ClosureReason — причина завершения задачи.
type ClosureReason string

const (
	ClosureHandedOff  ClosureReason = "handed_off_to"
	ClosureBlockedOn  ClosureReason = "blocked_on"
	ClosureDenied     ClosureReason = "denied"
	ClosureCanceled   ClosureReason = "canceled"
	ClosureNoFollowOn ClosureReason = "no_follow_on"
	ClosureEscalation ClosureReason = "escalation"
)

func (c ClosureReason) Valid() bool {
	switch c {
	case ClosureHandedOff, ClosureBlockedOn, ClosureDenied, ClosureCanceled, ClosureNoFollowOn, ClosureEscalation:
		return true
	}
	return false
}

// Task — задача в очереди.
type Task struct {
	ID                int64
	TeamID            int64
	ProjectID         *int64 // P1.0: свойство работы (наследуется от team.project или override)
	ParentTaskID      *int64
	DestinationRoleID int64
	SourceRoleID      *int64
	Title             string
	Body              string
	BodyContext       map[string]any
	State             TaskState
	ClosureReason     *ClosureReason
	ClosureTargetID   *int64
	BlockedSince      *time.Time
	ParkTimeoutSecs   *int
	Priority          int
	EstimatedSecs     *int
	ActualSecs        *int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         *time.Time
	CompletedAt       *time.Time
	ClosureRequiredAt *time.Time // P0.2: дедлайн закрытия (в in_progress = now + tasks.closure_required_secs)
}

// HistoryEntry — append-only запись о переходе состояния задачи.
type HistoryEntry struct {
	ID              int64
	QueueTaskID     int64
	FromState       *string
	ToState         string
	ClosureReason   *string
	ClosureTargetID *int64
	ActorRoleID     *int64
	ActorType       string // role | daemon | watchdog | human
	Comment         *string
	Metadata        map[string]any
	CreatedAt       time.Time
}

// TaskWake — P0.3: причина-триггер для разблокировки заблокированной задачи.
// kind: timer | task. Вид watchdog запланирован (P1.6), в коде пока не реализуется.
// Ref: kind=task → task_id (string) referenced-задачи; kind=timer → fire-at (RFC3339Nano).
// FiredAt: закрытие wake (fired ИЛИ resolve) — идемпотентность.
type TaskWake struct {
	ID           int64
	TaskID       int64
	Kind         string // timer | task (watchdog: planned)
	Ref          string
	Continuation string
	CreatedAt    time.Time
	FiredAt      *time.Time
}

const (
	WakeKindTimer    = "timer"
	WakeKindTask     = "task"
	WakeKindWatchdog = "watchdog" // P1.6: будит, когда сработал watchdog-джоб (ref = job id)
)
