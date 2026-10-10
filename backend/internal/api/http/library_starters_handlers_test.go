package httpapi_test

import (
	"net/http"
	"testing"
)

// P2.13 — Starter bundles: HTTP-вертикаль (seed + apply + pin-409).

func TestLibraryStarterList(t *testing.T) {
	srv := newTestServerSeeded(t, nil, true)

	st, out, _ := do(t, "GET", srv.URL+"/api/v1/library", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list: %d %v", st, out)
	}
	if out["total"].(float64) != 2 {
		t.Fatalf("total = %v, want 2 starters", out["total"])
	}
	byName := map[string]map[string]any{}
	for _, raw := range out["items"].([]any) {
		m := raw.(map[string]any)
		byName[m["name"].(string)] = m
	}
	for _, name := range []string{"dev-team", "review-workflow"} {
		m, ok := byName[name]
		if !ok {
			t.Fatalf("starter %q not in list", name)
		}
		if m["is_starter"] != true {
			t.Fatalf("%s: is_starter = %v, want true", name, m["is_starter"])
		}
	}
	// filter по type=team → только dev-team
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/library?type=team", nil, nil)
	if st != http.StatusOK {
		t.Fatal(st)
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("type=team total = %v, want 1 (dev-team)", out["total"])
	}
}

func TestLibraryStarterApplyTeam(t *testing.T) {
	srv := newTestServerSeeded(t, nil, true)
	base := srv.URL + "/api/v1"

	// id dev-team
	_, listOut, _ := do(t, "GET", base+"/library?type=team", nil, nil)
	devTeam := listOut["items"].([]any)[0].(map[string]any)
	itemID := int64(devTeam["id"].(float64))

	// apply → новая команда
	st, out, _ := do(t, "POST", base+"/library/"+itoa(itemID)+"/apply", map[string]any{
		"overrides": map[string]any{"name": "starter-team"},
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("apply: %d %v", st, out)
	}
	if out["status"] != "applied" {
		t.Fatalf("status = %v", out["status"])
	}
	created := out["created_resources"].(map[string]any)
	newTeam := int64(created["teams"].([]any)[0].(float64))

	// новая команда имеет lead/worker/reviewer
	st, out, _ = do(t, "GET", base+"/teams/"+itoa(newTeam), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get team: %d", st)
	}
	roleNames := map[string]bool{}
	for _, r := range out["roles"].([]any) {
		roleNames[r.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"lead", "worker", "reviewer"} {
		if !roleNames[want] {
			t.Fatalf("applied team missing role %q (has %v)", want, roleNames)
		}
	}
}

func TestLibraryStarterApplyTeamProject(t *testing.T) {
	srv := newTestServerSeeded(t, nil, true)
	base := srv.URL + "/api/v1"

	// проект (kind=new)
	parent := t.TempDir()
	st, projOut, _ := do(t, "POST", base+"/projects", map[string]any{
		"name": "proj-starter", "kind": "new", "parent_path": parent,
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create project: %d %v", st, projOut)
	}
	proj := projOut["project"].(map[string]any)
	projID := int64(proj["id"].(float64))

	_, listOut, _ := do(t, "GET", base+"/library?type=team", nil, nil)
	devTeam := listOut["items"].([]any)[0].(map[string]any)
	itemID := int64(devTeam["id"].(float64))

	st, out, _ := do(t, "POST", base+"/library/"+itoa(itemID)+"/apply", map[string]any{
		"project_id": projID,
		"overrides":  map[string]any{"name": "starter-proj-team"},
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("apply with project: %d %v", st, out)
	}
	created := out["created_resources"].(map[string]any)
	newTeam := int64(created["teams"].([]any)[0].(float64))

	// верифицируем project_id через список команд (teamView.project_id)
	st, teamsOut, _ := do(t, "GET", base+"/teams", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list teams: %d", st)
	}
	var found bool
	for _, raw := range teamsOut["teams"].([]any) {
		tm := raw.(map[string]any)
		if int64(tm["id"].(float64)) != newTeam {
			continue
		}
		found = true
		if tm["project_id"] == nil || int64(tm["project_id"].(float64)) != projID {
			t.Fatalf("team.project_id = %v, want %d", tm["project_id"], projID)
		}
	}
	if !found {
		t.Fatalf("applied team %d not in teams list", newTeam)
	}
}

func TestLibraryStarterPin409(t *testing.T) {
	srv := newTestServerSeeded(t, nil, true)
	base := srv.URL + "/api/v1"

	_, listOut, _ := do(t, "GET", base+"/library?type=team", nil, nil)
	devTeam := listOut["items"].([]any)[0].(map[string]any)
	itemID := int64(devTeam["id"].(float64))

	// rename/обновление стартера → 409
	st, out, _ := do(t, "PATCH", base+"/library/"+itoa(itemID), map[string]any{
		"name": "hacked",
	}, nil)
	if st != http.StatusConflict {
		t.Fatalf("patch starter: status=%d, want 409 (%v)", st, out)
	}
}

func TestLibraryStarterApplyWorkflow(t *testing.T) {
	srv := newTestServerSeeded(t, nil, true)
	base := srv.URL + "/api/v1"

	// целевая команда
	st, teamOut, _ := do(t, "POST", base+"/teams", map[string]any{"name": "wf-starter-target"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("team: %d", st)
	}
	targetID := int64(teamOut["id"].(float64))

	_, listOut, _ := do(t, "GET", base+"/library?type=workflow", nil, nil)
	rw := listOut["items"].([]any)[0].(map[string]any)
	itemID := int64(rw["id"].(float64))

	st, out, _ := do(t, "POST", base+"/library/"+itoa(itemID)+"/apply", map[string]any{
		"target_team_id": targetID,
	}, nil)
	if st != http.StatusOK {
		t.Fatalf("apply workflow: %d %v", st, out)
	}
	if out["status"] != "applied" {
		t.Fatalf("status = %v", out["status"])
	}

	// воркфлоу создан в target (3 блока)
	st, out, _ = do(t, "GET", base+"/workflows?team_id="+itoa(targetID), nil, nil)
	if out["total"].(float64) != 1 {
		t.Fatalf("target workflows = %v, want 1", out["total"])
	}
	wfID := int64(out["workflows"].([]any)[0].(map[string]any)["id"].(float64))
	st, out, _ = do(t, "GET", base+"/workflows/"+itoa(wfID), nil, nil)
	if len(out["blocks"].([]any)) != 3 {
		t.Fatalf("applied workflow blocks = %v, want 3", out["blocks"])
	}
}
