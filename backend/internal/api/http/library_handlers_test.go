package httpapi_test

import (
	"net/http"
	"testing"
)

// Slice 5b — Library + Audit + Metrics: вертикаль по HTTP.
func TestLibraryVertical(t *testing.T) {
	srv := newTestServer(t, nil)

	// команда-источник
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))

	// 1. save в library
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/library", map[string]any{
		"type": "team", "source_id": teamID, "name": "dev-team-template",
		"description": "standard dev team", "group": "teams",
		"tags": []string{"dev", "template"},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("save library: %d %v", st, out)
	}
	if out["status"] != "saved" {
		t.Fatalf("status = %v", out["status"])
	}
	itemID := int64(out["id"].(float64))
	if out["library_item_id"] == nil {
		t.Fatal("library_item_id missing")
	}

	// 2. duplicate → 409
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/library", map[string]any{
		"type": "team", "source_id": teamID, "name": "dev-team-template",
	}, nil)
	if st != http.StatusConflict {
		t.Fatalf("duplicate: status=%d, want 409", st)
	}

	// 3. list: 1 item, groups
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/library", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list: %d %v", st, out)
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("total = %v, want 1", out["total"])
	}
	groups := out["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["name"] != "teams" {
		t.Fatalf("groups = %v", groups)
	}
	item := out["items"].([]any)[0].(map[string]any)
	if item["type"] != "team" || item["version"] != "1.0.0" || item["group"] != "teams" {
		t.Fatalf("item = %v", item)
	}

	// 4. get: spec с segments/roles
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/library/"+itoa(itemID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get item: %d %v", st, out)
	}
	spec := out["spec"].(map[string]any)
	segs := spec["segments"].([]any)
	roles := spec["roles"].([]any)
	if len(segs) != 2 || len(roles) != 3 {
		t.Fatalf("spec: segments=%d roles=%d, want 2/3", len(segs), len(roles))
	}
	versions := out["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("versions = %v", versions)
	}

	// 5. apply → новая команда
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/library/"+itoa(itemID)+"/apply", map[string]any{
		"overrides": map[string]any{"name": "applied-team"},
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("apply: %d %v", st, out)
	}
	if out["status"] != "applied" {
		t.Fatalf("apply status = %v", out["status"])
	}
	created := out["created_resources"].(map[string]any)
	teamIDs := created["teams"].([]any)
	if len(teamIDs) != 1 {
		t.Fatalf("created teams = %v", created)
	}
	newTeamID := int64(teamIDs[0].(float64))

	// новая команда имеет ту же топологию
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/teams/"+itoa(newTeamID)+"/topology", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("topology: %d", st)
	}
	top := out
	if len(top["segments"].([]any)) != 2 || len(top["roles"].([]any)) != 3 {
		t.Fatalf("applied topology: segments=%d roles=%d", len(top["segments"].([]any)), len(top["roles"].([]any)))
	}

	// 6. apply merge в существующую команду
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/library/"+itoa(itemID)+"/apply", map[string]any{
		"target_team_id": teamID,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("apply merge: %d %v", st, out)
	}
	if out["status"] != "merged" {
		t.Fatalf("merge status = %v", out["status"])
	}

	// 7. invalidation: unknown item 404, invalid type 400
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/library/999", nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("unknown item: %d, want 404", st)
	}
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/library", map[string]any{
		"type": "nope", "source_id": 1, "name": "x",
	}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("invalid type: %d, want 400", st)
	}
}

func TestLibraryWorkflowApply(t *testing.T) {
	srv := newTestServer(t, nil)
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("team: %d", st)
	}
	teamID := int64(out["id"].(float64))

	// workflow
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows", map[string]any{
		"team_id": teamID, "name": "wf-lib",
		"blocks": []any{
			map[string]any{"type": "task", "label": "A"},
			map[string]any{"type": "agent"},
		},
		"connections": []any{map[string]any{"from_block_id": 0, "to_block_id": 1, "condition": "yes"}},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("workflow: %d %v", st, out)
	}
	wfID := int64(out["id"].(float64))

	// save workflow в library
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/library", map[string]any{
		"type": "workflow", "source_id": wfID, "name": "wf-template",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("save wf: %d %v", st, out)
	}
	itemID := int64(out["id"].(float64))

	// apply в другую команду
	st, out2, _ := do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "wf-target"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("target team: %d", st)
	}
	targetID := int64(out2["id"].(float64))
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/library/"+itoa(itemID)+"/apply", map[string]any{
		"target_team_id": targetID,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("apply wf: %d %v", st, out)
	}
	if out["status"] != "applied" {
		t.Fatalf("status = %v", out["status"])
	}

	// workflow создан в target
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows?team_id="+itoa(targetID), nil, nil)
	if out["total"].(float64) != 1 {
		t.Fatalf("target workflows = %v, want 1", out["total"])
	}
	wfList := out["workflows"].([]any)
	newWfID := int64(wfList[0].(map[string]any)["id"].(float64))
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows/"+itoa(newWfID), nil, nil)
	if len(out["blocks"].([]any)) != 2 || len(out["connections"].([]any)) != 1 {
		t.Fatalf("applied wf: blocks=%d conns=%d", len(out["blocks"].([]any)), len(out["connections"].([]any)))
	}

	// downloads_count вырос после apply
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/library/"+itoa(itemID), nil, nil)
	if st != http.StatusOK {
		t.Fatal(st)
	}
	if it := out["item"].(map[string]any); it["downloads_count"].(float64) < 1 {
		t.Fatalf("downloads_count = %v, want >= 1", it["downloads_count"])
	}
}

func TestAuditAndMetrics(t *testing.T) {
	srv := newTestServer(t, nil)

	// действия для audit
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("team: %d", st)
	}
	teamID := int64(out["id"].(float64))
	workerID := topologyRoleID(t, srv, teamID, "worker")
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks", map[string]any{
		"team_id": teamID, "destination_role_id": workerID, "title": "audit-task",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("task: %d", st)
	}
	st, _, _ = do(t, "PATCH", srv.URL+"/api/v1/tasks/1/state",
		map[string]any{"state": "done", "closure_reason": "no_follow_on"}, nil)
	if st != http.StatusOK {
		t.Fatalf("task state: %d", st)
	}

	// 1. audit log: записи есть
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/audit", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("audit: %d %v", st, out)
	}
	if out["total"].(float64) < 3 {
		t.Fatalf("audit total = %v, want >= 3", out["total"])
	}
	entries := out["entries"].([]any)
	byAction := map[string]map[string]any{}
	for _, raw := range entries {
		e := raw.(map[string]any)
		byAction[e["action"].(string)] = e
	}
	te, ok := byAction["team.create"]
	if !ok {
		t.Fatalf("team.create not in audit: %v", byAction)
	}
	if te["user_name"] == nil || te["user_name"] == "" {
		t.Fatalf("team.create user_name = %v", te)
	}
	tc, ok := byAction["task.create"]
	if !ok {
		t.Fatal("task.create not in audit")
	}
	if tc["resource"] == nil {
		t.Fatal("task.create resource missing")
	}
	if _, ok := byAction["task.state_update"]; !ok {
		t.Fatal("task.state_update not in audit")
	}

	// фильтр по action
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/audit?action=task.create", nil, nil)
	if out["total"].(float64) != 1 {
		t.Fatalf("audit by action total = %v, want 1", out["total"])
	}

	// 2. metrics: 12 точек на каждый ряд
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/dashboard/metrics?range=1h", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("metrics: %d %v", st, out)
	}
	metrics := out["metrics"].(map[string]any)
	// tasks_created/completed: сумма по всем бакетам = 1 (задача создана и закрыта в окне)
	for key, wantSum := range map[string]float64{"tasks_created": 1, "tasks_completed": 1} {
		pts, ok := metrics[key].([]any)
		if !ok || len(pts) != 12 {
			t.Fatalf("metrics.%s: len=%v, want 12", key, len(pts))
			continue
		}
		sum := 0.0
		for _, p := range pts {
			sum += p.(map[string]any)["value"].(float64)
		}
		if sum != wantSum {
			t.Fatalf("metrics.%s sum = %v, want %v (task created+done в 1h-окне)", key, sum, wantSum)
		}
	}
	// snapshot-ряды: 12 точек, значения >= 0
	for key := range map[string]bool{"sessions_active": true, "queue_size": true, "llm_tokens": true} {
		pts, ok := metrics[key].([]any)
		if !ok || len(pts) != 12 {
			t.Fatalf("metrics.%s: len=%v, want 12", key, len(pts))
			continue
		}
		for i, p := range pts {
			pm := p.(map[string]any)
			if pm["timestamp"] == "" || pm["value"].(float64) < 0 {
				t.Fatalf("metrics.%s[%d] = %v", key, i, pm)
			}
		}
	}
	// time_range
	tr := out["time_range"].(map[string]any)
	if tr["start"] == "" || tr["end"] == "" {
		t.Fatalf("time_range = %v", tr)
	}

	// invalid range → 400
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/dashboard/metrics?range=99x", nil, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("invalid range: %d, want 400", st)
	}
}

// Slice 6+ — Library apply для segment и role (контракт 20 §5.4).
func TestLibraryRoleSegmentApply(t *testing.T) {
	srv := newTestServer(t, nil)
	base := srv.URL + "/api/v1"

	// команда-источник: segments backend(lead, worker) + review(reviewer)
	st, out, _ := do(t, "POST", base+"/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("team: %d %v", st, out)
	}
	srcTeam := int64(out["id"].(float64))
	st, out, _ = do(t, "GET", base+"/teams/"+itoa(srcTeam), nil, nil)
	if st != http.StatusOK {
		t.Fatal(st)
	}
	segs := out["segments"].([]any)
	var backendSeg int64
	for _, sgm := range segs {
		sm := sgm.(map[string]any)
		switch sm["name"].(string) {
		case "backend":
			backendSeg = int64(sm["id"].(float64))
		}
	}
	var leadRole int64
	for _, rm := range out["roles"].([]any) {
		if rm.(map[string]any)["name"] == "lead" {
			leadRole = int64(rm.(map[string]any)["id"].(float64))
		}
	}
	// роль с существующим spec-файлом (I4: CreateRole проверяет файл)
	st, out, _ = do(t, "POST", base+"/segments/"+itoa(backendSeg)+"/roles", map[string]any{
		"name": "spec-lead", "agent_spec": "a.yaml",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create spec-lead: %d %v", st, out)
	}
	leadRole = int64(out["id"].(float64))

	// 1. save segment в library
	st, out, _ = do(t, "POST", base+"/library", map[string]any{
		"type": "segment", "source_id": backendSeg, "name": "backend-template",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("save segment: %d %v", st, out)
	}
	segItem := int64(out["id"].(float64))
	// 2. save role в library
	st, out, _ = do(t, "POST", base+"/library", map[string]any{
		"type": "role", "source_id": leadRole, "name": "lead-template",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("save role: %d %v", st, out)
	}
	roleItem := int64(out["id"].(float64))

	// spec: segment с 3 ролями, role с segment-именем
	st, out, _ = do(t, "GET", base+"/library/"+itoa(segItem), nil, nil)
	spec := out["spec"].(map[string]any)
	if spec["name"] != "backend" || len(spec["roles"].([]any)) != 3 {
		t.Fatalf("segment spec = %v", spec)
	}
	st, out, _ = do(t, "GET", base+"/library/"+itoa(roleItem), nil, nil)
	spec = out["spec"].(map[string]any)
	if spec["name"] != "spec-lead" || spec["segment"] != "backend" || spec["agent_spec"] != "a.yaml" {
		t.Fatalf("role spec = %v", spec)
	}

	// 3. apply segment в пустую команду (merge-семантика; 3 роли: lead, worker, spec-lead)
	st, out, _ = do(t, "POST", base+"/teams", map[string]any{"name": "seg-target"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("target: %d %v", st, out)
	}
	target := int64(out["id"].(float64))
	st, out, _ = do(t, "POST", base+"/library/"+itoa(segItem)+"/apply", map[string]any{
		"target_team_id": target,
	}, nil)
	if st != http.StatusOK || out["status"] != "merged" {
		t.Fatalf("apply segment: %d %v", st, out)
	}
	created := out["created_resources"].(map[string]any)
	if len(created["segments"].([]any)) != 1 || len(created["roles"].([]any)) != 3 {
		t.Fatalf("created = %v", created)
	}
	// в команде появились сегмент и 3 роли
	st, out, _ = do(t, "GET", base+"/teams/"+itoa(target), nil, nil)
	if len(out["segments"].([]any)) != 1 || len(out["roles"].([]any)) != 3 {
		t.Fatalf("target team: %d segs, %d roles", len(out["segments"].([]any)), len(out["roles"].([]any)))
	}
	// повторный apply — идемпотентно (ничего нового)
	st, out, _ = do(t, "POST", base+"/library/"+itoa(segItem)+"/apply", map[string]any{
		"target_team_id": target,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("re-apply segment: %d %v", st, out)
	}
	if cr, ok := out["created_resources"].(map[string]any); ok {
		if arr, _ := cr["roles"].([]any); len(arr) != 0 {
			t.Fatalf("re-apply created roles = %d, want 0", len(arr))
		}
	}

	// 4. apply role без target → 400; segment без target → 400
	st, _, _ = do(t, "POST", base+"/library/"+itoa(roleItem)+"/apply", map[string]any{}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("role apply w/o target: want 400, got %d", st)
	}
	st, _, _ = do(t, "POST", base+"/library/"+itoa(segItem)+"/apply", map[string]any{}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("segment apply w/o target: want 400, got %d", st)
	}

	// 5. apply role в target: overrides.segment = "review" (существует в source? нет → создаётся)
	st, out, _ = do(t, "POST", base+"/library/"+itoa(roleItem)+"/apply", map[string]any{
		"target_team_id": target, "overrides": map[string]any{"segment": "review"},
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("role apply: %d %v", st, out)
	}
	created = out["created_resources"].(map[string]any)
	if len(created["roles"].([]any)) != 1 || len(created["segments"].([]any)) != 1 {
		t.Fatalf("role apply created = %v", created)
	}
	// 6. повторный apply той же роли в тот же сегмент → 409
	st, _, _ = do(t, "POST", base+"/library/"+itoa(roleItem)+"/apply", map[string]any{
		"target_team_id": target, "overrides": map[string]any{"segment": "review"},
	}, nil)
	if st != http.StatusConflict {
		t.Fatalf("duplicate role apply: want 409, got %d", st)
	}

	// 7. apply role c segment_id из снапшота: в команду с двумя сегментами без overrides
	//    → имя из снапшота "backend" существует → туда
	st, out, _ = do(t, "POST", base+"/teams", map[string]any{"name": "multi-seg"}, nil)
	multi := int64(out["id"].(float64))
	do(t, "POST", base+"/teams/"+itoa(multi)+"/segments", map[string]any{"name": "a"}, nil)
	do(t, "POST", base+"/teams/"+itoa(multi)+"/segments", map[string]any{"name": "backend"}, nil)
	st, out, _ = do(t, "POST", base+"/library/"+itoa(roleItem)+"/apply", map[string]any{
		"target_team_id": multi,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("role apply by snapshot segment: %d %v", st, out)
	}
	// а в команду, где сегмента "backend" нет, но их >1 → создаст "backend"
	st, out, _ = do(t, "POST", base+"/teams", map[string]any{"name": "no-backend"}, nil)
	none := int64(out["id"].(float64))
	do(t, "POST", base+"/teams/"+itoa(none)+"/segments", map[string]any{"name": "x"}, nil)
	do(t, "POST", base+"/teams/"+itoa(none)+"/segments", map[string]any{"name": "y"}, nil)
	st, out, _ = do(t, "POST", base+"/library/"+itoa(roleItem)+"/apply", map[string]any{
		"target_team_id": none,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("role apply creates snapshot segment: %d %v", st, out)
	}
	if cr, ok := out["created_resources"].(map[string]any); ok {
		if n := len(cr["segments"].([]any)); n != 1 {
			t.Fatalf("created segments = %v, want 1", cr["segments"])
		}
	}

	// 8. downloads_count выросло (2 apply role + 2 apply segment)
	st, out, _ = do(t, "GET", base+"/library/"+itoa(segItem), nil, nil)
	if st != http.StatusOK {
		t.Fatal(st)
	}
	item := out["item"].(map[string]any)
	if item["downloads_count"].(float64) != 2 {
		t.Fatalf("segment downloads = %v, want 2", item["downloads_count"])
	}
	st, out, _ = do(t, "GET", base+"/library/"+itoa(roleItem), nil, nil)
	item = out["item"].(map[string]any)
	if item["downloads_count"].(float64) != 3 {
		t.Fatalf("role downloads = %v, want 3", item["downloads_count"])
	}

	// 9. archived target → 409
	do(t, "DELETE", base+"/teams/"+itoa(multi), nil, nil)
	st, _, _ = do(t, "POST", base+"/library/"+itoa(roleItem)+"/apply", map[string]any{
		"target_team_id": multi,
	}, nil)
	if st != http.StatusConflict {
		t.Fatalf("apply to archived team: want 409, got %d", st)
	}
}
