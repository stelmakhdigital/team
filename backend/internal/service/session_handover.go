package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// P1.5 — Session handover + compact-plan.
//
// Compact-plan: живые сессии с context_used_percentage >= sessions.context_wall_pct
// (данные live-полей из transcripts; без usage → omit). Handover: наследник той же
// роли с переданным контекст-пакетом (последние N строк transcript + путь к полному
// файлу + привязанная задача), перенос задачи (существующий handoff), stop предшественника.
// Честный per-step отчёт (ok/failed/skipped + итог succeeded/partial/failed); без
// LLM-summary (вызов вне scope).
//
// Инвариант «одна активная сессия на роль» (CreateSession) требует остановить
// предшественника ДО создания наследника. Фактический порядок: context → stop →
// task-handoff → successor.

const handoverTranscriptTail = 200 // последних строк transcript в контекст-пакете

// ---------- Compact-plan ----------

// CompactPlanEntry — сессия у контекст-стены (compact-plan).
type CompactPlanEntry struct {
	SessionID             int64    `json:"session_id"`
	TeamID                int64    `json:"team_id"`
	TeamName              string   `json:"team_name"`
	RoleID                int64    `json:"role_id"`
	RoleName              string   `json:"role_name"`
	RuntimeType           string   `json:"runtime_type"`
	State                 string   `json:"state"`
	Model                 string   `json:"model,omitempty"`
	WorkingDir            string   `json:"working_dir,omitempty"`
	QueueTaskID           *int64   `json:"queue_task_id,omitempty"`
	ContextUsedPercentage *float64 `json:"context_used_percentage,omitempty"`
	SuggestedAction       string   `json:"suggested_action"` // handover | wait_for_turn
}

// CompactPlan — живые сессии с context_used_percentage >= wall, сортировка desc.
// Возвращает также решённый wall (для UI/отладки).
func (s *SessionService) CompactPlan(ctx context.Context) ([]*CompactPlanEntry, int, error) {
	wall := s.wallPct(ctx)
	views, _, err := s.ListSessions(ctx, repository.SessionFilter{Active: true, Limit: 200})
	if err != nil {
		return nil, 0, err
	}
	out := make([]*CompactPlanEntry, 0)
	for _, v := range views {
		if v.ContextUsedPercentage == nil || *v.ContextUsedPercentage < float64(wall) {
			continue // без usage-данных — omit (неизвестно); ниже порога — не в плане
		}
		action := "handover"
		if v.State == string(models.SessionRunning) {
			action = "wait_for_turn" // ход ещё идёт — безопаснее дождаться конца хода
		}
		out = append(out, &CompactPlanEntry{
			SessionID: v.ID, TeamID: v.TeamID, TeamName: v.TeamName,
			RoleID: v.RoleID, RoleName: v.RoleName, RuntimeType: v.RuntimeType,
			State: v.State, Model: v.Model, WorkingDir: v.WorkingDir,
			QueueTaskID: v.QueueTaskID, ContextUsedPercentage: v.ContextUsedPercentage,
			SuggestedAction: action,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return *out[i].ContextUsedPercentage > *out[j].ContextUsedPercentage
	})
	return out, wall, nil
}

func (s *SessionService) wallPct(ctx context.Context) int {
	if s.ContextWallPctFunc != nil {
		if n, err := s.ContextWallPctFunc(ctx); err == nil && n > 0 && n <= 100 {
			return n
		}
	}
	return 80
}

// ---------- Handover ----------

// HandoverRequest — запрос handover.
type HandoverRequest struct {
	Reason           string `json:"reason"`
	TargetWorkingDir string `json:"target_working_dir,omitempty"` // default — из того же project/working_dir
	DryRun           bool   `json:"dry_run"`
}

// HandoverStep — честный результат одного шага.
type HandoverStep struct {
	Status string `json:"status"` // ok | failed | skipped | would
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// HandoverTaskRef — привязанная задача (в контекст-пакете).
type HandoverTaskRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
	NewID *int64 `json:"new_id,omitempty"` // после handoff (перенос задачи)
}

// HandoverContextPackage — что передаётся наследнику.
type HandoverContextPackage struct {
	ContextFile    string           `json:"context_file"`    // путь к сгенерированному пакету
	TranscriptPath string           `json:"transcript_path"` // путь к полному transcript-файлу
	Lines          int              `json:"lines"`           // сколько строк transcript включено
	Task           *HandoverTaskRef `json:"task,omitempty"`
}

// HandoverResult — результат handover (dry-run или полный прогон).
type HandoverResult struct {
	Status           string                  `json:"status"` // succeeded | partial | failed | planned
	DryRun           bool                    `json:"dry_run"`
	OldSessionID     int64                   `json:"old_session_id"`
	NewSessionID     *int64                  `json:"new_session_id,omitempty"`
	ContextPrepared  HandoverStep            `json:"context_prepared"`
	SuccessorStarted HandoverStep            `json:"successor_started"`
	TaskReassigned   HandoverStep            `json:"task_reassigned"`
	OldStopped       HandoverStep            `json:"old_stopped"`
	ContextPackage   *HandoverContextPackage `json:"context_package,omitempty"`
	SuccessorPreview *HandoverPreview        `json:"successor_preview,omitempty"` // dry-run: кто наследник
}

// HandoverPreview — dry-run: параметры будущего наследника.
type HandoverPreview struct {
	RoleID         int64  `json:"role_id"`
	RoleName       string `json:"role_name"`
	TeamID         int64  `json:"team_id"`
	TeamName       string `json:"team_name"`
	RuntimeType    string `json:"runtime_type"`
	WorkingDir     string `json:"working_dir"`
	HandoffComment string `json:"handoff_comment,omitempty"` // что будет в комментарии задачи
}

// Handover — dry-run (план без мутаций) или полный прогон (с честным per-step отчётом).
func (s *SessionService) Handover(ctx context.Context, id int64, req HandoverRequest) (*HandoverResult, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, NewValidation("reason is required", []FieldError{{Field: "reason", Reason: "required"}})
	}
	sess, err := s.getSession(ctx, id)
	if err != nil {
		return nil, err
	}
	// только живая сессия
	if !sess.State.Active() {
		return nil, NewConflict(fmt.Sprintf("session %d is %s (only active sessions can be handed over)", id, sess.State))
	}
	// только pi|tmux|process (все поддерживаемые)
	switch sess.RuntimeType {
	case "pi", "tmux", "process", "":
	default:
		return nil, NewValidation(fmt.Sprintf("runtime %q does not support handover", sess.RuntimeType))
	}

	role, err := s.roles.GetByID(ctx, s.db, sess.RoleID)
	if err != nil {
		return nil, err
	}
	team, err := s.teams.GetByID(ctx, s.db, sess.TeamID)
	if err != nil {
		return nil, err
	}
	var task *models.Task
	if sess.QueueTaskID != nil {
		if t, err := s.tasks.GetByID(ctx, s.db, *sess.QueueTaskID); err == nil {
			task = t
		}
	}
	handoffComment := fmt.Sprintf("handover from session %d — %s", sess.ID, strings.TrimSpace(req.Reason))

	// dry-run: план без мутаций
	if req.DryRun {
		preview := &HandoverPreview{
			RoleID: role.ID, RoleName: role.Name, TeamID: team.ID, TeamName: team.Name,
			RuntimeType: sess.RuntimeType, WorkingDir: sess.WorkingDir, HandoffComment: handoffComment,
		}
		taskReassign := HandoverStep{Status: "would"}
		if task == nil {
			taskReassign = HandoverStep{Status: "would", Reason: "no task attached"}
		}
		tailCount, _ := s.transcriptTailLines(sess.ID)
		return &HandoverResult{
			Status: "planned", DryRun: true, OldSessionID: id,
			ContextPrepared:  HandoverStep{Status: "would", Detail: fmt.Sprintf("%d строк transcript + полный путь", tailCount)},
			SuccessorStarted: HandoverStep{Status: "would", Detail: fmt.Sprintf("role=%s team=%s runtime=%s dir=%s", role.Name, team.Name, sess.RuntimeType, sess.WorkingDir)},
			TaskReassigned:   taskReassign,
			OldStopped:       HandoverStep{Status: "would"},
			ContextPackage: &HandoverContextPackage{
				ContextFile:    s.handoverContextPath(sess.ID),
				TranscriptPath: s.sessionLogPath(sess.ID),
				Lines:          tailCount,
				Task:           taskRef(task),
			},
			SuccessorPreview: preview,
		}, nil
	}

	// полный прогон: context → stop → task-handoff → successor
	res := &HandoverResult{Status: "succeeded", OldSessionID: id}

	// 1. контекст-пакет (пишем файл)
	tail, tailCount, err := s.readTranscriptTail(sess.ID, handoverTranscriptTail)
	if err != nil {
		res.ContextPrepared = HandoverStep{Status: "failed", Reason: "read transcript: " + err.Error()}
		res.Status = "failed"
		return res, nil
	}
	pkgFile := s.handoverContextPath(sess.ID)
	if err := s.writeContextPackage(pkgFile, sess, role.Name, team.Name, req.Reason, task, tail, tailCount); err != nil {
		res.ContextPrepared = HandoverStep{Status: "failed", Reason: "write context package: " + err.Error()}
		res.Status = "failed"
		return res, nil
	}
	res.ContextPrepared = HandoverStep{Status: "ok", Detail: pkgFile}
	res.ContextPackage = &HandoverContextPackage{
		ContextFile: pkgFile, TranscriptPath: s.sessionLogPath(sess.ID),
		Lines: tailCount, Task: taskRef(task),
	}

	// 2. stop старой сессии (свободит роль — инвариант «одна активная на роль»)
	if _, err := s.StopSession(ctx, sess.ID); err != nil {
		res.OldStopped = HandoverStep{Status: "failed", Reason: err.Error()}
		res.Status = "failed"
		return res, nil
	}
	res.OldStopped = HandoverStep{Status: "ok"}

	// 3. перенос задачи (существующий handoff → та же роль)
	var newTaskID *int64
	if task == nil {
		res.TaskReassigned = HandoverStep{Status: "skipped", Reason: "no task attached"}
	} else if task.State.Terminal() {
		res.TaskReassigned = HandoverStep{Status: "skipped", Reason: fmt.Sprintf("task %d is %s (terminal)", task.ID, task.State)}
	} else if s.TaskHandoffFunc == nil {
		res.TaskReassigned = HandoverStep{Status: "skipped", Reason: "handoff не подключён"}
	} else {
		hr, err := s.TaskHandoffFunc(ctx, HandoffTaskRequest{ID: task.ID, ToRoleID: role.ID, Comment: handoffComment})
		if err != nil {
			res.TaskReassigned = HandoverStep{Status: "skipped", Reason: "handoff неприменим: " + err.Error()}
		} else {
			newTaskID = &hr.NewTask.ID
			ref := taskRef(task)
			ref.NewID = newTaskID
			res.ContextPackage.Task = ref
			res.TaskReassigned = HandoverStep{Status: "ok", Detail: fmt.Sprintf("task %d → %d", task.ID, hr.NewTask.ID)}
		}
	}

	// 4. создание наследника (та же role/team/runtime/working_dir, контекст-пакет в config)
	cfg := map[string]any{
		"handover_from_session": sess.ID,
		"handover_reason":       strings.TrimSpace(req.Reason),
		"handover_context_file": pkgFile,
	}
	if task != nil {
		cfg["handover_task"] = map[string]any{"id": task.ID, "title": task.Title, "state": string(task.State)}
	}
	// pi: контекст в system_prompt (пишется в pi-конфиг); process/tmux: env-переменная.
	if sess.RuntimeType == "pi" {
		cfg["system_prompt"] = s.piHandoverPrompt(sess, role.Name, req.Reason, pkgFile, tail)
	} else {
		cfg["env"] = []string{fmt.Sprintf("HANDOVER_CONTEXT_FILE=%s", pkgFile)}
	}
	wd := req.TargetWorkingDir
	if wd == "" {
		wd = sess.WorkingDir
	}
	newSess, err := s.CreateSession(ctx, team.ID, CreateSessionRequest{
		RoleID: role.ID, QueueTaskID: newTaskID, WorkingDir: wd,
		RuntimeType: sess.RuntimeType, Command: sess.Command,
		Config: cfg,
	})
	if err != nil {
		res.SuccessorStarted = HandoverStep{Status: "failed", Reason: err.Error()}
		res.Status = "partial" // контекст + stop + handoff уже применены; наследник не стартовал
		return res, nil
	}
	res.SuccessorStarted = HandoverStep{Status: "ok", Detail: fmt.Sprintf("session %d started", newSess.ID)}
	res.NewSessionID = &newSess.ID
	return res, nil
}

// ---------- helpers ----------

func taskRef(t *models.Task) *HandoverTaskRef {
	if t == nil {
		return nil
	}
	return &HandoverTaskRef{ID: t.ID, Title: t.Title, State: string(t.State)}
}

func (s *SessionService) handoverContextPath(sessID int64) string {
	if s.LogsDir == "" {
		return ""
	}
	return filepath.Join(s.LogsDir, fmt.Sprintf("handover-%d.md", sessID))
}

// transcriptTailLines — число непустых строк в transcript (для dry-run preview).
func (s *SessionService) transcriptTailLines(sessID int64) (int, error) {
	path := s.sessionLogPath(sessID)
	if path == "" {
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return len(splitNonEmpty(string(data))), nil
}

// readTranscriptTail — последние n непустых строк transcript-файла.
func (s *SessionService) readTranscriptTail(sessID int64, n int) (string, int, error) {
	path := s.sessionLogPath(sessID)
	if path == "" {
		return "", 0, fmt.Errorf("no transcript path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, nil // transcript ещё не записан — честный пустой пакет
		}
		return "", 0, err
	}
	lines := splitNonEmpty(string(data))
	count := len(lines)
	start := 0
	if n > 0 && count > n {
		start = count - n
	}
	return strings.Join(lines[start:], "\n"), count - start, nil
}

// writeContextPackage — пишет контекст-пакет (последние N строк + путь + задача).
func (s *SessionService) writeContextPackage(path string, sess *models.Session, role, teamName, reason string, task *models.Task, tail string, tailCount int) error {
	if path == "" {
		return fmt.Errorf("no context path")
	}
	var b strings.Builder
	b.WriteString("# Handover context\n\n")
	b.WriteString(fmt.Sprintf("- From session: %d (%s, %s)\n", sess.ID, role, teamName))
	b.WriteString(fmt.Sprintf("- Runtime: %s\n", sess.RuntimeType))
	b.WriteString(fmt.Sprintf("- Working dir: %s\n", sess.WorkingDir))
	b.WriteString(fmt.Sprintf("- Reason: %s\n", strings.TrimSpace(reason)))
	if task != nil {
		b.WriteString(fmt.Sprintf("- Task: #%d %q (state=%s)\n", task.ID, task.Title, task.State))
	}
	b.WriteString(fmt.Sprintf("- Full transcript: %s\n", s.sessionLogPath(sess.ID)))
	b.WriteString(fmt.Sprintf("- Lines included: %d (last non-empty)\n\n", tailCount))
	b.WriteString("## Last transcript lines\n\n")
	if tail == "" {
		b.WriteString("(transcript empty)\n")
	} else {
		b.WriteString(tail)
		b.WriteString("\n")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// piHandoverPrompt — контекст-блок для system_prompt pi-наследника.
func (s *SessionService) piHandoverPrompt(sess *models.Session, role, reason, pkgFile, tail string) string {
	var b strings.Builder
	b.WriteString("You are continuing work from a handover of session ")
	b.WriteString(fmt.Sprintf("%d (role %s). Reason: %s. ", sess.ID, role, strings.TrimSpace(reason)))
	b.WriteString("The full context package is at: " + pkgFile + "\n")
	if tail != "" {
		b.WriteString("\nLast transcript lines (context):\n")
		b.WriteString(tail)
	}
	return b.String()
}

// configEnv — env из config (K=V), для process/tmux handover-контекста.
func configEnv(cfg map[string]any) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	switch v := cfg["env"].(type) {
	case []string:
		out = v
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
