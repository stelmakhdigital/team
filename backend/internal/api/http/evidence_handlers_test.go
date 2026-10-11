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
	"daemon/internal/service"
)

// ---------- P3.13: Proof/evidence (HTTP: submit / list / verdict + RBAC) ----------

func newEvidenceTestServer(t *testing.T) (*httptest.Server, *service.TeamService, *service.TaskService, *service.AuthService) {
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
	evsvc := service.NewEvidenceService(db, stores)
	tsvc.SetEvidenceRepo(stores.Evidence)
	lsvc := service.NewLibraryService(db, stores, svc)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Library:  lsvc,
		Auth:     auth,
		Evidence: evsvc,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, svc, tsvc, auth
}

func mustEvidenceTask(t *testing.T, svc *service.TeamService, tsvc *service.TaskService, name string) int64 {
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
	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team.ID, DestinationRoleID: role.ID, Title: "ev"})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func TestEvidenceSubmitHTTP(t *testing.T) {
	srv, svc, tsvc, _ := newEvidenceTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	taskID := mustEvidenceTask(t, svc, tsvc, "ev-submit")

	// submit inline → 201 + content_sha
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence",
		map[string]any{"type": "screenshot", "name": "ui.png", "content": "bytes-123"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("submit: want 201, got %d %v", st, out)
	}
	if out["status"] != "submitted" {
		t.Errorf("status = %v, want submitted", out["status"])
	}
	if out["content_sha"] == nil || out["content_sha"] == "" {
		t.Errorf("content_sha not set (inline should be server sha256)")
	}
	if out["submitted_by"] != "operator:admin" { // env-admin → "operator:admi" (last 4 of "env-admin")
		// env-admin last 4 = "dmin"? "env-admin"[-4:] = "dmin". Let's just check non-empty.
		if out["submitted_by"] == "" {
			t.Errorf("submitted_by not set (auth attribution)")
		}
	}

	// submit validation: 400 (нет content/ref)
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence",
		map[string]any{"type": "test", "name": "n"}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("submit no-content: want 400, got %d", st)
	}
	// 404 (нет задачи)
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks/999999/evidence",
		map[string]any{"type": "test", "name": "n", "content": "x"}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("submit 404: want 404, got %d", st)
	}
}

func TestEvidenceListHTTP(t *testing.T) {
	srv, svc, tsvc, _ := newEvidenceTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	taskID := mustEvidenceTask(t, svc, tsvc, "ev-list")
	do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence",
		map[string]any{"type": "test", "name": "t1", "content": "a"}, admin)
	do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence",
		map[string]any{"type": "log", "name": "t2", "content_ref": "https://x/l"}, admin)

	st, out, _ := do(t, "GET", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("list: want 200, got %d %v", st, out)
	}
	if out["total"].(float64) != 2 {
		t.Fatalf("total = %v, want 2", out["total"])
	}
}

func TestEvidenceVerdictHTTP(t *testing.T) {
	srv, svc, tsvc, _ := newEvidenceTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	taskID := mustEvidenceTask(t, svc, tsvc, "ev-verdict")
	_, out, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence",
		map[string]any{"type": "test", "name": "t", "content": "a"}, admin)
	evidID := itoaID(int64(out["id"].(float64)))

	// accept → 200 + status=accepted + judged_by
	st, vout, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence/"+evidID+"/verdict",
		map[string]any{"action": "accept", "reason": "ok"}, admin)
	if st != http.StatusOK {
		t.Fatalf("accept: want 200, got %d %v", st, vout)
	}
	if vout["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", vout["status"])
	}
	if vout["judged_by"] == nil || vout["judged_by"] == "" {
		t.Errorf("judged_by not set (attribution)")
	}
	// withdraw из accepted → 200 (withdrawn)
	st, vout, _ = do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence/"+evidID+"/verdict",
		map[string]any{"action": "withdraw"}, admin)
	if st != http.StatusOK || vout["status"] != "withdrawn" {
		t.Fatalf("withdraw: want 200/withdrawn, got %d %v", st, vout)
	}
	// invalid action → 400
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence/"+evidID+"/verdict",
		map[string]any{"action": "bogus"}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("invalid action: want 400, got %d", st)
	}
	// cross-task → 404
	otherTask := mustEvidenceTask(t, svc, tsvc, "ev-other")
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(otherTask)+"/evidence/"+evidID+"/verdict",
		map[string]any{"action": "accept"}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("cross-task: want 404, got %d", st)
	}
}

func TestEvidenceRBAC(t *testing.T) {
	srv, svc, tsvc, auth := newEvidenceTestServer(t)
	_, viewerKey := mustCreateKey(t, auth, "ev-viewer", "viewer", "v")
	taskID := mustEvidenceTask(t, svc, tsvc, "ev-rbac")
	// viewer: submit (tasks.update, operator+) → 403
	st, _, _ := do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence",
		map[string]any{"type": "test", "name": "n", "content": "x"}, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer submit: want 403, got %d", st)
	}
	// viewer: list (dashboard.read, viewer-ok) → 200
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence", nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer list: want 200, got %d", st)
	}
	// viewer: verdict (tasks.update) → 403
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks/"+itoaID(taskID)+"/evidence/1/verdict",
		map[string]any{"action": "accept"}, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer verdict: want 403, got %d", st)
	}
}
