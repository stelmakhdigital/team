package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

type sessionEnv struct {
	svc    *service.TeamService
	tsvc   *service.TaskService
	ssvc   *service.SessionService
	wd     *service.WatchdogService
	ctx    context.Context
	logDir string
}

func setupSessionEnv(t *testing.T) *sessionEnv {
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
	if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	logDir := t.TempDir()
	ssvc.LogsDir = filepath.Join(logDir, "logs")
	ssvc.ConfigsDir = filepath.Join(logDir, "configs")
	wd := service.NewWatchdogService(db, stores, service.WatchdogConfig{
		// отрицательные пороги в тестах: всё, что обновлено раньше "now+1h", считается stale/blocked
		StaleThreshold:   -time.Hour,
		BlockedThreshold: -time.Hour,
		ScanInterval:     time.Hour,
		Enabled:          false,
	})
	return &sessionEnv{svc: svc, tsvc: tsvc, ssvc: ssvc, wd: wd, ctx: context.Background(), logDir: logDir}
}

func (e *sessionEnv) teamWithRole(t *testing.T, name string) (teamID, roleID int64) {
	t.Helper()
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, name)
	_ = teamID
	return teamID, roleA
}

func TestSessionLifecycleProcess(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-life")

	// create: процесс живёт ~1с
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID:  roleID,
		Command: "sh",
		Args:    []string{"-c", "echo hello-daemon; sleep 1"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.State != models.SessionRunning {
		t.Fatalf("state after create = %s, want running", sess.State)
	}
	if sess.RuntimeRef == "" {
		t.Error("runtime_ref not set")
	}

	// вторая сессия той же роли → conflict
	if _, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: "true",
	}); err == nil {
		t.Error("second active session for role must fail")
	}

	// даём процессу время вывести echo до stop
	time.Sleep(150 * time.Millisecond)

	// stop → stopped
	stopped, err := e.ssvc.StopSession(e.ctx, sess.ID)
	if err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if stopped.State != models.SessionStopped {
		t.Fatalf("state after stop = %s", stopped.State)
	}

	// история: created, started, stopped
	hist, total, err := e.ssvc.GetSessionHistory(e.ctx, sess.ID, 100, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if total != 3 || len(hist) != 3 {
		t.Errorf("history total = %d (len %d), want 3", total, len(hist))
	}
	if hist[2].ToState != "stopped" {
		t.Errorf("last history entry = %s, want stopped", hist[2].ToState)
	}

	// transcript содержит вывод процесса
	tr, _, _, err := e.ssvc.GetTranscript(e.ctx, sess.ID, 100)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	found := false
	for _, line := range tr {
		if line.Content == "hello-daemon" {
			found = true
		}
	}
	if !found {
		t.Errorf("transcript missing process output: %+v", tr)
	}
}

func TestSessionReapExitedProcess(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-reap")

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: "sh", Args: []string{"-c", "exit 0"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// даём процессу завершиться
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, _ := e.ssvc.Reap(e.ctx)
		_ = n
		view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
		if err == nil && (view.State == "stopped" || view.State == "failed") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.State != "stopped" {
		t.Fatalf("state after reap = %s (exit code %v), want stopped", view.State, view.ExitCode)
	}
}

// Регресс (отчёт frontend): процесс упал (exit != 0) и user-stop пришёл ПОСЛЕ
// падения — состояние должно быть failed, а не stopped.
func TestSessionCrashThenUserStop(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-crash-stop")
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: "sh", Args: []string{"-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// ждём фиксации exit code адаптером (процесс уже мёртв)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = e.ssvc.Reap(e.ctx)
		view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
		if err == nil && view.State == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// user-stop после crash (идемпотентно для terminal-state → 409/200 — не критично)
	_, _ = e.ssvc.StopSession(e.ctx, sess.ID)
	view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.State != "failed" {
		t.Fatalf("state after crash+stop = %s, want failed", view.State)
	}
	if view.ExitCode == nil || *view.ExitCode != 7 {
		t.Errorf("exit code = %v, want 7", view.ExitCode)
	}
}

func TestSessionFailedProcessAndWatchdog(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-fail")

	// связываем с задачей
	task, err := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleID, Title: "for failed session"})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, QueueTaskID: &task.ID,
		Command: "sh", Args: []string{"-c", "exit 3"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// задача pending → in_progress (создание сессии)
	detail, err := e.tsvc.GetTask(e.ctx, task.ID)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if detail.Task.State != "in_progress" {
		t.Errorf("task state = %s, want in_progress after session start", detail.Task.State)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = e.ssvc.Reap(e.ctx)
		view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
		if err == nil && view.State == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	view, _ := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if view.State != "failed" {
		t.Fatalf("state = %s, want failed", view.State)
	}
	if view.ExitCode == nil || *view.ExitCode != 3 {
		t.Errorf("exit code = %v, want 3", view.ExitCode)
	}

	// watchdog: drift-алерт на failed сессию
	n := e.wd.Scan(e.ctx)
	if n == 0 {
		t.Fatal("watchdog scan created no alerts")
	}
	// повторный скан — дедупликация (нет непрочитанного drift на эту сессию)
	n = e.wd.Scan(e.ctx)
	if n != 0 {
		t.Errorf("watchdog duplicate alerts: %d", n)
	}
}

func TestWatchdogStaleAndBlocked(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-wd")

	// stale: in_progress давно
	t1, err := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleID, Title: "stale"})
	if err != nil {
		t.Fatalf("task1: %v", err)
	}
	if _, err := e.tsvc.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: t1.ID, State: models.TaskInProgress}); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	// blocked давно
	t2, err := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleID, Title: "blocked"})
	if err != nil {
		t.Fatalf("task2: %v", err)
	}
	if _, err := e.tsvc.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: t2.ID, State: models.TaskBlocked}); err != nil {
		t.Fatalf("blocked: %v", err)
	}

	n := e.wd.Scan(e.ctx)
	if n < 2 {
		t.Fatalf("watchdog created %d alerts, want >= 2 (stale + blocked)", n)
	}
	// дедупликация
	if n := e.wd.Scan(e.ctx); n != 0 {
		t.Errorf("duplicate alerts: %d", n)
	}
}

func TestCreateSessionValidation(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-val")

	// без команды (process)
	if _, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleID}); err == nil {
		t.Error("missing command must fail")
	}
	// container не поддерживается
	if _, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: "x", RuntimeType: "container"}); err == nil {
		t.Error("container runtime must fail")
	}
	// чужая команда
	_, otherRole := e.teamWithRole(t, "s-val-2")
	if _, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: otherRole, Command: "true"}); err == nil {
		t.Error("role from another team must fail")
	}
}
