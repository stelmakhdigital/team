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
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P3.16: posture interceptor + replay (HTTP) ----------

type postureTestSrv struct {
	srv     *httptest.Server
	auth    *service.AuthService
	posture *service.PostureService
	ctx     context.Context
	base    string
}

// newPostureTestServer — сервер с PostureService (interceptor) + auth.
func newPostureTestServer(t *testing.T) *postureTestSrv {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.yaml", "b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	ssvc.LogsDir = filepath.Join(t.TempDir(), "logs")
	ssvc.ConfigsDir = filepath.Join(t.TempDir(), "configs")
	asvc := service.NewAuditService(db, stores)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(ctx); err != nil {
		t.Fatal(err)
	}
	cvc, err := service.NewConfigService(db)
	if err != nil {
		t.Fatal(err)
	}
	posture := service.NewPostureService(db, stores, cvc, asvc)
	attention := service.NewAttentionService(db, stores)
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:        db,
		Audit:     asvc,
		Auth:      auth,
		Config:    cvc,
		Sessions:  ssvc,
		Workflows: service.NewWorkflowService(db, stores),
		Library:   service.NewLibraryService(db, stores, svc),
		Attention: attention,
		Posture:   posture,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &postureTestSrv{srv: srv, auth: auth, posture: posture, ctx: ctx, base: srv.URL + "/api/v1"}
}

// createPostureTeam — команда (пустая) через API.
func (e *postureTestSrv) createTeam(t *testing.T, opKey, name string) int64 {
	t.Helper()
	opHdr := map[string]string{"X-API-Key": opKey}
	st, out, _ := do(t, "POST", e.base+"/teams", map[string]any{"name": name}, opHdr)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	return int64(out["id"].(float64))
}

// teamState — state команды через GET (active/archived).
func (e *postureTestSrv) teamState(t *testing.T, opKey string, teamID int64) string {
	t.Helper()
	opHdr := map[string]string{"X-API-Key": opKey}
	st, out, _ := do(t, "GET", e.base+"/teams/"+itoaID(teamID), nil, opHdr)
	if st != http.StatusOK {
		t.Fatalf("get team: %d", st)
	}
	team, _ := out["team"].(map[string]any)
	state, _ := team["state"].(string)
	return state
}

// M8: human-led + sensitive → 202 (park); не выполняется; approve → replay 200 + replayed.
func TestPostureInterceptParkAndReplay(t *testing.T) {
	e := newPostureTestServer(t)
	teamID := e.createTeam(t, "env-admin", "park-team")
	// team → human_led
	if err := e.posture.SetTeamPosture(e.ctx, teamID, models.PostureHumanLed); err != nil {
		t.Fatalf("set posture: %v", err)
	}
	_, opKey := mustCreateKey(t, e.auth, "fe-op", "operator", "fe-op")
	opHdr := map[string]string{"X-API-Key": opKey}

	// DELETE team (sensitive) → 202 (park), НЕ выполняется
	st, out, _ := do(t, "DELETE", e.base+"/teams/"+itoaID(teamID), nil, opHdr)
	if st != http.StatusAccepted {
		t.Fatalf("park: status = %d %v, want 202", st, out)
	}
	hreqID, _ := out["human_request_id"].(float64)
	if hreqID == 0 {
		t.Fatalf("park: no human_request_id in %v", out)
	}
	// команда НЕ заархивирована (state active)
	if s := e.teamState(t, opKey, teamID); s != "active" {
		t.Fatalf("team after park: state = %s, want active", s)
	}

	// approve → replay 200 + status=replayed + replay_status=200
	st, out, _ = do(t, "POST", e.base+"/human-requests/"+itoaID(int64(hreqID))+"/decide",
		map[string]any{"decision": "approve", "reason": "ok"}, opHdr)
	if st != http.StatusOK {
		t.Fatalf("decide approve: status = %d %v, want 200", st, out)
	}
	if out["status"] != "replayed" {
		t.Fatalf("decide: status = %v, want replayed", out["status"])
	}
	if rs, _ := out["replay_status"].(float64); int(rs) != http.StatusOK {
		t.Fatalf("decide: replay_status = %v, want 200", out["replay_status"])
	}
	// команда ЗААРХИВИРОВАНА (replay прошёл)
	if s := e.teamState(t, opKey, teamID); s != "archived" {
		t.Fatalf("team after replay: state = %s, want archived", s)
	}
}

// M8: delegated + sensitive → 200 (no park), операция выполняется сразу.
func TestPostureDelegatedNoPark(t *testing.T) {
	e := newPostureTestServer(t)
	teamID := e.createTeam(t, "env-admin", "delegated-team")
	_, opKey := mustCreateKey(t, e.auth, "fe-op", "operator", "fe-op")
	opHdr := map[string]string{"X-API-Key": opKey}

	// delegated (default) → DELETE team выполняется сразу (200)
	st, out, _ := do(t, "DELETE", e.base+"/teams/"+itoaID(teamID), nil, opHdr)
	if st != http.StatusOK {
		t.Fatalf("delegated delete: status = %d %v, want 200 (no park)", st, out)
	}
	if _, parked := out["parked"]; parked {
		t.Fatalf("delegated: parked in %v, want no park", out)
	}
	if s := e.teamState(t, opKey, teamID); s != "archived" {
		t.Fatalf("team after delegated delete: state = %s, want archived", s)
	}
}

// M8: deny → no replay (status=denied), команда не удалена.
func TestPostureDenyNoReplay(t *testing.T) {
	e := newPostureTestServer(t)
	teamID := e.createTeam(t, "env-admin", "deny-team")
	_ = e.posture.SetTeamPosture(e.ctx, teamID, models.PostureHumanLed)
	_, opKey := mustCreateKey(t, e.auth, "fe-op", "operator", "fe-op")
	opHdr := map[string]string{"X-API-Key": opKey}

	st, out, _ := do(t, "DELETE", e.base+"/teams/"+itoaID(teamID), nil, opHdr)
	if st != http.StatusAccepted {
		t.Fatalf("park: status = %d, want 202", st)
	}
	hreqID, _ := out["human_request_id"].(float64)
	st, out, _ = do(t, "POST", e.base+"/human-requests/"+itoaID(int64(hreqID))+"/decide",
		map[string]any{"decision": "deny", "reason": "no"}, opHdr)
	if st != http.StatusOK {
		t.Fatalf("decide deny: status = %d %v, want 200", st, out)
	}
	if out["status"] != "denied" {
		t.Fatalf("decide deny: status = %v, want denied", out["status"])
	}
	if _, rs := out["replay_status"]; rs {
		t.Fatalf("denied: replay_status present in %v, want none", out)
	}
	// команда НЕ заархивирована (no replay, state active)
	if s := e.teamState(t, opKey, teamID); s != "active" {
		t.Fatalf("team after deny: state = %s, want active", s)
	}
}

// M8: replay НЕ обходит RBAC — approved-операция от viewer (без права) → replay 403.
func TestPostureReplayEnforcesRBAC(t *testing.T) {
	e := newPostureTestServer(t)
	teamID := e.createTeam(t, "env-admin", "rbac-team")
	_ = e.posture.SetTeamPosture(e.ctx, teamID, models.PostureHumanLed)
	_, viewerKey := mustCreateKey(t, e.auth, "v", "viewer", "viewer")
	_, opKey := mustCreateKey(t, e.auth, "fe-op", "operator", "fe-op")
	opHdr := map[string]string{"X-API-Key": opKey}

	// viewer не имеет teams.delete → human_request с auth_key = viewer (direct, bypass API auth).
	park, hreqID, _, err := e.posture.Check(e.ctx, "DELETE", "/api/v1/teams/"+itoaID(teamID), "", viewerKey)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !park || hreqID == 0 {
		t.Fatalf("check: park=%v hreqID=%d, want park", park, hreqID)
	}
	// approve (operator) → replay с viewer-ключом → RBAC 403 (viewer не имеет teams.delete).
	st, out, _ := do(t, "POST", e.base+"/human-requests/"+itoaID(hreqID)+"/decide",
		map[string]any{"decision": "approve"}, opHdr)
	if st != http.StatusOK {
		t.Fatalf("decide approve: status = %d %v, want 200", st, out)
	}
	if rs, _ := out["replay_status"].(float64); int(rs) != http.StatusForbidden {
		t.Fatalf("replay_status = %v, want 403 (RBAC not bypassed)", out["replay_status"])
	}
	// команда НЕ заархивирована (replay провален RBAC, state active)
	if s := e.teamState(t, opKey, teamID); s != "active" {
		t.Fatalf("team after RBAC-failed replay: state = %s, want active", s)
	}
}
