package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// WatchdogConfig — пороги watchdog.
type WatchdogConfig struct {
	// StaleThreshold — in_progress-задача без обновлений дольше порога → 'stale'.
	StaleThreshold time.Duration
	// BlockedThreshold — blocked-задача дольше порога → 'blocked'.
	BlockedThreshold time.Duration
	// ScanInterval — период фонового скана.
	ScanInterval time.Duration
	// Enabled — включает фоновый loop (однократные сканы в тестах работают всегда).
	Enabled bool
}

func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		StaleThreshold:   2 * time.Hour,
		BlockedThreshold: 1 * time.Hour,
		ScanInterval:     30 * time.Second,
		Enabled:          true,
	}
}

// WatchdogService — периодический скан: stale/blocked задачи, failed сессии.
type WatchdogService struct {
	db       *sql.DB
	tasks    taskStore
	roles    roleStore
	sessions sessionStore
	events   *repository.WatchdogRepo
	cfg      WatchdogConfig
	// Bus — real-time событие alert.created; nil = без событий.
	Bus *EventBus
	// P0.3: timer-wakes (подвязка на TaskService.FireArmedTimerWakes; nil = без).
	FireTimerWakes func(ctx context.Context) int
	// P1.6: watchdog-джобы (context-usage-threshold). nil = джобы не проверяются.
	Jobs        *repository.WatchdogJobRepo
	JobEvents   *repository.WatchdogJobEventRepo
	LiveSession func(ctx context.Context, sessionID int64) (*SessionView, error)
	// P1.6: fire watchdog-wakes parked-задач (подвязка на TaskService.FireArmedWatchdogWakes).
	FireWatchdogWakes func(ctx context.Context, jobID string) int
	// P2.14: budget-порог. TotalCost — общая стоимость (оценка); CostSvc — порог.
	// nil = budget-алерт выключен.
	TotalCost func(ctx context.Context) *CostInfo
	CostSvc   *CostService
}

func NewWatchdogService(db *sql.DB, s *repository.Stores, cfg WatchdogConfig) *WatchdogService {
	return &WatchdogService{
		db: db, tasks: s.Tasks, roles: s.Roles, sessions: s.Sessions,
		events: s.Watchdog, cfg: cfg,
	}
}

// Run — фоновый loop.
func (w *WatchdogService) Run(ctx context.Context) {
	if !w.cfg.Enabled {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Scan(ctx)
		}
	}
}

// Scan — один проход watchdog. Возвращает число созданных алертов.
func (w *WatchdogService) Scan(ctx context.Context) int {
	n := 0
	n += w.checkStaleTasks(ctx)
	n += w.checkBlockedTasks(ctx)
	n += w.checkFailedSessions(ctx)
	// P1.6: watchdog-джобы (context-usage-threshold) — fire / dead-target.
	n += w.checkContextThresholdJobs(ctx)
	// P2.14: budget-порог → watchdog-алерт (dedup: cooldown).
	n += w.checkBudgetAlert(ctx)
	// P0.3: timer-wakes, чей срок наступил → разблокировка (не создаёт алерт).
	if w.FireTimerWakes != nil {
		w.FireTimerWakes(ctx)
	}
	return n
}

func (w *WatchdogService) checkStaleTasks(ctx context.Context) int {
	threshold := time.Now().UTC().Add(-w.cfg.StaleThreshold)
	tasks, total, err := w.tasks.List(ctx, w.db, repository.TaskFilter{
		States: []models.TaskState{models.TaskInProgress}, Limit: 500,
	})
	if err != nil {
		return 0
	}
	n := 0
	for _, t := range tasks {
		_ = total
		if t.UpdatedAt.After(threshold) {
			continue
		}
		exists, err := w.events.ExistsUnread(ctx, w.db, "stale", &t.ID, nil)
		if err != nil {
			continue
		}
		if exists {
			continue
		}
		desc := fmt.Sprintf("task %q (id=%d) is stale: in_progress for over %s", t.Title, t.ID, w.cfg.StaleThreshold.Round(time.Minute))
		ev := &models.WatchdogEvent{
			TeamID: t.TeamID, QueueTaskID: &t.ID, RoleID: &t.DestinationRoleID,
			EventType: "stale", Severity: "medium",
			Description: desc, RequiresAction: false,
		}
		if err := w.events.Create(ctx, w.db, ev); err == nil {
			n++
			w.publishAlert(ev)
		}
	}
	return n
}

func (w *WatchdogService) checkBlockedTasks(ctx context.Context) int {
	threshold := time.Now().UTC().Add(-w.cfg.BlockedThreshold)
	tasks, _, err := w.tasks.List(ctx, w.db, repository.TaskFilter{
		States: []models.TaskState{models.TaskBlocked}, Limit: 500,
	})
	if err != nil {
		return 0
	}
	n := 0
	for _, t := range tasks {
		blockedAt := t.UpdatedAt
		if t.BlockedSince != nil {
			blockedAt = *t.BlockedSince
		}
		if blockedAt.After(threshold) {
			continue
		}
		exists, err := w.events.ExistsUnread(ctx, w.db, "blocked", &t.ID, nil)
		if err != nil {
			continue
		}
		if exists {
			continue
		}
		desc := fmt.Sprintf("task %q (id=%d) has been blocked since %s", t.Title, t.ID, blockedAt.Format(time.RFC3339))
		ev := &models.WatchdogEvent{
			TeamID: t.TeamID, QueueTaskID: &t.ID, RoleID: &t.DestinationRoleID,
			EventType: "blocked", Severity: "high",
			Description: desc, RequiresAction: true,
		}
		if err := w.events.Create(ctx, w.db, ev); err == nil {
			n++
			w.publishAlert(ev)
		}
	}
	return n
}

func (w *WatchdogService) checkFailedSessions(ctx context.Context) int {
	sessions, err := w.sessions.ListByState(ctx, w.db, models.SessionFailed, 200)
	if err != nil {
		return 0
	}
	n := 0
	for _, s := range sessions {
		exists, err := w.events.ExistsUnread(ctx, w.db, "drift", s.QueueTaskID, &s.ID)
		if err != nil {
			continue
		}
		if exists {
			continue
		}
		code := "unknown"
		if s.ExitCode != nil {
			code = fmt.Sprint(*s.ExitCode)
		}
		desc := fmt.Sprintf("session %d (role) exited with failure (exit_code=%s)", s.ID, code)
		ev := &models.WatchdogEvent{
			TeamID: s.TeamID, SessionID: &s.ID, RoleID: &s.RoleID,
			QueueTaskID: s.QueueTaskID,
			EventType:   "drift", Severity: "high",
			Description: desc, RequiresAction: true,
		}
		if err := w.events.Create(ctx, w.db, ev); err == nil {
			n++
			w.publishAlert(ev)
		}
	}
	return n
}

// ---------- P2.14: budget-порог → watchdog-алерт ----------

const budgetAlertCooldown = time.Hour

// checkBudgetAlert — если cost.budget_alert_cents > 0 и общая стоимость (оценка)
// > порога → алерт (type=budget, high). Dedup: cooldown budgetAlertCooldown
// (не спамить каждый скан). «Нет данных» (null) → алерт не создаётся.
func (w *WatchdogService) checkBudgetAlert(ctx context.Context) int {
	if w.TotalCost == nil || w.CostSvc == nil {
		return 0
	}
	thresholdCents, err := w.CostSvc.BudgetAlertCents(ctx)
	if err != nil || thresholdCents <= 0 {
		return 0 // выключен
	}
	total := w.TotalCost(ctx)
	if total == nil || !total.HasData || total.CostUSD == nil {
		return 0 // нет данных (не 0)
	}
	totalCents := int64(*total.CostUSD*100 + 0.5)
	if totalCents <= thresholdCents {
		return 0 // ниже порога
	}
	// dedup: было ли budget-событие за последние cooldown?
	recent, err := w.events.ExistsRecentByType(ctx, w.db, "budget", time.Now().UTC().Add(-budgetAlertCooldown))
	if err != nil || recent {
		return 0
	}
	// watchdog_events.team_id — FK на teams(id): глобальный budget-алерт привязываем
	// к первой команде (sentinel). Нет команд → алерт не создаётся.
	var teamID int64
	if err := w.db.QueryRowContext(ctx, `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&teamID); err != nil {
		return 0 // нет команд (или БД-ошибка) — не создаём глобальный алерт
	}
	desc := fmt.Sprintf("total LLM cost $%.2f (estimate) exceeds budget threshold $%.2f",
		*total.CostUSD, float64(thresholdCents)/100)
	ev := &models.WatchdogEvent{
		TeamID: teamID, EventType: "budget", Severity: "high",
		Description: desc, RequiresAction: true,
	}
	if err := w.events.Create(ctx, w.db, ev); err != nil {
		return 0
	}
	w.publishAlert(ev)
	return 1
}

// ---------- P1.6: watchdog-джобы (context-usage-threshold) ----------

// checkContextThresholdJobs — один проход по enabled context-threshold джобам.
// Возвращает число созданных алертов (fire). dead-target не создаёт алерт.
func (w *WatchdogService) checkContextThresholdJobs(ctx context.Context) int {
	if w.Jobs == nil || w.LiveSession == nil {
		return 0
	}
	jobs, err := w.Jobs.List(ctx, w.db, false, 200) // enabled только
	if err != nil {
		return 0
	}
	n := 0
	for _, j := range jobs {
		n += w.evalContextThresholdJob(ctx, j)
	}
	return n
}

// evalContextThresholdJob — оценка одного джоба: dead-target / quiet-skip / fire.
func (w *WatchdogService) evalContextThresholdJob(ctx context.Context, j *models.WatchdogJob) int {
	if j.TargetSessionID == nil || j.ThresholdPct == nil {
		return 0 // malformed (не должно быть: валидация при register)
	}
	v, err := w.LiveSession(ctx, *j.TargetSessionID)
	if err != nil {
		return w.markDeadTarget(ctx, j, "target-сессия удалена")
	}
	state := models.SessionState(v.State)
	if !state.Active() {
		return w.markDeadTarget(ctx, j, "target-сессия "+v.State+" (не активна)")
	}
	if v.ContextUsedPercentage == nil || *v.ContextUsedPercentage < float64(*j.ThresholdPct) {
		return 0 // ниже порога / без usage — quiet skip (не логируется, как у OpenRIG)
	}
	// дедуп: уже fired и процент не вырос >5 п.т. с last_fired → quiet skip
	if j.LastFiredPct != nil && *v.ContextUsedPercentage <= *j.LastFiredPct+5 {
		return 0
	}
	return w.fireContextThresholdJob(ctx, j, v)
}

// markDeadTarget — сессия умерла/остановлена → джоб enabled=false (честность).
func (w *WatchdogService) markDeadTarget(ctx context.Context, j *models.WatchdogJob, reason string) int {
	if !j.Enabled {
		return 0 // уже остановлен (идемпотентность)
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return 0
	}
	defer func() { _ = tx.Rollback() }()
	if err := w.Jobs.SetEnabled(ctx, tx, j.ID, false); err != nil {
		return 0 // уже удалён
	}
	if w.JobEvents != nil {
		_ = w.JobEvents.Create(ctx, tx, &models.WatchdogJobEvent{JobID: j.ID, Outcome: "dead-target", Detail: reason})
	}
	if err := tx.Commit(); err != nil {
		return 0
	}
	return 0
}

// fireContextThresholdJob — FIRE: алерт + fired_count++ + fire watchdog-wakes parked-задач.
func (w *WatchdogService) fireContextThresholdJob(ctx context.Context, j *models.WatchdogJob, v *SessionView) int {
	sessionID := *j.TargetSessionID
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return 0
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	// алерт (существующий EventBus; новый вид watchdog_job, high, link на сессию)
	pct := 0
	if v.ContextUsedPercentage != nil {
		pct = int(*v.ContextUsedPercentage)
	}
	msg := j.Message
	if msg == "" {
		msg = "context-usage threshold reached"
	}
	ev := &models.WatchdogEvent{
		TeamID: v.TeamID, SessionID: &sessionID, RoleID: &v.RoleID,
		QueueTaskID: v.QueueTaskID,
		EventType:   "watchdog_job", Severity: "high",
		Description: fmt.Sprintf("watchdog job %q: сессия %d context %d%% >= %d%% — %s",
			j.Name, sessionID, pct, *j.ThresholdPct, msg),
		RequiresAction: true,
	}
	if err := w.events.Create(ctx, tx, ev); err != nil {
		return 0
	}
	// fired_count++, last_fired_at/pct
	cpct := *v.ContextUsedPercentage
	if err := w.Jobs.RecordFire(ctx, tx, j.ID, now, &cpct); err != nil {
		return 0
	}
	// история (bounded)
	if w.JobEvents != nil {
		_ = w.JobEvents.Create(ctx, tx, &models.WatchdogJobEvent{
			JobID: j.ID, Outcome: "fired", Detail: msg,
			Pct: &cpct, CreatedAt: now,
		})
	}
	if err := tx.Commit(); err != nil {
		return 0
	}
	w.publishAlert(ev)
	// P1.6: fire watchdog-wakes parked-задач (ref = job id) — как timer-fire
	if w.FireWatchdogWakes != nil {
		w.FireWatchdogWakes(ctx, strconv.FormatInt(j.ID, 10))
	}
	return 1
}

// publishAlert — real-time событие alert.created (контракт 20 §WebSocket).
func (w *WatchdogService) publishAlert(ev *models.WatchdogEvent) {
	channels := []string{fmt.Sprintf("watchdog:%d", ev.TeamID)}
	if ev.TeamID != 0 {
		channels = append(channels, fmt.Sprintf("team:%d", ev.TeamID))
	}
	w.Bus.Publish(newEvent("alert.created", map[string]any{
		"alert_id":        ev.ID,
		"event_type":      ev.EventType,
		"severity":        ev.Severity,
		"description":     ev.Description,
		"requires_action": ev.RequiresAction,
	}, channels...))
}

// ---------- Views (контракт 20 §3.4) ----------

type AlertView struct {
	ID             int64   `json:"id"`
	TeamID         int64   `json:"team_id"`
	TeamName       string  `json:"team_name"`
	EventType      string  `json:"event_type"`
	Severity       string  `json:"severity"`
	Description    string  `json:"description"`
	QueueTaskID    *int64  `json:"queue_task_id,omitempty"`
	SessionID      *int64  `json:"session_id,omitempty"`
	RoleID         *int64  `json:"role_id,omitempty"`
	ActionTaken    *string `json:"action_taken,omitempty"`
	IsRead         bool    `json:"is_read"`
	RequiresAction bool    `json:"requires_action"`
	CreatedAt      string  `json:"created_at"`
}

type AlertStoreRef struct {
	Events *repository.WatchdogRepo
	Teams  teamStore
	DB     *sql.DB
}

func (a *AlertStoreRef) ListAlerts(ctx context.Context, f repository.AlertFilter) ([]*AlertView, int, error) {
	events, total, err := a.Events.List(ctx, a.DB, f)
	if err != nil {
		return nil, 0, err
	}
	names := map[int64]string{}
	out := make([]*AlertView, 0, len(events))
	for _, e := range events {
		name, ok := names[e.TeamID]
		if !ok {
			team, err := a.Teams.GetByID(ctx, a.DB, e.TeamID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					name = ""
				} else {
					return nil, 0, err
				}
			} else {
				name = team.Name
			}
			names[e.TeamID] = name
		}
		out = append(out, &AlertView{
			ID: e.ID, TeamID: e.TeamID, TeamName: name,
			EventType: e.EventType, Severity: e.Severity, Description: e.Description,
			QueueTaskID: e.QueueTaskID, SessionID: e.SessionID, RoleID: e.RoleID,
			ActionTaken: e.ActionTaken, IsRead: e.IsRead, RequiresAction: e.RequiresAction,
			CreatedAt: e.CreatedAt.Format(rfc3339),
		})
	}
	return out, total, nil
}

func (a *AlertStoreRef) MarkAlertRead(ctx context.Context, id int64) error {
	if err := a.Events.MarkRead(ctx, a.DB, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return notFound("alert")
		}
		return err
	}
	return nil
}
