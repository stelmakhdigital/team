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
	"daemon/internal/service"
)

// ---------- P3.14: Walk (HTTP: create / list / get / advance / pause / RBAC) ----------

// walkTestSender — mock Sender (всегда delivered), чтобы HTTP-тесты не трогали runtime.
type walkTestSender struct{}

func (walkTestSender) Send(ctx context.Context, sessionID int64, text string, submit bool) (*service.SendToSessionResult, error) {
	return &service.SendToSessionResult{Status: service.SendStatusDelivered, Sent: true, SentAt: "t"}, nil
}

func newWalkTestServer(t *testing.T) (*httptest.Server, *sql.DB, *service.TeamService, *service.AuthService) {
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
	walkSvc := service.NewWalkService(db, stores, walkTestSender{})
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, service.NewTaskService(db, stores), httpapi.Options{
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:     db,
		Auth:   auth,
		Walk:   walkSvc,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, db, svc, auth
}

// makeWalkSession — команда + роль + строка сессии (tmux).
func makeWalkSession(t *testing.T, db *sql.DB, svc *service.TeamService, name string) int64 {
	t.Helper()
	ctx := context.Background()
	team, err := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	role, err := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	sess := &models.Session{
		TeamID: team.ID, RoleID: role.ID,
		RuntimeType: "tmux", RuntimeRef: "walk-http", State: models.SessionRunning,
	}
	if err := repository.NewStores(db).Sessions.Create(ctx, db, sess); err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

func TestWalkCreateGetHTTP(t *testing.T) {
	srv, db, svc, _ := newWalkTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	sid := makeWalkSession(t, db, svc, "walk-http-create")

	// create → 201 + active/seq1 + steps.
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/sessions/"+itoaID(sid)+"/walks",
		map[string]any{"name": "onboarding", "mode": "manual", "steps": []string{"s1", "s2"}}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create: want 201, got %d %v", st, out)
	}
	wid := itoaID(int64(out["id"].(float64)))
	if out["status"] != "active" || out["current_seq"].(float64) != 1 || out["total"].(float64) != 2 {
		t.Fatalf("create = %+v, want active/seq1/2", out)
	}

	// get → 200 + steps.
	st, g, _ := do(t, "GET", srv.URL+"/api/v1/walks/"+wid, nil, admin)
	if st != http.StatusOK {
		t.Fatalf("get: want 200, got %d %v", st, g)
	}
	if g["status"] != "active" || len(g["steps"].([]any)) != 2 {
		t.Fatalf("get = %+v, want active + 2 steps", g)
	}

	// advance → 200 (delivered → seq2).
	st, a, _ := do(t, "POST", srv.URL+"/api/v1/walks/"+wid+"/advance", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("advance: want 200, got %d %v", st, a)
	}
	if a["current_seq"].(float64) != 2 || a["status"] != "active" {
		t.Fatalf("advance = %+v, want seq2/active", a)
	}
	// advance → completed.
	st, a2, _ := do(t, "POST", srv.URL+"/api/v1/walks/"+wid+"/advance", nil, admin)
	if st != http.StatusOK || a2["status"] != "completed" {
		t.Fatalf("advance2 = %d %+v, want completed", st, a2)
	}
	// advance on completed → 409.
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/walks/"+wid+"/advance", nil, admin)
	if st != http.StatusConflict {
		t.Fatalf("advance completed: want 409, got %d", st)
	}

	// 404 (нет walk'а).
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/walks/999999", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("get 404: want 404, got %d", st)
	}
	// 404 (create для несуществующей сессии).
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/sessions/999999/walks",
		map[string]any{"name": "w", "mode": "manual", "steps": []string{"a"}}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("create 404 session: want 404, got %d", st)
	}
}

func TestWalkListHTTP(t *testing.T) {
	srv, db, svc, _ := newWalkTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	sid := makeWalkSession(t, db, svc, "walk-http-list")
	do(t, "POST", srv.URL+"/api/v1/sessions/"+itoaID(sid)+"/walks",
		map[string]any{"name": "a", "mode": "manual", "steps": []string{"x"}}, admin)
	do(t, "POST", srv.URL+"/api/v1/sessions/"+itoaID(sid)+"/walks",
		map[string]any{"name": "b", "mode": "manual", "steps": []string{"y", "z"}}, admin)

	st, out, _ := do(t, "GET", srv.URL+"/api/v1/sessions/"+itoaID(sid)+"/walks", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("list: want 200, got %d %v", st, out)
	}
	if out["total"].(float64) != 2 {
		t.Fatalf("list total = %v, want 2", out["total"])
	}
}

func TestWalkRBAC(t *testing.T) {
	srv, db, svc, auth := newWalkTestServer(t)
	_, viewerKey := mustCreateKey(t, auth, "walk-viewer", "viewer", "v")
	sid := makeWalkSession(t, db, svc, "walk-http-rbac")

	// viewer: create (sessions.update, operator+) → 403.
	st, _, _ := do(t, "POST", srv.URL+"/api/v1/sessions/"+itoaID(sid)+"/walks",
		map[string]any{"name": "w", "mode": "manual", "steps": []string{"a"}}, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer create: want 403, got %d", st)
	}
	// viewer: advance (sessions.update) → 403.
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/walks/1/advance", nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer advance: want 403, got %d", st)
	}
	// viewer: get (dashboard.read, viewer-ok) → 200 (или 404, но не 403).
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/walks/1", nil, map[string]string{"X-API-Key": viewerKey})
	if st == http.StatusForbidden {
		t.Fatalf("viewer get: want NOT 403, got 403")
	}
}
