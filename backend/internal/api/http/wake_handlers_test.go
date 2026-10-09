package httpapi_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"daemon/internal/models"
	"daemon/internal/service"
)

// blockWithWake — блокировка задачи через API с wake; возвращает id.
func blockWithWake(t *testing.T, srvURL string, adminHdr map[string]string, taskID int64, wakeBody map[string]any) (int, map[string]any) {
	t.Helper()
	body := map[string]any{"state": "blocked"}
	if wakeBody != nil {
		body["wake"] = wakeBody
	}
	st, out, _ := do(t, "PATCH", srvURL+"/api/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/state", body, adminHdr)
	return st, out
}

func TestResolveTaskEndpoint(t *testing.T) {
	srv, auth, _, svc, tsvc, _ := newAttentionTestServer(t)
	ctx := context.Background()
	adminHdr := map[string]string{"X-API-Key": "env-admin"}

	team, _ := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "resolve-http"})
	seg, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	role, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	mkTask := func(title string) int64 {
		tk, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team.ID, DestinationRoleID: role.ID, Title: title})
		return tk.ID
	}
	block := func(id int64) {
		if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: id, State: models.TaskBlocked}); err != nil {
			t.Fatalf("block %d: %v", id, err)
		}
	}

	// no key -> 401
	blocked := mkTask("b1")
	block(blocked)
	if st, _, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(blocked, 10)+"/resolve",
		map[string]any{"decision": "x"}, nil); st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}

	// resolve без decision -> 400
	if st, _, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(blocked, 10)+"/resolve",
		map[string]any{"decision": ""}, adminHdr); st != http.StatusBadRequest {
		t.Fatalf("empty decision: want 400, got %d", st)
	}

	// resolve заблокированной -> 200, task pending
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(blocked, 10)+"/resolve",
		map[string]any{"decision": "dependency cleared"}, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("resolve: want 200, got %d %v", st, out)
	}
	tv, _ := out["task"].(map[string]any)
	if tv == nil || tv["state"] != "pending" {
		t.Fatalf("resolve task state = %v, want pending", out["task"])
	}
	if out["status"] != "resolved" {
		t.Errorf("status = %v, want resolved", out["status"])
	}

	// повторный resolve (теперь pending) -> 409
	if st, _, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(blocked, 10)+"/resolve",
		map[string]any{"decision": "again"}, adminHdr); st != http.StatusConflict {
		t.Fatalf("re-resolve non-blocked: want 409, got %d", st)
	}

	// resolve не-blocked (pending) -> 409
	other := mkTask("o1")
	if st, _, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(other, 10)+"/resolve",
		map[string]any{"decision": "x"}, adminHdr); st != http.StatusConflict {
		t.Fatalf("resolve non-blocked: want 409, got %d", st)
	}

	// RBAC: viewer (нет tasks.update) -> 403 на resolve
	_, viewerKey := mustCreateKey(t, auth, "resolve-viewer", "viewer", "resolve-viewer-user")
	if err := auth.Init(ctx); err != nil {
		t.Fatal(err)
	}
	blocked2 := mkTask("b2")
	block(blocked2)
	if st, _, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(blocked2, 10)+"/resolve",
		map[string]any{"decision": "x"}, map[string]string{"X-API-Key": viewerKey}); st != http.StatusForbidden {
		t.Fatalf("viewer resolve: want 403, got %d", st)
	}
}

func TestBlockWithWakeEndpoint(t *testing.T) {
	srv, _, _, svc, tsvc, _ := newAttentionTestServer(t)
	ctx := context.Background()
	adminHdr := map[string]string{"X-API-Key": "env-admin"}

	team, _ := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "blockwake-http"})
	seg, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	role, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	mkTask := func(title string) int64 {
		tk, _ := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team.ID, DestinationRoleID: role.ID, Title: title})
		return tk.ID
	}

	// timer wake -> 200 + wake в деталке
	id := mkTask("timer-wake")
	if st, _ := blockWithWake(t, srv.URL, adminHdr, id,
		map[string]any{"kind": "timer", "after": "30m", "continuation": "wait for CI"}); st != http.StatusOK {
		t.Fatalf("block+timer wake: want 200, got %d", st)
	}
	st, out, _ := do(t, "GET", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(id, 10), nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("detail: want 200, got %d", st)
	}
	wake, _ := out["wake"].(map[string]any)
	if wake == nil {
		t.Fatalf("detail missing wake: %v", out)
	}
	if wake["kind"] != "timer" {
		t.Errorf("wake.kind = %v, want timer", wake["kind"])
	}
	if wake["fired"] != false {
		t.Errorf("wake.fired = %v, want false", wake["fired"])
	}
	if wake["continuation"] != "wait for CI" {
		t.Errorf("wake.continuation = %v", wake["continuation"])
	}
	if _, ok := wake["remaining_secs"]; !ok {
		t.Error("timer wake missing remaining_secs")
	}

	// task wake -> 200 + kind=task, ref = task_id
	ref := mkTask("ref")
	id2 := mkTask("task-wake")
	if st, _ := blockWithWake(t, srv.URL, adminHdr, id2,
		map[string]any{"kind": "task", "task_id": ref, "continuation": "resume"}); st != http.StatusOK {
		t.Fatalf("block+task wake: want 200, got %d", st)
	}
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(id2, 10), nil, adminHdr)
	if st != http.StatusOK {
		t.Fatalf("detail (task wake): want 200, got %d", st)
	}
	wake, _ = out["wake"].(map[string]any)
	if wake == nil {
		t.Fatalf("detail missing wake: %v", out)
	}
	if wake["kind"] != "task" {
		t.Errorf("wake.kind = %v, want task", wake["kind"])
	}
	if wake["ref"] != strconv.FormatInt(ref, 10) {
		t.Errorf("wake.ref = %v, want %d", wake["ref"], ref)
	}

	// неверный kind -> 400
	bad := mkTask("bad-kind")
	if st, _ := blockWithWake(t, srv.URL, adminHdr, bad,
		map[string]any{"kind": "watchdog"}); st != http.StatusBadRequest {
		t.Fatalf("invalid wake kind: want 400, got %d", st)
	}

	// timer без after -> 400
	bad2 := mkTask("bad-after")
	if st, _ := blockWithWake(t, srv.URL, adminHdr, bad2,
		map[string]any{"kind": "timer"}); st != http.StatusBadRequest {
		t.Fatalf("timer without after: want 400, got %d", st)
	}

	// блок БЕЗ wake — совместимость -> 200, wake отсутствует
	nw := mkTask("no-wake")
	if st, out, _ := do(t, "PATCH", srv.URL+"/api/v1/tasks/"+strconv.FormatInt(nw, 10)+"/state",
		map[string]any{"state": "blocked"}, adminHdr); st != http.StatusOK {
		t.Fatalf("block without wake: want 200, got %d %v", st, out)
	}
}
