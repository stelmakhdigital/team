package httpapi_test

// C1 / P4.3 — Team bundle: export/import команды (roundtrip, валидации,
// auto-suffix, RBAC).

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

const bundleAdminKey = "bundle-admin"

// bundleTeamSpec — команда 2 сегмента + 3 роли (с layout и config) + relative.
func bundleTeamSpec() map[string]any {
	return map[string]any{
		"name": "dev-team",
		"spec": map[string]any{
			"segments": []map[string]any{
				{
					"name":   "Backend",
					"layout": map[string]any{"x": 10.0, "y": 20.0, "width": 420.0, "height": 260.0},
				},
				{
					"name":   "Review",
					"layout": map[string]any{"x": 500.0, "y": 20.0, "width": 380.0, "height": 260.0},
				},
			},
			"roles": []map[string]any{
				{
					"segment": "Backend", "name": "Lead", "agent_spec": "a.yaml", "profile": "lead",
					"config": map[string]any{"model": "test-model"},
					"layout": map[string]any{"x": 100.0, "y": 120.0},
				},
				{
					"segment": "Backend", "name": "Worker", "agent_spec": "b.yaml",
					"layout": map[string]any{"x": 200.0, "y": 220.0},
				},
				{
					"segment": "Review", "name": "Reviewer", "agent_spec": "a.yaml",
					"layout": map[string]any{"x": 560.0, "y": 120.0},
				},
			},
			"relatives": []map[string]any{
				{"from": "Backend.Lead", "to": "Backend.Worker", "type": "delegates_to"},
			},
		},
	}
}

func adminHdr() map[string]string { return map[string]string{"X-API-Key": bundleAdminKey} }

// setupBundleTeam — команда + relative-path + workflow (2 блока, связь),
// engine_enabled=true. Возвращает teamID.
func setupBundleTeam(t *testing.T, base string) (teamID int64) {
	t.Helper()
	st, out, _ := do(t, "POST", base+"/teams", bundleTeamSpec(), adminHdr())
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID = int64(out["id"].(float64))

	// relative id — из GET /teams/{id}
	st, out, _ = do(t, "GET", base+"/teams/"+itoaID(teamID), nil, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("get team: %d", st)
	}
	rels := out["relatives"].([]any)
	relID := int64(rels[0].(map[string]any)["id"].(float64))
	// path у relative (для проверки layout-переноса)
	st, out, _ = do(t, "PATCH", base+"/relatives/"+itoaID(relID)+"/layout",
		map[string]any{"path": []map[string]any{{"x": 30.0, "y": 40.0}}}, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("relative layout: %d %v", st, out)
	}

	// workflow: 2 блока + связь, затем engine_enabled=true
	st, out, _ = do(t, "POST", base+"/workflows", map[string]any{
		"team_id": teamID,
		"name":    "Build & Review",
		"blocks": []map[string]any{
			{"type": "task", "position": map[string]any{"x": 80, "y": 120}, "config": map[string]any{"role": "Backend.Lead"}, "label": "Implement"},
			{"type": "decision", "position": map[string]any{"x": 400, "y": 120}, "config": map[string]any{}},
		},
		"connections": []map[string]any{{"from_block_id": 0, "to_block_id": 1}},
	}, adminHdr())
	if st != http.StatusCreated {
		t.Fatalf("create workflow: %d %v", st, out)
	}
	wfID := int64(out["id"].(float64))
	engine := true
	st, out, _ = do(t, "PATCH", base+"/workflows/"+itoaID(wfID),
		map[string]any{"name": "Build & Review", "engine_enabled": engine}, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("toggle engine: %d %v", st, out)
	}
	return teamID
}

// TestTeamBundleRoundtrip — create → export → import → структура 1-в-1,
// layout сохранился, workflow привязан к новой команде, state начальный (draft).
func TestTeamBundleRoundtrip(t *testing.T) {
	srv := newTestServer(t, []string{bundleAdminKey})
	base := srv.URL + "/api/v1"
	teamID := setupBundleTeam(t, base)

	// ---- export
	st, raw, hdr := doRaw(t, "GET", base+"/teams/"+itoaID(teamID)+"/export", nil, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("export: want 200, got %d %v (hdr=%v)", st, raw, hdr)
	}
	var bundle map[string]any
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
		t.Fatalf("export json: %v", err)
	}
	if bundle["version"].(float64) != 1 || bundle["kind"].(string) != "team-bundle" {
		t.Fatalf("bundle header: %v", bundle)
	}
	team := bundle["team"].(map[string]any)
	if team["name"].(string) != "dev-team" {
		t.Fatalf("bundle team.name = %v", team["name"])
	}
	segs := team["segments"].([]any)
	if len(segs) != 2 {
		t.Fatalf("bundle segments: want 2, got %d", len(segs))
	}
	backendRoles := segs[0].(map[string]any)["roles"].([]any)
	if len(backendRoles) != 2 {
		t.Fatalf("bundle Backend roles: want 2, got %d", len(backendRoles))
	}
	lead := backendRoles[0].(map[string]any)
	if lead["name"].(string) != "Lead" || lead["agent_spec"].(string) != "a.yaml" {
		t.Fatalf("bundle Lead: %v", lead)
	}
	if lead["role"].(string) != "lead" {
		t.Fatalf("bundle Lead.role = %v, want lead (profile)", lead["role"])
	}
	if _, hasLayout := lead["config"].(map[string]any)["layout"]; hasLayout {
		t.Fatalf("bundle role config must not contain layout: %v", lead["config"])
	}
	rels := team["relatives"].([]any)
	rel0 := rels[0].(map[string]any)
	if rel0["from"].(string) != "Backend.Lead" || rel0["to"].(string) != "Backend.Worker" || rel0["kind"].(string) != "delegates_to" {
		t.Fatalf("bundle relative: %v", rel0)
	}
	// layout: сегменты с position+size, роли, relative с path
	layout := team["layout"].(map[string]any)
	lvSegs := layout["segments"].([]any)
	if len(lvSegs) != 2 {
		t.Fatalf("layout.segments: want 2, got %d", len(lvSegs))
	}
	ls0 := lvSegs[0].(map[string]any)
	pos := ls0["position"].(map[string]any)
	if pos["x"].(float64) != 10 || pos["width"].(float64) != 420 {
		t.Fatalf("layout segment position: %v", pos)
	}
	lvRels := layout["relatives"].([]any)
	lr0 := lvRels[0].(map[string]any)
	if lr0["from_role_id"].(float64) != 0 || lr0["to_role_id"].(float64) != 1 {
		t.Fatalf("layout relative indices: %v", lr0)
	}
	path := lr0["path"].([]any)
	if len(path) != 1 || path[0].(map[string]any)["x"].(float64) != 30 {
		t.Fatalf("layout relative path: %v", path)
	}
	// workflow в bundle
	wfs := bundle["workflows"].([]any)
	if len(wfs) != 1 {
		t.Fatalf("bundle workflows: want 1, got %d", len(wfs))
	}
	wf0 := wfs[0].(map[string]any)
	if wf0["name"].(string) != "Build & Review" || wf0["engine_enabled"].(bool) != true {
		t.Fatalf("bundle workflow: %v", wf0)
	}
	wfCfg := wf0["config"].(map[string]any)
	if len(wfCfg["blocks"].([]any)) != 2 || len(wfCfg["connections"].([]any)) != 1 {
		t.Fatalf("bundle workflow config: %v", wfCfg)
	}

	// ---- import (имя dev-team занято → auto-suffix "dev-team (2)")
	st, out, _ := do(t, "POST", base+"/teams/import", bundle, adminHdr())
	if st != http.StatusCreated {
		t.Fatalf("import: want 201, got %d %v", st, out)
	}
	newTeam := out["team"].(map[string]any)
	if newTeam["name"].(string) != "dev-team (2)" {
		t.Fatalf("import auto-suffix: %v", newTeam["name"])
	}
	newTeamID := int64(newTeam["id"].(float64))
	if newTeam["segments_count"].(float64) != 2 || newTeam["roles_count"].(float64) != 3 {
		t.Fatalf("import counts: %v", newTeam)
	}
	// 201 — полный TeamView (team + segments + roles + relatives), как GET /teams/{id}
	impSegs := out["segments"].([]any)
	impRoles := out["roles"].([]any)
	impRels := out["relatives"].([]any)
	if len(impSegs) != 2 || len(impRoles) != 3 || len(impRels) != 1 {
		t.Fatalf("import full TeamView: %d segs / %d roles / %d rels", len(impSegs), len(impRoles), len(impRels))
	}
	if impSegs[0].(map[string]any)["team_id"].(float64) != float64(newTeamID) || impRoles[0].(map[string]any)["team_id"].(float64) != float64(newTeamID) {
		t.Fatalf("import TeamView ids: %v", out)
	}
	importedWFs := out["workflows"].([]any)
	if len(importedWFs) != 1 {
		t.Fatalf("import workflows: %v", importedWFs)
	}
	if importedWFs[0].(map[string]any)["name"].(string) != "Build & Review" {
		t.Fatalf("imported workflow name (no collision in new team): %v", importedWFs[0])
	}

	// ---- структура 1-в-1: topology новой команды
	st, out, _ = do(t, "GET", base+"/teams/"+itoaID(newTeamID)+"/topology", nil, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("new topology: %d", st)
	}
	nSegs := out["segments"].([]any)
	nRoles := out["roles"].([]any)
	nRels := out["relatives"].([]any)
	if len(nSegs) != 2 || len(nRoles) != 3 || len(nRels) != 1 {
		t.Fatalf("new team structure: %d segs / %d roles / %d rels", len(nSegs), len(nRoles), len(nRels))
	}
	// config роли Lead перенесён (model), layout роли сохранён
	var leadNew map[string]any
	for _, r := range nRoles {
		rm := r.(map[string]any)
		if rm["name"].(string) == "Lead" {
			leadNew = rm
		}
	}
	if leadNew == nil || leadNew["agent_spec"].(string) != "a.yaml" {
		t.Fatalf("new Lead role: %v", leadNew)
	}
	// relative адреса сохранены
	nnr := nRels[0].(map[string]any)
	if nnr["from_role_name"].(string) != "Lead" || nnr["to_role_name"].(string) != "Worker" {
		t.Fatalf("new relative: %v", nnr)
	}
	// layout сохранён (позиции сегмента/роли + path relative)
	nLayout := out["layout"].(map[string]any)
	nLvSegs := nLayout["segments"].([]any)
	nPos := nLvSegs[0].(map[string]any)["position"].(map[string]any)
	if nPos["x"].(float64) != 10 || nPos["width"].(float64) != 420 {
		t.Fatalf("new layout segment: %v", nPos)
	}
	nLvRels := nLayout["relatives"].([]any)
	nPath := nLvRels[0].(map[string]any)["path"].([]any)
	if len(nPath) != 1 || nPath[0].(map[string]any)["x"].(float64) != 30 {
		t.Fatalf("new layout relative path: %v", nPath)
	}

	// ---- workflow привязан к новой команде, state начальный
	st, out, _ = do(t, "GET", base+"/workflows?team_id="+itoaID(newTeamID), nil, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("list workflows: %d", st)
	}
	listWFs := out["workflows"].([]any)
	if len(listWFs) != 1 {
		t.Fatalf("new team workflows: %d", len(listWFs))
	}
	nwf := listWFs[0].(map[string]any)
	if nwf["state"].(string) != "draft" {
		t.Fatalf("imported workflow state: %v (want draft — не активируем при импорте)", nwf["state"])
	}
	if nwf["engine_enabled"] != true {
		t.Fatalf("imported workflow engine_enabled: %v (want true)", nwf["engine_enabled"])
	}
	nwfID := int64(nwf["id"].(float64))
	st, out, _ = do(t, "GET", base+"/workflows/"+itoaID(nwfID), nil, adminHdr())
	if st != http.StatusOK {
		t.Fatalf("get workflow: %d", st)
	}
	if len(out["blocks"].([]any)) != 2 || len(out["connections"].([]any)) != 1 {
		t.Fatalf("imported workflow blocks/connections: %v", out)
	}
}

// TestTeamBundleImportUnsupportedVersion — version=2 → 400 unsupported_version.
func TestTeamBundleImportUnsupportedVersion(t *testing.T) {
	srv := newTestServer(t, []string{bundleAdminKey})
	base := srv.URL + "/api/v1"
	bundle := validMinimalBundle()
	bundle["version"] = 2
	st, out, _ := do(t, "POST", base+"/teams/import", bundle, adminHdr())
	if st != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %v", st, out)
	}
	if out["error"].(map[string]any)["code"].(string) != "unsupported_version" {
		t.Fatalf("code: %v", out["error"])
	}
}

// TestTeamBundleImportWrongKind — kind != team-bundle → 400.
func TestTeamBundleImportWrongKind(t *testing.T) {
	srv := newTestServer(t, []string{bundleAdminKey})
	base := srv.URL + "/api/v1"
	bundle := validMinimalBundle()
	bundle["kind"] = "task-bundle"
	st, out, _ := do(t, "POST", base+"/teams/import", bundle, adminHdr())
	if st != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %v", st, out)
	}
}

// validMinimalBundle — минимальный валидный bundle (1 сегмент, 1 роль).
func validMinimalBundle() map[string]any {
	return map[string]any{
		"version": 1,
		"kind":    "team-bundle",
		"team": map[string]any{
			"name": "min-team",
			"segments": []map[string]any{
				{"name": "S", "roles": []map[string]any{{"name": "R", "agent_spec": "a.yaml"}}},
			},
		},
		"workflows": []map[string]any{},
	}
}

// bundleFieldError — есть ли field-ошибка с нужным полем в details.errors.
func bundleFieldError(t *testing.T, out map[string]any, field string) bool {
	t.Helper()
	details, _ := out["error"].(map[string]any)["details"].(map[string]any)
	errs, _ := details["errors"].([]any)
	for _, e := range errs {
		if em, ok := e.(map[string]any); ok && em["field"].(string) == field {
			return true
		}
	}
	t.Fatalf("field %q not found in errors: %v", field, errs)
	return false
}

// TestTeamBundleImportValidation — пустое имя / нет сегментов / relative на
// несуществующую роль → 400 с field-ошибками по каждому.
func TestTeamBundleImportValidation(t *testing.T) {
	srv := newTestServer(t, []string{bundleAdminKey})
	base := srv.URL + "/api/v1"

	// пустое имя
	b := validMinimalBundle()
	b["team"].(map[string]any)["name"] = "   "
	st, out, _ := do(t, "POST", base+"/teams/import", b, adminHdr())
	if st != http.StatusBadRequest || !bundleFieldError(t, out, "team.name") {
		t.Fatalf("empty name: %d %v", st, out)
	}

	// нет сегментов
	b = validMinimalBundle()
	b["team"].(map[string]any)["segments"] = []map[string]any{}
	st, out, _ = do(t, "POST", base+"/teams/import", b, adminHdr())
	if st != http.StatusBadRequest || !bundleFieldError(t, out, "team.segments") {
		t.Fatalf("no segments: %d %v", st, out)
	}

	// relative на несуществующую роль (+ invalid kind)
	b = validMinimalBundle()
	b["team"].(map[string]any)["relatives"] = []map[string]any{
		{"from": "Nope.Ghost", "to": "S.R", "kind": "teleport"},
	}
	st, out, _ = do(t, "POST", base+"/teams/import", b, adminHdr())
	if st != http.StatusBadRequest {
		t.Fatalf("bad relative: want 400, got %d %v", st, out)
	}
	bundleFieldError(t, out, "team.relatives[0].from")
	bundleFieldError(t, out, "team.relatives[0].kind")
}

// TestTeamBundleImportNameCollision — повторный импорт: suffix (2), (3)...
func TestTeamBundleImportNameCollision(t *testing.T) {
	srv := newTestServer(t, []string{bundleAdminKey})
	base := srv.URL + "/api/v1"
	bundle := validMinimalBundle()
	for i, want := range []string{"min-team", "min-team (2)", "min-team (3)"} {
		st, out, _ := do(t, "POST", base+"/teams/import", bundle, adminHdr())
		if st != http.StatusCreated {
			t.Fatalf("import #%d: want 201, got %d %v", i+1, st, out)
		}
		if got := out["team"].(map[string]any)["name"].(string); got != want {
			t.Fatalf("import #%d name = %q, want %q", i+1, got, want)
		}
	}
}

// TestTeamBundleRBAC — viewer: export 200 / import 403.
func TestTeamBundleRBAC(t *testing.T) {
	srv, auth := newRBACTestServer(t, []string{bundleAdminKey})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	base := srv.URL + "/api/v1"
	teamID := setupBundleTeam(t, base)
	_, viewKey := mustCreateKey(t, auth, "bundle-viewer", "viewer", "bundle-viewer-user")

	// viewer: export — 200 (teams.read)
	st, raw, _ := doRaw(t, "GET", base+"/teams/"+itoaID(teamID)+"/export", nil,
		map[string]string{"X-API-Key": viewKey})
	if st != http.StatusOK {
		t.Fatalf("viewer export: want 200, got %d %v", st, raw)
	}
	// viewer: import — 403 (teams.update)
	bundle := validMinimalBundle()
	st, out, _ := do(t, "POST", base+"/teams/import", bundle,
		map[string]string{"X-API-Key": viewKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer import: want 403, got %d %v", st, out)
	}
}

// TestTeamBundleExport404 — неизвестная команда → 404.
func TestTeamBundleExport404(t *testing.T) {
	srv := newTestServer(t, []string{bundleAdminKey})
	base := srv.URL + "/api/v1"
	st, _, _ := doRaw(t, "GET", base+"/teams/999999/export", nil, adminHdr())
	if st != http.StatusNotFound {
		t.Fatalf("export unknown team: want 404, got %d", st)
	}
}
