package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	UpdateDestinationRole(ctx context.Context, tx repository.DBTX, id, roleID int64) error // P1.4: workflow route
	CountOpenByParent(ctx context.Context, tx repository.DBTX, parentID int64) (int, error)
	CountByStates(ctx context.Context, tx repository.DBTX, teamID *int64, states ...models.TaskState) (int, error)
	CountDoneSince(ctx context.Context, tx repository.DBTX, since time.Time) (int, error)
}

type historyStore interface {
	Create(ctx context.Context, tx repository.DBTX, h *models.HistoryEntry) error
	ListByTask(ctx context.Context, tx repository.DBTX, taskID int64, limit, offset int) ([]*models.HistoryEntry, int, error)
}

// wakeStore — P0.3: wake для заблокированной задачи.
type wakeStore interface {
	Create(ctx context.Context, tx repository.DBTX, w *models.TaskWake) error
	GetLatestByTask(ctx context.Context, tx repository.DBTX, taskID int64) (*models.TaskWake, error)
	GetActiveByTask(ctx context.Context, tx repository.DBTX, taskID int64) (*models.TaskWake, error)
	ListArmedTimers(ctx context.Context, tx repository.DBTX, now time.Time) ([]*models.TaskWake, error)
	ListArmedByRef(ctx context.Context, tx repository.DBTX, ref string) ([]*models.TaskWake, error)
	ListArmedWatchdogByJob(ctx context.Context, tx repository.DBTX, jobID string) ([]*models.TaskWake, error) // P1.6
	MarkFired(ctx context.Context, tx repository.DBTX, id int64, at time.Time) error
}

type TaskService struct {
	db       *sql.DB
	teams    teamStore
	roles    roleStore
	tasks    taskStore
	history  historyStore
	wakes    wakeStore
	sessions sessionStore
	alerts   *repository.WatchdogRepo
	projects *repository.ProjectRepo // P1.0
	// P3.13: evidence (readiness/acceptance в TaskView); nil = без evidence.
	evidence *repository.EvidenceRepo
	// Bus — real-time события (task.created/task.state_changed); nil = без событий.
	Bus *EventBus
	// P0.2: tasks.closure_required_secs на момент перехода в in_progress (layered config).
	// nil = дефолт 2h (7200s).
	ClosureRequiredFunc func(ctx context.Context) (time.Duration, error)
	// P1.4: workflow engine projection (вызывается в tx смены состояния задачи;
	// возвращает события для публикации после commit). nil = без проекции.
	ProjectionHook func(ctx context.Context, tx repository.DBTX, task *models.Task, from, to models.TaskState, note string) ([]WSEvent, error)
}

func NewTaskService(db *sql.DB, s *repository.Stores) *TaskService {
	return &TaskService{
		db: db, teams: s.Teams, roles: s.Roles, tasks: s.Tasks, history: s.History,
		wakes: s.Wakes, sessions: s.Sessions, alerts: s.Watchdog, projects: s.Projects,
	}
}

func (s *TaskService) SetEvidenceRepo(r *repository.EvidenceRepo) { s.evidence = r }

// resolveTaskProject — P1.0: project_id задачи = override (req.ProjectID) или
// наследуется от team.project. Возвращает nil, если проект не существует.
func (s *TaskService) resolveTaskProject(ctx context.Context, override *int64, team *models.Team) *int64 {
	var pid *int64
	if override != nil {
		pid = override
	} else if team != nil && team.ProjectID != nil {
		pid = team.ProjectID
	}
	if pid == nil || s.projects == nil {
		return pid
	}
	if _, err := s.projects.GetByID(ctx, s.db, *pid); err != nil {
		return nil // проект удалён/не существует → не привязываем
	}
	return pid
}

// ---------- Create ----------

type CreateTaskRequest struct {
	TeamID            int64          `json:"-"`
	ProjectID         *int64         `json:"project_id"` // P1.0: override; иначе наследуется от team.project
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

	// P1.0: разрешение project_id задачи ДО tx (чтобы не держать соединение)
	projectID := s.resolveTaskProject(ctx, req.ProjectID, team)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	task := &models.Task{
		TeamID: team.ID, ProjectID: projectID, ParentTaskID: req.ParentTaskID,
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

// closureRequired — P0.2: сколько ждать закрытия задачи (read-at-transition).
func (s *TaskService) closureRequired(ctx context.Context) time.Duration {
	if s.ClosureRequiredFunc != nil {
		if d, err := s.ClosureRequiredFunc(ctx); err == nil && d > 0 {
			return d
		}
	}
	return 2 * time.Hour
}

type UpdateTaskStateRequest struct {
	ID              int64                 `json:"-"`
	State           models.TaskState      `json:"state"`
	ClosureReason   *models.ClosureReason `json:"closure_reason,omitempty"`
	ClosureTargetID *int64                `json:"closure_target_id,omitempty"`
	Comment         string                `json:"comment,omitempty"`
	// P0.3: wake при переходе в blocked (опционально; без wake — как раньше).
	Wake        *WakeSpec `json:"wake,omitempty"`
	ActorType   string    `json:"-"` // human | daemon
	ActorRoleID *int64    `json:"-"`
}

// WakeSpec — P0.3: причина-триггер для разблокировки (block with wake).
// kind=timer: After (duration, e.g. "30m"); kind=task: TaskID (referenced-задача).
type WakeSpec struct {
	Kind         string `json:"kind"` // timer | task | watchdog (P1.6)
	After        string `json:"after,omitempty"`
	TaskID       *int64 `json:"task_id,omitempty"`
	JobID        *int64 `json:"job_id,omitempty"` // P1.6: kind=watchdog — id watchdog-джоба
	Continuation string `json:"continuation,omitempty"`
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
		// P0.2: выставить/пересчитать дедлайн закрытия при входе в in_progress
		// (включая blocked → in_progress: пересчёт). Из done/canceled — не трогаем.
		deadline := now.Add(s.closureRequired(ctx))
		updated.ClosureRequiredAt = &deadline
	case models.TaskPending:
		if current.State == models.TaskBlocked {
			updated.BlockedSince = nil
		}
	case models.TaskBlocked:
		updated.BlockedSince = &now
	case models.TaskDone, models.TaskCanceled:
		updated.CompletedAt = &now
	}

	// P0.3: wake при блокировке (опционально; валидация → 400).
	if req.State == models.TaskBlocked && req.Wake != nil {
		if err := s.createWake(ctx, tx, req.ID, req.Wake, now); err != nil {
			return nil, err
		}
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

	// P0.3: referenced-задача закрыта (done/canceled) → её task-wakes срабатывают.
	var firedDependent []*models.Task
	if req.State == models.TaskDone || req.State == models.TaskCanceled {
		if fd, err := s.fireDependentTaskWakes(ctx, tx, req.ID); err != nil {
			return nil, err
		} else {
			firedDependent = fd
		}
	}

	// P1.4: workflow engine projection (в той же tx; события — после commit).
	var wfEvents []WSEvent
	if s.ProjectionHook != nil {
		if ev, err := s.ProjectionHook(ctx, tx, &updated, current.State, req.State, req.Comment); err != nil {
			return nil, err
		} else {
			wfEvents = ev
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
	// P0.3: publish по каждой разблокированной по task-wake задаче.
	for _, ft := range firedDependent {
		s.Bus.Publish(newEvent("task.state_changed", map[string]any{
			"task_id": ft.ID, "from_state": "blocked", "to_state": string(ft.State),
			"updated_at": now.Format(rfc3339), "wake_fired": true,
		}, fmt.Sprintf("team:%d", ft.TeamID), fmt.Sprintf("task:%d", ft.ID)))
	}
	// P1.4: publish workflow-событий после commit.
	for _, ev := range wfEvents {
		s.Bus.Publish(ev)
	}
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
		TeamID: current.TeamID, ProjectID: current.ProjectID, ParentTaskID: current.ParentTaskID,
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
	// P0.3: handoff закрывает задачу (done) → её task-wakes срабатывают.
	firedDependent, err := s.fireDependentTaskWakes(ctx, tx, current.ID)
	if err != nil {
		return nil, err
	}
	// P1.4: workflow engine projection (в той же tx; события — после commit).
	var wfEvents []WSEvent
	if s.ProjectionHook != nil {
		if ev, err := s.ProjectionHook(ctx, tx, &updated, current.State, models.TaskDone, hc); err != nil {
			return nil, err
		} else {
			wfEvents = ev
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	handoffNow := time.Now().UTC()
	for _, ft := range firedDependent {
		s.Bus.Publish(newEvent("task.state_changed", map[string]any{
			"task_id": ft.ID, "from_state": "blocked", "to_state": string(ft.State),
			"updated_at": handoffNow.Format(rfc3339), "wake_fired": true,
		}, fmt.Sprintf("team:%d", ft.TeamID), fmt.Sprintf("task:%d", ft.ID)))
	}
	for _, ev := range wfEvents {
		s.Bus.Publish(ev)
	}
	return &HandoffTaskResult{ClosedTask: &updated, NewTask: newTask}, nil
}

// ---------- P0.3: Block with wake ----------

// createWake — P0.3: создание wake при блокировке (within tx). Валидация → 400.
// Если у задачи уже есть арм. wake — закрываем (идемпотентность re-block).
func (s *TaskService) createWake(ctx context.Context, tx repository.DBTX, taskID int64, spec *WakeSpec, now time.Time) error {
	if active, err := s.wakes.GetActiveByTask(ctx, tx, taskID); err == nil && active != nil {
		if err := s.wakes.MarkFired(ctx, tx, active.ID, now); err != nil {
			return err
		}
	}
	w := &models.TaskWake{TaskID: taskID, Kind: spec.Kind, Continuation: spec.Continuation}
	switch spec.Kind {
	case models.WakeKindTimer:
		d, err := time.ParseDuration(spec.After)
		if err != nil || d <= 0 {
			return NewValidation("invalid wake.after duration",
				[]FieldError{{Field: "wake.after", Reason: "must be a positive duration, e.g. 30m"}})
		}
		w.Ref = now.Add(d).UTC().Format(time.RFC3339Nano) // fire-at
	case models.WakeKindTask:
		if spec.TaskID == nil || *spec.TaskID <= 0 {
			return NewValidation("wake.task_id is required for kind=task",
				[]FieldError{{Field: "wake.task_id", Reason: "required"}})
		}
		w.Ref = strconv.FormatInt(*spec.TaskID, 10)
	case models.WakeKindWatchdog: // P1.6: будит, когда сработал watchdog-джоб (ref = job id)
		if spec.JobID == nil || *spec.JobID <= 0 {
			return NewValidation("wake.job_id is required for kind=watchdog",
				[]FieldError{{Field: "wake.job_id", Reason: "required"}})
		}
		w.Ref = strconv.FormatInt(*spec.JobID, 10)
	default:
		return NewValidation(fmt.Sprintf("invalid wake kind %q", spec.Kind),
			[]FieldError{{Field: "wake.kind", Reason: "must be timer|task|watchdog"}})
	}
	return s.wakes.Create(ctx, tx, w)
}

// fireWakeTx — ядро: разблокировка задачи по сработавшему wake (blocked → pending +
// history + fired_at), within tx. no-op (fired=false), если задача уже не blocked /
// wake уже fired. Не публикует (caller публикует после commit).
func (s *TaskService) fireWakeTx(ctx context.Context, tx repository.DBTX, w *models.TaskWake, reason string) (bool, error) {
	task, err := s.tasks.GetByID(ctx, tx, w.TaskID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil // задача удалена
		}
		return false, err
	}
	if task.State != models.TaskBlocked {
		return false, nil // уже разблокирована (идемпотентность)
	}
	now := time.Now().UTC()
	if err := s.wakes.MarkFired(ctx, tx, w.ID, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil // уже fired
		}
		return false, err
	}
	updated := *task
	updated.State = models.TaskPending
	updated.BlockedSince = nil
	if err := s.tasks.UpdateState(ctx, tx, w.TaskID, &updated); err != nil {
		return false, err
	}
	from := string(task.State)
	if err := s.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: w.TaskID, FromState: &from, ToState: string(models.TaskPending),
		ActorType: "daemon", Comment: ptr(reason),
		Metadata: map[string]any{"type": "wake_fired", "wake_kind": w.Kind},
	}); err != nil {
		return false, err
	}
	return true, nil
}

// fireWake — один wake (отдельная транзакция) + publish.
func (s *TaskService) fireWake(ctx context.Context, w *models.TaskWake, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	fired, err := s.fireWakeTx(ctx, tx, w, reason)
	if err != nil {
		return err
	}
	// P1.4: workflow engine projection (timer-wake unblock → instance running; в той же tx).
	var wfEvents []WSEvent
	if fired && s.ProjectionHook != nil {
		if task, err := s.tasks.GetByID(ctx, tx, w.TaskID); err == nil {
			if ev, err := s.ProjectionHook(ctx, tx, task, models.TaskBlocked, models.TaskPending, reason); err != nil {
				return err
			} else {
				wfEvents = ev
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, ev := range wfEvents {
		s.Bus.Publish(ev)
	}
	if fired {
		if task, err := s.tasks.GetByID(ctx, s.db, w.TaskID); err == nil {
			s.Bus.Publish(newEvent("task.state_changed", map[string]any{
				"task_id": task.ID, "from_state": "blocked", "to_state": string(task.State),
				"updated_at": time.Now().UTC().Format(rfc3339), "wake_fired": true,
			}, fmt.Sprintf("team:%d", task.TeamID), fmt.Sprintf("task:%d", task.ID)))
		}
	}
	return nil
}

// fireDependentTaskWakes — P0.3: referenced-задача закрыта (done/canceled) → её
// task-wakes срабатывают (blocked → pending). Within tx. Возвращает разблокированные
// задачи (для publish после commit).
func (s *TaskService) fireDependentTaskWakes(ctx context.Context, tx repository.DBTX, closedID int64) ([]*models.Task, error) {
	wakes, err := s.wakes.ListArmedByRef(ctx, tx, strconv.FormatInt(closedID, 10))
	if err != nil {
		return nil, err
	}
	var fired []*models.Task
	for _, w := range wakes {
		didFire, err := s.fireWakeTx(ctx, tx, w, fmt.Sprintf("wake task #%d closed", closedID))
		if err != nil {
			return nil, err
		}
		if didFire {
			if t, err := s.tasks.GetByID(ctx, tx, w.TaskID); err == nil {
				fired = append(fired, t)
			}
		}
	}
	return fired, nil
}

// FireArmedTimerWakes — P0.3: timer-wakes, чей срок наступил (watchdog-loop).
// Возвращает число сработавших.
func (s *TaskService) FireArmedTimerWakes(ctx context.Context) int {
	wakes, err := s.wakes.ListArmedTimers(ctx, s.db, time.Now().UTC())
	if err != nil {
		return 0
	}
	n := 0
	for _, w := range wakes {
		if err := s.fireWake(ctx, w, "wake timer fired"); err == nil {
			n++
		}
	}
	return n
}

// FireArmedWatchdogWakes — P1.6: watchdog-джоб сработал → будит parked-задачи, чей
// wake.kind=watchdog и ref = job id (разблокировка как timer-fire). Возвращает число.
func (s *TaskService) FireArmedWatchdogWakes(ctx context.Context, jobID string) int {
	wakes, err := s.wakes.ListArmedWatchdogByJob(ctx, s.db, jobID)
	if err != nil {
		return 0
	}
	n := 0
	for _, w := range wakes {
		if err := s.fireWake(ctx, w, "watchdog job fired"); err == nil {
			n++
		}
	}
	return n
}

type ResolveTaskRequest struct {
	ID          int64
	Decision    string // обязателен — почему задача снова в работу
	ActorType   string // human | daemon
	ActorRoleID *int64
}

// ResolveTask — P0.3: ручной resolve заблокированной задачи: blocked → pending +
// запись решения в history (type resolve, decision) + событие. 409 на не-blocked.
func (s *TaskService) ResolveTask(ctx context.Context, req ResolveTaskRequest) (*models.Task, error) {
	current, err := s.getTask(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if req.Decision == "" {
		return nil, NewValidation("decision is required to resolve a blocked task",
			[]FieldError{{Field: "decision", Reason: "required"}})
	}
	if current.State != models.TaskBlocked {
		return nil, NewConflict(fmt.Sprintf("task %d is %s, only blocked tasks can be resolved", current.ID, current.State))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	actor := req.ActorType
	if actor == "" {
		actor = "human"
	}
	// закрыть арм. wake (идемпотентность: не сработает повторно)
	if active, err := s.wakes.GetActiveByTask(ctx, tx, req.ID); err == nil && active != nil {
		if err := s.wakes.MarkFired(ctx, tx, active.ID, now); err != nil {
			return nil, err
		}
	}
	updated := *current
	updated.State = models.TaskPending
	updated.BlockedSince = nil
	if err := s.tasks.UpdateState(ctx, tx, req.ID, &updated); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("task")
		}
		return nil, err
	}
	from := string(current.State)
	if err := s.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: req.ID, FromState: &from, ToState: string(models.TaskPending),
		ActorType: actor, ActorRoleID: req.ActorRoleID,
		Comment:  ptr("resolve: " + req.Decision),
		Metadata: map[string]any{"type": "resolve", "decision": req.Decision},
	}); err != nil {
		return nil, err
	}
	// P1.4: workflow engine projection (unblock → instance running; в той же tx).
	var wfEvents []WSEvent
	if s.ProjectionHook != nil {
		if ev, err := s.ProjectionHook(ctx, tx, &updated, current.State, models.TaskPending, req.Decision); err != nil {
			return nil, err
		} else {
			wfEvents = ev
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	updated.UpdatedAt = now
	s.Bus.Publish(newEvent("task.state_changed", map[string]any{
		"task_id": updated.ID, "from_state": from, "to_state": string(updated.State),
		"updated_at": now.Format(rfc3339), "resolved": true,
	}, fmt.Sprintf("team:%d", updated.TeamID), fmt.Sprintf("task:%d", updated.ID)))
	for _, ev := range wfEvents {
		s.Bus.Publish(ev)
	}
	return &updated, nil
}

// ---------- Запросы ----------

// TaskView — задача в формате контракта (dashboard/tasks + list).
type TaskView struct {
	ID                int64          `json:"id"`
	TeamID            int64          `json:"team_id"`
	ProjectID         *int64         `json:"project_id,omitempty"` // P1.0
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
	IsOverdue         bool           `json:"is_overdue"`
	// P3.13: proof/evidence (closure ≠ acceptance — acceptance отдельное от state).
	EvidenceCount     int     `json:"evidence_count"`
	Readiness         string  `json:"readiness"`                     // ready | pending | none
	Acceptance        string  `json:"acceptance"`                    // accepted | rejected | pending | none
	ClosureRequiredAt *string `json:"closure_required_at,omitempty"` // P0.2: дедлайн закрытия
	RemainingSecs     *int64  `json:"remaining_secs,omitempty"`      // P0.2: closure_required_at - now (neg = просрочено)
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	StartedAt         *string `json:"started_at,omitempty"`
	CompletedAt       *string `json:"completed_at,omitempty"`
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

// ListOverdue — P0.2: bounded-срез просроченных задач (in_progress/blocked +
// closure_required_at < now). total = полное число просроченных.
func (s *TaskService) ListOverdue(ctx context.Context, limit, offset int) ([]*TaskView, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// кандидаты: in_progress/blocked с выставленным дедлайном (bounded);
	// фильтрация "просрочено" — в Go (portable; без SQL-сравнения TEXT-time).
	tasks, _, err := s.tasks.List(ctx, s.db, repository.TaskFilter{
		States:                 []models.TaskState{models.TaskInProgress, models.TaskBlocked},
		ClosureRequiredNotNull: true,
		Limit:                  500,
	})
	if err != nil {
		return nil, 0, err
	}
	now := time.Now().UTC()
	overdue := make([]*models.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.ClosureRequiredAt != nil && t.ClosureRequiredAt.Before(now) {
			overdue = append(overdue, t)
		}
	}
	// самое просроченное (ранний дедлайн) — сверху
	sort.Slice(overdue, func(i, j int) bool {
		return overdue[i].ClosureRequiredAt.Before(*overdue[j].ClosureRequiredAt)
	})
	total := len(overdue)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	views, err := s.viewsFor(ctx, overdue[offset:end])
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

type TaskDetail struct {
	Task     *TaskView   `json:"task"`
	Subtasks []*TaskView `json:"subtasks"`
	Wake     *WakeView   `json:"wake,omitempty"` // P0.3: current/most-recent wake
	// P2.14: оценка стоимости задачи (сумма сессий); null/omit = «нет данных».
	EstimatedCost *CostInfo `json:"estimated_cost,omitempty"`
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
	if w, err := s.wakes.GetLatestByTask(ctx, s.db, id); err == nil {
		sub.Wake = wakeView(w, time.Now().UTC())
	}
	return sub, nil
}

// WakeView — P0.3: wake в формате контракта (GET /tasks/{id}).
type WakeView struct {
	Kind          string  `json:"kind"`          // timer | task
	Ref           string  `json:"ref,omitempty"` // task: task_id; timer: fire-at (RFC3339)
	Continuation  string  `json:"continuation,omitempty"`
	Fired         bool    `json:"fired"`
	FiredAt       *string `json:"fired_at,omitempty"`
	RemainingSecs *int64  `json:"remaining_secs,omitempty"` // timer only: fire_at - now (neg = fired)
}

func wakeView(w *models.TaskWake, now time.Time) *WakeView {
	if w == nil {
		return nil
	}
	v := &WakeView{Kind: w.Kind, Ref: w.Ref, Continuation: w.Continuation, Fired: w.FiredAt != nil}
	if w.FiredAt != nil {
		v.FiredAt = ptr(w.FiredAt.Format(rfc3339))
	}
	if w.Kind == models.WakeKindTimer {
		if fa, err := time.Parse(time.RFC3339Nano, w.Ref); err == nil {
			v.RemainingSecs = ptr(int64(fa.Sub(now).Seconds()))
		}
	}
	return v
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
	teams, total, err := s.teams.List(ctx, s.db, 1000, 0, "")
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

	now := time.Now().UTC()
	out := make([]*TaskView, 0, len(tasks))
	for _, t := range tasks {
		v := &TaskView{
			ID: t.ID, TeamID: t.TeamID, ProjectID: t.ProjectID, TeamName: teamName[t.TeamID],
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
		// P0.2: дедлайн закрытия + is_overdue + remaining_secs
		if t.ClosureRequiredAt != nil {
			v.ClosureRequiredAt = ptr(t.ClosureRequiredAt.Format(rfc3339))
			if t.State == models.TaskInProgress || t.State == models.TaskBlocked {
				secs := int64(t.ClosureRequiredAt.Sub(now).Seconds())
				v.RemainingSecs = ptr(secs)
				if t.ClosureRequiredAt.Before(now) {
					v.IsOverdue = true
				}
			}
		}
		out = append(out, v)
	}
	// P3.13: evidence readiness/acceptance (batched; closure ≠ acceptance).
	if s.evidence != nil && len(tasks) > 0 {
		taskIDs := make([]int64, 0, len(tasks))
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.ID)
		}
		if summary, err := s.evidence.StatusSummaryByTasks(ctx, s.db, taskIDs); err == nil {
			for i, t := range tasks {
				counts := summary[t.ID] // nil = нет evidence
				out[i].EvidenceCount = EvidenceTotal(counts)
				out[i].Readiness = EvidenceReadiness(counts)
				out[i].Acceptance = EvidenceAcceptance(counts)
			}
		}
	}
	return out, nil
}

func startOfUTCDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func ptr[T any](v T) *T { return &v }
