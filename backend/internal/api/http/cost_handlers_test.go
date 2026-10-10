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
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P2.14: cost (HTTP-контракт) ----------

type costServer struct {
	srv    *httptest.Server
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	tsvc   *service.TaskService
	ssvc   *service.SessionService
	pvc    *service.ProjectService
	cfg    *service.ConfigService
}

func newCostTestServer(t *testing.T) *costServer {
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
	_ = os.WriteFile(filepath.Join(svc.SpecsDir, "agents", "a.yaml"), []byte("name: a\n"), 0o644)
	tsvc := service.NewTaskService(db, stores)
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	ssvc.LogsDir = t.TempDir() + "/logs"
	msvc := service.NewMetricsService(db)
	msvc.CostSeries = ssvc.CostSeries
	pvc := service.NewProjectService(db, stores)
	cfg, _ := service.NewConfigService(db)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc,
		Metrics:  msvc,
		Config:   cfg,
		Projects: pvc,
		Auth:     auth,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &costServer{srv: srv, db: db, stores: stores, svc: svc, tsvc: tsvc, ssvc: ssvc, pvc: pvc, cfg: cfg}
}

// mkCostTeamRole — команда + сегмент + роль (+ опционально задача/сессия).
func (s *costServer) mkTeamRole(t *testing.T, name string) (teamID, roleID int64) {
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
	role, err := s.svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/a.yaml", Layout: &service.Layout{}})
	if err != nil {
		t.Fatal(err)
	}
	return team.ID, role.ID
}

func TestConfigListHasCostKeys(t *testing.T) {
	s := newCostTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/config", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("GET /config: want 200, got %d", st)
	}
	sections, _ := out["sections"].([]any)
	var costSec map[string]any
	for _, sec := range sections {
		sm, _ := sec.(map[string]any)
		if sm["name"] == "cost" {
			costSec = sm
			break
		}
	}
	if costSec == nil {
		t.Fatalf("cost section not found in %v sections", len(sections))
	}
	keys := map[string]bool{}
	for _, k := range costSec["keys"].([]any) {
		kk, _ := k.(map[string]any)
		keys[kk["key"].(string)] = true
	}
	if !keys["cost.model_prices"] || !keys["cost.budget_alert_cents"] {
		t.Errorf("cost keys = %v, want both", keys)
	}
}

func TestMetricsHasCostSeries(t *testing.T) {
	s := newCostTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/dashboard/metrics", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("GET /metrics: want 200, got %d", st)
	}
	metrics, _ := out["metrics"].(map[string]any)
	if metrics == nil {
		t.Fatalf("metrics object missing: %v", out)
	}
	series, _ := metrics["llm_cost_usd"].([]any)
	if len(series) != 12 {
		t.Fatalf("llm_cost_usd = %d points, want 12", len(series))
	}
	if _, ok := metrics["llm_tokens"]; !ok {
		t.Errorf("llm_tokens series missing (должна остаться)")
	}
}

func TestTaskDetailHasCostField(t *testing.T) {
	s := newCostTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleID := s.mkTeamRole(t, "cost-task-team")
	task, err := s.tsvc.CreateTask(context.Background(), service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleID, Title: "cost-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/tasks/"+itoaID(task.ID), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("GET task: want 200, got %d %v", st, out)
	}
	ec, _ := out["estimated_cost"].(map[string]any)
	if ec == nil {
		t.Fatalf("estimated_cost missing from task detail: %v", out)
	}
	if hd, _ := ec["has_data"].(bool); hd {
		t.Errorf("estimated_cost.has_data = true, want false (no logs)")
	}
}

func TestProjectDetailHasCostField(t *testing.T) {
	s := newCostTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	_, _ = s.mkTeamRole(t, "cost-proj-team")
	res, err := s.pvc.CreateProject(context.Background(), service.CreateProjectRequest{Name: "cost-project", Kind: "existing", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/projects/"+itoaID(res.Project.ID), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("GET project: want 200, got %d %v", st, out)
	}
	ec, _ := out["estimated_cost"].(map[string]any)
	if ec == nil {
		t.Fatalf("estimated_cost missing from project detail: %v", out)
	}
	if hd, _ := ec["has_data"].(bool); hd {
		t.Errorf("estimated_cost.has_data = true, want false (no sessions)")
	}
}
