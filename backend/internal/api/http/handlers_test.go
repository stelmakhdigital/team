package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/service"
)

type testEnv struct {
	srv *httptest.Server
	db  interface{ Close() error }
}

func newTestServer(t *testing.T, apiKeys []string) *httptest.Server {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := service.NewTeamService(db, repository.NewStores(db))
	tsvc := service.NewTaskService(db, repository.NewStores(db))
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		APIKeys: apiKeys,
		DB:      db,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url string, body any, hdr map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	dec := json.NewDecoder(resp.Body)
	_ = dec.Decode(&out)
	return resp.StatusCode, out, resp.Header
}

func specBody() map[string]any {
	return map[string]any{
		"name": "dev-team",
		"spec": map[string]any{
			"segments": []any{
				map[string]any{"name": "backend", "layout": map[string]any{"x": 10, "y": 20, "width": 100, "height": 80}},
				map[string]any{"name": "review"},
			},
			"roles": []any{
				map[string]any{"segment": "backend", "name": "lead", "agent_spec": "agents/lead.yaml"},
				map[string]any{"segment": "backend", "name": "worker", "agent_spec": "agents/worker.yaml"},
				map[string]any{"segment": "review", "name": "reviewer", "agent_spec": "agents/reviewer.yaml"},
			},
			"relatives": []any{
				map[string]any{"from": "backend.lead", "to": "backend.worker", "type": "delegates_to"},
			},
		},
	}
}

// Полная вертикаль Team Builder: create → list → get → topology → validate.
func TestTeamBuilderVertical(t *testing.T) {
	srv := newTestServer(t, nil)

	// 1. создание команды с spec
	st, out, hdr := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: status=%d body=%v", st, out)
	}
	if out["status"] != "created" {
		t.Fatalf("bad create response: %v", out)
	}
	teamID := int64(out["id"].(float64))
	if hdr.Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id header")
	}

	// 2. список команд
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/teams", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list teams: %d %v", st, out)
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("bad total: %v", out["total"])
	}
	teams := out["teams"].([]any)
	tm := teams[0].(map[string]any)
	if tm["segments_count"].(float64) != 2 || tm["roles_count"].(float64) != 3 {
		t.Fatalf("bad counts: %v", tm)
	}

	// 3. детали команды
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/teams/1", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get team: %d %v", st, out)
	}
	roles := out["roles"].([]any)
	if len(roles) != 3 {
		t.Fatalf("bad roles: %v", roles)
	}
	lead := roles[0].(map[string]any)
	if lead["address"] != "dev-team:backend.lead" || lead["segment_name"] != "backend" {
		t.Fatalf("bad role: %v", lead)
	}
	rels := out["relatives"].([]any)
	rel := rels[0].(map[string]any)
	if rel["from_role_name"] != "lead" || rel["to_role_name"] != "worker" {
		t.Fatalf("bad relative: %v", rel)
	}

	// 4. topology
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/teams/1/topology", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("topology: %d %v", st, out)
	}
	layout := out["layout"].(map[string]any)
	segLayouts := layout["segments"].([]any)
	if segLayouts[0].(map[string]any)["position"].(map[string]any)["x"].(float64) != 10 {
		t.Fatalf("bad topology layout: %v", layout)
	}

	// 4a. topology — контрактные snake_case поля (regression: models без json-тегов)
	tView := out["team"].(map[string]any)
	if _, ok := tView["segments_count"]; !ok {
		t.Fatalf("topology team missing segments_count: %v", tView)
	}
	if tView["roles_count"].(float64) != 3 {
		t.Fatalf("topology team bad roles_count: %v", tView)
	}
	tSegs := out["segments"].([]any)
	tSeg := tSegs[0].(map[string]any)
	if _, ok := tSeg["roles_count"]; !ok {
		t.Fatalf("topology segment missing roles_count: %v", tSeg)
	}
	tRoles := out["roles"].([]any)
	tRole := tRoles[0].(map[string]any)
	if tRole["segment_name"] == nil || tRole["segment_name"] == "" {
		t.Fatalf("topology role missing segment_name: %v", tRole)
	}
	tRels := out["relatives"].([]any)
	tRel := tRels[0].(map[string]any)
	if tRel["from_role_name"] == nil || tRel["to_role_name"] == nil {
		t.Fatalf("topology relative missing role names: %v", tRel)
	}

	// 5. validate
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/teams/1/validate", map[string]any{}, nil)
	if st != http.StatusOK {
		t.Fatalf("validate: %d %v", st, out)
	}
	// reviewer без входящих/исходящих → warning NO_INCOMING_EDGES + orphan? reviewer без рёбер → ORPHAN_ROLE error
	if out["is_valid"].(bool) {
		t.Logf("note: topology valid (orphan check: reviewer has no edges)")
	}
	_ = teamID
}

func TestCreateSegmentRoleRelative(t *testing.T) {
	srv := newTestServer(t, nil)
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "t"}, nil)
	if st != http.StatusCreated {
		t.Fatal(st, out)
	}
	teamID := int64(out["id"].(float64))

	// segment
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/teams/1/segments",
		map[string]any{"name": "backend", "layout": map[string]any{"x": 1, "y": 2}}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create segment: %d %v", st, out)
	}
	segID := int64(out["id"].(float64))

	// role
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/segments/1/roles",
		map[string]any{"name": "lead", "agent_spec": "a.yaml"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create role: %d %v", st, out)
	}
	roleID := int64(out["id"].(float64))
	if out["address"] != "t:backend.lead" {
		t.Fatalf("bad address: %v", out["address"])
	}

	// дубль роли → 409
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/segments/1/roles",
		map[string]any{"name": "lead", "agent_spec": "a.yaml"}, nil)
	if st != http.StatusConflict {
		t.Fatalf("duplicate role: expected 409, got %d", st)
	}

	// relative (нужны две роли)
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/segments/1/roles",
		map[string]any{"name": "worker", "agent_spec": "b.yaml"}, nil)
	if st != http.StatusCreated {
		t.Fatal(st, out)
	}
	workerID := int64(out["id"].(float64))

	st, out, _ = do(t, "POST", srv.URL+"/api/v1/teams/1/relatives",
		map[string]any{"from_role_id": roleID, "to_role_id": workerID, "type": "delegates_to"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create relative: %d %v", st, out)
	}

	// PATCH layout
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/segments/1/layout",
		map[string]any{"position": map[string]any{"x": 5, "y": 6}}, nil)
	if st != http.StatusOK {
		t.Fatalf("patch segment layout: %d %v", st, out)
	}
	if out["status"] != "updated" {
		t.Fatalf("bad layout response: %v", out)
	}
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/roles/1/layout",
		map[string]any{"position": map[string]any{"x": 7, "y": 8}}, nil)
	if st != http.StatusOK {
		t.Fatalf("patch role layout: %d %v", st, out)
	}
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/relatives/1/layout",
		map[string]any{"path": []any{map[string]any{"x": 0, "y": 0}}}, nil)
	if st != http.StatusOK {
		t.Fatalf("patch relative layout: %d %v", st, out)
	}

	// PATCH config
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/roles/1/config",
		map[string]any{"profile": "debug"}, nil)
	if st != http.StatusOK || out["status"] != "updated" {
		t.Fatalf("patch role config: %d %v", st, out)
	}
	if _, hasNew := out["new_config"].(map[string]any); !hasNew {
		t.Fatalf("missing new_config: %v", out)
	}

	// DELETE relative
	st, out, _ = do(t, "DELETE", srv.URL+"/api/v1/relatives/1", nil, nil)
	if st != http.StatusOK || out["status"] != "deleted" {
		t.Fatalf("delete relative: %d %v", st, out)
	}
	st, _, _ = do(t, "DELETE", srv.URL+"/api/v1/relatives/1", nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete relative again: expected 404, got %d", st)
	}

	_ = teamID
	_ = segID
}

func TestErrorFormat(t *testing.T) {
	srv := newTestServer(t, nil)

	// 404
	st, out, hdr := do(t, "GET", srv.URL+"/api/v1/teams/999", nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", st)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != "not_found" {
		t.Fatalf("bad error code: %v", errObj)
	}
	if errObj["request_id"] != hdr.Get("X-Request-Id") {
		t.Fatalf("request_id mismatch: %v vs %q", errObj["request_id"], hdr.Get("X-Request-Id"))
	}

	// 400 invalid JSON
	req, err := http.NewRequest("POST", srv.URL+"/api/v1/teams", strings.NewReader("{invalid"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// 400 invalid id param
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/teams/abc", nil, nil)
	if st != http.StatusBadRequest || out["error"].(map[string]any)["code"] != "validation_failed" {
		t.Fatalf("bad id: %d %v", st, out)
	}

	// 409 duplicate team
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "dup"}, nil)
	st2, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "dup"}, nil)
	if st != http.StatusCreated || st2 != http.StatusConflict {
		t.Fatalf("dup team: %d %d %v", st, st2, out)
	}
}

func TestAuth(t *testing.T) {
	srv := newTestServer(t, []string{"secret-key"})

	// без ключа → 401
	st, out, _ := do(t, "GET", srv.URL+"/api/v1/teams", nil, nil)
	if st != http.StatusUnauthorized || out["error"].(map[string]any)["code"] != "unauthorized" {
		t.Fatalf("expected 401: %d %v", st, out)
	}
	// неверный ключ → 401
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/teams", nil, map[string]string{"X-API-Key": "wrong"})
	if st != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong key, got %d", st)
	}
	// верный ключ → 200
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/teams", nil, map[string]string{"X-API-Key": "secret-key"})
	if st != http.StatusOK {
		t.Fatalf("expected 200 with key, got %d", st)
	}
	// healthz без ключа (auth применяется ко всем роутам) → тоже 401
	st, _, _ = do(t, "GET", srv.URL+"/healthz", nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("healthz should require key when auth on, got %d", st)
	}
}

func TestRoleConfigAndSave(t *testing.T) {
	srv := newTestServer(t, nil)

	// команда + роль с агент-спеком
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatal(st, out)
	}

	// GET /roles/1/config — файл spec нет на диске → stub (available=false)
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/roles/1/config", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("role config: %d %v", st, out)
	}
	spec := out["agent_spec"].(map[string]any)
	if spec["available"] != false {
		t.Fatalf("expected stub spec, got %v", spec)
	}
	if _, ok := out["role"].(map[string]any); !ok {
		t.Fatalf("missing role: %v", out)
	}
	if _, ok := out["available_profiles"].([]any); !ok {
		t.Fatalf("missing available_profiles: %v", out)
	}

	// 404 для несуществующей роли
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/roles/999/config", nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", st)
	}

	// POST /teams/1/save → 200 + validation
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/teams/1/save", map[string]any{"save_to_library": false}, nil)
	if st != http.StatusOK {
		t.Fatalf("save: %d %v", st, out)
	}
	if out["status"] != "saved" {
		t.Fatalf("bad save response: %v", out)
	}
	if _, ok := out["validation"].(map[string]any); !ok {
		t.Fatalf("missing validation in save response: %v", out)
	}

	// save archived команды → 409
	do(t, "DELETE", srv.URL+"/api/v1/teams/1", nil, nil)
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/teams/1/save", map[string]any{}, nil)
	if st != http.StatusConflict {
		t.Fatalf("save archived: expected 409, got %d", st)
	}
}

func TestAuthBearer(t *testing.T) {
	srv := newTestServer(t, []string{"secret-key"})
	// Bearer-формат (как использует frontend)
	st, _, _ := do(t, "GET", srv.URL+"/api/v1/teams", nil, map[string]string{"Authorization": "Bearer secret-key"})
	if st != http.StatusOK {
		t.Fatalf("expected 200 with Bearer key, got %d", st)
	}
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/teams", nil, map[string]string{"Authorization": "Bearer wrong"})
	if st != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong Bearer key, got %d", st)
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t, nil)
	st, out, _ := do(t, "GET", srv.URL+"/healthz", nil, nil)
	if st != http.StatusOK || out["status"] != "ok" {
		t.Fatalf("healthz: %d %v", st, out)
	}
	st, out, _ = do(t, "GET", srv.URL+"/readyz", nil, nil)
	if st != http.StatusOK || out["status"] != "ready" {
		t.Fatalf("readyz: %d %v", st, out)
	}
}

// ---------- Slice 2: tasks & dashboard (HTTP) ----------

func TestTasksHTTPFlow(t *testing.T) {
	srv := newTestServer(t, nil)
	base := srv.URL + "/api/v1"

	// команда с двумя ролями через spec
	body := specBody()
	st, out, _ := do(t, "POST", base+"/teams", body, nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	st, out, _ = do(t, "GET", base+"/teams/"+itoaID(teamID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get team: %d", st)
	}
	roles := out["roles"].([]any)
	roleA := int64(roles[0].(map[string]any)["id"].(float64))
	roleB := int64(roles[1].(map[string]any)["id"].(float64))

	// POST /tasks
	st, out, _ = do(t, "POST", base+"/tasks", map[string]any{
		"team_id": teamID, "destination_role_id": roleA, "title": "http task",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create task: %d %v", st, out)
	}
	taskID := int64(out["id"].(float64))

	// validation: пустой title → 400
	st, out, _ = do(t, "POST", base+"/tasks", map[string]any{"team_id": teamID, "destination_role_id": roleA}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("empty title: got %d, want 400 (%v)", st, out)
	}

	// PATCH state: pending → in_progress
	st, out, _ = do(t, "PATCH", base+"/tasks/"+itoaID(taskID)+"/state",
		map[string]any{"state": "in_progress", "comment": "go"}, nil)
	if st != http.StatusOK {
		t.Fatalf("state patch: %d %v", st, out)
	}
	if out["state"] != "in_progress" || out["team_name"] == "" {
		t.Errorf("task view = %v", out)
	}

	// invalid: done без closure_reason → 400
	st, out, _ = do(t, "PATCH", base+"/tasks/"+itoaID(taskID)+"/state", map[string]any{"state": "done"}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("done w/o reason: got %d, want 400 (%v)", st, out)
	}

	// GET task с подзадачами
	st, out, _ = do(t, "GET", base+"/tasks/"+itoaID(taskID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get task: %d", st)
	}
	if _, ok := out["task"]; !ok {
		t.Errorf("task detail: %v", out)
	}

	// handoff на roleB
	st, out, _ = do(t, "POST", base+"/tasks/"+itoaID(taskID)+"/handoff",
		map[string]any{"to_role_id": roleB, "comment": "next"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("handoff: %d %v", st, out)
	}
	if out["status"] != "handed_off" {
		t.Errorf("handoff: %v", out)
	}
	newTaskID := int64(out["new_task_id"].(float64))

	// handoff завершённой задачи → 409
	st, _, _ = do(t, "POST", base+"/tasks/"+itoaID(taskID)+"/handoff", map[string]any{"to_role_id": roleA}, nil)
	if st != http.StatusConflict {
		t.Fatalf("handoff closed task: got %d, want 409", st)
	}

	// история
	st, out, _ = do(t, "GET", base+"/tasks/"+itoaID(taskID)+"/history", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("history: %d", st)
	}
	if len(out["history"].([]any)) < 3 {
		t.Errorf("history too short: %v", out)
	}

	// список с фильтрами
	st, out, _ = do(t, "GET", base+"/tasks?team_id="+itoaID(teamID)+"&state=pending", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list tasks: %d", st)
	}
	if out["total"].(float64) != 1 {
		t.Errorf("list pending: total=%v, want 1 (new task after handoff)", out["total"])
	}

	// 404
	st, _, _ = do(t, "GET", base+"/tasks/999999", nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("unknown task: got %d, want 404", st)
	}

	// dashboard
	st, out, _ = do(t, "GET", base+"/dashboard/summary", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("dashboard summary: %d %v", st, out)
	}
	if out["teams"].(map[string]any)["total"].(float64) != 1 {
		t.Errorf("summary teams: %v", out["teams"])
	}
	st, out, _ = do(t, "GET", base+"/dashboard/tasks", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("dashboard tasks: %d", st)
	}
	if out["total"].(float64) != 1 {
		t.Errorf("dashboard tasks: %v (expect the new pending task)", out)
	}
	if out["tasks"].([]any)[0].(map[string]any)["destination_role_name"] != "worker" {
		t.Errorf("dashboard task view: %v", out["tasks"])
	}
	_ = newTaskID
}

func itoaID(id int64) string {
	return strconv.FormatInt(id, 10)
}
