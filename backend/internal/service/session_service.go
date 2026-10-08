package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
)

type sessionStore interface {
	Create(ctx context.Context, tx repository.DBTX, s *models.Session) error
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Session, error)
	List(ctx context.Context, tx repository.DBTX, f repository.SessionFilter) ([]*models.Session, int, error)
	UpdateState(ctx context.Context, tx repository.DBTX, id int64, s *models.Session) error
	ListByState(ctx context.Context, tx repository.DBTX, state models.SessionState, limit int) ([]*models.Session, error)
	CountByStates(ctx context.Context, tx repository.DBTX, states ...models.SessionState) (int, error)
}

type sessionHistoryStore interface {
	Create(ctx context.Context, tx repository.DBTX, h *models.SessionHistoryEntry) error
	ListBySession(ctx context.Context, tx repository.DBTX, sessionID int64, limit, offset int) ([]*models.SessionHistoryEntry, int, error)
}

// SessionService — жизненный цикл агент-сессий.
type SessionService struct {
	db       *sql.DB
	runt     *runtime.Registry
	teams    teamStore
	roles    roleStore
	tasks    taskStore
	tHistory historyStore
	sessions sessionStore
	sHistory sessionHistoryStore
	// LogsDir — transcript-файлы (logs/sessions/<id>.log).
	// ConfigsDir — конфиги pi-сессий (configs/sessions/<id>.yaml).
	LogsDir    string
	ConfigsDir string
	// Bus — real-time события (session.started/session.stopped); nil = без событий.
	Bus *EventBus
}

func NewSessionService(db *sql.DB, s *repository.Stores, rt *runtime.Registry) *SessionService {
	return &SessionService{
		db: db, runt: rt,
		teams: s.Teams, roles: s.Roles, tasks: s.Tasks, tHistory: s.History,
		sessions: s.Sessions, sHistory: s.SessionHistory,
		LogsDir: "logs/sessions", ConfigsDir: "configs/sessions",
	}
}

type CreateSessionRequest struct {
	RoleID      int64          `json:"role_id"`
	QueueTaskID *int64         `json:"queue_task_id,omitempty"`
	RuntimeType string         `json:"runtime_type,omitempty"`
	Command     string         `json:"command,omitempty"`
	Args        []string       `json:"args,omitempty"`
	WorkingDir  string         `json:"working_dir,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
}

// CreateSession — создание + запуск сессии.
//
// command: из запроса → иначе config.command → иначе по runtime-типу
// (pi → "pi"; tmux/process без команды → 400).
func (s *SessionService) CreateSession(ctx context.Context, teamID int64, req CreateSessionRequest) (*models.Session, error) {
	if req.RoleID == 0 {
		return nil, NewValidation("invalid request", []FieldError{{Field: "role_id", Reason: "required"}})
	}
	team, err := s.requireActiveTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	role, err := s.requireRoleInTeam(ctx, req.RoleID, team.ID)
	if err != nil {
		return nil, err
	}
	rtType := req.RuntimeType
	if rtType == "" {
		rtType = "process"
	}
	if req.Config != nil {
		if v, ok := req.Config["runtime_type"].(string); ok && v != "" {
			rtType = v
		}
	}
	adapter, err := s.runt.Get(rtType)
	if err != nil {
		return nil, NewValidation(err.Error())
	}
	command := req.Command
	if command == "" && req.Config != nil {
		if v, ok := req.Config["command"].(string); ok {
			command = v
		}
	}
	if command == "" && rtType == "pi" {
		command = "pi"
	}
	if command == "" {
		return nil, NewValidation(fmt.Sprintf("command is required for runtime %q (pass command or config.command)", rtType),
			[]FieldError{{Field: "command", Reason: "required"}})
	}

	// одна активная сессия на роль
	active, _, err := s.sessions.List(ctx, s.db, repository.SessionFilter{RoleID: &req.RoleID, Active: true})
	if err != nil {
		return nil, err
	}
	if len(active) > 0 {
		return nil, NewConflict(fmt.Sprintf("role %s already has active session %d", role.Name, active[0].ID))
	}

	var task *models.Task
	if req.QueueTaskID != nil {
		task, err = s.tasks.GetByID(ctx, s.db, *req.QueueTaskID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, notFound("task")
			}
			return nil, err
		}
		if task.TeamID != team.ID {
			return nil, NewValidation(fmt.Sprintf("task %d belongs to another team", task.ID))
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	sess := &models.Session{
		TeamID: team.ID, RoleID: role.ID, QueueTaskID: req.QueueTaskID,
		RuntimeType: rtType, State: models.SessionStarting,
		WorkingDir: req.WorkingDir, Command: command, Config: req.Config,
	}
	if err := s.sessions.Create(ctx, tx, sess); err != nil {
		return nil, err
	}
	if err := s.sHistory.Create(ctx, tx, &models.SessionHistoryEntry{
		SessionID: sess.ID, ToState: string(models.SessionStarting),
		ActorType: "human", Comment: ptr("session created"),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// конфиг для pi
	var configFile string
	if rtType == "pi" {
		configFile = filepath.Join(s.ConfigsDir, fmt.Sprintf("session-%d.yaml", sess.ID))
		fields := runtime.AgentSpecFields{Name: role.Name}
		if m, ok := sess.Config["model"].(string); ok {
			fields.Model = m
		}
		if p, ok := sess.Config["system_prompt"].(string); ok {
			fields.SystemPrompt = p
		}
		if err := runtime.WriteSessionConfig(configFile, fields); err != nil {
			return nil, err
		}
	}

	outFile := filepath.Join(s.LogsDir, fmt.Sprintf("session-%d.log", sess.ID))
	if err := os.MkdirAll(s.LogsDir, 0o755); err != nil {
		return nil, err
	}
	ref, err := adapter.Start(ctx, runtime.Spec{
		SessionID:  sess.ID,
		RoleName:   role.Name,
		WorkingDir: sess.WorkingDir,
		Command:    command,
		Args:       req.Args,
		OutputFile: outFile,
		ConfigFile: configFile,
		ConfigsDir: s.ConfigsDir,
	})
	if err != nil {
		_, _ = s.markSessionState(ctx, sess, models.SessionFailed, nil, "start failed: "+err.Error())
		return nil, NewValidation("session start failed: " + err.Error())
	}

	now := time.Now().UTC()
	updated := *sess
	updated.State = models.SessionRunning
	updated.RuntimeRef = ref
	updated.StartedAt = &now
	if err := s.sessions.UpdateState(ctx, s.db, sess.ID, &updated); err != nil {
		_ = adapter.Stop(ctx, ref)
		return nil, err
	}
	fromState := string(models.SessionStarting)
	_ = s.sHistory.Create(ctx, s.db, &models.SessionHistoryEntry{
		SessionID: sess.ID, FromState: &fromState, ToState: string(models.SessionRunning),
		ActorType: "daemon", Comment: ptr("started, runtime_ref=" + ref),
	})

	// привязка к задаче: pending → in_progress
	if task != nil && task.State == models.TaskPending {
		if err := s.transitionTaskAsDaemon(ctx, task.ID, models.TaskInProgress,
			fmt.Sprintf("session %d started", sess.ID), nil); err != nil {
			return nil, err
		}
	}
	s.Bus.Publish(newEvent("session.started", map[string]any{
		"session_id":   sess.ID,
		"role_name":    role.Name,
		"runtime_type": rtType,
	}, fmt.Sprintf("team:%d", sess.TeamID), fmt.Sprintf("session:%d", sess.ID)))
	return &updated, nil
}

// StopSession — остановка сессии (идемпотентна для stopped).
func (s *SessionService) StopSession(ctx context.Context, id int64) (*models.Session, error) {
	sess, err := s.getSession(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess.State == models.SessionStopped {
		return sess, nil
	}
	if !sess.State.Active() {
		return nil, NewConflict(fmt.Sprintf("session %d is %s", id, sess.State))
	}
	adapter, err := s.runt.Get(sess.RuntimeType)
	if err != nil {
		return nil, NewValidation(err.Error())
	}
	if sess.RuntimeRef != "" {
		if err := adapter.Stop(ctx, sess.RuntimeRef); err != nil {
			return nil, NewValidation("stop runtime: " + err.Error())
		}
		// Если процесс уже мёртв — реальный exit code решает: != 0 → failed,
		// чтобы user-stop не затирал crash. st.State уже учитывает stoppedByUs.
		if st, err := adapter.Status(ctx, sess.RuntimeRef); err == nil && !st.Alive && st.ExitCode != nil {
			updated, err := s.markSessionState(ctx, sess, models.SessionState(st.State), st.ExitCode,
				fmt.Sprintf("stop requested, runtime already exited (exit_code=%d)", *st.ExitCode))
			if err != nil {
				return nil, err
			}
			return updated, nil
		}
	}
	updated, err := s.markSessionState(ctx, sess, models.SessionStopped, nil, "stopped by user")
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// markSessionState — смена состояния + запись в session_history.
func (s *SessionService) markSessionState(ctx context.Context, sess *models.Session, to models.SessionState, exitCode *int, comment string) (*models.Session, error) {
	now := time.Now().UTC()
	updated := *sess
	updated.State = to
	if exitCode != nil {
		updated.ExitCode = exitCode
	}
	if to == models.SessionStopped || to == models.SessionFailed {
		updated.StoppedAt = &now
	}
	if err := s.sessions.UpdateState(ctx, s.db, sess.ID, &updated); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("session")
		}
		return nil, err
	}
	from := string(sess.State)
	h := &models.SessionHistoryEntry{SessionID: sess.ID, FromState: &from, ToState: string(to), ActorType: "daemon"}
	if comment != "" {
		h.Comment = &comment
	}
	if exitCode != nil {
		h.Metadata = map[string]any{"exit_code": *exitCode}
	}
	if err := s.sHistory.Create(ctx, s.db, h); err != nil {
		return nil, err
	}
	if to == models.SessionStopped || to == models.SessionFailed {
		roleName := ""
		if r, err := s.roles.GetByID(ctx, s.db, sess.RoleID); err == nil {
			roleName = r.Name
		}
		data := map[string]any{"session_id": sess.ID, "role_name": roleName, "state": string(to)}
		if exitCode != nil {
			data["exit_code"] = *exitCode
		}
		s.Bus.Publish(newEvent("session.stopped", data,
			fmt.Sprintf("team:%d", sess.TeamID), fmt.Sprintf("session:%d", sess.ID)))
	}
	return &updated, nil
}

// Reap — проверка живости активных сессий (background loop и тесты).
// Возвращает число обновлённых сессий.
func (s *SessionService) Reap(ctx context.Context) (int, error) {
	count := 0
	states := []models.SessionState{
		models.SessionStarting, models.SessionRunning,
		models.SessionIdle, models.SessionStopping,
	}
	for _, state := range states {
		sessions, err := s.sessions.ListByState(ctx, s.db, state, 200)
		if err != nil {
			return count, err
		}
		for _, sess := range sessions {
			adapter, err := s.runt.Get(sess.RuntimeType)
			if err != nil {
				continue
			}
			st, err := adapter.Status(ctx, sess.RuntimeRef)
			if err != nil || st.Alive {
				continue
			}
			to := models.SessionStopped
			if st.State == runtime.StateFailed {
				to = models.SessionFailed
			}
			if _, err := s.markSessionState(ctx, sess, to, st.ExitCode,
				fmt.Sprintf("reaped: runtime exited (state=%s)", st.State)); err != nil {
				continue
			}
			count++
		}
	}
	return count, nil
}

func (s *SessionService) getSession(ctx context.Context, id int64) (*models.Session, error) {
	sess, err := s.sessions.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("session")
		}
		return nil, err
	}
	return sess, nil
}

// ListSessions — список с контрактными именами (team_name, role_name, uptime).
func (s *SessionService) ListSessions(ctx context.Context, f repository.SessionFilter) ([]*SessionView, int, error) {
	sessions, total, err := s.sessions.List(ctx, s.db, f)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.viewsFor(ctx, sessions)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

func (s *SessionService) GetSessionView(ctx context.Context, id int64) (*SessionView, error) {
	sess, err := s.getSession(ctx, id)
	if err != nil {
		return nil, err
	}
	views, err := s.viewsFor(ctx, []*models.Session{sess})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// DashboardSessions — активные сессии (dashboard/sessions).
func (s *SessionService) DashboardSessions(ctx context.Context) ([]*SessionView, int, error) {
	return s.ListSessions(ctx, repository.SessionFilter{Active: true, Limit: 100})
}

// DashboardCounts — total/running/failed для summary.
func (s *SessionService) DashboardCounts(ctx context.Context) (total, running, failed int, err error) {
	total, err = s.sessions.CountByStates(ctx, s.db, // все
		models.SessionStarting, models.SessionRunning, models.SessionIdle,
		models.SessionStopping, models.SessionStopped, models.SessionFailed)
	if err != nil {
		return 0, 0, 0, err
	}
	running, err = s.sessions.CountByStates(ctx, s.db, models.SessionStarting, models.SessionRunning, models.SessionIdle)
	if err != nil {
		return 0, 0, 0, err
	}
	failed, err = s.sessions.CountByStates(ctx, s.db, models.SessionFailed)
	return total, running, failed, err
}

func (s *SessionService) requireActiveTeam(ctx context.Context, teamID int64) (*models.Team, error) {
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

func (s *SessionService) requireRoleInTeam(ctx context.Context, roleID, teamID int64) (*models.Role, error) {
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

// transitionTaskAsDaemon — смена состояния задачи от имени daemon (reaper/session).
func (s *SessionService) transitionTaskAsDaemon(ctx context.Context, taskID int64, to models.TaskState, comment string, exit *int) error {
	task, err := s.tasks.GetByID(ctx, s.db, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // задача удалена — не критично
		}
		return err
	}
	if task.State == to || !models.CanTransition(task.State, to) {
		return nil
	}
	now := time.Now().UTC()
	updated := *task
	updated.State = to
	if to == models.TaskInProgress && updated.StartedAt == nil {
		updated.StartedAt = &now
	}
	if err := s.tasks.UpdateState(ctx, s.db, taskID, &updated); err != nil {
		return err
	}
	from := string(task.State)
	h := &models.HistoryEntry{
		QueueTaskID: taskID, FromState: &from, ToState: string(to),
		ActorType: "daemon",
	}
	if comment != "" {
		h.Comment = &comment
	}
	if exit != nil {
		h.Metadata = map[string]any{"exit_code": *exit}
	}
	return s.tHistory.Create(ctx, s.db, h)
}

type SessionView struct {
	ID             int64   `json:"id"`
	TeamID         int64   `json:"team_id"`
	TeamName       string  `json:"team_name"`
	RoleID         int64   `json:"role_id"`
	RoleName       string  `json:"role_name"`
	RuntimeType    string  `json:"runtime_type"`
	RuntimeRef     string  `json:"runtime_ref,omitempty"`
	State          string  `json:"state"`
	QueueTaskID    *int64  `json:"queue_task_id,omitempty"`
	QueueTaskTitle *string `json:"queue_task_title,omitempty"`
	Command        string  `json:"command,omitempty"`
	WorkingDir     string  `json:"working_dir,omitempty"`
	ExitCode       *int    `json:"exit_code,omitempty"`
	StartedAt      *string `json:"started_at,omitempty"`
	StoppedAt      *string `json:"stopped_at,omitempty"`
	UptimeSeconds  *int64  `json:"uptime_seconds,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func (s *SessionService) viewsFor(ctx context.Context, sessions []*models.Session) ([]*SessionView, error) {
	teamName := map[int64]string{}
	roleName := map[int64]string{}
	taskTitle := map[int64]string{}
	var firstErr error
	for _, x := range sessions {
		if firstErr != nil {
			break
		}
		if _, ok := teamName[x.TeamID]; !ok {
			team, err := s.teams.GetByID(ctx, s.db, x.TeamID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					teamName[x.TeamID] = ""
					continue
				}
				firstErr = err
				break
			}
			teamName[x.TeamID] = team.Name
		}
		if _, ok := roleName[x.RoleID]; !ok {
			role, err := s.roles.GetByID(ctx, s.db, x.RoleID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					roleName[x.RoleID] = ""
					continue
				}
				firstErr = err
				break
			}
			roleName[x.RoleID] = role.Name
		}
		if x.QueueTaskID != nil {
			if _, ok := taskTitle[*x.QueueTaskID]; !ok {
				task, err := s.tasks.GetByID(ctx, s.db, *x.QueueTaskID)
				if err != nil {
					if errors.Is(err, repository.ErrNotFound) {
						taskTitle[*x.QueueTaskID] = ""
						continue
					}
					firstErr = err
					break
				}
				taskTitle[*x.QueueTaskID] = task.Title
			}
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	out := make([]*SessionView, 0, len(sessions))
	now := time.Now().UTC()
	for _, x := range sessions {
		v := &SessionView{
			ID: x.ID, TeamID: x.TeamID, TeamName: teamName[x.TeamID],
			RoleID: x.RoleID, RoleName: roleName[x.RoleID],
			RuntimeType: x.RuntimeType, RuntimeRef: x.RuntimeRef,
			State: string(x.State), QueueTaskID: x.QueueTaskID,
			Command: x.Command, WorkingDir: x.WorkingDir, ExitCode: x.ExitCode,
			CreatedAt: x.CreatedAt.Format(rfc3339), UpdatedAt: x.UpdatedAt.Format(rfc3339),
		}
		if x.QueueTaskID != nil {
			v.QueueTaskTitle = ptr(taskTitle[*x.QueueTaskID])
		}
		if x.StartedAt != nil {
			v.StartedAt = ptr(x.StartedAt.Format(rfc3339))
			if x.State.Active() {
				up := int64(now.Sub(*x.StartedAt).Seconds())
				v.UptimeSeconds = &up
			}
		}
		if x.StoppedAt != nil {
			v.StoppedAt = ptr(x.StoppedAt.Format(rfc3339))
		}
		out = append(out, v)
	}
	return out, nil
}

// ---------- History (контракт 20 §6.2) ----------

type SessionHistoryView struct {
	ID          int64          `json:"id"`
	SessionID   int64          `json:"session_id"`
	FromState   *string        `json:"from_state,omitempty"`
	ToState     string         `json:"to_state"`
	ActorType   string         `json:"actor_type"`
	ActorRoleID *int64         `json:"actor_role_id,omitempty"`
	Comment     *string        `json:"comment,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   string         `json:"created_at"`
}

func (s *SessionService) GetSessionHistory(ctx context.Context, id int64, limit, offset int) ([]*SessionHistoryView, int, error) {
	if _, err := s.getSession(ctx, id); err != nil {
		return nil, 0, err
	}
	entries, total, err := s.sHistory.ListBySession(ctx, s.db, id, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*SessionHistoryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, &SessionHistoryView{
			ID: e.ID, SessionID: e.SessionID, FromState: e.FromState, ToState: e.ToState,
			ActorType: e.ActorType, ActorRoleID: e.ActorRoleID, Comment: e.Comment,
			Metadata: e.Metadata, CreatedAt: e.CreatedAt.Format(rfc3339),
		})
	}
	return out, total, nil
}

// ---------- Transcript (контракт 20 §6.4) ----------

type TranscriptEntry struct {
	ID          int64  `json:"id"`
	SessionID   int64  `json:"session_id"`
	MessageType string `json:"message_type"`
	Content     string `json:"content"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

// GetTranscript — последние `limit` непустых строк transcript-файла.
// process/tmux/pi пишут stdout+stderr в logs/sessions/<id>.log.
func (s *SessionService) GetTranscript(ctx context.Context, id int64, limit int) ([]*TranscriptEntry, int, bool, error) {
	if _, err := s.getSession(ctx, id); err != nil {
		return nil, 0, false, err
	}
	path := filepath.Join(s.LogsDir, fmt.Sprintf("session-%d.log", id))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []*TranscriptEntry{}, 0, false, nil
		}
		return nil, 0, false, err
	}
	lines := splitNonEmpty(string(data))
	total := len(lines)
	hasMore := false
	start := 0
	if limit > 0 && total > limit {
		hasMore = true
		start = total - limit
	}
	out := make([]*TranscriptEntry, 0, total-start)
	created := time.Now().UTC().Format(rfc3339)
	for i, line := range lines[start:] {
		out = append(out, &TranscriptEntry{
			ID: int64(start + i + 1), SessionID: id,
			MessageType: "system", Content: line, Role: "system",
			CreatedAt: created,
		})
	}
	return out, total, hasMore, nil
}

func splitNonEmpty(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
