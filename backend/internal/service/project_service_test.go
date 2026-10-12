package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// setupProjectEnv — sqlite in-memory + Stores + Team/Task/Project services.
func setupProjectEnv(t *testing.T) (*service.TeamService, *service.TaskService, *service.ProjectService, *sql.DB, context.Context) {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		d := filepath.Join(svc.SpecsDir, "agents")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	pvc := service.NewProjectService(db, stores)
	return svc, tsvc, pvc, db, context.Background()
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error status %d, got nil", status)
		return
	}
	ae, ok := err.(*service.AppError)
	if !ok || ae.Status != status {
		t.Fatalf("want status %d, got %v (%v)", status, ae, err)
	}
}

func TestProjectCreateExisting(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)
	dir := t.TempDir()
	// git-каталог (для git=true)
	gitDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// (a) success: existing + git
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-a", Kind: "existing", Path: gitDir})
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}
	if res.Project.Kind != models.ProjectExisting || !res.Project.Git {
		t.Errorf("existing: kind=%s git=%v, want existing/true", res.Project.Kind, res.Project.Git)
	}

	// (b) non-existent path → 400
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "nope", Kind: "existing", Path: filepath.Join(dir, "missing")})
	wantStatus(t, err, 400)

	// (c) path is a file → 400
	f := filepath.Join(dir, "file.txt")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "isfile", Kind: "existing", Path: f})
	wantStatus(t, err, 400)

	// (d) duplicate path → 409
	other := t.TempDir()
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "dup-path", Kind: "existing", Path: other})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "dup-path2", Kind: "existing", Path: other})
	wantStatus(t, err, 409)

	// (e) duplicate name → 409
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-a", Kind: "existing", Path: t.TempDir()})
	wantStatus(t, err, 409)
}

func TestProjectCreateNew(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)
	parent := t.TempDir()

	// (a) success: new → каталог создан, README, git
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-b", Kind: "new", ParentPath: parent})
	if err != nil {
		t.Fatalf("create new: %v", err)
	}
	target := filepath.Join(parent, "proj-b")
	if _, err := os.Stat(target); err != nil {
		t.Errorf("target dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Errorf("README.md not created: %v", err)
	}
	if !res.Project.Git && pvc != nil {
		// git должен быть (если доступен); иначе warning
		t.Logf("git=%v warning=%q", res.Project.Git, res.Warning)
	}

	// (b) non-existent parent → 400
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "nope", Kind: "new", ParentPath: filepath.Join(parent, "missing")})
	wantStatus(t, err, 400)

	// (c) target already exists → 409
	_, err = pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-b", Kind: "new", ParentPath: parent})
	wantStatus(t, err, 409)

	// (d) git недоступен → warning + git=false
	pvcNoGit := *pvc
	pvcNoGit.GitProbe = func() bool { return false }
	res2, err := pvcNoGit.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-c", Kind: "new", ParentPath: parent})
	if err != nil {
		t.Fatalf("create new (no git): %v", err)
	}
	if res2.Project.Git {
		t.Error("git should be false when git unavailable")
	}
	if res2.Warning == "" {
		t.Error("expected a warning when git unavailable")
	}
}

func TestProjectArchive(t *testing.T) {
	svc, _, pvc, _, ctx := setupProjectEnv(t)
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-d", Kind: "existing", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pid := res.Project.ID

	// archive
	p, err := pvc.ArchiveProject(ctx, pid)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !p.Archived {
		t.Error("project should be archived")
	}
	// idempotent
	p, err = pvc.ArchiveProject(ctx, pid)
	if err != nil || !p.Archived {
		t.Errorf("re-archive: %v (archived=%v)", err, p.Archived)
	}
	// update archived → 409
	name := "new-name"
	_, err = pvc.UpdateProject(ctx, pid, &name, nil)
	wantStatus(t, err, 409)
	// team в archived проект → 409
	_, err = svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "team-in-archived", ProjectID: &pid})
	wantStatus(t, err, 409)
}

func TestTaskProjectInheritance(t *testing.T) {
	svc, tsvc, pvc, _, ctx := setupProjectEnv(t)
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-e", Kind: "existing", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pid := res.Project.ID

	// team с project
	team, err := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "team-p", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	seg, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	roleA, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{X: 0, Y: 0}})

	// (a) наследование от team.project
	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team.ID, DestinationRoleID: roleA.ID, Title: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if task.ProjectID == nil || *task.ProjectID != pid {
		t.Errorf("task project_id = %v, want %d (inherited)", task.ProjectID, pid)
	}

	// (b) override
	team2, _ := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "team-q"}) // без проекта
	seg2, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team2.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	roleB, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg2.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	task2, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team2.ID, DestinationRoleID: roleB.ID, Title: "t2", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if task2.ProjectID == nil || *task2.ProjectID != pid {
		t.Errorf("task2 project_id = %v, want %d (override)", task2.ProjectID, pid)
	}

	// (c) handoff наследует project_id
	// task1 → done/handoff к roleA (тот же team)
	resH, err := tsvc.HandoffTask(ctx, service.HandoffTaskRequest{ID: task.ID, ToRoleID: roleA.ID, Comment: "h"})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if resH.NewTask.ProjectID == nil || *resH.NewTask.ProjectID != pid {
		t.Errorf("handed-off task project_id = %v, want %d", resH.NewTask.ProjectID, pid)
	}
}

func TestResolveWorkingDir(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-wd", Kind: "existing", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pid := res.Project.ID
	team := &models.Team{ID: 1, ProjectID: &pid}
	noProjTeam := &models.Team{ID: 2}
	taskWithProj := &models.Task{ID: 10, ProjectID: &pid}
	taskNoProj := &models.Task{ID: 11}

	// (1) explicit working_dir wins
	wd, pidRes, err := pvc.ResolveWorkingDir(ctx, "/explicit/dir", nil, taskWithProj, team)
	if err != nil || wd != "/explicit/dir" {
		t.Errorf("level1: wd=%s err=%v, want /explicit/dir", wd, err)
	}
	_ = pidRes

	// (2) task.project (нет explicit working_dir)
	wd, pidRes, err = pvc.ResolveWorkingDir(ctx, "", nil, taskWithProj, noProjTeam)
	if err != nil {
		t.Fatalf("level2: %v", err)
	}
	if pidRes == nil || *pidRes != pid {
		t.Errorf("level2: resolved project = %v, want %d", pidRes, pid)
	}
	if wd == "" {
		t.Error("level2: working_dir should be the project path")
	}

	// (3) team.project (task без проекта, нет explicit)
	wd, pidRes, err = pvc.ResolveWorkingDir(ctx, "", nil, taskNoProj, team)
	if err != nil {
		t.Fatalf("level3: %v", err)
	}
	if pidRes == nil || *pidRes != pid {
		t.Errorf("level3: resolved project = %v, want %d", pidRes, pid)
	}
	_ = wd

	// (4) absence (ничего не задано) → explicit/empty, без 400 (backward compat)
	wd, pidRes, err = pvc.ResolveWorkingDir(ctx, "", nil, taskNoProj, noProjTeam)
	if err != nil {
		t.Fatalf("absence: unexpected error %v (want no 400 for backward compat)", err)
	}
	if pidRes != nil {
		t.Errorf("absence: resolved project = %v, want nil", pidRes)
	}
	if wd != "" {
		t.Errorf("absence: working_dir = %q, want empty", wd)
	}

	// (5) explicit project_id не существует → 400
	missing := int64(999999)
	_, _, err = pvc.ResolveWorkingDir(ctx, "", &missing, nil, nil)
	wantStatus(t, err, 400)
}

func TestResolveWorkingDirArchived(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-arch", Kind: "existing", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pid := res.Project.ID
	_, _ = pvc.ArchiveProject(ctx, pid)
	team := &models.Team{ID: 1, ProjectID: &pid}
	// archived проект через team → 409
	_, _, err = pvc.ResolveWorkingDir(ctx, "", nil, nil, team)
	wantStatus(t, err, 409)
}

func TestProjectSummaryCounts(t *testing.T) {
	svc, tsvc, pvc, _, ctx := setupProjectEnv(t)
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-sum", Kind: "existing", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pid := res.Project.ID
	team, _ := svc.CreateTeam(ctx, service.CreateTeamRequest{Name: "team-sum", ProjectID: &pid})
	seg, _ := svc.CreateSegment(ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	roleA, _ := svc.CreateRole(ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	_, _ = tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: team.ID, DestinationRoleID: roleA.ID, Title: "t"})

	sum, err := pvc.GetProject(ctx, pid)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if sum.Counts.Teams != 1 || sum.Counts.Tasks != 1 {
		t.Errorf("counts teams=%d tasks=%d, want 1/1", sum.Counts.Teams, sum.Counts.Tasks)
	}
	// несуществующий проект → 404
	_, err = pvc.GetProject(ctx, 999999)
	if err == nil {
		t.Error("get missing project: want error")
	}
}

func TestProjectListFilter(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)
	a, _ := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "alpha", Kind: "existing", Path: t.TempDir()})
	b, _ := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "beta", Kind: "existing", Path: t.TempDir()})
	_, _ = pvc.ArchiveProject(ctx, b.Project.ID)

	// по умолчанию только активные
	_, total, err := pvc.ListProjects(ctx, repository.ProjectFilter{}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("active-only total = %d, want 1", total)
	}
	// include archived
	_, total, _ = pvc.ListProjects(ctx, repository.ProjectFilter{IncludeArch: true}, 50, 0)
	if total != 2 {
		t.Errorf("include-archived total = %d, want 2", total)
	}
	// archived only
	_, total, _ = pvc.ListProjects(ctx, repository.ProjectFilter{ArchivedOnly: true}, 50, 0)
	if total != 1 {
		t.Errorf("archived-only total = %d, want 1", total)
	}
	// name filter
	_, total, _ = pvc.ListProjects(ctx, repository.ProjectFilter{Name: "alph", IncludeArch: true}, 50, 0)
	if total != 1 {
		t.Errorf("name filter total = %d, want 1", total)
	}
	_ = a
}

// gitCommit — git add -A + commit в dir с заданным автором (test-fixture).
func gitCommit(t *testing.T, dir, author, message string) {
	t.Helper()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME="+author, "GIT_AUTHOR_EMAIL=author@example.com",
		"GIT_COMMITTER_NAME="+author, "GIT_COMMITTER_EMAIL=author@example.com")
	cmd := exec.Command("git", "commit", "-q", "-m", message, "--allow-empty")
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit (%s): %v\n%s", message, err, out)
	}
}

func gitAddAll(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}

// TestProjectFilesAttribution — W2.2: git-attribution (last_author/last_commit/last_at).
func TestProjectFilesAttribution(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)

	// git-репозиторий с двумя файлами и двумя авторами.
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git not available: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "alpha.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddAll(t, repo)
	gitCommit(t, repo, "Alice A", "feat: alpha v1")
	if err := os.WriteFile(filepath.Join(repo, "alpha.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "beta.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAddAll(t, repo)
	gitCommit(t, repo, "Bob B", "chore: alpha v2 + beta")

	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "attr-git", Kind: "existing", Path: repo})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	id := res.Project.ID

	// (a) attribute=true → attributed=true, поля заполнены; последний коммит alpha.txt — Bob B.
	lst, err := pvc.ListProjectFiles(ctx, id, "", true)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if !lst.Attributed {
		t.Fatal("attributed = false, want true")
	}
	byName := map[string]service.ProjectFileView{}
	for _, f := range lst.Files {
		if !f.IsDir {
			byName[f.Name] = f
		}
	}
	alpha := byName["alpha.txt"]
	if alpha.LastAuthor == nil || *alpha.LastAuthor != "Bob B" {
		t.Errorf("alpha last_author = %v, want Bob B", alpha.LastAuthor)
	}
	if alpha.LastCommit == nil || *alpha.LastCommit != "chore: alpha v2 + beta" {
		t.Errorf("alpha last_commit = %v, want subject", alpha.LastCommit)
	}
	if alpha.LastAt == nil || *alpha.LastAt == "" {
		t.Errorf("alpha last_at empty")
	}
	beta := byName["beta.md"]
	if beta.LastAuthor == nil || *beta.LastAuthor != "Bob B" {
		t.Errorf("beta last_author = %v, want Bob B", beta.LastAuthor)
	}

	// (a2) untracked-файл: атрибуция прошла (attributed=true), поля null.
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	lstU, err := pvc.ListProjectFiles(ctx, id, "", true)
	if err != nil {
		t.Fatalf("list files (untracked): %v", err)
	}
	if !lstU.Attributed {
		t.Error("attributed = false при наличии untracked-файла, want true (атрибуция прошла, данных нет)")
	}
	for _, f := range lstU.Files {
		if f.IsDir || f.Name != "untracked.txt" {
			continue
		}
		if f.LastAuthor != nil || f.LastCommit != nil || f.LastAt != nil {
			t.Errorf("untracked.txt должен иметь null-поля, got author=%v", f.LastAuthor)
		}
	}

	// (b) attribute=false → attributed=false, поля null (даже для git-проекта).
	lst2, err := pvc.ListProjectFiles(ctx, id, "", false)
	if err != nil {
		t.Fatalf("list files (no attr): %v", err)
	}
	if lst2.Attributed {
		t.Error("attributed = true без ?attribute=true, want false")
	}
	for _, f := range lst2.Files {
		if f.LastAuthor != nil || f.LastCommit != nil || f.LastAt != nil {
			t.Errorf("file %s has attribution without ?attribute=true", f.Name)
		}
	}

	// (c) non-git проект → attributed=false, поля null.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	resP, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "attr-plain", Kind: "existing", Path: plain})
	if err != nil {
		t.Fatal(err)
	}
	lst3, err := pvc.ListProjectFiles(ctx, resP.Project.ID, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if lst3.Attributed {
		t.Error("non-git: attributed = true, want false")
	}
	for _, f := range lst3.Files {
		if f.LastAuthor != nil || f.LastCommit != nil || f.LastAt != nil {
			t.Errorf("non-git file %s has attribution", f.Name)
		}
	}
}

// TestProjectFilesAttributionLimit — W2.2: > maxAttributedFiles файлов → attributed=false.
func TestProjectFilesAttributionLimit(t *testing.T) {
	_, _, pvc, _, ctx := setupProjectEnv(t)
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git not available: %v\n%s", err, out)
	}
	for i := 0; i < 101; i++ {
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitAddAll(t, repo)
	gitCommit(t, repo, "Alice A", "bulk")

	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "attr-limit", Kind: "existing", Path: repo})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	lst, err := pvc.ListProjectFiles(ctx, res.Project.ID, "", true)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if lst.Attributed {
		t.Error("101 files: attributed = true, want false (лимит)")
	}
	for _, f := range lst.Files {
		if f.LastAuthor != nil || f.LastCommit != nil || f.LastAt != nil {
			t.Errorf("file %s has attribution despite limit", f.Name)
		}
	}
}
