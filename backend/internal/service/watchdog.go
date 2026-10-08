package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
