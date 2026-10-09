package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// setupWakeEnv — как setupOverdueEnv, но возвращает *sql.DB для прямых обновлений
// task_wakes.ref (симуляция наступления срока timer-wake).
func setupWakeEnv(t *testing.T) (*service.TeamService, *service.TaskService, *sql.DB, context.Context) {
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
	return svc, tsvc, db, context.Background()
}

// setWakeFireAt — прямое обновление fire-at (ref) арм. wake задачи (для теста).
func setWakeFireAt(t *testing.T, db *sql.DB, ctx context.Context, taskID int64, when time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`UPDATE task_wakes SET ref = ? WHERE task_id = ? AND fired_at IS NULL`,
		when.UTC().Format(time.RFC3339Nano), taskID); err != nil {
		t.Fatalf("set wake ref: %v", err)
	}
}

func historyHas(t *testing.T, tsvc *service.TaskService, ctx context.Context, taskID int64, substr string) bool {
	t.Helper()
	hist, _, err := tsvc.GetTaskHistory(ctx, taskID, 200, 0)
	if err != nil {
		t.Fatalf("GetTaskHistory: %v", err)
	}
	for _, h := range hist {
		if h.Comment != nil && strings.Contains(*h.Comment, substr) {
			return true
		}
	}
	return false
}

func TestWakeCreatedOnBlockTimer(t *testing.T) {
	svc, tsvc, _, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-timer")

	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "timer", After: "30m", Continuation: "wait for CI"},
	}); err != nil {
		t.Fatalf("->blocked+wake: %v", err)
	}
	detail, err := tsvc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Wake == nil {
		t.Fatal("wake not in detail")
	}
	if detail.Wake.Kind != "timer" {
		t.Errorf("kind = %q, want timer", detail.Wake.Kind)
	}
	if detail.Wake.Continuation != "wait for CI" {
		t.Errorf("continuation = %q, want 'wait for CI'", detail.Wake.Continuation)
	}
	if detail.Wake.Fired {
		t.Error("fired = true, want false (armed)")
	}
	fa, err := time.Parse(time.RFC3339Nano, detail.Wake.Ref)
	if err != nil {
		t.Fatalf("parse ref (fire-at): %v", err)
	}
	if fa.Before(time.Now().UTC()) {
		t.Errorf("fire-at %v in the past, want now+30m", fa)
	}
	if detail.Wake.RemainingSecs == nil || *detail.Wake.RemainingSecs <= 0 {
		t.Errorf("remaining_secs = %v, want > 0", detail.Wake.RemainingSecs)
	}
}

func TestWakeCreatedOnBlockTask(t *testing.T) {
	svc, tsvc, _, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-task")

	ref, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "ref"})
	task, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "blocked"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "task", TaskID: &ref.ID, Continuation: "resume after ref"},
	}); err != nil {
		t.Fatal(err)
	}
	detail, _ := tsvc.GetTask(ctx, task.ID)
	if detail.Wake == nil {
		t.Fatal("wake not in detail")
	}
	if detail.Wake.Kind != "task" {
		t.Errorf("kind = %q, want task", detail.Wake.Kind)
	}
	if detail.Wake.Ref != strconv.FormatInt(ref.ID, 10) {
		t.Errorf("ref = %q, want %d", detail.Wake.Ref, ref.ID)
	}
	if detail.Wake.RemainingSecs != nil {
		t.Errorf("remaining_secs = %v, want nil for task-wake", detail.Wake.RemainingSecs)
	}
}

func TestWakeTimerFires(t *testing.T) {
	svc, tsvc, db, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-fire")

	task, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "timer", After: "1h", Continuation: "wait"},
	}); err != nil {
		t.Fatal(err)
	}
	// срок наступил
	setWakeFireAt(t, db, ctx, task.ID, time.Now().UTC().Add(-time.Minute))

	n := tsvc.FireArmedTimerWakes(ctx)
	if n != 1 {
		t.Fatalf("FireArmedTimerWakes = %d, want 1", n)
	}
	detail, _ := tsvc.GetTask(ctx, task.ID)
	if detail.Task.State != string(models.TaskPending) {
		t.Errorf("state = %s, want pending", detail.Task.State)
	}
	if detail.Wake == nil || !detail.Wake.Fired {
		t.Error("wake not fired")
	}
	if !historyHas(t, tsvc, ctx, task.ID, "wake timer fired") {
		t.Error("no 'wake timer fired' history entry")
	}
}

func TestWakeTaskTriggerOnClose(t *testing.T) {
	svc, tsvc, _, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-trigger")

	ref, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "ref"})
	blocked, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "blocked"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: blocked.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "task", TaskID: &ref.ID},
	}); err != nil {
		t.Fatal(err)
	}
	// referenced-задача закрыта (done) → wake срабатывает
	reason := models.ClosureNoFollowOn
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: ref.ID, State: models.TaskDone, ClosureReason: &reason}); err != nil {
		t.Fatal(err)
	}
	detail, _ := tsvc.GetTask(ctx, blocked.ID)
	if detail.Task.State != string(models.TaskPending) {
		t.Errorf("blocked task state = %s, want pending (task-wake fired)", detail.Task.State)
	}
	if detail.Wake == nil || !detail.Wake.Fired {
		t.Error("task-wake not fired")
	}
	if !historyHas(t, tsvc, ctx, blocked.ID, "wake task") {
		t.Error("no 'wake task N closed' history entry")
	}
}

func TestWakeIdempotency(t *testing.T) {
	svc, tsvc, db, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-idem")

	task, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "timer", After: "1h"},
	}); err != nil {
		t.Fatal(err)
	}
	setWakeFireAt(t, db, ctx, task.ID, time.Now().UTC().Add(-time.Minute))

	if n := tsvc.FireArmedTimerWakes(ctx); n != 1 {
		t.Fatalf("first fire = %d, want 1", n)
	}
	if n := tsvc.FireArmedTimerWakes(ctx); n != 0 {
		t.Fatalf("second fire = %d, want 0 (idempotent)", n)
	}
	detail, _ := tsvc.GetTask(ctx, task.ID)
	if detail.Task.State != string(models.TaskPending) {
		t.Errorf("state = %s, want pending", detail.Task.State)
	}
}

func TestResolveTask(t *testing.T) {
	svc, tsvc, _, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-resolve")

	// (a) resolve заблокированной → pending + история resolve
	task, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "timer", After: "1h"},
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err := tsvc.ResolveTask(ctx, service.ResolveTaskRequest{ID: task.ID, Decision: "dependency cleared"})
	if err != nil {
		t.Fatalf("ResolveTask: %v", err)
	}
	if resolved.State != models.TaskPending {
		t.Errorf("state = %s, want pending", resolved.State)
	}
	if !historyHas(t, tsvc, ctx, task.ID, "resolve: dependency cleared") {
		t.Error("no 'resolve:' history entry")
	}
	// арм. wake закрыта (не сработает повторно)
	detail, _ := tsvc.GetTask(ctx, task.ID)
	if detail.Wake == nil || !detail.Wake.Fired {
		t.Error("wake not closed on resolve (idempotency)")
	}
	if n := tsvc.FireArmedTimerWakes(ctx); n != 0 {
		t.Errorf("FireArmedTimerWakes after resolve = %d, want 0 (wake closed)", n)
	}

	// (b) resolve не-blocked → 409
	other, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "o"})
	if _, err := tsvc.ResolveTask(ctx, service.ResolveTaskRequest{ID: other.ID, Decision: "x"}); err == nil {
		t.Error("resolve non-blocked: want 409")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 409 {
		t.Errorf("resolve non-blocked: want 409, got %v", err)
	}

	// (c) resolve без decision → 400
	block2, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "b2"})
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: block2.ID, State: models.TaskBlocked}); err != nil {
		t.Fatal(err)
	}
	if _, err := tsvc.ResolveTask(ctx, service.ResolveTaskRequest{ID: block2.ID, Decision: ""}); err == nil {
		t.Error("resolve empty decision: want 400")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 400 {
		t.Errorf("resolve empty decision: want 400, got %v", err)
	}
}

func TestBlockWakeValidation(t *testing.T) {
	svc, tsvc, _, ctx := setupWakeEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, svc, "wake-valid")

	mk := func() int64 {
		tk, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
		return tk.ID
	}

	// (a) неверный kind → 400
	id := mk()
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: id, State: models.TaskBlocked, Wake: &service.WakeSpec{Kind: "watchdog"}}); err == nil {
		t.Error("invalid wake kind: want 400")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 400 {
		t.Errorf("invalid wake kind: want 400, got %v", err)
	}

	// (b) timer без after → 400
	id = mk()
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: id, State: models.TaskBlocked, Wake: &service.WakeSpec{Kind: "timer"}}); err == nil {
		t.Error("timer without after: want 400")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 400 {
		t.Errorf("timer without after: want 400, got %v", err)
	}

	// (c) task без task_id → 400
	id = mk()
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{
		ID: id, State: models.TaskBlocked, Wake: &service.WakeSpec{Kind: "task"}}); err == nil {
		t.Error("task wake without task_id: want 400")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 400 {
		t.Errorf("task wake without task_id: want 400, got %v", err)
	}

	// (d) блок БЕЗ wake — совместимость (ошибка нет)
	id = mk()
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: id, State: models.TaskBlocked}); err != nil {
		t.Errorf("block without wake: want ok (compatibility), got %v", err)
	}
}
