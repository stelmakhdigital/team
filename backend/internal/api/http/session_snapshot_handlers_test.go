package httpapi_test

import (
	"context"
	"database/sql"
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

// ---------- P3.12: agent_image (HTTP: snapshot / launch / pin / prune) ----------

type snapServer struct {
	srv    *httptest.Server
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	ssvc   *service.SessionService
	auth   *service.AuthService
}

func newSnapTestServer(t *testing.T) *snapServer {
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
	_ = os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755)
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		_ = os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644)
	}
	tsvc := service.NewTaskService(db, stores)
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	ssvc.LogsDir = t.TempDir() + "/logs"
	ssvc.ConfigsDir = t.TempDir() + "/configs"
	ssvc.SetLibraryRepo(stores.Library)
	ssvc.SetLibraryVersionRepo(stores.LibraryVersions)
	lsvc := service.NewLibraryService(db, stores, svc)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc,
		Library:  lsvc,
		Auth:     auth,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &snapServer{srv: srv, db: db, stores: stores, svc: svc, ssvc: ssvc, auth: auth}
}

func (s *snapServer) mkTeamRole(t *testing.T, name string) (teamID, roleA, roleB int64) {
	t.Helper()
	ctx := context.Background()
	team, err := s.svc.CreateTeam(ctx, service.CreateTeamRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := s.svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_b", AgentSpec: "agents/role_b.yaml", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	return team.ID, a.ID, b.ID
}

func (s *snapServer) mkSession(t *testing.T, teamID, roleID int64) int64 {
	t.Helper()
	sess, err := s.ssvc.CreateSession(context.Background(), teamID, service.CreateSessionRequest{
		RoleID: roleID, RuntimeType: "process",
		Command: "sh", Args: []string{"-c", "echo snap; sleep 1"},
		Config: map[string]any{"model": "claude-x", "system_prompt": "sp"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess.ID
}

func TestSnapshotHTTP(t *testing.T) {
	s := newSnapTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, _ := s.mkTeamRole(t, "snap-http")
	sessID := s.mkSession(t, teamID, roleA)

	// snapshot → 201
	st, out, _ := do(t, "POST", s.srv.URL+"/api/v1/sessions/"+itoaID(sessID)+"/snapshot",
		map[string]any{"name": "http-snap"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("snapshot: want 201, got %d %v", st, out)
	}
	if out["type"] != "agent_image" {
		t.Errorf("type = %v, want agent_image", out["type"])
	}

	// 404 сессия
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/sessions/999999/snapshot", map[string]any{"name": "x"}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("snapshot 404: want 404, got %d", st)
	}

	// 409 дубль имени
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/sessions/"+itoaID(sessID)+"/snapshot",
		map[string]any{"name": "http-snap"}, admin)
	if st != http.StatusConflict {
		t.Fatalf("snapshot 409: want 409, got %d", st)
	}
}

func TestSnapshotHTTPRBACViewer(t *testing.T) {
	s := newSnapTestServer(t)
	_, viewerKey := mustCreateKey(t, s.auth, "snap-viewer", "viewer", "v")
	teamID, roleA, _ := s.mkTeamRole(t, "snap-http-rbac")
	sessID := s.mkSession(t, teamID, roleA)
	// viewer: snapshot = sessions.update (operator+) → 403
	st, _, _ := do(t, "POST", s.srv.URL+"/api/v1/sessions/"+itoaID(sessID)+"/snapshot",
		map[string]any{"name": "x"}, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer snapshot: want 403, got %d", st)
	}
}

func TestLaunchHTTP(t *testing.T) {
	s := newSnapTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, _ := s.mkTeamRole(t, "snap-launch")
	sessID := s.mkSession(t, teamID, roleA)
	// снапшот
	_, out, _ := do(t, "POST", s.srv.URL+"/api/v1/sessions/"+itoaID(sessID)+"/snapshot",
		map[string]any{"name": "launch-http", "summary": "ctx"}, admin)
	libID := int64(out["id"].(float64))
	// стоп сессии (role_a занята)
	_, _ = s.ssvc.StopSession(context.Background(), sessID)

	// launch → 201
	st, lout, _ := do(t, "POST", s.srv.URL+"/api/v1/library/"+itoaID(libID)+"/launch",
		map[string]any{"team_id": teamID}, admin)
	if st != http.StatusCreated {
		t.Fatalf("launch: want 201, got %d %v", st, lout)
	}
	if lout["status"] != "started" {
		t.Errorf("status = %v, want started", lout["status"])
	}

	// 400 не-agent_image: сохраним team-снапшот (не agent_image)
	ls := service.NewLibraryService(s.db, s.stores, s.svc)
	teamSnap, err := ls.Save(context.Background(), service.SaveToLibraryRequest{Type: models.LibTeam, SourceID: teamID, Name: "team-http"})
	if err != nil {
		t.Fatalf("save team: %v", err)
	}
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/library/"+itoaID(teamSnap.ID)+"/launch",
		map[string]any{"team_id": teamID}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("launch non-agent_image: want 400, got %d", st)
	}
}

func TestPinPruneHTTP(t *testing.T) {
	s := newSnapTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, _ := s.mkTeamRole(t, "snap-pin")
	sessID := s.mkSession(t, teamID, roleA)
	_, out, _ := do(t, "POST", s.srv.URL+"/api/v1/sessions/"+itoaID(sessID)+"/snapshot",
		map[string]any{"name": "pin-http"}, admin)
	libID := int64(out["id"].(float64))

	// pin → 200
	st, _, _ := do(t, "POST", s.srv.URL+"/api/v1/library/"+itoaID(libID)+"/pin", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("pin: want 200, got %d", st)
	}
	// prune pinned → 409
	st, _, _ = do(t, "DELETE", s.srv.URL+"/api/v1/library/"+itoaID(libID), nil, admin)
	if st != http.StatusConflict {
		t.Fatalf("prune pinned: want 409, got %d", st)
	}
	// unpin → 200
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/library/"+itoaID(libID)+"/unpin", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("unpin: want 200, got %d", st)
	}
	// prune → 200
	st, _, _ = do(t, "DELETE", s.srv.URL+"/api/v1/library/"+itoaID(libID), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("prune: want 200, got %d", st)
	}
}
