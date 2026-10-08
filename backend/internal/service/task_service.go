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

const rfc3339 = time.RFC3339

// StaleTaskAfter — порог "stale" для UI (is_stale).
const StaleTaskAfter = 2 * time.Hour

type taskStore interface {
	Create(ctx context.Context, tx repository.DBTX, t *models.Task) error
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Task, error)
	List(ctx context.Context, tx repository.DBTX, f repository.TaskFilter) ([]*models.Task, int, error)
	UpdateState(ctx context.Context, tx repository.DBTX, id int64, t *models.Task) error
	CountOpenByParent(ctx context.Context, tx repository.DBTX, parentID int64) (int, error)
	CountByStates(ctx context.Context, tx repository.DBTX, teamID *int64, states ...models.TaskState) (int, error)
	CountDoneSince(ctx context.Context, tx repository.DBTX, since time.Time) (int, error)
}

type historyStore interface {
	Create(ctx context.Context, tx repository.DBTX, h *models.HistoryEntry) error
	ListByTask(ctx context.Context, tx repository.DBTX, taskID int64, limit, offset int) ([]*models.HistoryEntry, int, error)
}

type TaskService struct {
	db       *sql.DB
	teams    teamStore
	roles    roleStore
	tasks    taskStore
	history  historyStore
	sessions sessionStore
	alerts   *repository.WatchdogRepo
	// Bus — real-time события (task.created/task.state_changed); nil = без событий.
	Bus *EventBus
}

func NewTaskService(db *sql.DB, s *repository.Stores) *TaskService {
	return &TaskService{
		db: db, teams: s.Teams, roles: s.Roles, tasks: s.Tasks, history: s.History,
		sessions: s.Sessions, alerts: s.Watchdog,
	}
}

// ---------- Create ----------

type CreateTaskRequest struct {
	TeamID            int64          `json:"-"`
	ParentTaskID      *int64         `json:"parent_task_id"`
	DestinationRoleID int64          `json:"destination_role_id"`
	SourceRoleID      *int64         `json:"source_role_id"`
	Title             string         `json:"title"`
	Body              string         `json:"body,omitempty"`
	BodyContext       map[string]any `json:"body_context,omitempty"`
	Priority          int            `json:"priority,omitempty"`
}

func (s *TaskService) CreateTask(ctx context.Context, req CreateTaskRequest) (*models.Task, error) {
	if req.Title == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "title", Reason: "required"}})
	}
	if req.DestinationRoleID == 0 {
		return nil, NewValidation("invalid request", []FieldError{{Field: "destination_role_id", Reason: "required"}})
	}
	team, err := s.requireActiveTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireRoleInTeam(ctx, req.DestinationRoleID, team.ID); err != nil {
		return nil, err
	}
	if req.SourceRoleID != nil {
		if _, err := s.requireRoleInTeam(ctx, *req.SourceRoleID, team.ID); err != nil {
			return nil, err
		}
	}
	if req.ParentTaskID != nil {
		parent, err := s.getTask(ctx, *req.ParentTaskID)
		if err != nil {
			return nil, err
		}
		if parent.TeamID != team.ID {
			return nil, NewValidation(fmt.Sprintf("parent task %d belongs to another team", *req.ParentTaskID))
		}
		if parent.State.Terminal() {
			return nil, NewConflict(fmt.Sprintf("parent task %d is %s, cannot add subtasks", parent.ID, parent.State))
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	task := &models.Task{
		TeamID: team.ID, ParentTaskID: req.ParentTaskID,
		DestinationRoleID: req.DestinationRoleID, SourceRoleID: req.SourceRoleID,
		Title: req.Title, Body: req.Body, BodyContext: req.BodyContext,
		State: models.TaskPending, Priority: req.Priority,
	}
	if err := s.tasks.Create(ctx, tx, task); err != nil {
		return nil, err
	}
	if err := s.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: task.ID, ToState: string(models.TaskPending),
		ActorType: "human",
		Comment:   ptr("task created"),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.Bus.Publish(newEvent("task.created", map[string]any{
		"task_id": task.ID, "team_id": team.ID,
		"title": task.Title, "state": string(task.State),
	}, fmt.Sprintf("team:%d", team.ID), fmt.Sprintf("task:%d", task.ID)))
	return task, nil
}

type UpdateTaskStateRequest struct {
	ID              int64                 `json:"-"`
	State           models.TaskState      `json:"state"`
	ClosureReason   *models.ClosureReason `json:"closure_reason,omitempty"`
	ClosureTargetID *int64                `json:"closure_target_id,omitempty"`
	Comment         string                `json:"comment,omitempty"`
	ActorType       string                `json:"-"` // human | daemon
	ActorRoleID     *int64                `json:"-"`
}

// UpdateTaskState — переход состояния задачи + append в history + авто-закрытие родителя.
func (s *TaskService) UpdateTaskState(ctx context.Context, req UpdateTaskStateRequest) (*models.Task, error) {
	current, err := s.getTask(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if !req.State.Valid() {
		return nil, NewValidation(fmt.Sprintf("invalid task state %q", req.State))
	}
	if current.State == req.State {
		return current, nil // no-op (идемпотентно)
	}
	if !models.CanTransition(current.State, req.State) {
		return nil, NewConflict(fmt.Sprintf("cannot transition task from %s to %s", current.State, req.State))
	}
	if req.State == models.TaskDone {
		if req.ClosureReason == nil || !(*req.ClosureReason).Valid() {
			return nil, NewValidation("closure_reason is required when completing a task",
				[]FieldError{{Field: "closure_reason", Reason: "required (handed_off_to|blocked_on|denied|canceled|no_follow_on|escalation)"}})
		}
	}
	if req.ClosureReason != nil && !(*req.ClosureReason).Valid() {
		return nil, NewValidation(fmt.Sprintf("invalid closure_reason %q", *req.ClosureReason))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	updated := *current
	updated.State = req.State
	updated.ClosureReason = req.ClosureReason
	updated.ClosureTargetID = req.ClosureTargetID
	switch req.State {
	case models.TaskInProgress:
		if updated.StartedAt == nil {
			updated.StartedAt = &now
		}
		if current.State == models.TaskBlocked {
			updated.BlockedSince = nil
		}
	case models.TaskPending:
		if current.State == models.TaskBlocked {
			updated.BlockedSince = nil
		}
	case models.TaskBlocked:
		updated.BlockedSince = &now
	case models.TaskDone, models.TaskCanceled:
		updated.CompletedAt = &now
	}

	if err := s.tasks.UpdateState(ctx, tx, req.ID, &updated); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("task")
		}
		return nil, err
	}

	history := &models.HistoryEntry{
		QueueTaskID: req.ID,
		ToState:     string(req.State),
		ActorType:   req.ActorType,
		ActorRoleID: req.ActorRoleID,
	}
	if history.ActorType == "" {
		history.ActorType = "human"
	}
	from := string(current.State)
	history.FromState = &from
	if req.ClosureReason != nil {
		v := string(*req.ClosureReason)
		history.ClosureReason = &v
	}
	history.ClosureTargetID = req.ClosureTargetID
	if req.Comment != "" {
		history.Comment = &req.Comment
	}
	if err := s.history.Create(ctx, tx, history); err != nil {
		return nil, err
	}

	// Авто-закрытие родителя, если все подзадачи завершены.
	if current.ParentTaskID != nil && (req.State == models.TaskDone || req.State == models.TaskCanceled) {
		if err := s.maybeCloseParent(ctx, tx, *current.ParentTaskID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	updated.UpdatedAt = now
	s.Bus.Publish(newEvent("task.state_changed", map[string]any{
		"task_id": updated.ID, "from_state": from,
		"to_state": string(updated.State), "updated_at": now.Format(rfc3339),
	}, fmt.Sprintf("team:%d", updated.TeamID), fmt.Sprintf("task:%d", updated.ID)))
	return &updated, nil
}

func (s *TaskService) maybeCloseParent(ctx context.Context, tx repository.DBTX, parentID int64) error {
	open, err := s.tasks.CountOpenByParent(ctx, tx, parentID)
	if err != nil {
		return err
	}
	if open > 0 {
		return nil
	}
	parent, err := s.tasks.GetByID(ctx, tx, parentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	if parent.State.Terminal() {
		return nil
	}
	reason := models.ClosureNoFollowOn
	now := time.Now().UTC()
	updated := *parent
	updated.State = models.TaskDone
	updated.ClosureReason = &reason
	updated.CompletedAt = &now
	if err := s.tasks.UpdateState(ctx, tx, parentID, &updated); err != nil {
		return err
	}
	from := string(parent.State)
	return s.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: parentID, FromState: &from, ToState: string(models.TaskDone),
		ClosureReason: ptr(string(reason)), ActorType: "daemon",
		Comment: ptr("all subtasks finished — closed automatically"),
	})
}

// ---------- Handoff ----------

type HandoffTaskRequest struct {
	ID       int64  `json:"-"`
	ToRoleID int64  `json:"to_role_id"`
	Comment  string `json:"comment,omitempty"`
}

type HandoffTaskResult struct {
	ClosedTask *models.Task `json:"closed_task"`
	NewTask    *models.Task `json:"new_task"`
}

// HandoffTask — закрытие задачи (handed_off_to) + создание новой для целевой роли.
func (s *TaskService) HandoffTask(ctx context.Context, req HandoffTaskRequest) (*HandoffTaskResult, error) {
	current, err := s.getTask(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current.State.Terminal() {
		return nil, NewConflict(fmt.Sprintf("task %d is %s, cannot hand off", current.ID, current.State))
	}
	if _, err := s.requireRoleInTeam(ctx, req.ToRoleID, current.TeamID); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	newTask := &models.Task{
		TeamID: current.TeamID, ParentTaskID: current.ParentTaskID,
		DestinationRoleID: req.ToRoleID, SourceRoleID: &current.DestinationRoleID,
		Title: current.Title, Body: current.Body, BodyContext: current.BodyContext,
		State: models.TaskPending, Priority: current.Priority,
	}
	if err := s.tasks.Create(ctx, tx, newTask); err != nil {
		return nil, err
	}
	if err := s.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: newTask.ID, ToState: string(models.TaskPending), ActorType: "daemon",
		Comment: ptr(fmt.Sprintf("handed off from task %d", current.ID)),
	}); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	updated := *current
	updated.State = models.TaskDone
	updated.ClosureReason = ptr(models.ClosureHandedOff)
	updated.ClosureTargetID = &newTask.ID
	updated.CompletedAt = &now
	if err := s.tasks.UpdateState(ctx, tx, current.ID, &updated); err != nil {
		return nil, err
	}
	from := string(current.State)
	hc := req.Comment
	if hc == "" {
		hc = fmt.Sprintf("handed off to role %d", req.ToRoleID)
	}
	if err := s.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: current.ID, FromState: &from, ToState: string(models.TaskDone),
		ClosureReason: ptr(string(models.ClosureHandedOff)), ClosureTargetID: &newTask.ID,
		ActorType: "human", Comment: &hc,
	}); err != nil {
		return nil, err
	}

	if current.ParentTaskID != nil {
		if err := s.maybeCloseParent(ctx, tx, *current.ParentTaskID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &HandoffTaskResult{ClosedTask: &updated, NewTask: newTask}, nil
}

// ---------- Запросы ----------

// TaskView — задача в формате контракта (dashboard/tasks + list).
type TaskView struct {
	ID                int64          `json:"id"`
	TeamID            int64          `json:"team_id"`
	TeamName          string         `json:"team_name"`
	ParentTaskID      *int64         `json:"parent_task_id,omitempty"`
	Title             string         `json:"title"`
	Body              string         `json:"body,omitempty"`
	BodyContext       map[string]any `json:"body_context,omitempty"`
	State             string         `json:"state"`
	Priority          int            `json:"priority"`
	DestinationRoleID int64          `json:"destination_role_id"`
	DestinationRole   string         `json:"destination_role_name"`
	SourceRoleID      *int64         `json:"source_role_id,omitempty"`
	SourceRole        string         `json:"source_role_name,omitempty"`
	ClosureReason     *string        `json:"closure_reason,omitempty"`
	ClosureTargetID   *int64         `json:"closure_target_id,omitempty"`
	IsStale           bool           `json:"is_stale"`
	IsBlocked         bool           `json:"is_blocked"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
	StartedAt         *string        `json:"started_at,omitempty"`
	CompletedAt       *string        `json:"completed_at,omitempty"`
}

func (s *TaskService) ListTasks(ctx context.Context, f repository.TaskFilter) ([]*TaskView, int, error) {
	tasks, total, err := s.tasks.List(ctx, s.db, f)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.viewsFor(ctx, tasks)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

type TaskDetail struct {
	Task     *TaskView   `json:"task"`
	Subtasks []*TaskView `json:"subtasks"`
}

func (s *TaskService) GetTask(ctx context.Context, id int64) (*TaskDetail, error) {
	task, err := s.getTask(ctx, id)
	if err != nil {
		return nil, err
	}
	views, err := s.viewsFor(ctx, []*models.Task{task})
	if err != nil {
		return nil, err
	}
	sub := &TaskDetail{Task: views[0], Subtasks: []*TaskView{}}
	if children, _, err := s.tasks.List(ctx, s.db, repository.TaskFilter{ParentTaskID: &id, Limit: 200}); err != nil {
		return nil, err
	} else if childViews, err := s.viewsFor(ctx, children); err == nil {
		sub.Subtasks = childViews
	}
	return sub, nil
}

type TaskHistoryEntryView struct {
	ID              int64          `json:"id"`
	QueueTaskID     int64          `json:"queue_task_id"`
	FromState       *string        `json:"from_state,omitempty"`
	ToState         string         `json:"to_state"`
	ClosureReason   *string        `json:"closure_reason,omitempty"`
	ClosureTargetID *int64         `json:"closure_target_id,omitempty"`
	ActorType       string         `json:"actor_type"`
	Comment         *string        `json:"comment,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       string         `json:"created_at"`
}

func (s *TaskService) GetTaskHistory(ctx context.Context, id int64, limit, offset int) ([]*TaskHistoryEntryView, int, error) {
	if _, err := s.getTask(ctx, id); err != nil {
		return nil, 0, err
	}
	entries, total, err := s.history.ListByTask(ctx, s.db, id, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*TaskHistoryEntryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, &TaskHistoryEntryView{
			ID: e.ID, QueueTaskID: e.QueueTaskID, FromState: e.FromState, ToState: e.ToState,
			ClosureReason: e.ClosureReason, ClosureTargetID: e.ClosureTargetID,
			ActorType: e.ActorType, Comment: e.Comment, Metadata: e.Metadata,
			CreatedAt: e.CreatedAt.Format(rfc3339),
		})
	}
	return out, total, nil
}

// ---------- Dashboard ----------

type DashboardSummary struct {
	Teams     DashboardTeams  `json:"teams"`
	Tasks     DashboardTasks  `json:"tasks"`
	Sessions  DashboardZero   `json:"sessions"` // slice 3
	Alerts    DashboardAlerts `json:"alerts"`   // slice 3 (контракт: critical/warning)
	UpdatedAt string          `json:"updated_at"`
}

type DashboardTeams struct {
	Total  int `json:"total"`
	Active int `json:"active"`
}

type DashboardTasks struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	DoneToday  int `json:"done_today"`
}

type DashboardZero struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

// DashboardAlerts — форма alerts в dashboard/summary (контракт 20 §3.1: total/critical/warning).
type DashboardAlerts struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	Warning  int `json:"warning"`
}

func (s *TaskService) DashboardSummary(ctx context.Context) (*DashboardSummary, error) {
	teams, total, err := s.teams.List(ctx, s.db, 1000, 0)
	if err != nil {
		return nil, err
	}
	active := 0
	for _, t := range teams {
		if t.State == models.TeamActive {
			active++
		}
	}
	pending, err := s.tasks.CountByStates(ctx, s.db, nil, models.TaskPending)
	if err != nil {
		return nil, err
	}
	inProgress, err := s.tasks.CountByStates(ctx, s.db, nil, models.TaskInProgress)
	if err != nil {
		return nil, err
	}
	blocked, err := s.tasks.CountByStates(ctx, s.db, nil, models.TaskBlocked)
	if err != nil {
		return nil, err
	}
	doneToday, err := s.tasks.CountDoneSince(ctx, s.db, startOfUTCDay(time.Now().UTC()))
	if err != nil {
		return nil, err
	}
	sessTotal, err := s.sessions.CountByStates(ctx, s.db,
		models.SessionStarting, models.SessionRunning, models.SessionIdle,
		models.SessionStopping, models.SessionStopped, models.SessionFailed)
	if err != nil {
		return nil, err
	}
	sessActive, err := s.sessions.CountByStates(ctx, s.db,
		models.SessionStarting, models.SessionRunning, models.SessionIdle)
	if err != nil {
		return nil, err
	}
	sessFailed, err := s.sessions.CountByStates(ctx, s.db, models.SessionFailed)
	if err != nil {
		return nil, err
	}
	alertsTotal, err := s.alerts.CountAll(ctx, s.db)
	if err != nil {
		return nil, err
	}
	alertsCritical, err := s.alerts.CountBySeverity(ctx, s.db, "critical")
	if err != nil {
		return nil, err
	}
	// "warning" в summary = алерты уровня high (контракт не имеет severity warning)
	alertsWarning, err := s.alerts.CountBySeverity(ctx, s.db, "high")
	if err != nil {
		return nil, err
	}
	return &DashboardSummary{
		Teams: DashboardTeams{Total: total, Active: active},
		Tasks: DashboardTasks{
			Total: pending + inProgress + blocked, Pending: pending,
			InProgress: inProgress, Blocked: blocked, DoneToday: doneToday,
		},
		Sessions:  DashboardZero{Total: sessTotal, Running: sessActive, Failed: sessFailed},
		Alerts:    DashboardAlerts{Total: alertsTotal, Critical: alertsCritical, Warning: alertsWarning},
		UpdatedAt: time.Now().UTC().Format(rfc3339),
	}, nil
}

func (s *TaskService) DashboardTasks(ctx context.Context) ([]*TaskView, int, error) {
	return s.ListTasks(ctx, repository.TaskFilter{ActiveOnly: true, Limit: 100})
}

// ---------- Внутреннее ----------

func (s *TaskService) getTask(ctx context.Context, id int64) (*models.Task, error) {
	t, err := s.tasks.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("task")
		}
		return nil, err
	}
	return t, nil
}

func (s *TaskService) requireActiveTeam(ctx context.Context, teamID int64) (*models.Team, error) {
	team, err := s.teams.GetByID(ctx, s.db, teamID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("team")
		}
		return nil, err
	}
	if team.State != models.TeamActive {
		return nil, NewConflict(fmt.Sprintf("team %q is %s", team.Name, team.State))
	}
	return team, nil
}

func (s *TaskService) requireRoleInTeam(ctx context.Context, roleID, teamID int64) (*models.Role, error) {
	role, err := s.roles.GetByID(ctx, s.db, roleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	if role.TeamID != teamID {
		return nil, NewValidation(fmt.Sprintf("role %d does not belong to team %d", roleID, teamID))
	}
	return role, nil
}

// viewsFor — маппинг моделей в контрактные TaskView (имена team/roles).
func (s *TaskService) viewsFor(ctx context.Context, tasks []*models.Task) ([]*TaskView, error) {
	teamName := map[int64]string{}
	roleName := map[int64]string{}
	var err error
	resolve := func(teamID, roleID int64) {
		if _, ok := teamName[teamID]; !ok && err == nil {
			team, e := s.teams.GetByID(ctx, s.db, teamID)
			if e != nil {
				if errors.Is(e, repository.ErrNotFound) {
					teamName[teamID] = ""
					return
				}
				err = e
				return
			}
			teamName[teamID] = team.Name
		}
		if roleID != 0 {
			if _, ok := roleName[roleID]; !ok && err == nil {
				role, e := s.roles.GetByID(ctx, s.db, roleID)
				if e != nil {
					if errors.Is(e, repository.ErrNotFound) {
						roleName[roleID] = ""
						return
					}
					err = e
					return
				}
				roleName[roleID] = role.Name
			}
		}
	}
	for _, t := range tasks {
		resolve(t.TeamID, t.DestinationRoleID)
		if t.SourceRoleID != nil {
			resolve(t.TeamID, *t.SourceRoleID)
		}
	}
	if err != nil {
		return nil, err
	}

	out := make([]*TaskView, 0, len(tasks))
	for _, t := range tasks {
		v := &TaskView{
			ID: t.ID, TeamID: t.TeamID, TeamName: teamName[t.TeamID],
			ParentTaskID: t.ParentTaskID,
			Title:        t.Title, Body: t.Body, BodyContext: t.BodyContext,
			State: string(t.State), Priority: t.Priority,
			DestinationRoleID: t.DestinationRoleID, DestinationRole: roleName[t.DestinationRoleID],
			SourceRoleID:    t.SourceRoleID,
			ClosureTargetID: t.ClosureTargetID,
			IsBlocked:       t.State == models.TaskBlocked,
			CreatedAt:       t.CreatedAt.Format(rfc3339), UpdatedAt: t.UpdatedAt.Format(rfc3339),
		}
		if t.SourceRoleID != nil {
			v.SourceRole = roleName[*t.SourceRoleID]
		}
		if t.ClosureReason != nil {
			v.ClosureReason = ptr(string(*t.ClosureReason))
		}
		if t.StartedAt != nil {
			v.StartedAt = ptr(t.StartedAt.Format(rfc3339))
		}
		if t.CompletedAt != nil {
			v.CompletedAt = ptr(t.CompletedAt.Format(rfc3339))
		}
		if t.State == models.TaskInProgress && time.Since(t.UpdatedAt) > StaleTaskAfter {
			v.IsStale = true
		}
		out = append(out, v)
	}
	return out, nil
}

func startOfUTCDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func ptr[T any](v T) *T { return &v }
