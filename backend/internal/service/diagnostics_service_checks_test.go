package service

import (
	"context"
	"database/sql"
	"os"
	"syscall"
	"testing"

	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
)

// ---------- P1.8: per-check тесты (каждый check — отдельная функция) ----------
// Внутренний файл (package service), чтобы вызывать unexported check-функции напрямую.

func newDiagSvc(t *testing.T, logDir string) (*sql.DB, *DiagnosticsService) {
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
	reg := runtime.NewRegistry()
	svc := NewDiagnosticsService(db, stores, reg, "sqlite", logDir, "test")
	return db, svc
}

func TestCheckDaemon(t *testing.T) {
	_, svc := newDiagSvc(t, t.TempDir())
	c := svc.checkDaemon()
	if c.Name != "daemon" || c.Status != CheckOK {
		t.Fatalf("daemon = %+v, want ok", c)
	}
	if c.Evidence["version"] != "test" {
		t.Fatalf("daemon.version = %v, want test", c.Evidence["version"])
	}
	if c.Evidence["uptime_s"] == nil {
		t.Fatal("daemon.uptime_s missing")
	}
}

func TestCheckDatabasePing(t *testing.T) {
	db, svc := newDiagSvc(t, t.TempDir())
	if c := svc.checkDatabasePing(context.Background()); c.Status != CheckOK {
		t.Fatalf("db.ping = %+v, want ok", c)
	}
	// закрытая БД → fail
	_ = db.Close()
	if c := svc.checkDatabasePing(context.Background()); c.Status != CheckFail {
		t.Fatalf("db.ping (closed) = %+v, want fail", c)
	}
}

// Tmux — зависимо от окружения (ok если сервер, warn если нет/не установлен).
// Assert: статус валиден + имя корректно (не panic, read-only).
func TestCheckTmux(t *testing.T) {
	_, svc := newDiagSvc(t, t.TempDir())
	c := svc.checkTmux(context.Background())
	if c.Name != "tmux.server" {
		t.Fatalf("name = %s, want tmux.server", c.Name)
	}
	if c.Status != CheckOK && c.Status != CheckWarn {
		t.Fatalf("tmux.status = %s, want ok|warn", c.Status)
	}
	if c.Detail == "" {
		t.Fatal("tmux.detail empty")
	}
}

func TestCheckLogsDir(t *testing.T) {
	// ok: существующий dir
	_, svc := newDiagSvc(t, t.TempDir())
	if c := svc.checkLogsDir(); c.Status != CheckOK {
		t.Fatalf("logs (exists) = %+v, want ok", c)
	}
	// warn: пустой path
	_, svcEmpty := newDiagSvc(t, "")
	if c := svcEmpty.checkLogsDir(); c.Status != CheckWarn {
		t.Fatalf("logs (empty path) = %+v, want warn", c)
	}
	// fail: несуществующий dir
	_, svcMissing := newDiagSvc(t, t.TempDir()+"/nope")
	if c := svcMissing.checkLogsDir(); c.Status != CheckFail {
		t.Fatalf("logs (missing) = %+v, want fail", c)
	}
}

func TestCheckGitNoProjects(t *testing.T) {
	_, svc := newDiagSvc(t, t.TempDir())
	c := svc.checkGit(context.Background())
	if c.Name != "git" || c.Status != CheckOK {
		t.Fatalf("git (no projects) = %+v, want ok", c)
	}
}

// checkSessionDivergence: registry nil → warn (liveness not checked), не падает.
func TestCheckSessionDivergenceRegistryNil(t *testing.T) {
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	stores := repository.NewStores(db)
	svc := NewDiagnosticsService(db, stores, nil, "sqlite", t.TempDir(), "test")
	c := svc.checkSessionDivergence(context.Background())
	if c.Name != "sessions.divergence" || c.Status != CheckWarn {
		t.Fatalf("divergence (registry nil) = %+v, want warn", c)
	}
}

// ---------- P1.8: fail → Attention (ReportFail) ----------

// ReportFail: fail-чек (logs.dir missing) → создаёт ОДИН unread diagnostics_fail (critical).
func TestReportFailCreatesEvent(t *testing.T) {
	db, svc := newDiagSvc(t, t.TempDir()+"/nope") // logs.dir → fail
	res := svc.Run(context.Background())
	if len(res.FailChecks()) == 0 {
		t.Fatal("expected fail check(s)")
	}
	id, err := svc.ReportFail(context.Background(), res)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected created event id, got 0")
	}
	evs, _, err := svc.stores.Watchdog.List(context.Background(), db, repository.AlertFilter{UnreadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evs {
		if e.EventType == "diagnostics_fail" && e.Severity == "critical" && !e.IsRead {
			found = true
		}
	}
	if !found {
		t.Fatal("no unread critical diagnostics_fail event")
	}
}

// ReportFail: дедуп — если unread diagnostics_fail уже есть, не дублирует.
func TestReportFailDedupe(t *testing.T) {
	_, svc := newDiagSvc(t, t.TempDir()+"/nope")
	res := svc.Run(context.Background())
	if id, err := svc.ReportFail(context.Background(), res); err != nil || id == 0 {
		t.Fatalf("first report: id=%d err=%v", id, err)
	}
	// второй раз → 0 (не дублирует)
	if id, err := svc.ReportFail(context.Background(), res); err != nil || id != 0 {
		t.Fatalf("second report: id=%d (want 0, dedup) err=%v", id, err)
	}
}

// ReportFail: без fail-чек'ов → событие не создаёт.
func TestReportFailNoFails(t *testing.T) {
	_, svc := newDiagSvc(t, t.TempDir()) // всё ok
	res := svc.Run(context.Background())
	if len(res.FailChecks()) != 0 {
		t.Fatalf("expected no fails, got %v", res.FailChecks())
	}
	if id, err := svc.ReportFail(context.Background(), res); err != nil || id != 0 {
		t.Fatalf("no-fails report: id=%d (want 0) err=%v", id, err)
	}
}

// EnsureAlive — helper-проверка (осуществительно): наш pid жив (для zombie-теста).
func TestOurPidAlive(t *testing.T) {
	if err := syscall.Kill(os.Getpid(), 0); err != nil {
		t.Fatalf("our pid not alive: %v", err)
	}
}
