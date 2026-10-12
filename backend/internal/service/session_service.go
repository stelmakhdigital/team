package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	UpdateConfig(ctx context.Context, tx repository.DBTX, id int64, s *models.Session) error
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
	projects *repository.ProjectRepo // P1.0
	// LogsDir — transcript-файлы (logs/sessions/<id>.log).
	// ConfigsDir — конфиги pi-сессий (configs/sessions/<id>.yaml).
	LogsDir    string
	ConfigsDir string
	// Bus — real-time события (session.started/session.stopped); nil = без событий.
	Bus *EventBus
	// WatchdogEvents — drift-алерт при обнаружении мёртвого runtime (Reap).
	// nil = алерты не создаются (только transition + событие).
	WatchdogEvents *repository.WatchdogRepo
	// P1.5 — compact-plan: sessions.context_wall_pct в рантайме (layered config).
	// nil = дефолт 80.
	ContextWallPctFunc func(ctx context.Context) (int, error)
	// P2.14 — cost: оценка стоимости сессий (nil = стоимость не считается).
	cost *CostService
	// P3.12 — agent_image: снапшот сессии (создаёт library-элемент type=agent_image).
	// nil = snapshot недоступен (404/501 в handler).
	library         *repository.LibraryRepo
	libraryVersions *repository.LibraryVersionRepo // P3.12: append-only версии снапшота
	// P1.5 — handover: переиспользуем существующую механику handoff задачи
	// (TaskService.HandoffTask). nil = task-шаг handover skipped.
	TaskHandoffFunc func(ctx context.Context, req HandoffTaskRequest) (*HandoffTaskResult, error)
	// P1.7 — briefing builder (nil = briefing выключен для сессии). Возвращает
	// собранный брифинг (Text + Data) по role/task/project.
	BriefingFunc func(ctx context.Context, p BriefingParams) *Briefing
	// RoleSpecFunc — резолвит agent_spec роли (путь) в AgentSpec (nil если не найден).
	// QA-fix: в CreateSession, если runtime_type не задан в запросе, определяется из spec
	// (pi_config.model → pi; иначе spec.runtime.type), а model подставляется в config.
	// nil = runtime определяется только из запроса (старое поведение).
	RoleSpecFunc func(specPath string) *AgentSpec
	// tails — tailer'ы session.output (live-терминал, slice 7); tailMu — защита.
	tailMu sync.Mutex
	tails  map[int64]*tailState
}

func NewSessionService(db *sql.DB, s *repository.Stores, rt *runtime.Registry) *SessionService {
	return &SessionService{
		db: db, runt: rt,
		teams: s.Teams, roles: s.Roles, tasks: s.Tasks, tHistory: s.History,
		sessions: s.Sessions, sHistory: s.SessionHistory, projects: s.Projects,
		LogsDir: "logs/sessions", ConfigsDir: "configs/sessions",
		tails: make(map[int64]*tailState),
	}
}

// SetTestRuntimeOverride — test hook: замена адаптера рантайма (детерминированные
// тесты merge/launch). Не используется в проде.
func (s *SessionService) SetTestRuntimeOverride(runtimeType string, a runtime.Adapter) {
	if s.runt != nil {
		s.runt.SetOverride(runtimeType, a)
	}
}

// SetCostService — P2.14: подключить расчёт стоимости (оценка по токенам).
func (s *SessionService) SetCostService(c *CostService) { s.cost = c }

// SetLibraryRepo — P3.12: подключить LibraryRepo (для снапшота сессии в agent_image).
func (s *SessionService) SetLibraryRepo(r *repository.LibraryRepo) { s.library = r }

// SetLibraryVersionRepo — P3.12: подключить LibraryVersionRepo (версии снапшота).
func (s *SessionService) SetLibraryVersionRepo(r *repository.LibraryVersionRepo) {
	s.libraryVersions = r
}

type CreateSessionRequest struct {
	RoleID      int64          `json:"role_id"`
	QueueTaskID *int64         `json:"queue_task_id,omitempty"`
	ProjectID   *int64         `json:"project_id,omitempty"` // P1.0: явный проект (опционально)
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
	// T4: role.Config (pi_config override) → в session config. Приоритет:
	// explicit session config > role override > agent_spec дефолт (spec-резолв ниже).
	// layout не переносится (позиция на канвасе, не session config).
	if len(role.Config) > 0 {
		for k, v := range role.Config {
			if k == "layout" {
				continue
			}
			if req.Config == nil {
				req.Config = map[string]any{}
			}
			if _, ok := req.Config[k]; !ok {
				req.Config[k] = v
			}
		}
	}
	rtType := req.RuntimeType
	if rtType == "" && req.Config != nil {
		if v, ok := req.Config["runtime_type"].(string); ok && v != "" {
			rtType = v
		}
	}
	// QA-fix: runtime_type не задан в запросе → резолвим из agent-spec роли
	// (pi_config.model → pi; иначе spec.runtime.type). model из spec подставляем в config
	// (нужен для pi-конфига). Без этого briefing-launch падал 400 (дефолт process без command).
	// T5: roleSpec — base (spec pi_config) для merge c role.Config["agent_resources"].
	var roleSpec *AgentSpec
	if s.RoleSpecFunc != nil && role.AgentSpec != "" {
		if spec := s.RoleSpecFunc(role.AgentSpec); spec != nil {
			roleSpec = spec
			if rtType == "" {
				if spec.PiConfig.Model != "" {
					rtType = "pi"
				} else if spec.Runtime.Type != "" {
					rtType = spec.Runtime.Type
				}
			}
			if spec.PiConfig.Model != "" {
				if req.Config == nil {
					req.Config = map[string]any{}
				}
				if _, ok := req.Config["model"]; !ok {
					req.Config["model"] = spec.PiConfig.Model
				}
			}
		}
	}
	if rtType == "" {
		rtType = "process"
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

	// P1.0: разрешение working_dir + project_id (explicit > task.project > team.project)
	workingDir, projectID, err := resolveWorkingDir(ctx, s.db, s.projects, req.WorkingDir, req.ProjectID, task, team)
	if err != nil {
		return nil, err
	}

	// P1.7: собрать briefing (read-only, до tx). nil-Builder / disabled → none.
	var briefingText, briefingApplied string
	if s.BriefingFunc != nil && !briefingDisabled(req.Config) {
		var base *Briefing
		override := cfgStr(req.Config, "briefing_override")
		if strings.TrimSpace(override) != "" {
			base = &Briefing{} // override: base не нужен
		} else {
			base = s.BriefingFunc(ctx, BriefingParams{
				TeamID: team.ID, RoleID: role.ID, QueueTaskID: req.QueueTaskID,
				WorkingDir: workingDir, ProjectID: projectID,
			})
		}
		briefingText, briefingApplied = ApplyBriefingOverrides(base, override, cfgStr(req.Config, "briefing_append"))
	}
	// P1.7: поля, известные до insert → в config (персистится sessions.Create).
	if briefingApplied != "none" && strings.TrimSpace(briefingText) != "" {
		if req.Config == nil {
			req.Config = map[string]any{}
		}
		req.Config["briefing_applied"] = briefingApplied
		req.Config["briefing_text"] = briefingText
		if rtType == "pi" {
			if existing := cfgStr(req.Config, "system_prompt"); existing != "" {
				req.Config["system_prompt"] = strings.TrimSpace(briefingText) + "\n\n" + existing
			} else {
				req.Config["system_prompt"] = strings.TrimSpace(briefingText)
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	sess := &models.Session{
		TeamID: team.ID, RoleID: role.ID, QueueTaskID: req.QueueTaskID, ProjectID: projectID,
		RuntimeType: rtType, State: models.SessionStarting,
		WorkingDir: workingDir, Command: command, Config: req.Config,
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

	// P1.7: process/tmux-доставка — файл + env + path (pi-системный prompt уже в config до tx).
	if briefingApplied != "none" && strings.TrimSpace(briefingText) != "" && rtType != "pi" {
		if bp := s.briefingFilePath(sess.ID); bp != "" {
			if err := s.writeBriefingFile(bp, briefingText); err == nil {
				req.Config["briefing_path"] = bp
				req.Config["env"] = appendConfigEnv(req.Config["env"], "BRIEFING_FILE="+bp)
				if err := s.sessions.UpdateConfig(ctx, s.db, sess.ID, sess); err != nil {
					_ = err // путь/внешние данные уже готовы; персист-ошибка не ломает старт
				}
			}
		}
	}

	// конфиг для pi
	var configFile string
	var piFields *runtime.AgentSpecFields
	if rtType == "pi" {
		configFile = filepath.Join(s.ConfigsDir, fmt.Sprintf("session-%d.yaml", sess.ID))
		fields := runtime.AgentSpecFields{Name: role.Name}
		if m, ok := sess.Config["model"].(string); ok {
			fields.Model = m
		}
		if p, ok := sess.Config["system_prompt"].(string); ok {
			fields.SystemPrompt = p
		}
		if t, ok := sess.Config["thinking"].(string); ok {
			fields.Thinking = t
		}
		// P3.12: skills/tools из config (launch из agent_image / явная настройка).
		if sk := stringSliceFromAny(sess.Config["skills"]); len(sk) > 0 {
			fields.Skills = sk
		}
		if tl := stringSliceFromAny(sess.Config["tools"]); len(tl) > 0 {
			fields.Tools = tl
		}
		if sl := cfgStr(sess.Config, "sandbox_level"); sl != "" {
			fields.SandboxLevel = sl
		}
		if ne, _ := sess.Config["no_extensions"].(bool); ne {
			fields.NoExtensions = true
		}
		// T4: pi_config override из role.Config (temperature/max_tokens/mcp/startup_files).
		// pi не поддерживает --temperature/--max-tokens/--mcp как CLI-флаги → значения
		// пишутся в конфиг сессии (reference) + персистятся в role.Config.
		if tmp, ok := cfgFloat(sess.Config, "temperature"); ok {
			fields.Temperature = tmp
		}
		if mt, ok := cfgInt(sess.Config, "max_tokens"); ok {
			fields.MaxTokens = mt
		}
		if ms := mcpServersFromAny(sess.Config["mcp"]); len(ms) > 0 {
			fields.MCPServers = ms
		}
		if sf := startupFilesFromAny(sess.Config["startup_files"]); len(sf) > 0 {
			fields.StartupFiles = sf
		}
		// T5: Agent resources — merge base (spec) + override (role.Config["agent_resources"]).
		// skills (enabled) → --skill; startup files (orientation=system) → system_prompt;
		// plugins → конфиг сессии (persist). Override перекрывает base по key (name/path).
		if roleSpec != nil {
			ovPlugins, ovSkills, ovFiles := agentResourcesOverride(sess.Config)
			// skills: merged (base + override), если явно не задан (P3.12 flat)
			if len(fields.Skills) == 0 {
				fields.Skills = mergeEnabledSkills(roleSpec.PiConfig.Skills, ovSkills)
			}
			// startup files: merged (base + override) — перекрывает flat T4
			mergedFiles := mergeStartupFiles(roleSpec.Startup.Files, ovFiles)
			if len(mergedFiles) > 0 {
				fields.StartupFiles = mergedFiles
			}
			// startup files (orientation=system) → system_prompt (prepend)
			var systemPaths []string
			for _, f := range mergedFiles {
				if f.Orientation == "system" {
					systemPaths = append(systemPaths, f.Path)
				}
			}
			if len(systemPaths) > 0 {
				header := "## Startup files (system)\n" + strings.Join(systemPaths, "\n") + "\n\n"
				if fields.SystemPrompt != "" {
					fields.SystemPrompt = header + fields.SystemPrompt
				} else {
					fields.SystemPrompt = header
				}
			}
			// plugins: merged (base + override) → конфиг сессии (persist)
			if mp := mergePlugins(roleSpec.PiConfig.Plugins, ovPlugins); len(mp) > 0 {
				sess.Config["plugins"] = mp
			}
		}
		if err := runtime.WriteSessionConfig(configFile, fields); err != nil {
			return nil, err
		}
		piFields = &fields // QA-fix: передаём поля адаптеру (строит pi CLI-флаги)
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
		Env:        configEnv(req.Config), // P1.5: env из config (handover-контекст для process/tmux)
		OutputFile: outFile,
		ConfigFile: configFile,
		ConfigsDir: s.ConfigsDir,
		Pi:         piFields, // QA-fix: pi-адаптер строит --model/--system-prompt/... из полей
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
		"role_id":      role.ID,
		"role_name":    role.Name,
		"runtime_type": rtType,
	}, fmt.Sprintf("team:%d", sess.TeamID), fmt.Sprintf("session:%d", sess.ID)))
	s.startOutputTailer(sess.ID, sess.TeamID, role.Name, outFile) // slice 7: live-терминал (no-op без Bus)
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
		s.stopOutputTailer(sess.ID)
		roleName := ""
		if r, err := s.roles.GetByID(ctx, s.db, sess.RoleID); err == nil {
			roleName = r.Name
		}
		data := map[string]any{"session_id": sess.ID, "role_id": sess.RoleID, "role_name": roleName, "state": string(to)}
		if exitCode != nil {
			data["exit_code"] = *exitCode
		}
		// T2: корректное событие — session.failed для failed, session.stopped для stopped.
		// (раньше всегда session.stopped, даже для failed)
		evt := "session.stopped"
		if to == models.SessionFailed {
			evt = "session.failed"
		}
		s.Bus.Publish(newEvent(evt, data,
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
				"runtime died / watchdog: process not alive"); err != nil {
				continue
			}
			// T2: drift-алерт (attention-item) при обнаружении мёртвого runtime.
			// Только если НЕ остановлена пользователем (StoppedByUs): user-stop —
			// ожидаемо, не «смерть». Дедуп по unread drift на сессию.
			if !st.StoppedByUs {
				s.createDeadRuntimeAlert(ctx, sess, to, st.ExitCode)
			}
			count++
		}
	}
	return count, nil
}

// createDeadRuntimeAlert — drift-алерт (attention-item) при обнаружении мёртвого
// runtime (Reap). Type "drift", high, RequiresAction. Дедуп: если уже есть unread
// drift-алерт на эту сессию — не дублируем. nil WatchdogEvents = без алерта.
func (s *SessionService) createDeadRuntimeAlert(ctx context.Context, sess *models.Session, to models.SessionState, exitCode *int) {
	if s.WatchdogEvents == nil {
		return
	}
	if exists, err := s.WatchdogEvents.ExistsUnread(ctx, s.db, "drift", sess.QueueTaskID, &sess.ID); err == nil && exists {
		return
	}
	code := "n/a"
	if exitCode != nil {
		code = fmt.Sprint(*exitCode)
	}
	desc := fmt.Sprintf("session %d: runtime died (process not alive; state=%s, exit_code=%s) — watchdog",
		sess.ID, to, code)
	ev := &models.WatchdogEvent{
		TeamID: sess.TeamID, SessionID: &sess.ID, RoleID: &sess.RoleID,
		QueueTaskID: sess.QueueTaskID,
		EventType:   "drift", Severity: "high",
		Description: desc, RequiresAction: true,
	}
	_ = s.WatchdogEvents.Create(ctx, s.db, ev)
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
	// P1.7: canonical-текст брифинга (только в detail, чтобы не раздувать list)
	if v := views[0]; v != nil {
		if bt := cfgStr(sess.Config, "briefing_text"); bt != "" {
			v.BriefingText = bt
		}
		// P3.16: posture (override сессии + effective + postura команды).
		if sess.Posture != nil {
			p := string(*sess.Posture)
			v.Posture = &p
		}
		teamPosture := models.PostureDelegated
		if team, terr := s.teams.GetByID(ctx, s.db, sess.TeamID); terr == nil && team.Posture.Valid() {
			teamPosture = team.Posture
		}
		v.TeamPosture = string(teamPosture)
		if sess.Posture != nil && sess.Posture.Valid() {
			v.EffectivePosture = string(*sess.Posture)
		} else {
			v.EffectivePosture = string(teamPosture)
		}
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
	ProjectID      *int64  `json:"project_id,omitempty"` // P1.0
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
	// Live-метрики (slice 7, контракт 20 §3.6). опциональные: null/omit =
	// неизвестно (рантайм не отдаёт) → UI показывает «--». Additive.
	Model                    string   `json:"model,omitempty"`
	ContextUsedPercentage    *float64 `json:"context_used_percentage,omitempty"`
	ContextTotalInputTokens  *int64   `json:"context_total_input_tokens,omitempty"`
	ContextTotalOutputTokens *int64   `json:"context_total_output_tokens,omitempty"`
	// P2.14 — оценка стоимости (estimate). null/omit = «нет данных» (не 0).
	EstimatedCost *CostInfo `json:"estimated_cost,omitempty"`
	LogPath       string    `json:"log_path,omitempty"`
	// P1.7 — briefing (startup context): что передано агенту.
	BriefingApplied string `json:"briefing_applied,omitempty"` // builtin|override|none
	BriefingPath    string `json:"briefing_path,omitempty"`    // файл (process/tmux)
	BriefingText    string `json:"briefing_text,omitempty"`    // canonical markdown (detail)
	// P3.16 — Mode/permission posture (delegated vs human-led).
	Posture          *string `json:"posture,omitempty"` // override сессии (null = наследует от команды)
	EffectivePosture string  `json:"effective_posture"` // resolved (badge)
	TeamPosture      string  `json:"team_posture"`      // postura команды (для "inherit")
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
			ID: x.ID, TeamID: x.TeamID, ProjectID: x.ProjectID, TeamName: teamName[x.TeamID],
			RoleID: x.RoleID, RoleName: roleName[x.RoleID],
			RuntimeType: x.RuntimeType, RuntimeRef: x.RuntimeRef,
			State: string(x.State), QueueTaskID: x.QueueTaskID,
			Command: x.Command, WorkingDir: x.WorkingDir, ExitCode: x.ExitCode,
			CreatedAt: x.CreatedAt.Format(rfc3339), UpdatedAt: x.UpdatedAt.Format(rfc3339),
			LogPath: s.sessionLogPath(x.ID), Model: s.sessionModel(x),
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
		s.enrichLiveMetrics(v, x)
		// P2.14: оценка стоимости (estimate) по токенам + цене модели.
		if s.cost != nil {
			v.EstimatedCost = s.cost.SessionCost(ctx, v.ContextTotalInputTokens, v.ContextTotalOutputTokens, v.Model)
		}
		// P1.7: briefing из config (только applied+path в списке; text — в detail)
		if ba := cfgStr(x.Config, "briefing_applied"); ba != "" {
			v.BriefingApplied = ba
		}
		if bp := cfgStr(x.Config, "briefing_path"); bp != "" {
			v.BriefingPath = bp
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
	sess, err := s.getSession(ctx, id)
	if err != nil {
		return nil, 0, false, err
	}
	path := filepath.Join(s.LogsDir, fmt.Sprintf("session-%d.log", id))
	data, rerr := os.ReadFile(path)
	var lines []string
	if rerr == nil && len(data) > 0 {
		lines = splitNonEmpty(string(data))
	} else if rerr == nil || os.IsNotExist(rerr) {
		// T2: лог-файл пуст/отсутствует (pi DirectTTY — TUI не пишет в лог) →
		// capture on-the-fly. process → ErrNoScreen → пусто (как раньше).
		lines = s.captureTranscript(ctx, sess, 200)
	} else {
		return nil, 0, false, rerr
	}
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

// captureTranscript — on-the-fly capture текущего экрана сессии (pi DirectTTY / tmux).
// Используется, когда лог-файл пуст (TUI не пишет в лог). process → ErrNoScreen → nil;
// мёртвая сессия → ошибка → nil (пусто, как раньше).
func (s *SessionService) captureTranscript(ctx context.Context, sess *models.Session, lines int) []string {
	if sess.RuntimeRef == "" {
		return nil
	}
	adapter, err := s.runt.Get(sess.RuntimeType)
	if err != nil {
		return nil
	}
	captured, err := adapter.Capture(ctx, sess.RuntimeRef, lines)
	if err != nil {
		return nil
	}
	return captured
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
