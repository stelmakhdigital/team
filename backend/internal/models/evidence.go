package models

import "time"

// EvidenceType — тип артефакта-доказательства (P3.13).
type EvidenceType string

const (
	EvidenceScreenshot EvidenceType = "screenshot"
	EvidenceTest       EvidenceType = "test"
	EvidenceLog        EvidenceType = "log"
	EvidenceOther      EvidenceType = "other"
)

func (t EvidenceType) Valid() bool {
	switch t {
	case EvidenceScreenshot, EvidenceTest, EvidenceLog, EvidenceOther:
		return true
	}
	return false
}

// EvidenceStatus — статус артефакта (жизненный цикл: submitted → verdict).
// «Closure ≠ acceptance»: это verdict по артефакту, НЕ state задачи.
type EvidenceStatus string

const (
	EvidenceSubmitted EvidenceStatus = "submitted"
	EvidenceAccepted  EvidenceStatus = "accepted"
	EvidenceRejected  EvidenceStatus = "rejected"
	EvidenceWithdrawn EvidenceStatus = "withdrawn"
)

func (s EvidenceStatus) Valid() bool {
	switch s {
	case EvidenceSubmitted, EvidenceAccepted, EvidenceRejected, EvidenceWithdrawn:
		return true
	}
	return false
}

// EvidenceAction — действие судьи (judge).
type EvidenceAction string

const (
	EvidenceAccept   EvidenceAction = "accept"
	EvidenceReject   EvidenceAction = "reject"
	EvidenceWithdraw EvidenceAction = "withdraw"
)

func (a EvidenceAction) Valid() bool {
	switch a {
	case EvidenceAccept, EvidenceReject, EvidenceWithdraw:
		return true
	}
	return false
}

// VerdictStatus — статус, которому соответствует действие судьи.
func (a EvidenceAction) VerdictStatus() EvidenceStatus {
	switch a {
	case EvidenceAccept:
		return EvidenceAccepted
	case EvidenceReject:
		return EvidenceRejected
	case EvidenceWithdraw:
		return EvidenceWithdrawn
	}
	return EvidenceSubmitted
}

// TaskEvidence — артефакт-доказательство, привязанный к задаче (P3.13).
// content_sha = candidate-sha (целостность): inline → серверный sha256 контента;
// content_ref (внешняя ссылка/путь) → sha, принятый из body.
type TaskEvidence struct {
	ID            int64
	TaskID        int64
	Type          EvidenceType
	Name          string
	ContentRef    *string
	ContentSHA    *string
	Description   *string
	Status        EvidenceStatus
	VerdictReason *string
	SubmittedBy   string
	JudgedBy      *string
	JudgedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (e *TaskEvidence) Valid() bool {
	return e != nil && e.Type.Valid() && e.Name != "" && e.Status.Valid()
}
