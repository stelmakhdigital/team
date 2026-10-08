package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

func setupTaskEnv(t *testing.T) (*service.TeamService, *service.TaskService, context.Context) {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	return svc, tsvc, context.Background()
}

// makeTeamWithRoles — команда с одним сегментом и двумя ролями.
func makeTeamWithRoles(t *testing.T, svc *service.TeamService, teamName string) (teamID, roleA, roleB int64) {
	t.Helper()
	team, err := svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: teamName})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	seg, err := svc.CreateSegment(context.Background(), service.CreateSegmentRequest{
		TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	a, err := svc.CreateRole(context.Background(), service.CreateRoleRequest{
		SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatalf("CreateRole a: %v", err)
	}
	b, err := svc.CreateRole(context.Background(), service.CreateRoleRequest{
		SegmentID: seg.ID, Name: "role_b", AgentSpec: "agents/role_b.yaml", Layout: &service.Layout{X: 1, Y: 0}})
	if err != nil {
		t.Fatalf("CreateRole b: %v", err)
	}
	return team.ID, a.ID, b.ID
}

func TestTaskLifecycle(t *testing.T) {
	svc, tsvc, ctx := setupTaskEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "t-lifecycle")

	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "main task"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.State != models.TaskPending {
		t.Fatalf("initial state = %s, want pending", task.State)
	}

	// pending -> in_progress
	task, err = tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskInProgress, Comment: "start"})
	if err != nil {
		t.Fatalf("->in_progress: %v", err)
	}
	if task.StartedAt == nil {
		t.Error("started_at not set on in_progress")
	}

	// in_progress -> blocked
	task, err = tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked, Comment: "waiting"})
	if err != nil {
		t.Fatalf("->blocked: %v", err)
	}
	if task.BlockedSince == nil {
		t.Error("blocked_since not set")
	}

	// blocked -> pending (unblock)
	task, err = tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskPending})
	if err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if task.BlockedSince != nil {
		t.Error("blocked_since should be cleared on unblock")
	}

	// done требует closure_reason
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskDone}); err == nil {
		t.Error("done without closure_reason must fail")
	}
	reason := models.ClosureNoFollowOn
	task, err = tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskDone, ClosureReason: &reason, Comment: "done"})
	if err != nil {
		t.Fatalf("->done: %v", err)
	}
	if task.CompletedAt == nil {
		t.Error("completed_at not set")
	}

	// из терминального — нельзя
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskPending}); err == nil {
		t.Error("transition from terminal must fail")
	}

	// история: 5 записей (created, start, blocked, unblock, done)
	hist, total, err := tsvc.GetTaskHistory(ctx, task.ID, 100, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if total != 5 || len(hist) != 5 {
		t.Fatalf("history = %d entries, want 5", total)
	}
	if hist[0].ToState != "pending" || hist[4].ToState != "done" {
		t.Errorf("history order: first=%s last=%s", hist[0].ToState, hist[4].ToState)
	}
}

func TestTaskParentAutoClose(t *testing.T) {
	svc, tsvc, ctx := setupTaskEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "t-parent")

	parent, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "parent"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	sub, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "sub", ParentTaskID: &parent.ID})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}

	reason := models.ClosureNoFollowOn
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: sub.ID, State: models.TaskDone, ClosureReason: &reason}); err != nil {
		t.Fatalf("sub done: %v", err)
	}

	detail, err := tsvc.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if detail.Task.State != "done" {
		t.Fatalf("parent state = %s, want auto-closed done", detail.Task.State)
	}
	if detail.Task.ClosureReason == nil || *detail.Task.ClosureReason != "no_follow_on" {
		t.Errorf("parent closure_reason = %v, want no_follow_on", detail.Task.ClosureReason)
	}
}

func TestTaskHandoff(t *testing.T) {
	svc, tsvc, ctx := setupTaskEnv(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, svc, "t-handoff")

	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "to handoff"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := tsvc.HandoffTask(ctx, service.HandoffTaskRequest{
		ID: task.ID, ToRoleID: roleB, Comment: "передаём"})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if res.ClosedTask.State != models.TaskDone ||
		*res.ClosedTask.ClosureReason != models.ClosureHandedOff {
		t.Errorf("closed task = %s/%v, want done/handed_off_to", res.ClosedTask.State, res.ClosedTask.ClosureReason)
	}
	if res.NewTask.DestinationRoleID != roleB {
		t.Errorf("new task role = %d, want %d", res.NewTask.DestinationRoleID, roleB)
	}
	if res.NewTask.SourceRoleID == nil || *res.NewTask.SourceRoleID != roleA {
		t.Error("new task source_role_id not set to original destination")
	}
	if res.NewTask.Title != "to handoff" {
		t.Errorf("new task title = %q", res.NewTask.Title)
	}
	// handoff из терминальной — ошибка
	if _, err := tsvc.HandoffTask(ctx, service.HandoffTaskRequest{ID: task.ID, ToRoleID: roleA}); err == nil {
		t.Error("handoff of closed task must fail")
	}
}

func TestTaskValidation(t *testing.T) {
	svc, tsvc, ctx := setupTaskEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "t-valid")

	// нет title
	if _, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA}); err == nil {
		t.Error("empty title must fail")
	}
	// чужая роль
	_, _, foreignRole := makeTeamWithRoles(t, svc, "t-other")
	if _, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: foreignRole, Title: "x"}); err == nil {
		t.Error("role from another team must fail")
	}
	// команда архивирована
	archID, _, _ := makeTeamWithRoles(t, svc, "t-arch")
	if err := svc.ArchiveTeam(ctx, archID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: archID, DestinationRoleID: roleA, Title: "x"}); err == nil {
		t.Error("task for archived team must fail")
	}
	// невалидный переход pending -> done без reason уже покрыт; проверим pending->pending (no-op)
	task, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "noop"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskPending}); err != nil {
		t.Errorf("pending->pending no-op must succeed: %v", err)
	}
}

func TestDashboard(t *testing.T) {
	svc, tsvc, ctx := setupTaskEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "t-dash")

	// одна задача pending
	if _, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "dash task"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	summary, err := tsvc.DashboardSummary(ctx)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Teams.Total != 1 || summary.Teams.Active != 1 {
		t.Errorf("teams = %+v", summary.Teams)
	}
	if summary.Tasks.Pending != 1 || summary.Tasks.Total != 1 {
		t.Errorf("tasks = %+v", summary.Tasks)
	}
	if summary.Sessions.Total != 0 || summary.Alerts.Total != 0 {
		t.Errorf("sessions/alerts must be zero before slice 3")
	}

	tasks, total, err := tsvc.DashboardTasks(ctx)
	if err != nil {
		t.Fatalf("dashboard tasks: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("dashboard tasks = %d", total)
	}
	if tasks[0].TeamName != "t-dash" || tasks[0].DestinationRole != "role_a" {
		t.Errorf("dashboard task view = %+v", tasks[0])
	}

	// завершённая задача не в active-списке
	tk, _ := tsvc.GetTask(ctx, tasks[0].ID)
	reason := models.ClosureNoFollowOn
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: tk.Task.ID, State: models.TaskDone, ClosureReason: &reason}); err != nil {
		t.Fatalf("done: %v", err)
	}
	if _, total, _ := tsvc.DashboardTasks(ctx); total != 0 {
		t.Errorf("done task still in active list: %d", total)
	}
}
