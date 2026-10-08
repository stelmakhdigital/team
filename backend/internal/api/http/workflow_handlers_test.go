package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Slice 5 — Workflow Editor: вертикаль по HTTP.
func TestWorkflowVertical(t *testing.T) {
	srv := newTestServer(t, nil)

	// команда
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))

	// 1. POST /workflows с блоками и connections
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows", map[string]any{
		"team_id":     teamID,
		"name":        "deploy-flow",
		"description": "build → review → deploy",
		"blocks": []any{
			map[string]any{"type": "task", "position": map[string]any{"x": 0, "y": 0}, "label": "Build"},
			map[string]any{"type": "decision", "position": map[string]any{"x": 100, "y": 0}},
			map[string]any{"type": "agent", "position": map[string]any{"x": 200, "y": 50}, "config": map[string]any{"role": "worker"}},
		},
		"connections": []any{
			map[string]any{"from_block_id": 0, "to_block_id": 1},
			map[string]any{"from_block_id": 1, "to_block_id": 2, "condition": "yes"},
		},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create workflow: %d %v", st, out)
	}
	if out["status"] != "created" {
		t.Fatalf("status = %v", out["status"])
	}
	wfID := int64(out["id"].(float64))

	// 2. GET /workflows?team_id= — 1 шт., total=1
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows?team_id="+itoa(teamID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list workflows: %d %v", st, out)
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("total = %v, want 1", out["total"])
	}
	wfList := out["workflows"].([]any)
	wfView := wfList[0].(map[string]any)
	if wfView["name"] != "deploy-flow" || wfView["state"] != "draft" {
		t.Fatalf("workflow view = %v", wfView)
	}

	// 3. GET /workflows/{id} — blocks + connections
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows/"+itoa(wfID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get workflow: %d %v", st, out)
	}
	blocks := out["blocks"].([]any)
	conns := out["connections"].([]any)
	if len(blocks) != 3 || len(conns) != 2 {
		t.Fatalf("blocks=%d conns=%d, want 3/2", len(blocks), len(conns))
	}
	b0 := blocks[0].(map[string]any)
	pos := b0["position"].(map[string]any)
	if pos["x"].(float64) != 0 || b0["label"] != "Build" {
		t.Fatalf("block[0] = %v", b0)
	}
	c1 := conns[1].(map[string]any)
	if c1["condition"] != "yes" {
		t.Fatalf("connection condition = %v", c1["condition"])
	}
	// from/to — реальные id блоков
	if itoaInt64(c1["from_block_id"]) != itoaInt64(blocks[1].(map[string]any)["id"]) {
		t.Fatalf("connection from_block_id = %v", c1["from_block_id"])
	}

	// 4. POST block
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/blocks", map[string]any{
		"type": "manual", "position": map[string]any{"x": 5, "y": 6},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create block: %d %v", st, out)
	}
	blockID := int64(out["id"].(float64))

	// 5. POST connection
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/connections", map[string]any{
		"from_block_id": itoaInt64(blocks[2].(map[string]any)["id"]), "to_block_id": blockID,
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create connection: %d %v", st, out)
	}

	// 6. PATCH block — drag&drop: changes.position {old,new}
	st, out, _ = do(t, "PATCH", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/blocks/"+itoa(blockID), map[string]any{
		"position": map[string]any{"x": 10, "y": 20},
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("update block: %d %v", st, out)
	}
	if out["status"] != "updated" {
		t.Fatalf("status = %v", out["status"])
	}
	changes := out["changes"].(map[string]any)
	pch := changes["position"].(map[string]any)
	oldP := pch["old"].(map[string]any)
	newP := pch["new"].(map[string]any)
	if oldP["x"].(float64) != 5 || newP["x"].(float64) != 10 {
		t.Fatalf("position changes = %v", pch)
	}

	// 7. валидация: 400 invalid type, 404 unknown workflow, 400 cross-workflow connection
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/blocks", map[string]any{
		"type": "nope",
	}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("invalid block type: status=%d, want 400", st)
	}
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/workflows/999", nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("unknown workflow: status=%d, want 404", st)
	}
	// workflow в другой команде
	st, out2, _ := do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "other-team"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("team2: %d %v", st, out2)
	}
	team2ID := int64(out2["id"].(float64))
	st, out2, _ = do(t, "POST", srv.URL+"/api/v1/workflows", map[string]any{
		"team_id": team2ID, "name": "other",
		"blocks": []any{map[string]any{"type": "task"}},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("workflow2: %d", st)
	}
	otherWf := out2
	_, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows?team_id="+itoa(team2ID), nil, nil)
	otherBlocks := out["workflows"].([]any)
	_ = otherBlocks
	_ = otherWf
	// connection из блока team2 в наш workflow → 400
	// (нужен id блока team2: GET workflow2)
	otherWfID := int64(out2["id"].(float64))
	_, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows/"+itoa(otherWfID), nil, nil)
	otherBlockID := itoaInt64(out["blocks"].([]any)[0].(map[string]any)["id"])
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/workflows/"+itoa(wfID)+"/connections", map[string]any{
		"from_block_id": itoaInt64(blocks[0].(map[string]any)["id"]), "to_block_id": otherBlockID,
	}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("cross-workflow connection: status=%d, want 400", st)
	}
}

// TestWorkflowDeleteHTTP — vertical delete по HTTP (контракт 20 §2.6–2.8, R6.4).
func TestWorkflowDeleteHTTP(t *testing.T) {
	srv := newTestServer(t, nil)

	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))

	st, out, _ = do(t, "POST", srv.URL+"/api/v1/workflows", map[string]any{
		"team_id": teamID,
		"name":    "del-flow",
		"blocks": []any{
			map[string]any{"type": "task"},
			map[string]any{"type": "decision"},
			map[string]any{"type": "agent"},
		},
		"connections": []any{
			map[string]any{"from_block_id": 0, "to_block_id": 1},
			map[string]any{"from_block_id": 1, "to_block_id": 2},
		},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create workflow: %d %v", st, out)
	}
	wfID := int64(out["id"].(float64))

	st, out, _ = do(t, "GET", srv.URL+"/api/v1/workflows/"+itoa(wfID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get workflow: %d", st)
	}
	blocks := out["blocks"].([]any)
	conns := out["connections"].([]any)
	if len(blocks) != 3 || len(conns) != 2 {
		t.Fatalf("initial: blocks=%d conns=%d, want 3/2", len(blocks), len(conns))
	}

	// 1. DELETE block (средний, id=blocks[1]) → 200, status=deleted, removed_connections=2
	midBlockID := itoa(itoaInt64(blocks[1].(map[string]any)["id"]))
	st, out, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/blocks/%s", wfID, midBlockID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("delete block: %d %v", st, out)
	}
	if out["status"] != "deleted" {
		t.Fatalf("status = %v, want deleted", out["status"])
	}
	if rc := out["removed_connections"].(float64); rc != 2 {
		t.Fatalf("removed_connections = %v, want 2", out["removed_connections"])
	}
	// блок уже удалён → 404
	st, _, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/blocks/%s", wfID, midBlockID), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete already-deleted block: %d, want 404", st)
	}
	// неизвестный блок → 404
	st, _, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/blocks/99999", wfID), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete unknown block: %d, want 404", st)
	}

	// 2. DELETE connection — после удаления среднего блока каскадом снесены обе
	// связи; создаём свежую связь между оставшимися блоками (0 и 2) и удаляем её
	st, out, _ = do(t, "POST", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/connections", wfID), map[string]any{
		"from_block_id": itoaInt64(blocks[0].(map[string]any)["id"]),
		"to_block_id":   itoaInt64(blocks[2].(map[string]any)["id"]),
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create connection: %d %v", st, out)
	}
	newConnID := itoa(itoaInt64(out["id"].(float64)))
	st, out, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/connections/%s", wfID, newConnID), nil, nil)
	if st != http.StatusOK || out["status"] != "deleted" {
		t.Fatalf("delete connection: %d %v", st, out)
	}
	st, _, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/connections/%s", wfID, newConnID), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete already-deleted connection: %d, want 404", st)
	}
	// неизвестная связь → 404
	st, _, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d/connections/99999", wfID), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete unknown connection: %d, want 404", st)
	}

	// 3. DELETE workflow → 200; GET → 404; повторно → 404
	st, out, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d", wfID), nil, nil)
	if st != http.StatusOK || out["status"] != "deleted" {
		t.Fatalf("delete workflow: %d %v", st, out)
	}
	st, _, _ = do(t, "GET", srv.URL+fmt.Sprintf("/api/v1/workflows/%d", wfID), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("get deleted workflow: %d, want 404", st)
	}
	st, _, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/%d", wfID), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete already-deleted workflow: %d, want 404", st)
	}
	// неизвестный workflow → 404
	st, _, _ = do(t, "DELETE", srv.URL+fmt.Sprintf("/api/v1/workflows/99999"), nil, nil)
	if st != http.StatusNotFound {
		t.Fatalf("delete unknown workflow: %d, want 404", st)
	}
}

// TestWorkflowDeleteRBAC — viewer не может удалять (workflows.update), оператор может (контракт 20 §2.6–2.8).
func TestWorkflowDeleteRBAC(t *testing.T) {
	srv, auth := newRBACTestServer(t, []string{"env-admin"})
	_, viewKey := mustCreateKey(t, auth, "viewer-cli", "viewer", "viewer-user")
	_, opKey := mustCreateKey(t, auth, "operator-cli", "operator", "operator-user")
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	base := srv.URL + "/api/v1"
	ohdr := map[string]string{"X-API-Key": opKey}
	vhdr := map[string]string{"X-API-Key": viewKey}

	// оператор создаёт команду + workflow с блоком
	st, out, _ := do(t, "POST", base+"/teams", map[string]any{"name": "rbac-del-team"}, ohdr)
	if st != http.StatusCreated {
		t.Fatalf("operator create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	st, out, _ = do(t, "POST", base+"/workflows", map[string]any{
		"team_id": teamID, "name": "rbac-flow",
		"blocks": []any{map[string]any{"type": "task"}},
	}, ohdr)
	if st != http.StatusCreated {
		t.Fatalf("operator create workflow: %d %v", st, out)
	}
	wfID := int64(out["id"].(float64))
	st, out, _ = do(t, "GET", base+fmt.Sprintf("/workflows/%d", wfID), nil, ohdr)
	blockID := itoa(itoaInt64(out["blocks"].([]any)[0].(map[string]any)["id"]))

	// viewer: DELETE block → 403 forbidden
	st, out, _ = do(t, "DELETE", base+fmt.Sprintf("/workflows/%d/blocks/%s", wfID, blockID), nil, vhdr)
	if st != http.StatusForbidden {
		t.Fatalf("viewer delete block: want 403, got %d %v", st, out)
	}
	// viewer: DELETE workflow → 403
	st, _, _ = do(t, "DELETE", base+fmt.Sprintf("/workflows/%d", wfID), nil, vhdr)
	if st != http.StatusForbidden {
		t.Fatalf("viewer delete workflow: want 403, got %d", st)
	}
	// оператор: DELETE workflow → 200
	st, out, _ = do(t, "DELETE", base+fmt.Sprintf("/workflows/%d", wfID), nil, ohdr)
	if st != http.StatusOK || out["status"] != "deleted" {
		t.Fatalf("operator delete workflow: %d %v", st, out)
	}
}
