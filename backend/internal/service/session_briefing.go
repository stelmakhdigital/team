package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// P1.7 — Session briefing (startup context).
//
// Брифинг — компактный контекст «кто я, что делать, где работаю, что
// случилось», который демон собирает при запуске сессии (+ handover) и передаёт
// агенту. Canonical = markdown-строка (briefing_text) + briefing_data (JSON).
// Пустые источники / ошибки → omit (не ошибки). Bounded-ограничения на все
// списки; git — read-only (`git -C <path>`), ошибка/нет git → omit.

const (
	briefingMaxTaskEvents   = 5
	briefingMaxDirectMsgs   = 3
	briefingMaxChatMsgs     = 2
	briefingMaxAlerts       = 3
	briefingMaxGitCommits   = 5
	briefingMaxProjectTasks = 5
	briefingMsgTruncate     = 500
	briefingBodyTruncate    = 2000
)

// BriefingParams — что собирать.
type BriefingParams struct {
	TeamID      int64
	RoleID      int64
	QueueTaskID *int64
	WorkingDir  string
	ProjectID   *int64
}

// ---------- structured data (JSON) ----------

type BriefingTaskEvent struct {
	Kind string `json:"kind"` // transition
	Text string `json:"text"`
	At   string `json:"at"`
}

type BriefingTask struct {
	ID      int64               `json:"id"`
	Title   string              `json:"title"`
	State   string              `json:"state"`
	Body    string              `json:"body,omitempty"`
	Parent  *int64              `json:"parent_id,omitempty"`
	Project *string             `json:"project,omitempty"`
	Recent  []BriefingTaskEvent `json:"recent,omitempty"`
}

type BriefingTeam struct {
	Name      string   `json:"name"`
	Address   string   `json:"address,omitempty"` // segment.role (адрес агента)
	Neighbors []string `json:"neighbors,omitempty"`
}

type BriefingGit struct {
	Branch  string   `json:"branch"`
	Head    string   `json:"head"`
	Commits []string `json:"commits,omitempty"`
}

type BriefingProjectTask struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Priority int    `json:"priority"`
}

type BriefingProject struct {
	Name        string                `json:"name"`
	Path        string                `json:"path"`
	Git         *BriefingGit          `json:"git,omitempty"`
	OpenTasks   int                   `json:"open_tasks"`
	ActiveTasks []BriefingProjectTask `json:"active_tasks,omitempty"`
}

type BriefingMessage struct {
	Kind string `json:"kind"` // direct | chatroom
	From string `json:"from"`
	Body string `json:"body"`
	At   string `json:"at"`
}

type BriefingAlert struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	AgeSecs     int64  `json:"age_secs"`
}

type BriefingData struct {
	Task     *BriefingTask     `json:"task,omitempty"`
	Team     *BriefingTeam     `json:"team,omitempty"`
	Project  *BriefingProject  `json:"project,omitempty"`
	Messages []BriefingMessage `json:"messages,omitempty"`
	Alerts   []BriefingAlert   `json:"alerts,omitempty"`
}

// Briefing — собранный брифинг (canonical = Text + Data).
type Briefing struct {
	Text string       `json:"briefing_text"`
	Data BriefingData `json:"briefing_data"`
}

// BriefingService — сборщик брифинга (все источники; omit при пустых/ошибках).
type BriefingService struct {
	db        *sql.DB
	teams     *repository.TeamRepo
	segments  *repository.SegmentRepo
	roles     *repository.RoleRepo
	relatives *repository.RelativeRepo
	tasks     *repository.TaskRepo
	history   *repository.HistoryRepo
	projects  *repository.ProjectRepo
	messages  *repository.MessageRepo
	chatrooms *repository.ChatroomRepo
	chatMsgs  *repository.ChatroomMessageRepo
	alerts    *repository.WatchdogRepo
}

func NewBriefingService(db *sql.DB, s *repository.Stores) *BriefingService {
	return &BriefingService{
		db: db, teams: s.Teams, segments: s.Segments, roles: s.Roles,
		relatives: s.Relatives, tasks: s.Tasks, history: s.History,
		projects: s.Projects, messages: s.Messages, chatrooms: s.Chatrooms,
		chatMsgs: s.ChatroomMessages, alerts: s.Watchdog,
	}
}

// Build — собирает брифинг по параметрам. Никогда не возвращает ошибку
// (пустые источники / ошибки → omit).
func (b *BriefingService) Build(ctx context.Context, p BriefingParams) *Briefing {
	var data BriefingData
	if p.QueueTaskID != nil {
		if t, err := b.tasks.GetByID(ctx, b.db, *p.QueueTaskID); err == nil {
			data.Task = b.buildTask(ctx, t, p.ProjectID)
		}
	}
	if team, err := b.teams.GetByID(ctx, b.db, p.TeamID); err == nil {
		data.Team = b.buildTeam(ctx, team, p.RoleID)
	}
	if p.ProjectID != nil {
		if pr, err := b.projects.GetByID(ctx, b.db, *p.ProjectID); err == nil {
			data.Project = b.buildProject(ctx, pr, p.WorkingDir)
		}
	}
	data.Messages = b.buildMessages(ctx, p.TeamID, p.RoleID)
	data.Alerts = b.buildAlerts(ctx, p.TeamID)
	return &Briefing{Text: renderBriefing(data), Data: data}
}

// ---------- сборщики по источникам ----------

func (b *BriefingService) buildTask(ctx context.Context, t *models.Task, projectID *int64) *BriefingTask {
	bt := &BriefingTask{
		ID: t.ID, Title: t.Title, State: string(t.State),
		Body: truncateStr(t.Body, briefingBodyTruncate), Parent: t.ParentTaskID,
	}
	// project name (из задачи или из параметра)
	pid := t.ProjectID
	if pid == nil {
		pid = projectID
	}
	if pid != nil {
		if pr, err := b.projects.GetByID(ctx, b.db, *pid); err == nil {
			bt.Project = &pr.Name
		}
	}
	if hist, _, err := b.history.ListByTask(ctx, b.db, t.ID, briefingMaxTaskEvents, 0); err == nil {
		for _, h := range hist {
			text := fmt.Sprintf("%s → %s", orDash(h.FromState), h.ToState)
			if h.Comment != nil && strings.TrimSpace(*h.Comment) != "" {
				text = fmt.Sprintf("%s — %s", text, truncateStr(*h.Comment, 200))
			}
			bt.Recent = append(bt.Recent, BriefingTaskEvent{
				Kind: "transition", Text: text, At: h.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	return bt
}

func (b *BriefingService) buildTeam(ctx context.Context, team *models.Team, roleID int64) *BriefingTeam {
	bt := &BriefingTeam{Name: team.Name}
	role, err := b.roles.GetByID(ctx, b.db, roleID)
	if err != nil {
		return bt
	}
	if seg, err := b.segments.GetByID(ctx, b.db, role.SegmentID); err == nil {
		bt.Address = seg.Name + "." + role.Name
	} else {
		bt.Address = role.Name
	}
	// соседи: relatives, исходящие от этой роли (role → other, type)
	if rels, err := b.relatives.ListByTeam(ctx, b.db, team.ID); err == nil && len(rels) > 0 {
		nameByID := map[int64]string{}
		if roles, err := b.roles.ListByTeam(ctx, b.db, team.ID); err == nil {
			for _, r := range roles {
				nameByID[r.ID] = r.Name
			}
		}
		for _, rel := range rels {
			if rel.FromRoleID == roleID {
				if to := nameByID[rel.ToRoleID]; to != "" {
					bt.Neighbors = append(bt.Neighbors, fmt.Sprintf("%s → %s", rel.Type, to))
				}
			}
		}
	}
	return bt
}

func (b *BriefingService) buildProject(ctx context.Context, pr *models.Project, workingDir string) *BriefingProject {
	bp := &BriefingProject{Name: pr.Name, Path: pr.Path}
	if ts, total, err := b.tasks.List(ctx, b.db, repository.TaskFilter{
		ProjectID: &pr.ID, ActiveOnly: true, Limit: briefingMaxProjectTasks,
	}); err == nil {
		bp.OpenTasks = total
		for _, t := range ts {
			bp.ActiveTasks = append(bp.ActiveTasks, BriefingProjectTask{
				ID: t.ID, Title: t.Title, State: string(t.State), Priority: t.Priority,
			})
		}
	}
	dir := pr.Path
	if dir == "" {
		dir = workingDir
	}
	if pr.Git && dir != "" {
		if g := gitSnapshot(dir); g != nil {
			bp.Git = g
		}
	}
	return bp
}

func (b *BriefingService) buildMessages(ctx context.Context, teamID, roleID int64) []BriefingMessage {
	var out []BriefingMessage
	// direct к роли
	if msgs, _, err := b.messages.List(ctx, b.db, repository.MessageFilter{
		TeamID: &teamID, ToRoleID: &roleID, Limit: briefingMaxDirectMsgs,
	}); err == nil {
		for _, m := range msgs {
			out = append(out, BriefingMessage{
				Kind: "direct", From: messageFrom(m.FromRoleName),
				Body: truncateStr(m.Body, briefingMsgTruncate),
				At:   m.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	// чат сегмента роли
	role, err := b.roles.GetByID(ctx, b.db, roleID)
	if err == nil {
		if rooms, err := b.chatrooms.List(ctx, b.db, &teamID); err == nil {
			for _, c := range rooms {
				if c.SegmentID == nil || *c.SegmentID != role.SegmentID {
					continue
				}
				if cms, _, err := b.chatMsgs.List(ctx, b.db, c.ID, briefingMaxChatMsgs, 0); err == nil {
					for _, m := range cms {
						out = append(out, BriefingMessage{
							Kind: "chatroom", From: chatroomFrom(m),
							Body: truncateStr(m.Body, briefingMsgTruncate),
							At:   m.CreatedAt.UTC().Format(time.RFC3339),
						})
					}
				}
				break
			}
		}
	}
	if len(out) > briefingMaxDirectMsgs+briefingMaxChatMsgs {
		out = out[:briefingMaxDirectMsgs+briefingMaxChatMsgs]
	}
	return out
}

func (b *BriefingService) buildAlerts(ctx context.Context, teamID int64) []BriefingAlert {
	var out []BriefingAlert
	if evs, _, err := b.alerts.List(ctx, b.db, repository.AlertFilter{
		TeamID: &teamID, UnreadOnly: true, Limit: briefingMaxAlerts,
	}); err == nil {
		now := time.Now().UTC()
		for _, e := range evs {
			out = append(out, BriefingAlert{
				Title: e.EventType, Description: truncateStr(e.Description, 200),
				AgeSecs: int64(now.Sub(e.CreatedAt).Seconds()),
			})
		}
	}
	return out
}

// ---------- git (read-only) ----------

func gitSnapshot(dir string) *BriefingGit {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	branch := gitOut(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	head := gitOut(ctx, dir, "rev-parse", "--short", "HEAD")
	if branch == "" && head == "" {
		return nil
	}
	g := &BriefingGit{Branch: branch, Head: head}
	if log := gitOut(ctx, dir, "log", "--oneline", "-"+fmt.Sprint(briefingMaxGitCommits)); log != "" {
		for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				g.Commits = append(g.Commits, line)
			}
		}
	}
	return g
}

func gitOut(ctx context.Context, dir string, args ...string) string {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------- helpers ----------

func truncateStr(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "∅"
	}
	return *s
}

func messageFrom(name *string) string {
	if name == nil || *name == "" {
		return "You"
	}
	return *name
}

func chatroomFrom(m *repository.ChatroomMessageRow) string {
	if m.FromRoleName != nil && *m.FromRoleName != "" {
		return *m.FromRoleName
	}
	return "You"
}

// renderBriefing — canonical markdown из BriefingData.
func renderBriefing(d BriefingData) string {
	var b strings.Builder
	b.WriteString("# Session briefing\n\n")
	b.WriteString("Кто ты, что делать, где работаешь и что уже случилось.\n")
	if d.Task != nil {
		b.WriteString("\n## Task\n")
		b.WriteString(fmt.Sprintf("- #%d %q (state=%s)", d.Task.ID, d.Task.Title, d.Task.State))
		if d.Task.Project != nil {
			b.WriteString(fmt.Sprintf(", project: %s", *d.Task.Project))
		}
		b.WriteString("\n")
		if d.Task.Parent != nil {
			b.WriteString(fmt.Sprintf("- parent: #%d\n", *d.Task.Parent))
		}
		if d.Task.Body != "" {
			b.WriteString("\nBody:\n" + d.Task.Body + "\n")
		}
		if len(d.Task.Recent) > 0 {
			b.WriteString("\nRecent activity:\n")
			for _, ev := range d.Task.Recent {
				b.WriteString(fmt.Sprintf("- %s (%s)\n", ev.Text, ev.At))
			}
		}
	}
	if d.Team != nil {
		b.WriteString("\n## Team\n")
		b.WriteString(fmt.Sprintf("- team: %s", d.Team.Name))
		if d.Team.Address != "" {
			b.WriteString(fmt.Sprintf(" (you: %s)", d.Team.Address))
		}
		b.WriteString("\n")
		for _, n := range d.Team.Neighbors {
			b.WriteString(fmt.Sprintf("- neighbor: %s\n", n))
		}
	}
	if d.Project != nil {
		b.WriteString("\n## Project\n")
		b.WriteString(fmt.Sprintf("- %s @ %s\n", d.Project.Name, d.Project.Path))
		if d.Project.Git != nil {
			b.WriteString(fmt.Sprintf("- git: %s @ %s\n", d.Project.Git.Branch, d.Project.Git.Head))
			for _, c := range d.Project.Git.Commits {
				b.WriteString(fmt.Sprintf("  - %s\n", c))
			}
		}
		b.WriteString(fmt.Sprintf("- open tasks: %d\n", d.Project.OpenTasks))
		for _, t := range d.Project.ActiveTasks {
			b.WriteString(fmt.Sprintf("  - #%d %q (%s, prio %d)\n", t.ID, t.Title, t.State, t.Priority))
		}
	}
	if len(d.Messages) > 0 {
		b.WriteString("\n## Recent messages\n")
		for _, m := range d.Messages {
			b.WriteString(fmt.Sprintf("- [%s] %s: %s (%s)\n", m.Kind, m.From, m.Body, m.At))
		}
	}
	if len(d.Alerts) > 0 {
		b.WriteString("\n## Alerts (unread)\n")
		for _, a := range d.Alerts {
			b.WriteString(fmt.Sprintf("- %s: %s (%ds ago)\n", a.Title, a.Description, a.AgeSecs))
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// ---------- delivery helpers (CreateSession + preview) ----------

// BriefingDelivery — как briefing будет передан агенту (по адаптеру).
type BriefingDelivery struct {
	Mode   string `json:"mode"`   // pi-system-prompt | file-env
	Detail string `json:"detail"` // человекочитаемо
}

func BriefingDeliveryFor(runtimeType string) BriefingDelivery {
	if runtimeType == "pi" {
		return BriefingDelivery{Mode: "pi-system-prompt",
			Detail: "briefing_text → system_prompt в pi-конфиге сессии"}
	}
	return BriefingDelivery{Mode: "file-env",
		Detail: "briefing_text → файл <logs>/briefings/<id>.md + env BRIEFING_FILE"}
}

// ApplyBriefingOverrides — canonical текст + applied (builtin|override) с учётом
// override/append. base=nil и нет override/append → ("", "none").
func ApplyBriefingOverrides(base *Briefing, override, appendStr string) (string, string) {
	o := strings.TrimSpace(override)
	a := strings.TrimSpace(appendStr)
	baseText := ""
	if base != nil {
		baseText = strings.TrimSpace(base.Text)
	}
	var text, applied string
	switch {
	case o != "":
		text, applied = o, "override"
	case baseText != "":
		text, applied = baseText, "builtin"
	case a != "":
		text, applied = a, "override"
	default:
		return "", "none"
	}
	if a != "" && text != a {
		text = text + "\n\n" + a
	}
	return text, applied
}

func briefingDisabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg["briefing"].(string); ok && v == "none" {
		return true
	}
	if v, ok := cfg["briefing_disable"].(bool); ok && v {
		return true
	}
	return false
}

func cfgStr(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	if s, ok := cfg[key].(string); ok {
		return s
	}
	return ""
}

// appendConfigEnv — добавляет K=V в config["env"] ([]string), сохраняя тип.
func appendConfigEnv(env any, kv string) any {
	var out []string
	switch v := env.(type) {
	case []string:
		out = v
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return append(out, kv)
}

// ---------- SessionService helpers (файл брифинга) ----------

// briefingFilePath — <logs>/briefings/<session_id>.md (process/tmux-доставка).
func (s *SessionService) briefingFilePath(sessID int64) string {
	if s.LogsDir == "" {
		return ""
	}
	return filepath.Join(s.LogsDir, "briefings", fmt.Sprintf("%d.md", sessID))
}

func (s *SessionService) writeBriefingFile(path, text string) error {
	if path == "" {
		return fmt.Errorf("no briefing path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
