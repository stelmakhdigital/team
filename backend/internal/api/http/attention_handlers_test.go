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
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

func newAttentionTestServer(t *testing.T) (*httptest.Server, *service.AuthService, *sql.DB, *service.TeamService, *service.TaskService, *repository.WatchdogRepo) {
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
	if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", "a.yaml"), []byte("name: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tsvc := service.NewTaskService(db, stores)
	attention := service.NewAttentionService(db, stores)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	cvc, err := service.NewConfigService(db)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:        db,
		Auth:      auth,
		Attention: attention,
		Config:    cvc,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, auth, db, svc, tsvc, stores.Watchdog
}

func TestAttentionEndpoint(t *testing.T) {
	srv, auth, db, svc, tsvc, watchdog := newAttentionTestServer(t)
	ctx := context.Background()

	// seed: team + role + 1 blocked task + 1 unread blocked alert
	team, err := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "att-http"})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	seg, err := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	role, err := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team.ID, DestinationRoleID: role.ID, Title: "blocked-task"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := tsvc.UpdateTaskState(ctx, service.UpdateTaskStateRequest{ID: task.ID, State: models.TaskBlocked}); err != nil {
		t.Fatalf("UpdateTaskState: %v", err)
	}
	if err := watchdog.Create(ctx, db, &models.WatchdogEvent{
		TeamID: team.ID, EventType: "blocked", Severity: "high", Description: "blocked detected",
		IsRead: false, RequiresAction: true, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Create alert: %v", err)
	}

	base := srv.URL + "/api/v1/attention"

	// no key -> 401
	st, _, _ := do(t, "GET", base, nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}

	// admin (env key) -> 200, >=2 items (blocked task + unread alert)
	st, out, _ := do(t, "GET", base, nil, map[string]string{"X-API-Key": "env-admin"})
	if st != http.StatusOK {
		t.Fatalf("admin GET attention: want 200, got %d %v", st, out)
	}
	if got := out["total"].(float64); got < 2 {
		t.Fatalf("total = %v, want >= 2 (blocked task + unread alert)", got)
	}
	items := out["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("items empty, want >= 2")
	}
	first := items[0].(map[string]any)
	for _, f := range []string{"kind", "id", "severity", "title", "detail", "suggested_action", "link"} {
		if _, ok := first[f]; !ok {
			t.Fatalf("item missing field %q: %v", f, first)
		}
	}

	// limit=1 -> 1 item, total unchanged
	st, out, _ = do(t, "GET", base+"?limit=1", nil, map[string]string{"X-API-Key": "env-admin"})
	if st != http.StatusOK {
		t.Fatalf("limit=1: want 200, got %d", st)
	}
	if got := out["total"].(float64); got < 2 {
		t.Fatalf("limit=1 total = %v, want >= 2", got)
	}
	if items := out["items"].([]any); len(items) != 1 {
		t.Fatalf("limit=1 -> %d items, want 1", len(items))
	}

	// viewer -> 200 (viewer-ok: attention GET maps to dashboard.read, у viewer есть *.read)
	_, viewKey := mustCreateKey(t, auth, "att-viewer", "viewer", "att-viewer-user")
	if err := auth.Init(ctx); err != nil {
		t.Fatal(err)
	}
	st, out, _ = do(t, "GET", base, nil, map[string]string{"X-API-Key": viewKey})
	if st != http.StatusOK {
		t.Fatalf("viewer GET attention: want 200 (viewer-ok), got %d %v", st, out)
	}
}
