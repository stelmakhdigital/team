package models

import "time"

// P1.6 — Watchdog-джобы (register, context-usage-threshold).
// Джоб переживает рестарт (persisted); watchdog-loop проверяет enabled джобы на каждом скане.

// Policy v1 — context-usage-threshold: target-сессия жива и context_used_percentage
// >= threshold_pct → fire. Прочие политики — документированный follow-up.
const (
	WatchdogJobPolicyContextUsageThreshold = "context-usage-threshold"
)

// WatchdogJob — регистрируемый watchdog-джоб.
type WatchdogJob struct {
	ID              int64
	Name            string
	Policy          string // v1: context-usage-threshold
	TargetSessionID *int64 // для context-threshold обязательна (nullable FK)
	ThresholdPct    *int   // для context-threshold: 1..100
	Message         string
	Enabled         bool
	CreatedBy       string
	CreatedAt       time.Time
	FiredCount      int
	LastFiredAt     *time.Time
	LastFiredPct    *float64
}

// WatchdogJobEvent — история fire / dead-target (bounded; quiet skips не логируются).
type WatchdogJobEvent struct {
	ID        int64
	JobID     int64
	Outcome   string // fired | dead-target
	Detail    string
	Pct       *float64
	CreatedAt time.Time
}
