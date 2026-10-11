package models

import "time"

// ---------- P3.16: Mode/permission posture (delegated vs human-led) ----------

// Posture — режим команды/сессии. delegated = авто (без подтверждения);
// human_led = чувствительные операции паркуются → human_request (approve/deny).
type Posture string

const (
	PostureDelegated Posture = "delegated"
	PostureHumanLed  Posture = "human_led"
)

func (p Posture) Valid() bool {
	switch p {
	case PostureDelegated, PostureHumanLed:
		return true
	}
	return false
}

// HumanRequestStatus — статус паркованной операции.
type HumanRequestStatus string

const (
	HRPending  HumanRequestStatus = "pending"
	HRApproved HumanRequestStatus = "approved"
	HRDenied   HumanRequestStatus = "denied"
	HRReplayed HumanRequestStatus = "replayed"
	HRExpired  HumanRequestStatus = "expired"
)

func (s HumanRequestStatus) Valid() bool {
	switch s {
	case HRPending, HRApproved, HRDenied, HRReplayed, HRExpired:
		return true
	}
	return false
}

// HumanRequest — паркованная чувствительная операция (human-led posture).
// approve → replay оригинального запроса (один раз); deny → без replay.
type HumanRequest struct {
	ID             int64
	ScopeType      string
	ScopeID        *int64
	TeamID         *int64
	Method         string
	Path           string
	Body           *string
	AuthKey        *string
	Operation      string
	Reversible     bool
	Status         HumanRequestStatus
	DecisionReason *string
	DecidedBy      *string
	DecidedAt      *time.Time
	ReplayStatus   *int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
