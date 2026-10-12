package httpapi_test

import (
	"context"
	"database/sql"
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
	"daemon/internal/service"
)

func newProjectTestServer(t *testing.T) (*httptest.Server, *service.AuthService, *sql.DB, *service.TeamService) {
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
	pvc := service.NewProjectService(db, stores)
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Auth:     auth,
		Projects: pvc,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, auth, db, svc
}

func TestProjectsCRUDEndpoint(t *testing.T) {
	srv, _, _, _ := newProjectTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	base := srv.URL + "/api/v1/projects"
	workDir := t.TempDir()

	// no key → 401
	st, _, _ := do(t, "GET", base, nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}

	// create (existing) → 201
	st, out, _ := do(t, "POST", base, map[string]any{"name": "proj-h", "kind": "existing", "path": workDir}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create: want 201, got %d %v", st, out)
	}
	proj := out["project"].(map[string]any)
	id := int64(proj["id"].(float64))
	if proj["kind"] != "existing" {
		t.Errorf("kind = %v, want existing", proj["kind"])
	}

	// create (non-existent path) → 400
	st, out, _ = do(t, "POST", base, map[string]any{"name": "bad", "kind": "existing", "path": filepath.Join(workDir, "nope")}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("create bad path: want 400, got %d %v", st, out)
	}

	// list → 200, 1
	st, out, _ = do(t, "GET", base, nil, admin)
	if st != http.StatusOK {
		t.Fatalf("list: want 200, got %d", st)
	}
	if got := out["total"].(float64); got != 1 {
		t.Errorf("total = %v, want 1", got)
	}

	// get one → 200, counts
	st, out, _ = do(t, "GET", base+"/8", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("get missing: want 404, got %d", st)
	}
	st, out, _ = do(t, "GET", base+"/"+itoaID(id), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("get: want 200, got %d %v", st, out)
	}
	if _, ok := out["counts"]; !ok {
		t.Error("get: missing counts")
	}

	// patch name → 200
	newName := "proj-h2"
	st, out, _ = do(t, "PATCH", base+"/"+itoaID(id), map[string]any{"name": newName}, admin)
	if st != http.StatusOK {
		t.Fatalf("patch name: want 200, got %d %v", st, out)
	}
	if out["project"].(map[string]any)["name"] != newName {
		t.Errorf("patch name: name = %v, want %s", out["project"].(map[string]any)["name"], newName)
	}
	// patch path → 409 (immutable)
	st, out, _ = do(t, "PATCH", base+"/"+itoaID(id), map[string]any{"path": "/elsewhere"}, admin)
	if st != http.StatusConflict {
		t.Fatalf("patch path: want 409, got %d %v", st, out)
	}

	// archive → 200, archived
	st, out, _ = do(t, "DELETE", base+"/"+itoaID(id), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("archive: want 200, got %d %v", st, out)
	}
	if out["project"].(map[string]any)["archived"] != true {
		t.Errorf("archive: archived = %v, want true", out["project"].(map[string]any)["archived"])
	}
	// idempotent
	st, out, _ = do(t, "DELETE", base+"/"+itoaID(id), nil, admin)
	if st != http.StatusOK || out["project"].(map[string]any)["archived"] != true {
		t.Errorf("re-archive: st=%d out=%v", st, out)
	}
}

func TestProjectsRBAC(t *testing.T) {
	srv, auth, _, _ := newProjectTestServer(t)
	base := srv.URL + "/api/v1/projects"
	workDir := t.TempDir()

	// viewer: read OK, create denied
	_, viewerKey := mustCreateKey(t, auth, "proj-viewer", "viewer", "v-user")
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	viewer := map[string]string{"X-API-Key": viewerKey}
	st, _, _ := do(t, "GET", base, nil, viewer)
	if st != http.StatusOK {
		t.Fatalf("viewer list: want 200, got %d", st)
	}
	st, out, _ := do(t, "POST", base, map[string]any{"name": "x", "kind": "existing", "path": workDir}, viewer)
	if st != http.StatusForbidden {
		t.Fatalf("viewer create: want 403, got %d %v", st, out)
	}

	// operator: create OK
	_, opKey := mustCreateKey(t, auth, "proj-operator", "operator", "o-user")
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	operator := map[string]string{"X-API-Key": opKey}
	st, out, _ = do(t, "POST", base, map[string]any{"name": "proj-op", "kind": "existing", "path": workDir}, operator)
	if st != http.StatusCreated {
		t.Fatalf("operator create: want 201, got %d %v", st, out)
	}
}

func TestTaskFilterByProject(t *testing.T) {
	srv, _, _, svc := newProjectTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}

	// project + team(project) + role + task
	projDir := t.TempDir()
	// create project via HTTP
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/projects", map[string]any{"name": "flt-proj", "kind": "existing", "path": projDir}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create project: %d %v", st, out)
	}
	pid := int64(out["project"].(map[string]any)["id"].(float64))

	// team with project
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/teams", map[string]any{"name": "flt-team", "project_id": pid}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	// role
	segSt, segOut, _ := do(t, "POST", srv.URL+"/api/v1/teams/"+itoaID(teamID)+"/segments", map[string]any{"name": "core"}, admin)
	if segSt != http.StatusCreated {
		t.Fatalf("create segment: %d %v", segSt, segOut)
	}
	segID := int64(segOut["id"].(float64))
	roleSt, roleOut, _ := do(t, "POST", srv.URL+"/api/v1/segments/"+itoaID(segID)+"/roles", map[string]any{"name": "lead", "agent_spec": "agents/a.yaml"}, admin)
	if roleSt != http.StatusCreated {
		t.Fatalf("create role: %d %v", roleSt, roleOut)
	}
	roleID := int64(roleOut["id"].(float64))
	_ = svc

	// task (наследует project_id от team)
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/tasks", map[string]any{"team_id": teamID, "destination_role_id": roleID, "title": "flt-task"}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create task: %d %v", st, out)
	}
	taskID := int64(out["id"].(float64))
	_ = taskID

	// filter by project → 1 task
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/tasks?project_id="+itoaID(pid), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("filter tasks: want 200, got %d", st)
	}
	if got := out["total"].(float64); got != 1 {
		t.Errorf("filter by project total = %v, want 1", got)
	}
	// no filter → 1 (same), other project → 0
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/tasks", nil, admin)
	if st != http.StatusOK || out["total"].(float64) != 1 {
		t.Errorf("all tasks total = %v, want 1", out["total"])
	}
}

// TestProjectArtifactsAndAgentSpecs — GET /projects/{id}/files (+ file download, traversal-защита)
// и GET /agentspecs (список spec из SpecsDir).
func TestProjectArtifactsAndAgentSpecs(t *testing.T) {
	srv, _, _, _ := newProjectTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	workDir := t.TempDir()

	// содержимое проекта
	if err := os.MkdirAll(filepath.Join(workDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "docs", "spec.md"), []byte("# spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "node_modules", "x", "i.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, out, _ := do(t, "POST", srv.URL+"/api/v1/projects",
		map[string]any{"name": "proj-art", "kind": "existing", "path": workDir}, admin)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %v", st, out)
	}
	id := int64(out["project"].(map[string]any)["id"].(float64))

	// список на корневом уровне: только README.md + docs (dir); node_modules пропущен,
	// вложенные файлы НЕ включаются (дерево строит UI по раскрытию)
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("list files: %d %v", st, out)
	}
	files := out["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("root list len = %d, want 2 (README.md, docs): %v", len(files), files)
	}
	first := files[0].(map[string]any)
	if first["name"].(string) != "docs" || !first["is_dir"].(bool) {
		t.Fatalf("dirs first / wrong entry: %v", first)
	}
	second := files[1].(map[string]any)
	if second["name"].(string) != "README.md" || second["is_dir"].(bool) {
		t.Fatalf("second entry wrong: %v", second)
	}
	if out["path"].(string) != "" {
		t.Fatalf("root path = %v, want empty", out["path"])
	}
	if out["truncated"].(bool) {
		t.Fatalf("unexpected truncated: %v", out)
	}

	// раскрытие подкаталога: ?path=docs → spec.md
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files?path=docs", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("list docs: %d %v", st, out)
	}
	docs := out["files"].([]any)
	if len(docs) != 1 || docs[0].(map[string]any)["name"].(string) != "spec.md" {
		t.Fatalf("docs listing = %v, want [spec.md]", docs)
	}
	if out["path"].(string) != "docs" {
		t.Fatalf("docs path = %v, want docs", out["path"])
	}

	// traversal (../) в ?path → 400/404, не 200
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files?path=..%2F..%2Fetc", nil, admin)
	if st == http.StatusOK {
		t.Fatalf("traversal in path: want non-200, got 200")
	}
	// ?path на файл → 404
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files?path=README.md", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("path on file: want 404, got %d", st)
	}

	// download: README.md
	st, body, hdr := doRaw(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files/README.md", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("download: %d %s", st, body)
	}
	if body != "# hello" {
		t.Fatalf("file body = %q, want %q", body, "# hello")
	}
	ct := hdr.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/") {
		t.Fatalf("content-type = %q, want text/*", ct)
	}

	// traversal (../) → 400/404, не 200
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files/..%2F..%2Fetc%2Fpasswd", nil, admin)
	if st == http.StatusOK {
		t.Fatalf("traversal: want non-200, got 200")
	}

	// 404: нет файла / нет проекта
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/projects/"+itoaID(id)+"/files/nope.txt", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("missing file: want 404, got %d", st)
	}
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/projects/999999/files", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("missing project files: want 404, got %d", st)
	}

	// GET /agentspecs — только верхний уровень SpecsDir (.yaml/.yml, без подкаталогов).
	// В newProjectTestServer a.yaml лежит в подкаталоге agents/ → верхний уровень пуст.
	specsURL := srv.URL + "/api/v1/agentspecs"
	st, out, _ = do(t, "GET", specsURL, nil, admin)
	if st != http.StatusOK {
		t.Fatalf("agentspecs: %d %v", st, out)
	}
	specs, ok := out["specs"].([]any)
	if !ok {
		t.Fatalf("agentspecs response shape: %v", out)
	}
	if len(specs) != 0 {
		t.Fatalf("agentspecs = %v, want empty (a.yaml в подкаталоге)", specs)
	}
}
