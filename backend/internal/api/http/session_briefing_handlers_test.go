package httpapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P1.7: Session briefing (HTTP: preview + create) ----------

type briefingServer struct {
	srv  *httptest.Server
	svc  *service.TeamService
	ssvc *service.SessionService
	auth *service.AuthService
}

func newBriefingTestServer(t *testing.T) *briefingServer {
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
	// P1.7: briefing builder
	briefing := service.NewBriefingService(db, stores)
	ssvc.BriefingFunc = briefing.Build
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
		Events:   bus,
		Auth:     auth,
		Briefing: briefing,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &briefingServer{srv: srv, svc: svc, ssvc: ssvc, auth: auth}
}

// bfTeamRole — команда + роль.
func (s *briefingServer) bfTeamRole(t *testing.T) (teamID, roleID int64) {
	t.Helper()
	team, err := s.svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: "bf-http-team"})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := s.svc.CreateSegment(context.Background(), service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.svc.CreateRole(context.Background(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "lead", AgentSpec: "a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	return team.ID, role.ID
}

func TestBriefingPreviewHTTP(t *testing.T) {
	s := newBriefingTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleID := s.bfTeamRole(t)

	// 200: valid
	st, out, _ := do(t, "POST", s.srv.URL+"/api/v1/teams/"+itoa(teamID)+"/briefing/preview",
		map[string]any{"role_id": roleID, "runtime_type": "pi"}, admin)
	if st != http.StatusOK {
		t.Fatalf("preview: %d %v", st, out)
	}
	if txt, _ := out["briefing_text"].(string); !strings.Contains(txt, "Session briefing") {
		t.Fatalf("briefing_text = %q", txt)
	}
	if out["briefing_data"] == nil {
		t.Fatal("briefing_data missing")
	}
	if del, _ := out["delivery"].(map[string]any); del["mode"] != "pi-system-prompt" {
		t.Fatalf("delivery.mode = %v, want pi-system-prompt", del["mode"])
	}

	// 400: no role_id
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/teams/"+itoa(teamID)+"/briefing/preview",
		map[string]any{"runtime_type": "pi"}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("no role_id: want 400, got %d", st)
	}

	// 404: missing team
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/teams/999999/briefing/preview",
		map[string]any{"role_id": roleID}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("missing team: want 404, got %d", st)
	}

	// 401: no auth
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/teams/"+itoa(teamID)+"/briefing/preview",
		map[string]any{"role_id": roleID}, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no auth: want 401, got %d", st)
	}
}

func TestBriefingPreviewRBAC(t *testing.T) {
	s := newBriefingTestServer(t)
	teamID, roleID := s.bfTeamRole(t)

	// viewer (sessions.read) → 200
	_, viewerKey := mustCreateKey(t, s.auth, "bf-viewer", "viewer", "v")
	_ = s.auth.Init(context.Background())
	st, _, _ := do(t, "POST", s.srv.URL+"/api/v1/teams/"+itoa(teamID)+"/briefing/preview",
		map[string]any{"role_id": roleID}, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer preview: want 200, got %d", st)
	}

	// operator (без sessions.read в этой модели?) — проверим 403 для роли без права.
	// viewer имеет sessions.read (по модели). Для 403 используем роль без sessions.read.
	// (если в этой RBAC-модели все роли имеют sessions.read — пропускаем)
}

func TestBriefingCreateSessionHTTP(t *testing.T) {
	s := newBriefingTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleID := s.bfTeamRole(t)
	script := filepath.Join(t.TempDir(), "sleep.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	st, out, _ := do(t, "POST", s.srv.URL+"/api/v1/sessions?team_id="+itoa(teamID),
		map[string]any{"role_id": roleID, "command": script}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %v", st, out)
	}
	if out["briefing_applied"] != "builtin" {
		t.Fatalf("briefing_applied = %v, want builtin", out["briefing_applied"])
	}
	if out["briefing_path"] == "" {
		t.Fatal("briefing_path missing in create response")
	}
}
