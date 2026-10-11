package models

import "time"

// ---------- P3.14: Walk (paced-доставка контекста в сессию) ----------

// WalkMode — режим pacing.
type WalkMode string

const (
	WalkModeInterval WalkMode = "interval" // авто-advance каждые interval_secs (reaper-loop)
	WalkModeManual   WalkMode = "manual"   // advance вручную
)

func (m WalkMode) Valid() bool {
	switch m {
	case WalkModeInterval, WalkModeManual:
		return true
	}
	return false
}

// WalkStatus — состояние walk'а.
type WalkStatus string

const (
	WalkActive    WalkStatus = "active"
	WalkPaused    WalkStatus = "paused"    // guard (retained) — нужен человек
	WalkCompleted WalkStatus = "completed" // все шаги отправлены
	WalkFailed    WalkStatus = "failed"    // Send-ошибка (нет экрана/мёртв)
)

func (s WalkStatus) Valid() bool {
	switch s {
	case WalkActive, WalkPaused, WalkCompleted, WalkFailed:
		return true
	}
	return false
}

// WalkStepState — состояние шага.
type WalkStepState string

const (
	WalkStepPending   WalkStepState = "pending"
	WalkStepSent      WalkStepState = "sent"
	WalkStepDelivered WalkStepState = "delivered"
	WalkStepStaged    WalkStepState = "staged"
	WalkStepRetained  WalkStepState = "retained" // guard: интерактивный prompt
	WalkStepFailed    WalkStepState = "failed"
	WalkStepSkipped   WalkStepState = "skipped"
)

func (s WalkStepState) Valid() bool {
	switch s {
	case WalkStepPending, WalkStepSent, WalkStepDelivered, WalkStepStaged,
		WalkStepRetained, WalkStepFailed, WalkStepSkipped:
		return true
	}
	return false
}

// Walk — именованная упорядоченная последовательность шагов в сессию.
type Walk struct {
	ID           int64
	SessionID    int64
	Name         string
	Description  *string
	Mode         WalkMode
	IntervalSecs *int
	Status       WalkStatus
	CurrentSeq   int
	LastSentAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// WalkStep — один текстовый шаг walk'а.
type WalkStep struct {
	ID         int64
	WalkID     int64
	Seq        int
	Content    string
	State      WalkStepState
	SendStatus *string
	Note       *string
	SentAt     *time.Time
	CreatedAt  time.Time
}
