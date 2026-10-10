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
	"time"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P1.6: watchdog-джобы (HTTP) ----------

type wdJobServer struct {
	srv  *httptest.Server
	svc  *service.TeamService
	ssvc *service.SessionService
	auth *service.AuthService
	db   *sql.DB
}

func newWdJobTestServer(t *testing.T) *wdJobServer {
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
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	bus := service.NewEventBus()
	tsvc.Bus = bus
	ssvc.Bus = bus
	stores2 := repository.NewStores(db)
	alerts := &service.AlertStoreRef{Events: stores2.Watchdog, Teams: stores2.Teams, DB: db}
	wdjobs := service.NewWatchdogJobService(db, stores)
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc, Alerts: alerts,
		Events:       bus,
		Auth:         auth,
		WatchdogJobs: wdjobs,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &wdJobServer{srv: srv, svc: svc, ssvc: ssvc, auth: auth, db: db}
}

// wdLiveSession — команда + роль + живая сессия (no-arg sleep).
func (s *wdJobServer) wdLiveSession(t *testing.T) int64 {
	t.Helper()
	team, err := s.svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: "wd-http-team"})
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
	script := filepath.Join(t.TempDir(), "sleep.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sess, err := s.ssvc.CreateSession(context.Background(), team.ID, service.CreateSessionRequest{RoleID: role.ID, Command: script})
	if err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

func TestWatchdogJobHTTPCRUD(t *testing.T) {
	s := newWdJobTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	sessID := s.wdLiveSession(t)

	// register (201)
	st, out, _ := do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs",
		map[string]any{"name": "ctx", "target_session_id": sessID, "threshold_pct": 80, "message": "context high"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("register: %d %v", st, out)
	}
	jobID := int64(out["id"].(float64))

	// validation (400): нет name
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs",
		map[string]any{"target_session_id": sessID, "threshold_pct": 80}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("no name: want 400, got %d", st)
	}
	// validation (404): нет сессии
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs",
		map[string]any{"name": "x", "target_session_id": 99999, "threshold_pct": 80}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("missing session: want 404, got %d", st)
	}

	// list (200, includes)
	st, listOut, _ := do(t, "GET", s.srv.URL+"/api/v1/watchdog/jobs", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("list: %d %v", st, listOut)
	}
	jobs := listOut["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("list = %d, want 1", len(jobs))
	}

	// show (200, recent empty)
	st, showOut, _ := do(t, "GET", s.srv.URL+"/api/v1/watchdog/jobs/"+itoa(jobID), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("show: %d %v", st, showOut)
	}
	if showOut["recent_evaluations"] == nil {
		t.Fatal("show: recent_evaluations missing")
	}

	// stop (200, enabled=false)
	st, stopOut, _ := do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs/"+itoa(jobID)+"/stop", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("stop: %d %v", st, stopOut)
	}
	if stopOut["enabled"] != false {
		t.Fatalf("stop: enabled = %v, want false", stopOut["enabled"])
	}

	// delete (200, fired_count=0)
	st, _, _ = do(t, "DELETE", s.srv.URL+"/api/v1/watchdog/jobs/"+itoa(jobID), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("delete: %d", st)
	}
	// show после delete → 404
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/watchdog/jobs/"+itoa(jobID), nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("show after delete: want 404, got %d", st)
	}
}

func TestWatchdogJobHTTPDelete409(t *testing.T) {
	s := newWdJobTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	sessID := s.wdLiveSession(t)
	_, out, _ := do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs",
		map[string]any{"name": "ctx", "target_session_id": sessID, "threshold_pct": 80}, admin)
	jobID := int64(out["id"].(float64))

	// fired_count>0 → 409 (устанавливаем fired_count=1 напрямую через репо)
	repo := repository.NewStores(s.db).WatchdogJobs
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var cpct float64 = 90
	if err := repo.RecordFire(context.Background(), tx, jobID, time.Now().UTC(), &cpct); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	st, out, _ := do(t, "DELETE", s.srv.URL+"/api/v1/watchdog/jobs/"+itoa(jobID), nil, admin)
	if st != http.StatusConflict {
		t.Fatalf("delete (fired>0): want 409, got %d %v", st, out)
	}
}

func TestWatchdogJobHTTPRBAC(t *testing.T) {
	s := newWdJobTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	sessID := s.wdLiveSession(t)

	// viewer: list/show 200
	_, viewerKey := mustCreateKey(t, s.auth, "wd-viewer", "viewer", "v-user")
	_ = s.auth.Init(context.Background())
	viewer := map[string]string{"X-API-Key": viewerKey}
	st, _, _ := do(t, "GET", s.srv.URL+"/api/v1/watchdog/jobs", nil, viewer)
	if st != http.StatusOK {
		t.Fatalf("viewer list: want 200, got %d", st)
	}
	// viewer: register 403
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs",
		map[string]any{"name": "x", "target_session_id": sessID, "threshold_pct": 80}, viewer)
	if st != http.StatusForbidden {
		t.Fatalf("viewer register: want 403, got %d", st)
	}
	// operator: register 201
	_, opKey := mustCreateKey(t, s.auth, "wd-operator", "operator", "o-user")
	_ = s.auth.Init(context.Background())
	operator := map[string]string{"X-API-Key": opKey}
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/watchdog/jobs",
		map[string]any{"name": "ctx", "target_session_id": sessID, "threshold_pct": 80}, operator)
	if st != http.StatusCreated {
		t.Fatalf("operator register: want 201, got %d", st)
	}
	_ = admin
}
