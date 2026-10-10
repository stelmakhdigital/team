package httpapi_test

import (
	"context"
	"database/sql"
	"fmt"
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

// ---------- P2.7: поиск по транскриптам (HTTP: /sessions/search, RBAC) ----------

type searchServer struct {
	srv    *httptest.Server
	auth   *service.AuthService
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	ssvc   *service.SessionService
}

func newSearchTestServer(t *testing.T) *searchServer {
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
	rtRegistry := runtime.NewRegistry()
	ssvc := service.NewSessionService(db, stores, rtRegistry)
	ssvc.LogsDir = t.TempDir() + "/logs"
	ssvc.ConfigsDir = t.TempDir() + "/configs"
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, service.NewTaskService(db, stores), httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc,
		Auth:     auth,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &searchServer{srv: srv, auth: auth, db: db, stores: stores, svc: svc, ssvc: ssvc}
}

// mkSearchTeamRoles — команда + 2 роли (как makeTeamWithRoles).
func (s *searchServer) mkTeamRoles(t *testing.T, name string) (teamID, roleA, roleB int64) {
	t.Helper()
	team, err := s.svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := s.svc.CreateSegment(context.Background(), service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.svc.CreateRole(context.Background(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.svc.CreateRole(context.Background(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_b", AgentSpec: "agents/role_b.yaml", Layout: &service.Layout{X: 1, Y: 0}})
	if err != nil {
		t.Fatal(err)
	}
	return team.ID, a.ID, b.ID
}

func (s *searchServer) mkSession(t *testing.T, teamID, roleID int64, state models.SessionState) int64 {
	t.Helper()
	sess := &models.Session{TeamID: teamID, RoleID: roleID, RuntimeType: "process", State: state}
	if err := s.stores.Sessions.Create(context.Background(), s.db, sess); err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

func (s *searchServer) mkTranscript(t *testing.T, sessionID int64, content string) {
	t.Helper()
	if err := os.MkdirAll(s.ssvc.LogsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.ssvc.LogsDir, "session-"+strconv.FormatInt(sessionID, 10)+".log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSearchHTTP(t *testing.T) {
	s := newSearchTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, roleB := s.mkTeamRoles(t, "search-http")
	s1 := s.mkSession(t, teamID, roleA, models.SessionRunning)
	s2 := s.mkSession(t, teamID, roleB, models.SessionStopped)
	s.mkTranscript(t, s1, "line1\nthe answer is 42\nline3\nline4\n")
	s.mkTranscript(t, s2, "nothing here\nanother 42 hit\n")

	// 200 + результаты (2 совпадения "42")
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=42", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("search: want 200, got %d %v", st, out)
	}
	if out["total"].(float64) != 2 {
		t.Fatalf("total = %v, want 2", out["total"])
	}
	results, _ := out["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	// item: session_id + role_name + team_name + state + line_number + context
	r0 := results[0].(map[string]any)
	if r0["session_id"] == nil || r0["role_name"] == nil || r0["state"] == nil {
		t.Fatalf("result item missing fields: %v", r0)
	}
	if r0["line_number"] == nil || r0["context"] == nil || r0["match_line"] == nil {
		t.Fatalf("result item missing match fields: %v", r0)
	}

	// 400: q пустой
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=", nil, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("empty q: want 400, got %d", st)
	}
	// 400: q слишком короткий (1 символ)
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=4", nil, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("short q: want 400, got %d", st)
	}

	// 401: без ключа
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=42", nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}

	// фильтр role_id → только сессии roleA
	_, outRole, _ := do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/search?q=42&role_id=%d", s.srv.URL, roleA), nil, admin)
	if outRole["total"].(float64) != 1 {
		t.Fatalf("role filter total = %v, want 1", outRole["total"])
	}

	// limit/offset/has_more
	_, outLim, _ := do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=42&limit=1&offset=0", nil, admin)
	if outLim["total"].(float64) != 2 {
		t.Fatalf("limit: total = %v, want 2", outLim["total"])
	}
	if outLim["has_more"] != true {
		t.Fatalf("limit: has_more = %v, want true", outLim["has_more"])
	}
	if limRes, _ := outLim["results"].([]any); len(limRes) != 1 {
		t.Fatalf("limit: results = %d, want 1", len(limRes))
	}
}

func TestSessionSearchHTTPRBAC(t *testing.T) {
	s := newSearchTestServer(t)
	teamID, roleA, _ := s.mkTeamRoles(t, "search-rbac")
	sid := s.mkSession(t, teamID, roleA, models.SessionRunning)
	s.mkTranscript(t, sid, "hello needle\n")

	// viewer: 200 (sessions.read — viewer-ok)
	_, viewerKey := mustCreateKey(t, s.auth, "search-viewer", "viewer", "v")
	st, _, _ := do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=needle", nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer GET: want 200, got %d", st)
	}
	// оператор: 200
	_, opKey := mustCreateKey(t, s.auth, "search-operator", "operator", "o")
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=needle", nil, map[string]string{"X-API-Key": opKey})
	if st != http.StatusOK {
		t.Fatalf("operator GET: want 200, got %d", st)
	}
}

func TestSessionSearchEmpty(t *testing.T) {
	s := newSearchTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, _ := s.mkTeamRoles(t, "search-empty")
	sid := s.mkSession(t, teamID, roleA, models.SessionRunning)
	s.mkTranscript(t, sid, "no match here\n")
	// q не найден → пустой список (не 404)
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/sessions/search?q=zzznotfound", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("empty: want 200, got %d", st)
	}
	if out["total"].(float64) != 0 {
		t.Fatalf("empty: total = %v, want 0", out["total"])
	}
}
