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

// ---------- P1.5: compact-plan + handover (HTTP) ----------

type handoverServer struct {
	srv  *httptest.Server
	ssvc *service.SessionService
	svc  *service.TeamService
	tsvc *service.TaskService
	auth *service.AuthService
}

func newHandoverTestServer(t *testing.T) *handoverServer {
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
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	ssvc.LogsDir = t.TempDir() + "/logs"
	ssvc.ConfigsDir = t.TempDir() + "/configs"
	ssvc.ContextWallPctFunc = func(ctx context.Context) (int, error) { return 80, nil }
	ssvc.TaskHandoffFunc = tsvc.HandoffTask
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	bus := service.NewEventBus()
	tsvc.Bus = bus
	ssvc.Bus = bus
	stores2 := repository.NewStores(db)
	alerts := &service.AlertStoreRef{Events: stores2.Watchdog, Teams: stores2.Teams, DB: db}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc, Alerts: alerts,
		Events: bus,
		Auth:   auth,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &handoverServer{srv: srv, ssvc: ssvc, svc: svc, tsvc: tsvc, auth: auth}
}

// hoTeamRole — команда + роль + живая сессия (no-arg command) + transcript с usage.
func (h *handoverServer) hoTeamRoleSession(t *testing.T, usage int) (teamID, roleID, sessID int64) {
	t.Helper()
	team, err := h.svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: "ho-http-team"})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := h.svc.CreateSegment(context.Background(), service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	role, err := h.svc.CreateRole(context.Background(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "lead", AgentSpec: "a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "sleep.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sess, err := h.ssvc.CreateSession(context.Background(), team.ID, service.CreateSessionRequest{RoleID: role.ID, Command: script})
	if err != nil {
		t.Fatal(err)
	}
	line := []byte("{\"usage\":{\"input_tokens\":" + itoa(int64(usage)) + ",\"output_tokens\":100}}\n")
	if err := os.MkdirAll(h.ssvc.LogsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.ssvc.LogsDir, "session-"+itoa(sess.ID)+".log"), line, 0o644); err != nil {
		t.Fatal(err)
	}
	return team.ID, role.ID, sess.ID
}

func TestCompactPlanHTTP(t *testing.T) {
	h := newHandoverTestServer(t)
	_, _, sessID := h.hoTeamRoleSession(t, 190000) // 95%
	admin := map[string]string{"X-API-Key": "env-admin"}

	st, out, _ := do(t, "GET", h.srv.URL+"/api/v1/compact-plan", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("compact-plan: %d %v", st, out)
	}
	if out["wall_pct"].(float64) != 80 {
		t.Fatalf("wall_pct = %v, want 80", out["wall_pct"])
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("total = %v, want 1 (95%% above wall)", out["total"])
	}
	list := out["sessions"].([]any)
	if sid := list[0].(map[string]any)["session_id"].(float64); int64(sid) != sessID {
		t.Fatalf("plan session = %v, want %d", sid, sessID)
	}
}

func TestCompactPlanHTTPRBAC(t *testing.T) {
	h := newHandoverTestServer(t)
	_, _, _ = h.hoTeamRoleSession(t, 190000)
	// viewer: read OK
	_, viewerKey := mustCreateKey(t, h.auth, "ho-viewer", "viewer", "v-user")
	_ = h.auth.Init(context.Background())
	viewer := map[string]string{"X-API-Key": viewerKey}
	st, _, _ := do(t, "GET", h.srv.URL+"/api/v1/compact-plan", nil, viewer)
	if st != http.StatusOK {
		t.Fatalf("viewer compact-plan: want 200, got %d", st)
	}
}

func TestHandoverHTTPDryRunAndFull(t *testing.T) {
	h := newHandoverTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleID, sessID := h.hoTeamRoleSession(t, 190000)
	_ = teamID
	_ = roleID

	// dry-run: план без мутаций
	st, out, _ := do(t, "POST", h.srv.URL+"/api/v1/sessions/"+itoa(sessID)+"/handover",
		map[string]any{"reason": "context wall", "dry_run": true}, admin)
	if st != http.StatusOK {
		t.Fatalf("handover dry-run: %d %v", st, out)
	}
	if out["status"] != "planned" {
		t.Fatalf("dry-run status = %v, want planned", out["status"])
	}
	if out["new_session_id"] != nil {
		t.Fatalf("dry-run created a session: %v", out["new_session_id"])
	}

	// full run: per-step отчёт
	st, out, _ = do(t, "POST", h.srv.URL+"/api/v1/sessions/"+itoa(sessID)+"/handover",
		map[string]any{"reason": "context wall 95%"}, admin)
	if st != http.StatusOK {
		t.Fatalf("handover: %d %v", st, out)
	}
	if out["status"] != "succeeded" {
		t.Fatalf("handover status = %v, want succeeded: %v", out["status"], out)
	}
	if out["new_session_id"] == nil {
		t.Fatal("new_session_id missing")
	}
	steps := out
	if old, _ := steps["old_stopped"].(map[string]any); old["status"] != "ok" {
		t.Fatalf("old_stopped = %v", old)
	}

	// повторный (old=stopped) → 409
	st, _, _ = do(t, "POST", h.srv.URL+"/api/v1/sessions/"+itoa(sessID)+"/handover",
		map[string]any{"reason": "again"}, admin)
	if st != http.StatusConflict {
		t.Fatalf("repeat handover: want 409, got %d", st)
	}
}

func TestHandoverHTTPValidationAndRBAC(t *testing.T) {
	h := newHandoverTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	_, _, sessID := h.hoTeamRoleSession(t, 190000)

	// reason обязателен → 400
	st, _, _ := do(t, "POST", h.srv.URL+"/api/v1/sessions/"+itoa(sessID)+"/handover",
		map[string]any{"dry_run": true}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("missing reason: want 400, got %d", st)
	}
	// viewer: handover 403
	_, viewerKey := mustCreateKey(t, h.auth, "ho-v2", "viewer", "v2")
	_ = h.auth.Init(context.Background())
	viewer := map[string]string{"X-API-Key": viewerKey}
	st, _, _ = do(t, "POST", h.srv.URL+"/api/v1/sessions/"+itoa(sessID)+"/handover",
		map[string]any{"reason": "x"}, viewer)
	if st != http.StatusForbidden {
		t.Fatalf("viewer handover: want 403, got %d", st)
	}
}
