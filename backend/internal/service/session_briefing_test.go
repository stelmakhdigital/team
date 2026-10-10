package service_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- P1.7: Session briefing (core — сборка из задачи/команды/проекта) ----------

// initGitRepo — создаёт временный git-repo с одним коммитом.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "T")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
}

// newBriefingEnv — db + stores + BriefingService.
func newBriefingEnv(t *testing.T) (*sessionEnv, *service.BriefingService) {
	t.Helper()
	e := setupSessionEnv(t)
	b := service.NewBriefingService(e.db, e.stores)
	return e, b
}

func TestBriefingTaskTeamProject(t *testing.T) {
	e, b := newBriefingEnv(t)
	ctx := e.ctx
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "bf-team")

	// проект (existing, без git)
	projDir := t.TempDir()
	psvc := service.NewProjectService(e.db, e.stores)
	res, err := psvc.CreateProject(ctx, service.CreateProjectRequest{Name: "demo", Kind: "existing", Path: projDir})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// задача на roleA + переход (pending → in_progress)
	task, err := e.tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "main task", ProjectID: &res.Project.ID})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := e.tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskInProgress, Comment: "start work"}); err != nil {
		t.Fatalf("transition: %v", err)
	}

	// relative (roleA delegates_to roleB)
	if _, err := e.svc.CreateRelative(ctx, service.CreateRelativeRequest{TeamID: teamID, FromRoleID: roleA, ToRoleID: roleB, Type: models.RelDelegatesTo}); err != nil {
		t.Fatalf("CreateRelative: %v", err)
	}

	// direct-сообщение к roleA
	msvc := service.NewMessageService(e.db, e.stores)
	if _, err := msvc.SendMessage(ctx, service.SendMessageRequest{TeamID: teamID, ToRoleID: &roleA, Type: "direct", Body: "please pick this up"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// unread-алерт по команде
	if err := e.stores.Watchdog.Create(ctx, e.db, &models.WatchdogEvent{
		TeamID: teamID, EventType: "stale", Severity: "high", Description: "task is stale", RequiresAction: true,
	}); err != nil {
		t.Fatalf("Create alert: %v", err)
	}

	brief := b.Build(ctx, service.BriefingParams{
		TeamID: teamID, RoleID: roleA, QueueTaskID: &task.ID,
		WorkingDir: projDir, ProjectID: &res.Project.ID,
	})
	if brief == nil {
		t.Fatal("Build returned nil")
	}
	// Task
	if brief.Data.Task == nil {
		t.Fatalf("Task missing: %+v", brief.Data)
	}
	if brief.Data.Task.ID != task.ID || brief.Data.Task.Title != "main task" || brief.Data.Task.State != string(models.TaskInProgress) {
		t.Fatalf("Task wrong: %+v", brief.Data.Task)
	}
	if brief.Data.Task.Project == nil || *brief.Data.Task.Project != "demo" {
		t.Fatalf("Task.Project = %v, want demo", brief.Data.Task.Project)
	}
	if len(brief.Data.Task.Recent) == 0 {
		t.Fatal("Task.Recent empty (expected transition)")
	}
	// Team
	if brief.Data.Team == nil || brief.Data.Team.Name != "bf-team" {
		t.Fatalf("Team wrong: %+v", brief.Data.Team)
	}
	if brief.Data.Team.Address != "core.role_a" {
		t.Fatalf("Team.Address = %q, want core.role_a", brief.Data.Team.Address)
	}
	if len(brief.Data.Team.Neighbors) == 0 || !strings.Contains(brief.Data.Team.Neighbors[0], "role_b") {
		t.Fatalf("Team.Neighbors = %v, want delegate to role_b", brief.Data.Team.Neighbors)
	}
	// Project (без git)
	if brief.Data.Project == nil || brief.Data.Project.Name != "demo" {
		t.Fatalf("Project wrong: %+v", brief.Data.Project)
	}
	if brief.Data.Project.Git != nil {
		t.Fatalf("Project.Git should be omitted (no git), got %+v", brief.Data.Project.Git)
	}
	// Messages
	if len(brief.Data.Messages) == 0 || !strings.Contains(brief.Data.Messages[0].Body, "pick this up") {
		t.Fatalf("Messages wrong: %+v", brief.Data.Messages)
	}
	// Alerts
	if len(brief.Data.Alerts) == 0 || brief.Data.Alerts[0].Title != "stale" {
		t.Fatalf("Alerts wrong: %+v", brief.Data.Alerts)
	}
	// markdown contains the sections
	for _, want := range []string{"## Task", "## Team", "## Project", "## Recent messages", "## Alerts (unread)"} {
		if !strings.Contains(brief.Text, want) {
			t.Fatalf("briefing_text missing %q:\n%s", want, brief.Text)
		}
	}
}

func TestBriefingEmptyOmits(t *testing.T) {
	e, b := newBriefingEnv(t)
	ctx := e.ctx
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-empty")
	brief := b.Build(ctx, service.BriefingParams{TeamID: teamID, RoleID: roleA})
	if brief.Data.Task != nil {
		t.Fatalf("Task should be omitted, got %+v", brief.Data.Task)
	}
	if brief.Data.Project != nil {
		t.Fatalf("Project should be omitted, got %+v", brief.Data.Project)
	}
	if strings.Contains(brief.Text, "## Task") || strings.Contains(brief.Text, "## Project") {
		t.Fatalf("briefing_text has Task/Project sections:\n%s", brief.Text)
	}
	if brief.Data.Team == nil || brief.Data.Team.Name != "bf-empty" {
		t.Fatalf("Team should be present: %+v", brief.Data.Team)
	}
}

func TestBriefingGitPresent(t *testing.T) {
	e, b := newBriefingEnv(t)
	ctx := e.ctx
	projDir := t.TempDir()
	initGitRepo(t, projDir)
	psvc := service.NewProjectService(e.db, e.stores)
	res, err := psvc.CreateProject(ctx, service.CreateProjectRequest{Name: "gitproj", Kind: "existing", Path: projDir})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if !res.Project.Git {
		t.Skip("project not detected as git")
	}
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-git")
	brief := b.Build(ctx, service.BriefingParams{TeamID: teamID, RoleID: roleA, ProjectID: &res.Project.ID, WorkingDir: projDir})
	if brief.Data.Project == nil || brief.Data.Project.Git == nil {
		t.Fatalf("Project.Git missing: %+v", brief.Data.Project)
	}
	if brief.Data.Project.Git.Branch == "" || brief.Data.Project.Git.Head == "" {
		t.Fatalf("Git branch/head empty: %+v", brief.Data.Project.Git)
	}
	if len(brief.Data.Project.Git.Commits) == 0 {
		t.Fatalf("Git commits empty: %+v", brief.Data.Project.Git)
	}
}

func TestBriefingGitAbsentOmits(t *testing.T) {
	e, b := newBriefingEnv(t)
	ctx := e.ctx
	projDir := t.TempDir() // нет .git
	psvc := service.NewProjectService(e.db, e.stores)
	res, _ := psvc.CreateProject(ctx, service.CreateProjectRequest{Name: "nogit", Kind: "existing", Path: projDir})
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-nogit")
	brief := b.Build(ctx, service.BriefingParams{TeamID: teamID, RoleID: roleA, ProjectID: &res.Project.ID, WorkingDir: projDir})
	if brief.Data.Project == nil {
		t.Fatalf("Project missing: %+v", brief.Data)
	}
	if brief.Data.Project.Git != nil {
		t.Fatalf("Git should be omitted (no .git), got %+v", brief.Data.Project.Git)
	}
}

func TestBriefingBounded(t *testing.T) {
	e, b := newBriefingEnv(t)
	ctx := e.ctx
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "bf-bound")
	task, _ := e.tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "busy task"})
	// несколько переходов (pending → in_progress → blocked → in_progress …)
	states := []models.TaskState{
		models.TaskInProgress, models.TaskBlocked, models.TaskInProgress,
		models.TaskBlocked, models.TaskInProgress, models.TaskDone,
	}
	for i, st := range states {
		_, _ = e.tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: st, Comment: "c" + string(rune('0'+i))})
	}
	brief := b.Build(ctx, service.BriefingParams{TeamID: teamID, RoleID: roleA, QueueTaskID: &task.ID})
	if brief.Data.Task == nil {
		t.Fatal("Task missing")
	}
	if len(brief.Data.Task.Recent) > 5 {
		t.Fatalf("Task.Recent = %d, want <= 5", len(brief.Data.Task.Recent))
	}
	_ = roleB
	_ = repository.ErrNotFound
}
