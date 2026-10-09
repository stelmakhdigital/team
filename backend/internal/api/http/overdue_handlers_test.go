package httpapi_test

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"testing"
	"time"

	"daemon/internal/models"
	"daemon/internal/service"
)

// seedOverdueTask — задача в in_progress с заданным closure_required_at (дедлайн).
func seedOverdueTask(t *testing.T, ctx context.Context, db *sql.DB, tsvc *service.TaskService,
	teamID, roleID int64, title string, due time.Time) int64 {
	t.Helper()
	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleID, Title: title})
	if err != nil {
		t.Fatalf("CreateTask %q: %v", title, err)
	}
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskInProgress}); err != nil {
		t.Fatalf("->in_progress %q: %v", title, err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE queue_tasks SET closure_required_at = ? WHERE id = ?`,
		due.UTC().Format(time.RFC3339Nano), task.ID); err != nil {
		t.Fatalf("set closure_required_at %q: %v", title, err)
	}
	return task.ID
}

func TestTasksOverdueEndpoint(t *testing.T) {
	srv, auth, db, svc, tsvc, _ := newAttentionTestServer(t)
	ctx := context.Background()
	adminHdr := map[string]string{"X-API-Key": "env-admin"}

	team, err := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "od-http"})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	seg, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	role, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/a.yaml", Layout: &service.Layout{X: 0, Y: 0}})

	base := srv.URL + "/api/v1/tasks/overdue"

	// no key -> 401
	if st, _, _ := do(t, "GET", base, nil, nil); st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}

	// admin, empty -> total 0
	st, out, _ := do(t, "GET", base, nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("admin GET overdue (empty): want 200, got %d %v", st, out)
	}
	if got := out["total"].(float64); got != 0 {
		t.Fatalf("empty total = %v, want 0", got)
	}
	if items := out["tasks"].([]any); len(items) != 0 {
		t.Fatalf("empty tasks = %d, want 0", len(items))
	}

	// 2 просроченные + 1 в будущем (не просрочена)
	od1 := seedOverdueTask(t, ctx, db, tsvc, team.ID, role.ID, "od-1", time.Now().UTC().Add(-2*time.Hour))
	seedOverdueTask(t, ctx, db, tsvc, team.ID, role.ID, "od-2", time.Now().UTC().Add(-1*time.Hour))
	seedOverdueTask(t, ctx, db, tsvc, team.ID, role.ID, "not-od", time.Now().UTC().Add(2*time.Hour))

	// admin -> total 2, обе is_overdue, поля на месте
	st, out, _ = do(t, "GET", base, nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("admin GET overdue: want 200, got %d %v", st, out)
	}
	if got := out["total"].(float64); got != 2 {
		t.Fatalf("total = %v, want 2", got)
	}
	items := out["tasks"].([]any)
	if len(items) != 2 {
		t.Fatalf("tasks = %d, want 2", len(items))
	}
	first := items[0].(map[string]any)
	if first["is_overdue"] != true {
		t.Errorf("first is_overdue = %v, want true", first["is_overdue"])
	}
	for _, f := range []string{"id", "title", "state", "is_overdue", "closure_required_at", "remaining_secs"} {
		if _, ok := first[f]; !ok {
			t.Fatalf("overdue item missing field %q: %v", f, first)
		}
	}
	// самая просроченная (ранний дедлайн) — сверху
	if first["title"] != "od-1" {
		t.Errorf("most overdue first = %v, want od-1", first["title"])
	}
	if rs, ok := first["remaining_secs"].(float64); !ok || rs >= 0 {
		t.Errorf("remaining_secs = %v, want < 0 (просрочено)", first["remaining_secs"])
	}

	// limit=1 -> 1 item, total 2
	st, out, _ = do(t, "GET", base+"?limit=1", nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("limit=1: want 200, got %d", st)
	}
	if got := out["total"].(float64); got != 2 {
		t.Fatalf("limit=1 total = %v, want 2", got)
	}
	if items := out["tasks"].([]any); len(items) != 1 {
		t.Fatalf("limit=1 -> %d items, want 1", len(items))
	}

	// is_overdue в деталке (GET /tasks/{id} → { task: {...}, subtasks })
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(od1, 10), nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("detail: want 200, got %d", st)
	}
	taskView, ok := out["task"].(map[string]any)
	if !ok {
		t.Fatalf("detail missing task object: %v", out)
	}
	if taskView["is_overdue"] != true {
		t.Errorf("detail is_overdue = %v, want true", taskView["is_overdue"])
	}
	if taskView["closure_required_at"] == nil {
		t.Error("detail missing closure_required_at")
	}

	// viewer -> 200 (dashboard.read, у viewer есть *.read)
	_, viewKey := mustCreateKey(t, auth, "od-viewer", "viewer", "od-viewer-user")
	if err := auth.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := do(t, "GET", base, nil, map[string]string{"X-API-Key": viewKey}); st != http.StatusOK {
		t.Fatalf("viewer GET overdue: want 200, got %d", st)
	}
}

func TestAttentionTaskOverdue(t *testing.T) {
	srv, _, db, svc, tsvc, _ := newAttentionTestServer(t)
	ctx := context.Background()
	adminHdr := map[string]string{"X-API-Key": "env-admin"}

	team, _ := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "att-od"})
	seg, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	role, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/a.yaml", Layout: &service.Layout{X: 0, Y: 0}})

	// in_progress задача с просроченным дедлайном
	seedOverdueTask(t, ctx, db, tsvc, team.ID, role.ID, "overdue-task", time.Now().UTC().Add(-3*time.Hour))

	// GET /attention -> есть item с kind=task_overdue
	st, out, _ := do(t, "GET", srv.URL+"/api/v1/attention", nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("GET attention: want 200, got %d %v", st, out)
	}
	items := out["items"].([]any)
	found := false
	for _, raw := range items {
		it := raw.(map[string]any)
		if it["kind"] == "task_overdue" {
			found = true
			if it["severity"] != "high" {
				t.Errorf("task_overdue severity = %v, want high", it["severity"])
			}
			if it["suggested_action"] == "" {
				t.Error("task_overdue missing suggested_action")
			}
			if _, ok := it["link"]; !ok {
				t.Error("task_overdue missing link")
			}
		}
	}
	if !found {
		t.Fatalf("no task_overdue item in attention: %v", items)
	}
}
