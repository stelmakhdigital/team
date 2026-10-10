package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// P1.6 — Watchdog-джобы (register, context-usage-threshold). CRUD + валидация.

// RegisterWatchdogJobRequest — запрос регистрации джоба.
type RegisterWatchdogJobRequest struct {
	Name            string `json:"name"`
	Policy          string `json:"policy"`
	TargetSessionID *int64 `json:"target_session_id"`
	ThresholdPct    *int   `json:"threshold_pct"`
	Message         string `json:"message"`
}

// WatchdogJobView — джоб в списке.
type WatchdogJobView struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Policy          string   `json:"policy"`
	TargetSessionID *int64   `json:"target_session_id,omitempty"`
	TargetRoleName  string   `json:"target_role_name,omitempty"`
	ThresholdPct    *int     `json:"threshold_pct,omitempty"`
	Message         string   `json:"message"`
	Enabled         bool     `json:"enabled"`
	CreatedBy       string   `json:"created_by"`
	CreatedAt       string   `json:"created_at"`
	FiredCount      int      `json:"fired_count"`
	LastFiredAt     *string  `json:"last_fired_at,omitempty"`
	LastFiredPct    *float64 `json:"last_fired_pct,omitempty"`
}

// WatchdogJobEventView — одно событие (fire / dead-target).
type WatchdogJobEventView struct {
	Outcome   string   `json:"outcome"`
	Detail    string   `json:"detail"`
	Pct       *float64 `json:"pct,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// WatchdogJobDetail — джоб + recent evaluations (bounded).
type WatchdogJobDetail struct {
	WatchdogJobView
	Recent []WatchdogJobEventView `json:"recent_evaluations"`
}

// WatchdogJobService — CRUD регистрируемых watchdog-джобов.
type WatchdogJobService struct {
	db        *sql.DB
	jobs      *repository.WatchdogJobRepo
	jobEvents *repository.WatchdogJobEventRepo
	sessions  *repository.SessionRepo
	roles     *repository.RoleRepo
}

func NewWatchdogJobService(db *sql.DB, s *repository.Stores) *WatchdogJobService {
	return &WatchdogJobService{
		db: db, jobs: s.WatchdogJobs, jobEvents: s.WatchdogJobEvents,
		sessions: s.Sessions, roles: s.Roles,
	}
}

// Register — регистрация джоба: валидация policy-полей + существование сессии (404).
func (s *WatchdogJobService) Register(ctx context.Context, req *RegisterWatchdogJobRequest, createdBy string) (*WatchdogJobView, error) {
	if req == nil {
		return nil, NewValidation("request is required", []FieldError{{Field: "body", Reason: "required"}})
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, NewValidation("name is required", []FieldError{{Field: "name", Reason: "required"}})
	}
	policy := req.Policy
	if policy == "" {
		policy = models.WatchdogJobPolicyContextUsageThreshold
	}
	if policy != models.WatchdogJobPolicyContextUsageThreshold {
		return nil, NewValidation(fmt.Sprintf("unsupported policy %q (v1: %s)",
			policy, models.WatchdogJobPolicyContextUsageThreshold),
			[]FieldError{{Field: "policy", Reason: "only context-usage-threshold in v1"}})
	}
	// context-usage-threshold: target_session_id + threshold_pct обязательны
	if req.TargetSessionID == nil || *req.TargetSessionID <= 0 {
		return nil, NewValidation("target_session_id is required for context-usage-threshold",
			[]FieldError{{Field: "target_session_id", Reason: "required"}})
	}
	if req.ThresholdPct == nil || *req.ThresholdPct < 1 || *req.ThresholdPct > 100 {
		return nil, NewValidation("threshold_pct must be 1..100",
			[]FieldError{{Field: "threshold_pct", Reason: "must be 1..100"}})
	}
	// сессия должна существовать (404)
	if _, err := s.sessions.GetByID(ctx, s.db, *req.TargetSessionID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("session")
		}
		return nil, err
	}
	j := &models.WatchdogJob{
		Name: name, Policy: policy, TargetSessionID: req.TargetSessionID,
		ThresholdPct: req.ThresholdPct, Message: req.Message,
		Enabled: true, CreatedBy: createdBy,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.jobs.Create(ctx, tx, j); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.jobView(ctx, j)
}

// List — список джобов (all: все; иначе только enabled; limit).
func (s *WatchdogJobService) List(ctx context.Context, all bool, limit int) ([]*WatchdogJobView, error) {
	if limit <= 0 {
		limit = 100
	}
	jobs, err := s.jobs.List(ctx, s.db, all, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*WatchdogJobView, 0, len(jobs))
	for _, j := range jobs {
		view, err := s.jobView(ctx, j)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// Get — джоб + recent evaluations (bounded).
func (s *WatchdogJobService) Get(ctx context.Context, id int64) (*WatchdogJobDetail, error) {
	j, err := s.jobs.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("watchdog job")
		}
		return nil, err
	}
	view, err := s.jobView(ctx, j)
	if err != nil {
		return nil, err
	}
	events, err := s.jobEvents.ListByJob(ctx, s.db, id, 20)
	if err != nil {
		return nil, err
	}
	recent := make([]WatchdogJobEventView, 0, len(events))
	for _, e := range events {
		recent = append(recent, WatchdogJobEventView{
			Outcome: e.Outcome, Detail: e.Detail, Pct: e.Pct,
			CreatedAt: e.CreatedAt.Format(rfc3339),
		})
	}
	return &WatchdogJobDetail{WatchdogJobView: *view, Recent: recent}, nil
}

// Stop — оператор останавливает джоб (enabled=false; scheduler пропускает).
func (s *WatchdogJobService) Stop(ctx context.Context, id int64) (*WatchdogJobView, error) {
	j, err := s.jobs.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("watchdog job")
		}
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.jobs.SetEnabled(ctx, tx, id, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	j.Enabled = false
	return s.jobView(ctx, j)
}

// Delete — удаление джоба: если fired_count=0; иначе 409 (останься в истории).
func (s *WatchdogJobService) Delete(ctx context.Context, id int64) error {
	j, err := s.jobs.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return notFound("watchdog job")
		}
		return err
	}
	if j.FiredCount > 0 {
		return conflict(fmt.Sprintf("watchdog job %d has fired %d times — use stop instead", id, j.FiredCount))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.jobs.Delete(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// jobView — модель → view (+ target_role_name для EntityLink).
func (s *WatchdogJobService) jobView(ctx context.Context, j *models.WatchdogJob) (*WatchdogJobView, error) {
	v := &WatchdogJobView{
		ID: j.ID, Name: j.Name, Policy: j.Policy,
		TargetSessionID: j.TargetSessionID, ThresholdPct: j.ThresholdPct,
		Message: j.Message, Enabled: j.Enabled, CreatedBy: j.CreatedBy,
		CreatedAt:  j.CreatedAt.Format(rfc3339),
		FiredCount: j.FiredCount, LastFiredPct: j.LastFiredPct,
	}
	if j.LastFiredAt != nil {
		s := j.LastFiredAt.Format(rfc3339)
		v.LastFiredAt = &s
	}
	v.TargetRoleName = s.targetRoleName(ctx, j.TargetSessionID)
	return v, nil
}

// targetRoleName — role_name target-сессии (для отображения EntityLink).
func (s *WatchdogJobService) targetRoleName(ctx context.Context, sessionID *int64) string {
	if sessionID == nil {
		return ""
	}
	sess, err := s.sessions.GetByID(ctx, s.db, *sessionID)
	if err != nil {
		return ""
	}
	role, err := s.roles.GetByID(ctx, s.db, sess.RoleID)
	if err != nil {
		return ""
	}
	return role.Name
}
