package httpapi_test

import (
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
