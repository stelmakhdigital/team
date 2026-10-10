package service_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P1.8: Diagnostics (Health/Doctor) ----------

type diagEnv struct {
	db     *sql.DB
	stores *repository.Stores
	reg    *runtime.Registry
	svc    *service.TeamService
	logDir string
}

func newDiagEnv(t *testing.T) *diagEnv {
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
	return &diagEnv{db: db, stores: stores, reg: runtime.NewRegistry(), svc: svc, logDir: t.TempDir()}
}

func (e *diagEnv) diag() *service.DiagnosticsService {
	return service.NewDiagnosticsService(e.db, e.stores, e.reg, "sqlite", e.logDir, "test")
}

func (e *diagEnv) byName(res *service.DiagnosticsResult) map[string]service.CheckResult {
	m := make(map[string]service.CheckResult, len(res.Checks))
	for _, c := range res.Checks {
		m[c.Name] = c
	}
	return m
}

func TestDiagnosticsBasic(t *testing.T) {
	e := newDiagEnv(t)
	res := e.diag().Run(context.Background())
	by := e.byName(res)
	// 7 чек'ов
	if len(res.Checks) != 7 {
		t.Fatalf("checks = %d, want 7", len(res.Checks))
	}
	if by["daemon"].Status != service.CheckOK {
		t.Fatalf("daemon = %s, want ok", by["daemon"].Status)
	}
	if by["database.ping"].Status != service.CheckOK {
		t.Fatalf("database.ping = %s, want ok", by["database.ping"].Status)
	}
	// свежая БД: все миграции применены
	if by["database.migrations"].Status != service.CheckOK {
		t.Fatalf("database.migrations = %s (%s), want ok", by["database.migrations"].Status, by["database.migrations"].Detail)
	}
	if by["logs.dir"].Status != service.CheckOK {
		t.Fatalf("logs.dir = %s, want ok", by["logs.dir"].Status)
	}
	// нет активных сессий
	if by["sessions.divergence"].Status != service.CheckOK {
		t.Fatalf("sessions.divergence = %s, want ok", by["sessions.divergence"].Status)
	}
	// нет git-проектов
	if by["git"].Status != service.CheckOK {
		t.Fatalf("git = %s, want ok (no projects)", by["git"].Status)
	}
	if res.Overall != service.CheckOK {
		t.Fatalf("overall = %s, want ok", res.Overall)
	}
	if res.GeneratedAt == "" {
		t.Fatal("generated_at empty")
	}
}

// Divergence: БД говорит active, а адаптер — мёртв → fail.
func TestDiagnosticsDivergenceStale(t *testing.T) {
	e := newDiagEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "diag-div")
	// active-сессия с dead runtime_ref (process) → адаптер скажет Alive=false
	if err := e.stores.Sessions.Create(context.Background(), e.db, &models.Session{
		TeamID: teamID, RoleID: roleA,
		RuntimeType: "process", State: models.SessionRunning, RuntimeRef: "999999999",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	res := e.diag().Run(context.Background())
	by := e.byName(res)
	if by["sessions.divergence"].Status != service.CheckFail {
		t.Fatalf("sessions.divergence = %s (%s), want fail", by["sessions.divergence"].Status, by["sessions.divergence"].Detail)
	}
	if res.Overall != service.CheckFail {
		t.Fatalf("overall = %s, want fail", res.Overall)
	}
}

// Migrations: не все применены → warn.
func TestDiagnosticsMigrationPending(t *testing.T) {
	e := newDiagEnv(t)
	// уберём одну применённую миграцию → applied < available
	if _, err := e.db.Exec(`DELETE FROM schema_migrations WHERE version = 1`); err != nil {
		t.Fatal(err)
	}
	res := e.diag().Run(context.Background())
	by := e.byName(res)
	if by["database.migrations"].Status != service.CheckWarn {
		t.Fatalf("database.migrations = %s (%s), want warn", by["database.migrations"].Status, by["database.migrations"].Detail)
	}
}

// Git: реальный git-проект → ok (checked, branch).
func TestDiagnosticsGitRepo(t *testing.T) {
	e := newDiagEnv(t)
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "t@t")
	runGit("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-q", "-m", "init")
	// добавляем проект (git=true)
	if err := e.stores.Projects.Create(context.Background(), e.db, &models.Project{
		Name: "diag-repo", Path: repo, Kind: models.ProjectExisting, Git: true,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	res := e.diag().Run(context.Background())
	by := e.byName(res)
	if by["git"].Status != service.CheckOK {
		t.Fatalf("git = %s (%s), want ok", by["git"].Status, by["git"].Detail)
	}
	if ev, _ := by["git"].Evidence["checked"].(int); ev != 1 {
		t.Fatalf("git.checked = %v, want 1", by["git"].Evidence["checked"])
	}
}

// Logs: несуществующий dir → fail.
func TestDiagnosticsLogsDirMissing(t *testing.T) {
	e := newDiagEnv(t)
	e.logDir = filepath.Join(t.TempDir(), "does-not-exist")
	res := e.diag().Run(context.Background())
	by := e.byName(res)
	if by["logs.dir"].Status != service.CheckFail {
		t.Fatalf("logs.dir = %s, want fail", by["logs.dir"].Status)
	}
	if res.Overall != service.CheckFail {
		t.Fatalf("overall = %s, want fail", res.Overall)
	}
}
