package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ---------- P3.14: Walk (paced-доставка контекста в сессию) ----------
//
// Walk — именованная упорядоченная последовательность текстовых шагов,
// доставляемых в сессию по одному через Send (P2.11). Pacing = interval
// (auto-advance из reaper-loop, не чаще poll) или manual (вручную).
// retained (guard) → walk paused (нужен человек); Send-ошибка → walk failed.

// Sender — интерфейс на SessionService.Send (тестируемость: mock в тестах).
type Sender interface {
	Send(ctx context.Context, sessionID int64, text string, submit bool) (*SendToSessionResult, error)
}

// WalkClock — injectable-часы (AdvanceDue, тесты).
type WalkClock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// WalkService — pacing-доставка шагов в сессию.
type WalkService struct {
	db       *sql.DB
	walks    *repository.WalkRepo
	sessions *repository.SessionRepo
	sender   Sender
	clock    WalkClock
	bus      *EventBus
}

// NewWalkService — конструктор. sender реализует *SessionService.
func NewWalkService(db *sql.DB, stores *repository.Stores, sender Sender) *WalkService {
	return &WalkService{
		db: db, walks: stores.Walks, sessions: stores.Sessions,
		sender: sender, clock: realClock{},
	}
}

// SetClock — инъекция часов (тесты AdvanceDue).
func (s *WalkService) SetClock(c WalkClock) {
	if c != nil {
		s.clock = c
	}
}

// SetBus — EventBus (Pulse: walk.step_sent / walk.completed), nil-safe.
func (s *WalkService) SetBus(b *EventBus) { s.bus = b }

// ---------- Views ----------

// WalkStepView — шаг в формате контракта.
type WalkStepView struct {
	ID         int64   `json:"id"`
	Seq        int     `json:"seq"`
	Content    string  `json:"content"`
	State      string  `json:"state"`
	SendStatus *string `json:"send_status,omitempty"`
	Note       *string `json:"note,omitempty"`
	SentAt     *string `json:"sent_at,omitempty"`
}

// WalkView — walk в формате контракта (с progress; steps — только в GetWalk).
type WalkView struct {
	ID           int64           `json:"id"`
	SessionID    int64           `json:"session_id"`
	Name         string          `json:"name"`
	Description  *string         `json:"description,omitempty"`
	Mode         string          `json:"mode"`
	IntervalSecs *int            `json:"interval_secs,omitempty"`
	Status       string          `json:"status"`
	CurrentSeq   int             `json:"current_seq"`
	LastSentAt   *string         `json:"last_sent_at,omitempty"`
	Progress     string          `json:"progress"`
	SentCount    int             `json:"sent_count"`
	Total        int             `json:"total"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
	Steps        []*WalkStepView `json:"steps,omitempty"`
}

// CreateWalkRequest — тело POST /sessions/{id}/walks.
type CreateWalkRequest struct {
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	Mode         string   `json:"mode"`
	IntervalSecs *int     `json:"interval_secs,omitempty"`
	Steps        []string `json:"steps"`
}

func walkStepView(st *models.WalkStep) *WalkStepView {
	v := &WalkStepView{
		ID: st.ID, Seq: st.Seq, Content: st.Content,
		State: string(st.State), SendStatus: st.SendStatus, Note: st.Note,
	}
	if st.SentAt != nil {
		v.SentAt = ptr(st.SentAt.Format(rfc3339))
	}
	return v
}

func (s *WalkService) toView(w *models.Walk, steps []*models.WalkStep) *WalkView {
	v := &WalkView{
		ID: w.ID, SessionID: w.SessionID, Name: w.Name, Description: w.Description,
		Mode: string(w.Mode), IntervalSecs: w.IntervalSecs, Status: string(w.Status),
		CurrentSeq: w.CurrentSeq,
		CreatedAt:  w.CreatedAt.Format(rfc3339), UpdatedAt: w.UpdatedAt.Format(rfc3339),
		Progress: "0/0", SentCount: 0, Total: 0,
	}
	if w.LastSentAt != nil {
		v.LastSentAt = ptr(w.LastSentAt.Format(rfc3339))
	}
	if steps != nil {
		v.Total = len(steps)
		stepViews := make([]*WalkStepView, 0, len(steps))
		for _, st := range steps {
			stepViews = append(stepViews, walkStepView(st))
			// «отправлено» = Send завершён delivery-статусом (включая retained — попытка).
			switch st.State {
			case models.WalkStepSent, models.WalkStepDelivered,
				models.WalkStepStaged, models.WalkStepRetained:
				v.SentCount++
			}
		}
		v.Progress = fmt.Sprintf("%d/%d", v.SentCount, v.Total)
		v.Steps = stepViews
	}
	return v
}

// ---------- Getters ----------

func (s *WalkService) getSession(ctx context.Context, id int64) (*models.Session, error) {
	sess, err := s.sessions.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("session")
		}
		return nil, err
	}
	return sess, nil
}

func (s *WalkService) getWalk(ctx context.Context, id int64) (*models.Walk, error) {
	w, err := s.walks.GetWalk(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("walk")
		}
		return nil, err
	}
	return w, nil
}

// GetWalk — walk + steps + progress.
func (s *WalkService) GetWalk(ctx context.Context, walkID int64) (*WalkView, error) {
	w, err := s.getWalk(ctx, walkID)
	if err != nil {
		return nil, err
	}
	steps, err := s.walks.ListStepsByWalk(ctx, s.db, walkID)
	if err != nil {
		return nil, err
	}
	return s.toView(w, steps), nil
}

// ListWalks — walks сессии (без steps — деталь в GetWalk). 404 (нет сессии).
func (s *WalkService) ListWalks(ctx context.Context, sessionID int64) ([]*WalkView, error) {
	if _, err := s.getSession(ctx, sessionID); err != nil {
		return nil, err
	}
	walks, err := s.walks.ListWalksBySession(ctx, s.db, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]*WalkView, 0, len(walks))
	for _, w := range walks {
		out = append(out, s.toView(w, nil))
	}
	return out, nil
}

// ---------- Create ----------

// CreateWalk — создать walk + шаги. Валидация: session (404), ≥1 шаг, mode valid,
// interval_secs>0 если interval, content непустой ≤ sendMaxTextLen.
func (s *WalkService) CreateWalk(ctx context.Context, sessionID int64, req CreateWalkRequest) (*WalkView, error) {
	if _, err := s.getSession(ctx, sessionID); err != nil {
		return nil, err // 404
	}
	if req.Name == "" {
		return nil, NewValidation("name is required")
	}
	mode := models.WalkMode(req.Mode)
	if !mode.Valid() {
		return nil, NewValidation("mode must be 'interval' or 'manual'")
	}
	if len(req.Steps) == 0 {
		return nil, NewValidation("at least one step is required")
	}
	var intervalSecs *int
	if mode == models.WalkModeInterval {
		if req.IntervalSecs == nil || *req.IntervalSecs <= 0 {
			return nil, NewValidation("interval_secs must be > 0 for interval mode")
		}
		intervalSecs = req.IntervalSecs
	}
	steps := make([]*models.WalkStep, 0, len(req.Steps))
	for i, content := range req.Steps {
		if len(strings.TrimSpace(content)) == 0 {
			return nil, NewValidation(fmt.Sprintf("step %d content is empty", i+1))
		}
		if len(content) > sendMaxTextLen {
			return nil, NewValidation(fmt.Sprintf("step %d content too long (max %d)", i+1, sendMaxTextLen))
		}
		steps = append(steps, &models.WalkStep{
			WalkID: 0, Seq: i + 1, Content: content, State: models.WalkStepPending,
		})
	}
	w := &models.Walk{
		SessionID: sessionID, Name: req.Name, Description: req.Description,
		Mode: mode, IntervalSecs: intervalSecs, Status: models.WalkActive, CurrentSeq: 1,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.walks.CreateWalk(ctx, tx, w); err != nil {
		return nil, err
	}
	for _, st := range steps {
		st.WalkID = w.ID
	}
	if err := s.walks.CreateSteps(ctx, tx, steps); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.toView(w, steps), nil
}

// ---------- Advance ----------

// Advance — отправить следующий шаг (Send). retained → paused; failed → failed;
// все шаги → completed. paused/completed/failed → 409.
func (s *WalkService) Advance(ctx context.Context, walkID int64) (*WalkView, error) {
	w, err := s.getWalk(ctx, walkID)
	if err != nil {
		return nil, err // 404
	}
	if w.Status != models.WalkActive {
		return nil, NewConflict(fmt.Sprintf("walk is %s, cannot advance", w.Status)) // 409
	}
	step, err := s.walks.GetStepBySeq(ctx, s.db, walkID, w.CurrentSeq)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// шагов больше нет → completed (сafety net).
			if uerr := s.walks.UpdateWalkStatus(ctx, s.db, walkID, models.WalkCompleted); uerr != nil {
				return nil, uerr
			}
			s.publishWalkCompleted(ctx, w)
			return s.GetWalk(ctx, walkID)
		}
		return nil, err
	}
	total, err := s.walks.CountSteps(ctx, s.db, walkID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	res, sendErr := s.sender.Send(ctx, w.SessionID, step.Content, true)
	if sendErr != nil {
		// Send-ошибка (409/нет экрана/мёртв) → step failed + walk failed.
		note := sendErr.Error()
		if uerr := s.walks.UpdateStep(ctx, s.db, step.ID, models.WalkStepFailed, nil, &note, nil); uerr != nil {
			return nil, uerr
		}
		if uerr := s.walks.UpdateWalkStatus(ctx, s.db, walkID, models.WalkFailed); uerr != nil {
			return nil, uerr
		}
		return s.GetWalk(ctx, walkID)
	}
	// Успех: state из Send.status (delivered/staged/retained).
	st := string(res.Status)
	var note *string
	if res.Guard != "" {
		g := "guard: " + res.Guard
		note = &g
	}
	if uerr := s.walks.UpdateStep(ctx, s.db, step.ID, models.WalkStepState(res.Status), &st, note, &now); uerr != nil {
		return nil, uerr
	}
	walkStatus := models.WalkActive
	if res.Status == SendStatusRetained {
		walkStatus = models.WalkPaused // guard: нужен человек; current_seq не меняется (retry при resume)
		if uerr := s.walks.UpdateWalkStatus(ctx, s.db, walkID, walkStatus); uerr != nil {
			return nil, uerr
		}
	} else {
		newSeq := w.CurrentSeq + 1
		if newSeq > total {
			walkStatus = models.WalkCompleted
		}
		if uerr := s.walks.UpdateWalkAdvance(ctx, s.db, walkID, newSeq, now, walkStatus); uerr != nil {
			return nil, uerr
		}
	}
	s.publishStepSent(ctx, w, step, string(res.Status))
	if walkStatus == models.WalkCompleted {
		s.publishWalkCompleted(ctx, w)
	}
	return s.GetWalk(ctx, walkID)
}

// AdvanceDue — auto-advance (interval-режим): по каждому active interval-walk, если
// due (один шаг за тик — pacing). Вызывается из reaper-loop.
func (s *WalkService) AdvanceDue(ctx context.Context) error {
	walks, err := s.walks.ListActiveIntervalWalks(ctx, s.db)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, w := range walks {
		if w.IntervalSecs == nil || *w.IntervalSecs <= 0 {
			continue
		}
		// due: current_seq==1 → от created_at; current_seq>1 → от last_sent_at.
		var ref time.Time
		switch {
		case w.CurrentSeq <= 1:
			ref = w.CreatedAt
		case w.LastSentAt != nil:
			ref = *w.LastSentAt
		default:
			ref = w.CreatedAt
		}
		if now.Sub(ref) < time.Duration(*w.IntervalSecs)*time.Second {
			continue // не время
		}
		if _, err := s.Advance(ctx, w.ID); err != nil {
			continue // один шаг за тик; failed/paused — перестанет авто-advancиться
		}
	}
	return nil
}

// ---------- Pause / Resume / Skip ----------

// Pause — active → paused. completed/failed → 409.
func (s *WalkService) Pause(ctx context.Context, walkID int64) (*WalkView, error) {
	w, err := s.getWalk(ctx, walkID)
	if err != nil {
		return nil, err
	}
	if w.Status != models.WalkActive {
		return nil, NewConflict(fmt.Sprintf("walk is %s, cannot pause", w.Status))
	}
	if err := s.walks.UpdateWalkStatus(ctx, s.db, walkID, models.WalkPaused); err != nil {
		return nil, err
	}
	return s.GetWalk(ctx, walkID)
}

// Resume — paused → active. completed/failed → 409.
func (s *WalkService) Resume(ctx context.Context, walkID int64) (*WalkView, error) {
	w, err := s.getWalk(ctx, walkID)
	if err != nil {
		return nil, err
	}
	if w.Status != models.WalkPaused {
		return nil, NewConflict(fmt.Sprintf("walk is %s, cannot resume", w.Status))
	}
	if err := s.walks.UpdateWalkStatus(ctx, s.db, walkID, models.WalkActive); err != nil {
		return nil, err
	}
	return s.GetWalk(ctx, walkID)
}

// Skip — текущий pending-шаг → skipped, current_seq++ (без Send). Удобно пропустить
// зависший prompt-step. completed/failed → 409.
func (s *WalkService) Skip(ctx context.Context, walkID int64) (*WalkView, error) {
	w, err := s.getWalk(ctx, walkID)
	if err != nil {
		return nil, err
	}
	if w.Status != models.WalkActive {
		return nil, NewConflict(fmt.Sprintf("walk is %s, cannot skip", w.Status))
	}
	step, err := s.walks.GetStepBySeq(ctx, s.db, walkID, w.CurrentSeq)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NewConflict("no current step to skip")
		}
		return nil, err
	}
	total, err := s.walks.CountSteps(ctx, s.db, walkID)
	if err != nil {
		return nil, err
	}
	if err := s.walks.UpdateStep(ctx, s.db, step.ID, models.WalkStepSkipped, nil, nil, nil); err != nil {
		return nil, err
	}
	newSeq := w.CurrentSeq + 1
	status := models.WalkActive
	if newSeq > total {
		status = models.WalkCompleted
	}
	if err := s.walks.UpdateWalkSeq(ctx, s.db, walkID, newSeq, status); err != nil {
		return nil, err
	}
	return s.GetWalk(ctx, walkID)
}

// ---------- Events (Pulse, P2.9) ----------

func (s *WalkService) publishStepSent(ctx context.Context, w *models.Walk, step *models.WalkStep, status string) {
	if s.bus == nil {
		return
	}
	teamID := s.sessionTeamID(ctx, w.SessionID)
	data := map[string]any{
		"walk_id": w.ID, "session_id": w.SessionID, "team_id": teamID,
		"seq": step.Seq, "status": status,
	}
	s.bus.Publish(newEvent("walk.step_sent", data,
		fmt.Sprintf("session:%d", w.SessionID), fmt.Sprintf("team:%d", teamID)))
}

func (s *WalkService) publishWalkCompleted(ctx context.Context, w *models.Walk) {
	if s.bus == nil {
		return
	}
	teamID := s.sessionTeamID(ctx, w.SessionID)
	data := map[string]any{"walk_id": w.ID, "session_id": w.SessionID, "team_id": teamID}
	s.bus.Publish(newEvent("walk.completed", data,
		fmt.Sprintf("session:%d", w.SessionID), fmt.Sprintf("team:%d", teamID)))
}

func (s *WalkService) sessionTeamID(ctx context.Context, sessionID int64) int64 {
	sess, err := s.sessions.GetByID(ctx, s.db, sessionID)
	if err != nil {
		return 0
	}
	return sess.TeamID
}
