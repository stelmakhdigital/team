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
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P2.9: HTTP /events (лента + фильтры + limit/offset + RBAC) ----------

type eventServer struct {
	srv    *httptest.Server
	auth   *service.AuthService
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	tsvc   *service.TaskService
	evs    *service.EventService
}

func newEventTestServer(t *testing.T) *eventServer {
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
	tsvc := service.NewTaskService(db, stores)
	ssvc := service.NewSessionService(db, stores, rtRegistry)
	ssvc.LogsDir = t.TempDir() + "/logs"
	ssvc.ConfigsDir = t.TempDir() + "/configs"
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	// P2.9: шина + sink (события → events)
	bus := service.NewEventBus()
	tsvc.Bus = bus
	ssvc.Bus = bus
	evs := service.NewEventService(db, stores)
	evs.SetSink(bus)
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:        db,
		Sessions:  ssvc,
		Events:    bus,
		Auth:      auth,
		EventsSvc: evs,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &eventServer{srv: srv, auth: auth, db: db, stores: stores, svc: svc, tsvc: tsvc, evs: evs}
}

func (s *eventServer) mkTeamRoles(t *testing.T, name string) (teamID, roleA, roleB int64) {
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

func TestEventsHTTP(t *testing.T) {
	s := newEventTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, roleB := s.mkTeamRoles(t, "ev-http")
	// операция → событие (sink)
	if _, err := s.tsvc.CreateTask(context.Background(), service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "http task one",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.tsvc.CreateTask(context.Background(), service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleB, Title: "http task two",
	}); err != nil {
		t.Fatal(err)
	}

	// 200 + события + total
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/events", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("GET /events: want 200, got %d %v", st, out)
	}
	if out["total"].(float64) < 2 {
		t.Fatalf("total = %v, want >= 2", out["total"])
	}
	events, _ := out["events"].([]any)
	if len(events) == 0 {
		t.Fatal("events empty")
	}
	ev0 := events[0].(map[string]any)
	// поля + enrichment (entity_title для task, team_name)
	if ev0["type"] == nil || ev0["summary"] == nil || ev0["entity_kind"] == nil || ev0["created_at"] == nil {
		t.Fatalf("event item missing fields: %v", ev0)
	}
	if ev0["team_name"] == nil {
		t.Errorf("team_name не подбрано: %v", ev0)
	}
	if ev0["entity_kind"] == "task" && ev0["entity_title"] == nil {
		t.Errorf("entity_title (task) не подбрано: %v", ev0)
	}
	// created_at DESC: первое — самое новое
	if out["total"].(float64) == float64(len(events)) {
		// все события в одной странице
	}

	// фильтр type=task.created → только task.created
	_, outType, _ := do(t, "GET", s.srv.URL+"/api/v1/events?type=task.created", nil, admin)
	if outType["total"].(float64) < 2 {
		t.Fatalf("type filter total = %v, want >= 2", outType["total"])
	}
	// фильтр team_id
	_, outTeam, _ := do(t, "GET", fmt.Sprintf("%s/api/v1/events?team_id=%d", s.srv.URL, teamID), nil, admin)
	if outTeam["total"].(float64) < 2 {
		t.Fatalf("team filter total = %v, want >= 2", outTeam["total"])
	}
	// limit/offset/has_more
	_, outLim, _ := do(t, "GET", s.srv.URL+"/api/v1/events?limit=1&offset=0", nil, admin)
	if outLim["total"].(float64) < 2 {
		t.Fatalf("limit: total = %v", outLim["total"])
	}
	if outLim["has_more"] != true {
		t.Fatalf("limit=1: has_more = %v, want true", outLim["has_more"])
	}
	if limEvents, _ := outLim["events"].([]any); len(limEvents) != 1 {
		t.Fatalf("limit=1: events = %d, want 1", len(limEvents))
	}

	// W5.2: фильтр q — substring, case-insensitive по summary
	// (summary task.created = "Task #N created: http task one/two")
	_, outQ, _ := do(t, "GET", s.srv.URL+"/api/v1/events?q=task+one", nil, admin)
	if outQ["total"].(float64) != 1 {
		t.Fatalf("q=task one: total = %v, want 1", outQ["total"])
	}
	// case-insensitive: верхний регистр находит тот же матч
	_, outQU, _ := do(t, "GET", s.srv.URL+"/api/v1/events?q=HTTP+TASK", nil, admin)
	if outQU["total"].(float64) != 2 {
		t.Fatalf("q=HTTP TASK: total = %v, want 2", outQU["total"])
	}
	// no-match
	_, outQN, _ := do(t, "GET", s.srv.URL+"/api/v1/events?q=nonexistent-xyz", nil, admin)
	if outQN["total"].(float64) != 0 {
		t.Fatalf("q=no-match: total = %v, want 0", outQN["total"])
	}
	// комбо с type-фильтром: пересечение → 0 / совпадение → 1
	_, outQC, _ := do(t, "GET", s.srv.URL+"/api/v1/events?q=one&type=task.created", nil, admin)
	if outQC["total"].(float64) != 1 {
		t.Fatalf("q+type(task.created): total = %v, want 1", outQC["total"])
	}
	_, outQC0, _ := do(t, "GET", s.srv.URL+"/api/v1/events?q=one&type=session.started", nil, admin)
	if outQC0["total"].(float64) != 0 {
		t.Fatalf("q+type(session.started): total = %v, want 0", outQC0["total"])
	}

	// 401: без ключа
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/events", nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}
}

func TestEventsHTTPRBAC(t *testing.T) {
	s := newEventTestServer(t)
	teamID, roleA, _ := s.mkTeamRoles(t, "ev-rbac")
	if _, err := s.tsvc.CreateTask(context.Background(), service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "rbac task",
	}); err != nil {
		t.Fatal(err)
	}
	// viewer: 200 (dashboard.read — viewer-ok)
	_, viewerKey := mustCreateKey(t, s.auth, "ev-viewer", "viewer", "v")
	st, _, _ := do(t, "GET", s.srv.URL+"/api/v1/events", nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer GET /events: want 200, got %d", st)
	}
	// operator: 200
	_, opKey := mustCreateKey(t, s.auth, "ev-operator", "operator", "o")
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/events", nil, map[string]string{"X-API-Key": opKey})
	if st != http.StatusOK {
		t.Fatalf("operator GET /events: want 200, got %d", st)
	}
}
