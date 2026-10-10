package httpapi_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P2.15: whoami (HTTP: /whoami, RBAC read viewer-ok) ----------

type whoamiServer struct {
	srv    *httptest.Server
	auth   *service.AuthService
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	ssvc   *service.SessionService
}

func newWhoamiTestServer(t *testing.T) *whoamiServer {
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
	if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	rtRegistry := runtime.NewRegistry()
	ssvc := service.NewSessionService(db, stores, rtRegistry)
	ssvc.LogsDir = t.TempDir() + "/logs"
	msvc := service.NewMessageService(db, stores)
	whoami := service.NewWhoamiService(db, stores, svc, tsvc, ssvc, msvc)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc,
		Auth:     auth,
		Whoami:   whoami,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &whoamiServer{srv: srv, auth: auth, db: db, stores: stores, svc: svc, ssvc: ssvc}
}

// mkWhoamiTeamRoleSession — команда + роль + сессия (для by-session).
func (s *whoamiServer) mkTeamRoleSession(t *testing.T, name string) (teamID, roleID, sessionID int64) {
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
	role, err := s.svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	sess := &models.Session{TeamID: team.ID, RoleID: role.ID, RuntimeType: "tmux", RuntimeRef: "whoami-http", State: models.SessionRunning}
	if err := s.stores.Sessions.Create(ctx, s.db, sess); err != nil {
		t.Fatal(err)
	}
	return team.ID, role.ID, sess.ID
}

func TestWhoamiHTTP(t *testing.T) {
	s := newWhoamiTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleID, sessionID := s.mkTeamRoleSession(t, "whoami-http-team")

	// по session_id → 200 + agent/session
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/whoami?session_id="+strconv.FormatInt(sessionID, 10), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("by session: want 200, got %d %v", st, out)
	}
	agent, _ := out["agent"].(map[string]any)
	if agent == nil || int64(agent["role_id"].(float64)) != roleID {
		t.Errorf("agent = %v", agent)
	}
	if sess, _ := out["session"].(map[string]any); sess == nil || int64(sess["id"].(float64)) != sessionID {
		t.Errorf("session = %v", out["session"])
	}
	if mt, _ := out["my_tasks"].(map[string]any); mt == nil {
		t.Errorf("my_tasks missing")
	}

	// по role_id → 200, session отсутствует
	st, out, _ = do(t, "GET", s.srv.URL+"/api/v1/whoami?role_id="+strconv.FormatInt(roleID, 10), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("by role: want 200, got %d %v", st, out)
	}
	if out["session"] != nil {
		t.Errorf("session should be absent when by role, got %v", out["session"])
	}

	// без параметров → 400
	st, out, _ = do(t, "GET", s.srv.URL+"/api/v1/whoami", nil, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("no params: want 400, got %d %v", st, out)
	}

	// нет сессии → 404
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/whoami?session_id=999999", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("no session: want 404, got %d", st)
	}
	_ = teamID
}

func TestWhoamiRBACViewer(t *testing.T) {
	s := newWhoamiTestServer(t)
	_, roleID, _ := s.mkTeamRoleSession(t, "whoami-rbac")
	// viewer: 200 (dashboard.read — read, viewer-ok)
	_, viewerKey := mustCreateKey(t, s.auth, "whoami-viewer", "viewer", "w")
	st, _, _ := do(t, "GET", s.srv.URL+"/api/v1/whoami?role_id="+strconv.FormatInt(roleID, 10),
		nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer whoami: want 200, got %d", st)
	}
}
