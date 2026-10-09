package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// setupOverdueEnv — как setupTaskEnv, но возвращает *sql.DB для прямых обновлений
// closure_required_at в тестах (симуляция просрочки).
func setupOverdueEnv(t *testing.T) (*service.TeamService, *service.TaskService, *sql.DB, context.Context) {
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
	if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	return svc, tsvc, db, context.Background()
}

func setClosureAt(t *testing.T, db *sql.DB, ctx context.Context, taskID int64, when time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`UPDATE queue_tasks SET closure_required_at = ? WHERE id = ?`,
		when.UTC().Format(time.RFC3339Nano), taskID); err != nil {
		t.Fatalf("set closure_required_at: %v", err)
	}
}

func TestOverdueDeadlineSetOnInProgress(t *testing.T) {
	svc, tsvc, _, ctx := setupOverdueEnv(t)
	tsvc.ClosureRequiredFunc = func(context.Context) (time.Duration, error) { return 2 * time.Hour, nil }
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "overdue-set")

	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	before := time.Now().UTC()
	task, err = tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskInProgress})
	if err != nil {
		t.Fatalf("->in_progress: %v", err)
	}
	// closure_required_at выставлен (~ now + 2h)
	detail, err := tsvc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.ClosureRequiredAt == nil {
		t.Fatal("closure_required_at not set on in_progress")
	}
	due, err := time.Parse(time.RFC3339Nano, *detail.Task.ClosureRequiredAt)
	if err != nil {
		t.Fatalf("parse closure_required_at: %v", err)
	}
	want := before.Add(2 * time.Hour)
	if diff := due.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Errorf("deadline = %v, want ~%v (now+2h)", due, want)
	}
	// будущее → НЕ просрочена
	if detail.Task.IsOverdue {
		t.Error("is_overdue = true, want false (deadline in future)")
	}
	if detail.Task.RemainingSecs == nil || *detail.Task.RemainingSecs <= 0 {
		t.Errorf("remaining_secs = %v, want > 0", detail.Task.RemainingSecs)
	}
}

func TestOverdueRecalcOnBlockedToInProgress(t *testing.T) {
	svc, tsvc, _, ctx := setupOverdueEnv(t)
	tsvc.ClosureRequiredFunc = func(context.Context) (time.Duration, error) { return 2 * time.Hour, nil }
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "overdue-recalc")

	task, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskInProgress}); err != nil {
		t.Fatal(err)
	}
	first, _ := tsvc.GetTask(ctx, task.ID)
	firstDue, _ := time.Parse(time.RFC3339Nano, *first.Task.ClosureRequiredAt)

	// in_progress -> blocked (дедлайн не трогается)
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskBlocked}); err != nil {
		t.Fatal(err)
	}
	block, _ := tsvc.GetTask(ctx, task.ID)
	if *block.Task.ClosureRequiredAt != *first.Task.ClosureRequiredAt {
		t.Error("deadline changed on -> blocked (must persist)")
	}
	// blocked -> in_progress: пересчёт (новый дедлайн позже). View хранит RFC3339 (сек.),
	// поэтому спим через границу секунды, чтобы пересчёт был виден.
	time.Sleep(1100 * time.Millisecond)
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskInProgress}); err != nil {
		t.Fatal(err)
	}
	again, _ := tsvc.GetTask(ctx, task.ID)
	againDue, _ := time.Parse(time.RFC3339Nano, *again.Task.ClosureRequiredAt)
	if !againDue.After(firstDue) {
		t.Errorf("deadline not recalculated on blocked->in_progress: first=%v again=%v", firstDue, againDue)
	}
}

func TestOverdueIsOverdueLogic(t *testing.T) {
	svc, tsvc, db, ctx := setupOverdueEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "overdue-logic")
	past := time.Now().UTC().Add(-time.Hour) // дедлайн уже прошёл

	// (a) in_progress + past deadline -> is_overdue = true
	ta, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "ip"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: ta.ID, State: models.TaskInProgress}); err != nil {
		t.Fatal(err)
	}
	setClosureAt(t, db, ctx, ta.ID, past)
	va, _ := tsvc.GetTask(ctx, ta.ID)
	if !va.Task.IsOverdue {
		t.Error("in_progress + past deadline: is_overdue = false, want true")
	}

	// (b) blocked + past deadline -> is_overdue = true
	tb, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "bl"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: tb.ID, State: models.TaskInProgress}); err != nil {
		t.Fatal(err)
	}
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: tb.ID, State: models.TaskBlocked}); err != nil {
		t.Fatal(err)
	}
	setClosureAt(t, db, ctx, tb.ID, past)
	vb, _ := tsvc.GetTask(ctx, tb.ID)
	if !vb.Task.IsOverdue {
		t.Error("blocked + past deadline: is_overdue = false, want true")
	}

	// (c) done + past deadline -> is_overdue = false (terminal, не учитывается)
	tc, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "dn"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: tc.ID, State: models.TaskInProgress}); err != nil {
		t.Fatal(err)
	}
	setClosureAt(t, db, ctx, tc.ID, past)
	reason := models.ClosureNoFollowOn
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: tc.ID, State: models.TaskDone, ClosureReason: &reason}); err != nil {
		t.Fatal(err)
	}
	vc, _ := tsvc.GetTask(ctx, tc.ID)
	if vc.Task.IsOverdue {
		t.Error("done + past deadline: is_overdue = true, want false")
	}
	// (d) закрытие НЕ сбрасывает closure_required_at
	if vc.Task.ClosureRequiredAt == nil {
		t.Error("closure_required_at reset on done (must be preserved)")
	}
}

func TestListOverdue(t *testing.T) {
	svc, tsvc, db, ctx := setupOverdueEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "overdue-list")
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(2 * time.Hour)

	// (a) пусто (никаких in_progress/blocked с дедлайном)
	views, total, err := tsvc.ListOverdue(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(views) != 0 {
		t.Fatalf("empty: total=%d len=%d, want 0", total, len(views))
	}

	// 2 просроченные + 1 не-просроченная (future) + 1 pending (без дедлайна)
	mkOverdue := func(title string, when time.Time) {
		tk, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: title})
		if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: tk.ID, State: models.TaskInProgress}); err != nil {
			t.Fatal(err)
		}
		setClosureAt(t, db, ctx, tk.ID, when)
	}
	mkOverdue("od-1", past.Add(-time.Hour)) // самая просроченная
	mkOverdue("od-2", past)
	mkOverdue("not-od", future) // в будущем → не просрочена

	// (b) с данными: total=2, обе is_overdue, сортировка — самая просроченная сверху
	views, total, err = tsvc.ListOverdue(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(views) != 2 {
		t.Fatalf("len = %d, want 2", len(views))
	}
	if !views[0].IsOverdue || !views[1].IsOverdue {
		t.Error("items not flagged is_overdue")
	}
	if views[0].Title != "od-1" {
		t.Errorf("first (most overdue) = %q, want od-1", views[0].Title)
	}
	if views[0].RemainingSecs == nil || *views[0].RemainingSecs >= 0 {
		t.Errorf("od-1 remaining_secs = %v, want < 0 (просрочено)", views[0].RemainingSecs)
	}

	// (c) limit
	views, total, err = tsvc.ListOverdue(ctx, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(views) != 1 {
		t.Errorf("limit=1: total=%d len=%d, want total=2 len=1", total, len(views))
	}
}
