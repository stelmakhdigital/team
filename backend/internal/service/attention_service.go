package service

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ---------- P0.1 Attention view ("Что требует меня") ----------
//
// Единый операторский срез: всё, что требует действия человека. Источники
// (реальные данные, без новых миграций):
//
//   - task_escalated: незавершённые задачи с closure_reason = 'escalation' (critical)
//   - task_blocked:   задачи в state = 'blocked' (high) — дедуплицируются с escalated
//   - alert_unread:   непрочитанные watchdog-алерты (severity по типу: blocked→critical,
//                     stale→high, остальные→normal)
//
// Оставлены в схеме, но сейчас пусты/отсутствуют (см. docs/contracts/api-decisions.md):
//   - workflow_attention: P1.4 ещё не существует.
//   - session_failed: у сессий нет модели "непрочитано", поэтому вид опущён.

// Кинды attention-элементов (контракт).
const (
	AttentionKindTaskBlocked        = "task_blocked"
	AttentionKindTaskEscalated      = "task_escalated"
	AttentionKindTaskOverdue        = "task_overdue" // P0.2: closure_required_at просрочен
	AttentionKindAlertUnread        = "alert_unread"
	AttentionKindWorkflowAttention  = "workflow_attention"
	AttentionKindSessionFailed      = "session_failed"
	AttentionKindSessionContextWall = "session_context_wall" // P1.5: сессия у контекст-стены
)

// AttentionLink — целевая деталка для UI-CTA (EntityLink → path).
type AttentionLink struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

// AttentionItem — один элемент среза "требует внимания".
type AttentionItem struct {
	Kind            string        `json:"kind"`
	ID              string        `json:"id"`
	Severity        string        `json:"severity"` // critical | high | normal
	Title           string        `json:"title"`
	Detail          string        `json:"detail"`
	CreatedAt       string        `json:"created_at"`
	SuggestedAction string        `json:"suggested_action"`
	Link            AttentionLink `json:"link"`
}

// AttentionResult — ответ GET /api/v1/attention.
type AttentionResult struct {
	Total int             `json:"total"`
	Items []AttentionItem `json:"items"`
}

type attentionTaskStore interface {
	List(ctx context.Context, tx repository.DBTX, f repository.TaskFilter) ([]*models.Task, int, error)
}

type attentionAlertStore interface {
	List(ctx context.Context, tx repository.DBTX, f repository.AlertFilter) ([]*models.WatchdogEvent, int, error)
}

type attentionTeamStore interface {
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Team, error)
}

// AttentionService — агрегация источников внимания.
type AttentionService struct {
	db        *sql.DB
	teams     attentionTeamStore
	tasks     attentionTaskStore
	alerts    attentionAlertStore
	instances *repository.WorkflowInstanceRepo // P1.4: workflow_attention
	wf        *repository.WorkflowRepo         // P1.4: workflow name
	// P1.5: compact-plan (session_context_wall); nil = без источника.
	contextWallFunc func(ctx context.Context) ([]*CompactPlanEntry, error)
	now             func() time.Time
}

func NewAttentionService(db *sql.DB, s *repository.Stores) *AttentionService {
	return &AttentionService{db: db, teams: s.Teams, tasks: s.Tasks, alerts: s.Watchdog,
		instances: s.WorkflowInstances, wf: s.Workflows, now: time.Now}
}

// SetContextWallFunc — P1.5: подвязка compact-plan (session_context_wall).
func (s *AttentionService) SetContextWallFunc(f func(ctx context.Context) ([]*CompactPlanEntry, error)) {
	s.contextWallFunc = f
}

// attentionFetchCap — защита от неограниченной выборки (attention-срез мал по
// сути; limit/offset поверх уже отсортированного набора).
const attentionFetchCap = 500

// List собирает все attention-элементы, сортирует (severity desc → created_at asc),
// применяет limit/offset. total = полное число элементов (для badge-счётчика).
func (s *AttentionService) List(ctx context.Context, limit, offset int) (*AttentionResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	now := s.now()

	type entry struct {
		item  AttentionItem
		since time.Time
	}
	entries := make([]entry, 0, 16)
	escalated := map[int64]bool{}
	shown := map[int64]bool{} // все уже выпущенные task-ID (дедуп overdue, P0.2)

	// 1) escalated (critical) — незавершённые задачи с closure_reason = escalation
	escStr := string(models.ClosureEscalation)
	escTasks, _, err := s.tasks.List(ctx, s.db, repository.TaskFilter{ActiveOnly: true, ClosureReason: &escStr, Limit: attentionFetchCap})
	if err != nil {
		return nil, fmt.Errorf("attention: list escalated tasks: %w", err)
	}
	for _, t := range escTasks {
		escalated[t.ID] = true
		shown[t.ID] = true
		since := t.UpdatedAt
		if t.CreatedAt.After(since) {
			since = t.CreatedAt
		}
		entries = append(entries, entry{item: s.taskItem(ctx, t, AttentionKindTaskEscalated, "critical", since, now), since: since})
	}

	// 2) blocked (high) — дедуп: escalated-задачи уже учтены выше
	blockedState := models.TaskBlocked
	blockedTasks, _, err := s.tasks.List(ctx, s.db, repository.TaskFilter{State: &blockedState, Limit: attentionFetchCap})
	if err != nil {
		return nil, fmt.Errorf("attention: list blocked tasks: %w", err)
	}
	for _, t := range blockedTasks {
		if escalated[t.ID] {
			continue
		}
		shown[t.ID] = true
		since := t.CreatedAt
		if t.BlockedSince != nil {
			since = *t.BlockedSince
		}
		entries = append(entries, entry{item: s.taskItem(ctx, t, AttentionKindTaskBlocked, "high", since, now), since: since})
	}

	// 3) overdue (high, P0.2) — in_progress/blocked с просроченным closure_required_at.
	//    Дедуп: задачи, уже выпущенные как escalated/blocked, не дублируются.
	candTasks, _, err := s.tasks.List(ctx, s.db, repository.TaskFilter{
		States:                 []models.TaskState{models.TaskInProgress, models.TaskBlocked},
		ClosureRequiredNotNull: true,
		Limit:                  attentionFetchCap,
	})
	if err != nil {
		return nil, fmt.Errorf("attention: list overdue candidates: %w", err)
	}
	for _, t := range candTasks {
		if shown[t.ID] || t.ClosureRequiredAt == nil || !t.ClosureRequiredAt.Before(now) {
			continue
		}
		shown[t.ID] = true
		since := *t.ClosureRequiredAt
		entries = append(entries, entry{item: s.taskOverdueItem(ctx, t, since, now), since: since})
	}

	// 4) unread watchdog alerts (severity по типу)
	alerts, _, err := s.alerts.List(ctx, s.db, repository.AlertFilter{UnreadOnly: true, Limit: attentionFetchCap})
	if err != nil {
		return nil, fmt.Errorf("attention: list unread alerts: %w", err)
	}
	for _, a := range alerts {
		sev, action := alertSeverityAndAction(a.EventType)
		entries = append(entries, entry{item: s.alertItem(ctx, a, sev, action), since: a.CreatedAt})
	}

	// 5) workflow_attention (P1.4) — failed (critical) + waiting (high) instances.
	if s.instances != nil {
		insts, err := s.instances.ListByStates(ctx, s.db, models.WfInstanceFailed, models.WfInstanceWaiting)
		if err != nil {
			return nil, fmt.Errorf("attention: list workflow instances: %w", err)
		}
		for _, inst := range insts {
			entries = append(entries, entry{item: s.workflowItem(ctx, inst), since: inst.UpdatedAt})
		}
	}

	// 6) session_context_wall (P1.5) — сессии у контекст-стены (high).
	if s.contextWallFunc != nil {
		wallEntries, err := s.contextWallFunc(ctx)
		if err != nil {
			return nil, fmt.Errorf("attention: compact-plan: %w", err)
		}
		for _, e := range wallEntries {
			entries = append(entries, entry{item: s.contextWallItem(e), since: now})
		}
	}

	// Сортировка: severity desc (critical>high>normal) → created_at asc (старое дольше ждёт).
	sort.SliceStable(entries, func(i, j int) bool {
		ri, rj := severityRank(entries[i].item.Severity), severityRank(entries[j].item.Severity)
		if ri != rj {
			return ri < rj
		}
		return entries[i].since.Before(entries[j].since)
	})

	total := len(entries)
	items := make([]AttentionItem, 0, limit)
	for i := offset; i < len(entries) && len(items) < limit; i++ {
		it := entries[i].item
		it.CreatedAt = entries[i].since.Format(rfc3339)
		items = append(items, it)
	}
	return &AttentionResult{Total: total, Items: items}, nil
}

func (s *AttentionService) taskItem(ctx context.Context, t *models.Task, kind, severity string, since, now time.Time) AttentionItem {
	teamName := s.teamName(ctx, t.TeamID)
	action := "Разблокировать"
	if kind == AttentionKindTaskEscalated {
		action = "Разобрать эскалацию"
	}
	detail := fmt.Sprintf("Team %s · ждёт %s", teamName, humanDuration(now.Sub(since)))
	return AttentionItem{
		Kind:            kind,
		ID:              strconv.FormatInt(t.ID, 10),
		Severity:        severity,
		Title:           t.Title,
		Detail:          detail,
		CreatedAt:       since.Format(rfc3339),
		SuggestedAction: action,
		Link:            AttentionLink{Path: fmt.Sprintf("/tasks/%d", t.ID), Label: t.Title},
	}
}

// taskOverdueItem — P0.2: просроченная задача (closure_required_at < now).
func (s *AttentionService) taskOverdueItem(ctx context.Context, t *models.Task, since, now time.Time) AttentionItem {
	teamName := s.teamName(ctx, t.TeamID)
	detail := fmt.Sprintf("Team %s · просрочено на %s", teamName, humanDuration(now.Sub(since)))
	return AttentionItem{
		Kind:            AttentionKindTaskOverdue,
		ID:              strconv.FormatInt(t.ID, 10),
		Severity:        "high",
		Title:           t.Title,
		Detail:          detail,
		CreatedAt:       since.Format(rfc3339),
		SuggestedAction: "Открыть задачу",
		Link:            AttentionLink{Path: fmt.Sprintf("/tasks/%d", t.ID), Label: t.Title},
	}
}

func (s *AttentionService) alertItem(ctx context.Context, a *models.WatchdogEvent, severity, action string) AttentionItem {
	teamName := s.teamName(ctx, a.TeamID)
	detail := a.Description
	if teamName != "" {
		detail = teamName + " · " + detail
	}
	detail = detail + " · " + humanDuration(s.now().Sub(a.CreatedAt))
	var link AttentionLink
	switch {
	case a.SessionID != nil:
		link = AttentionLink{Path: fmt.Sprintf("/sessions/%d", *a.SessionID), Label: "Сессия " + strconv.FormatInt(*a.SessionID, 10)}
	case a.QueueTaskID != nil:
		link = AttentionLink{Path: fmt.Sprintf("/tasks/%d", *a.QueueTaskID), Label: "Задача " + strconv.FormatInt(*a.QueueTaskID, 10)}
	default:
		link = AttentionLink{Path: "/dashboard", Label: "Дашборд"}
	}
	return AttentionItem{
		Kind:            AttentionKindAlertUnread,
		ID:              strconv.FormatInt(a.ID, 10),
		Severity:        severity,
		Title:           "Watchdog: " + a.EventType,
		Detail:          detail,
		CreatedAt:       a.CreatedAt.Format(rfc3339),
		SuggestedAction: action,
		Link:            link,
	}
}

func (s *AttentionService) teamName(ctx context.Context, teamID int64) string {
	if teamID == 0 {
		return "—"
	}
	team, err := s.teams.GetByID(ctx, s.db, teamID)
	if err != nil || team == nil {
		return "#" + strconv.FormatInt(teamID, 10)
	}
	return team.Name
}

// contextWallItem — P1.5: session_context_wall (high) — сессия у контекст-стены.
func (s *AttentionService) contextWallItem(e *CompactPlanEntry) AttentionItem {
	pct := "?"
	if e.ContextUsedPercentage != nil {
		pct = strconv.FormatInt(int64(*e.ContextUsedPercentage), 10) + "%"
	}
	title := "Context wall: " + e.RoleName
	if e.Model != "" {
		title += " (" + e.Model + ")"
	}
	action := "Handover"
	if e.SuggestedAction == "wait_for_turn" {
		action = "Дождаться конца хода"
	}
	detail := "context " + pct
	if e.WorkingDir != "" {
		detail += " · " + e.WorkingDir
	}
	return AttentionItem{
		Kind:            AttentionKindSessionContextWall,
		ID:              "ctxwall-" + strconv.FormatInt(e.SessionID, 10),
		Severity:        "high",
		Title:           title,
		Detail:          detail,
		CreatedAt:       time.Now().UTC().Format(rfc3339),
		SuggestedAction: action,
		Link:            AttentionLink{Path: fmt.Sprintf("/sessions/%d", e.SessionID), Label: title},
	}
}

// workflowItem — P1.4: workflow_attention (failed → critical; waiting → high).
func (s *AttentionService) workflowItem(ctx context.Context, inst *models.WorkflowInstance) AttentionItem {
	wfName := s.workflowName(ctx, inst.WorkflowID)
	title := "Workflow: " + wfName
	severity := "high"
	action := "Разобрать ожидание"
	detail := "ждёт (кто/на чём)"
	if inst.State == models.WfInstanceFailed {
		severity = "critical"
		action = "Resume / abort"
		detail = "инстанс упал — resume или abort"
	}
	path := fmt.Sprintf("/workflows/%d/instances/%d", inst.WorkflowID, inst.ID)
	return AttentionItem{
		Kind:            AttentionKindWorkflowAttention,
		ID:              "wf-" + strconv.FormatInt(inst.ID, 10),
		Severity:        severity,
		Title:           title,
		Detail:          detail,
		CreatedAt:       inst.UpdatedAt.Format(rfc3339),
		SuggestedAction: action,
		Link:            AttentionLink{Path: path, Label: title},
	}
}

func (s *AttentionService) workflowName(ctx context.Context, workflowID int64) string {
	if s.wf == nil {
		return "#" + strconv.FormatInt(workflowID, 10)
	}
	wf, err := s.wf.GetByID(ctx, s.db, workflowID)
	if err != nil || wf == nil {
		return "#" + strconv.FormatInt(workflowID, 10)
	}
	return wf.Name
}

// alertSeverityAndAction — severity/сuggested action по типу алерта.
func alertSeverityAndAction(eventType string) (severity, action string) {
	switch eventType {
	case "blocked":
		return "critical", "Разобрать блокировку"
	case "watchdog_job": // P1.6: сработал watchdog-джоб (context-threshold)
		return "high", "Посмотреть сессию"
	case "stale":
		return "high", "Проверить сессию"
	default:
		return "normal", "Посмотреть"
	}
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "high":
		return 1
	default:
		return 2
	}
}

// humanDuration — компактный человекочитаемый elapsed ("5m", "2h 10m", "3d 4h").
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "только что"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dм", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dч", h)
		}
		return fmt.Sprintf("%dч %dм", h, m)
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	if h == 0 {
		return fmt.Sprintf("%dд", days)
	}
	return fmt.Sprintf("%dд %dч", days, h)
}
