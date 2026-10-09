package httpapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P1.4: Workflow engine — HTTP вертикаль + RBAC ----------

func newEngineTestServer(t *testing.T) (*httptest.Server, *service.AuthService) {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(svc.SpecsDir, "a.yaml"), []byte("name: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tsvc := service.NewTaskService(db, stores)
	wsvc := service.NewWorkflowService(db, stores)
	engine := service.NewWorkflowEngine(db, stores)
	attention := service.NewAttentionService(db, stores)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	rtRegistry := runtime.NewRegistry()
	ssvc := service.NewSessionService(db, stores, rtRegistry)
	bus := service.NewEventBus()
	tsvc.Bus = bus
	engine.Bus = bus
	tsvc.ProjectionHook = engine.ProjectStepClose
	stores2 := repository.NewStores(db)
	alerts := &service.AlertStoreRef{Events: stores2.Watchdog, Teams: stores2.Teams, DB: db}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc, Alerts: alerts,
		Workflows: wsvc, Events: bus,
		Auth: auth, Attention: attention, Engine: engine,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, auth
}

// engineHTTPSetup — команда + сегмент + n ролей; возвращает teamID + roleIDs.
func engineHTTPSetup(t *testing.T, srv *httptest.Server, admin map[string]string, n int) (int64, []int64) {
	t.Helper()
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "engine-team"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/teams/"+itoa(teamID)+"/segments", map[string]any{"name": "core"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create segment: %d %v", st, out)
	}
	segID := int64(out["id"].(float64))
	roleIDs := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		name := "role_" + string(rune('a'+i))
		st, out, _ = do(t, "POST", srv.URL+"/api/v1/segments/"+itoa(segID)+"/roles",
			map[string]any{"name": name, "agent_spec": "a.yaml"}, admin)
		if st != http.StatusCreated {
			t.Fatalf("create role %s: %d %v", name, st, out)
		}
		roleIDs = append(roleIDs, int64(out["id"].(float64)))
	}
	return teamID, roleIDs
}

func TestWorkflowEngineVertical(t *testing.T) {
	srv, _ := newEngineTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roles := engineHTTPSetup(t, srv, admin, 2)

	// workflow (task A → task B)
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/workflows", map[string]any{
		"team_id": teamID, "name": "engine-flow",
		"blocks": []any{
			map[string]any{"type": "task", "label": "A", "config": map[string]any{"role_id": roles[0]}},
			map[string]any{"type": "task", "label": "B", "config": map[string]any{"role_id": roles[1]}},
		},
		"connections": []any{map[string]any{"from_block_id": 0, "to_block_id": 1}},
	}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create workflow: %d %v", st, out)
	}
	wfID := int64(out["id"].(float64))

	// engine не включён → run 409
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/run", map[string]any{"root_objective": "x"}, admin)
	if st != http.StatusConflict {
		t.Fatalf("run on non-enabled: want 409, got %d %v", st, out)
	}

	// enable engine
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/workflows/"+itoa(wfID), map[string]any{"name": "engine-flow", "engine_enabled": true}, admin)
	if st != http.StatusOK {
		t.Fatalf("enable engine: %d %v", st, out)
	}

	// run
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/run", map[string]any{"root_objective": "build"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("run: %d %v", st, out)
	}
	inst := out["instance"].(map[string]any)
	iid := int64(inst["id"].(float64))
	if inst["state"] != "running" {
		t.Fatalf("run state = %v, want running", inst["state"])
	}

	// list instances (1)
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/instances", nil, admin)
	if st != http.StatusOK || out["total"].(float64) != 1 {
		t.Fatalf("list instances: %d %v", st, out)
	}

	// get instance: 1 active step с task
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflow-instances/"+itoa(iid), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("get instance: %d %v", st, out)
	}
	steps := out["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["state"] != "active" {
		t.Fatalf("instance steps = %v", steps)
	}
	taskID := int64(steps[0].(map[string]any)["task_id"].(float64))

	// close task A (done, no_follow_on) → projection создаёт B
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/tasks/"+itoa(taskID)+"/state",
		map[string]any{"state": "done", "closure_reason": "no_follow_on"}, admin)
	if st != http.StatusOK {
		t.Fatalf("close task A: %d %v", st, out)
	}
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflow-instances/"+itoa(iid), nil, admin)
	if len(out["steps"].([]any)) != 2 {
		t.Fatalf("after close: steps = %d, want 2", len(out["steps"].([]any)))
	}

	// trace: 2 записи
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflow-instances/"+itoa(iid)+"/trace", nil, admin)
	if st != http.StatusOK || out["total"].(float64) != 2 {
		t.Fatalf("trace: %d %v", st, out)
	}

	// route active-шага на роль B
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflow-instances/"+itoa(iid)+"/route",
		map[string]any{"to_role_id": roles[1]}, admin)
	if st != http.StatusOK {
		t.Fatalf("route: %d %v", st, out)
	}

	// abort → aborted
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflow-instances/"+itoa(iid)+"/abort", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("abort: %d %v", st, out)
	}
	if out["instance"].(map[string]any)["state"] != "aborted" {
		t.Fatalf("abort state = %v", out["instance"].(map[string]any)["state"])
	}
	// повторный abort → 409 (уже завершён)
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/workflow-instances/"+itoa(iid)+"/abort", nil, admin)
	if st != http.StatusConflict {
		t.Fatalf("second abort: want 409, got %d", st)
	}
}

func TestWorkflowEngineRBAC(t *testing.T) {
	srv, auth := newEngineTestServer(t)
	// viewer: read instances OK, run denied (403)
	_, viewerKey := mustCreateKey(t, auth, "wf-viewer", "viewer", "v-user")
	_ = auth.Init(context.Background())
	viewer := map[string]string{"X-API-Key": viewerKey}

	// нужен существующий workflow для read
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, _ := engineHTTPSetup(t, srv, admin, 1)
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/workflows", map[string]any{
		"team_id": teamID, "name": "rbac-flow",
		"blocks": []any{map[string]any{"type": "task", "config": map[string]any{"role_id": 1}}},
	}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create workflow: %d %v", st, out)
	}
	wfID := int64(out["id"].(float64))

	// viewer: GET instances = 200 (workflows.read)
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/instances", nil, viewer)
	if st != http.StatusOK {
		t.Fatalf("viewer list instances: want 200, got %d", st)
	}
	// viewer: POST run = 403 (workflows.update)
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/run", map[string]any{}, viewer)
	if st != http.StatusForbidden {
		t.Fatalf("viewer run: want 403, got %d", st)
	}

	// operator: POST run OK (workflows.update)
	_, opKey := mustCreateKey(t, auth, "wf-operator", "operator", "o-user")
	_ = auth.Init(context.Background())
	operator := map[string]string{"X-API-Key": opKey}
	// enable engine (operator: workflows.update)
	st, _, _ = do(t, "PATCH", srv.URL+"/api/v1/workflows/"+itoa(wfID), map[string]any{"name": "rbac-flow", "engine_enabled": true}, operator)
	if st != http.StatusOK {
		t.Fatalf("operator enable engine: want 200, got %d", st)
	}
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/run", map[string]any{}, operator)
	if st != http.StatusCreated {
		t.Fatalf("operator run: want 201, got %d", st)
	}
}
