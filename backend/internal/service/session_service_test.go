package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	db     *sql.DB
	stores *repository.Stores
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
	ssvc.WatchdogEvents = stores.Watchdog
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
	return &sessionEnv{svc: svc, tsvc: tsvc, ssvc: ssvc, wd: wd, ctx: context.Background(), logDir: logDir, db: db, stores: stores}
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

	// T2: Reap (reaper-loop) создаёт drift-алерт при обнаружении мёртвого (failed) runtime.
	countDrift := func() int {
		var n int
		if err := e.db.QueryRowContext(e.ctx, "SELECT count(*) FROM watchdog_events WHERE event_type='drift'").Scan(&n); err != nil {
			t.Fatalf("count drift: %v", err)
		}
		return n
	}
	if c := countDrift(); c != 1 {
		t.Fatalf("drift alerts after reap = %d, want 1 (Reap creates it on dead-runtime detection)", c)
	}
	// повторный скан watchdog — дедупликация (не дублирует drift-алерт Reap)
	_ = e.wd.Scan(e.ctx)
	if c := countDrift(); c != 1 {
		t.Fatalf("drift alerts after watchdog scan = %d, want 1 (dedup)", c)
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

// ---------- slice 7: session.output (WS live-терминал) ----------

// TestSessionOutputEvents — тейлер transcript-лога: батчи session.output
// (≤500ms, только при новых строках), каналы session:{id}+dashboard,
// тишина после stop.
func TestSessionOutputEvents(t *testing.T) {
	e := setupSessionEnv(t)
	bus := service.NewEventBus()
	e.ssvc.Bus = bus
	teamID, roleID := e.teamWithRole(t, "s-out")

	ch, unsub := bus.Subscribe()
	defer unsub()

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID:  roleID,
		Command: "sh",
		Args:    []string{"-c", "echo l1; sleep 0.6; echo l2; sleep 5"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	wantCh := fmt.Sprintf("session:%d", sess.ID)
	var texts []string
	deadline := time.Now().Add(6 * time.Second)
	for len(texts) < 2 && time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			if ev.Type != "session.output" {
				continue
			}
			data, _ := ev.Data.(map[string]any)
			if data == nil {
				t.Fatal("session.output data nil")
			}
			if sid, _ := data["session_id"].(int64); sid != sess.ID {
				t.Fatalf("session_id = %v", data["session_id"])
			}
			channels := map[string]bool{}
			for _, c := range ev.Channels {
				channels[c] = true
			}
			if !channels[wantCh] || !channels["dashboard"] {
				t.Fatalf("channels = %v, want %s + dashboard", ev.Channels, wantCh)
			}
			lines, _ := data["lines"].([]any)
			if len(lines) == 0 {
				t.Fatal("empty lines batch")
			}
			for _, l := range lines {
				lm, _ := l.(map[string]any)
				if lm["stream"] != "stdout" {
					t.Fatalf("stream = %v", lm["stream"])
				}
				if _, ok := lm["ts"].(string); !ok || lm["ts"] == "" {
					t.Fatalf("ts missing: %v", lm)
				}
				texts = append(texts, fmt.Sprint(lm["text"]))
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "l1") || !strings.Contains(joined, "l2") {
		t.Fatalf("output lines missing: %q", joined)
	}

	// stop → тейлер остановлен, новых session.output нет
	if _, err := e.ssvc.StopSession(e.ctx, sess.ID); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	silence := time.After(1500 * time.Millisecond) // ≥3 тика
waitLoop:
	for {
		select {
		case ev := <-ch:
			if ev.Type == "session.output" {
				t.Fatalf("session.output after stop: %v", ev.Data)
			}
		case <-silence:
			break waitLoop
		}
	}
}

// TestSessionLiveMetricsView — live-поля в SessionView: log_path всегда,
// context-поля из JSONL usage (omit без usage), model — только pi.
func TestSessionLiveMetricsView(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-live")

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID:  roleID,
		Command: "sh",
		Args: []string{"-c",
			`printf '{"message":{"usage":{"input_tokens":200,"cache_read_input_tokens":0,"output_tokens":15}}}\n'; sleep 1`},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSessionView: %v", err)
	}
	if view.LogPath == "" {
		t.Fatal("log_path empty")
	}
	if view.Model != "" {
		t.Fatalf("model = %q, want empty for process runtime", view.Model)
	}
	if view.ContextTotalInputTokens == nil || *view.ContextTotalInputTokens != 200 {
		t.Fatalf("total_input = %v", view.ContextTotalInputTokens)
	}
	if view.ContextTotalOutputTokens == nil || *view.ContextTotalOutputTokens != 15 {
		t.Fatalf("total_output = %v", view.ContextTotalOutputTokens)
	}
	if view.ContextUsedPercentage == nil {
		t.Fatal("pct nil")
	}
	if d := *view.ContextUsedPercentage - 0.1; d > 1e-9 || d < -1e-9 {
		t.Fatalf("pct = %v, want 0.1", *view.ContextUsedPercentage)
	}
	if _, err := e.ssvc.StopSession(e.ctx, sess.ID); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
}

// TestSessionLiveMetricsNoUsage — без JSONL usage в логе → context-поля omit.
func TestSessionLiveMetricsNoUsage(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-live2")

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID:  roleID,
		Command: "sh",
		Args:    []string{"-c", "echo plain-output; sleep 0.5"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	view, err := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSessionView: %v", err)
	}
	if view.ContextUsedPercentage != nil || view.ContextTotalInputTokens != nil || view.ContextTotalOutputTokens != nil {
		t.Fatalf("no usage => context fields must be omit: %+v", view)
	}
	if _, err := e.ssvc.StopSession(e.ctx, sess.ID); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
}

// ---------- T2: dead-runtime detection (drift-алерт + корректное событие) ----------

func driftAlertCount(t *testing.T, e *sessionEnv) int {
	t.Helper()
	var n int
	if err := e.db.QueryRowContext(e.ctx, "SELECT count(*) FROM watchdog_events WHERE event_type='drift'").Scan(&n); err != nil {
		t.Fatalf("count drift: %v", err)
	}
	return n
}

// T2: мёртвый runtime (crash, exit != 0) → failed + drift-алерт (Reap) + событие
// session.failed (не session.stopped).
func TestSessionDeadRuntimeCrash(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-dead-crash")

	bus := service.NewEventBus()
	var eventTypes []string
	bus.SetSink(func(ev service.WSEvent) { eventTypes = append(eventTypes, ev.Type) })
	e.ssvc.Bus = bus

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: "sh", Args: []string{"-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = e.ssvc.Reap(e.ctx)
		if v, err := e.ssvc.GetSessionView(e.ctx, sess.ID); err == nil && v.State == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	view, _ := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if view.State != "failed" {
		t.Fatalf("state = %s, want failed", view.State)
	}
	if view.ExitCode == nil || *view.ExitCode != 7 {
		t.Fatalf("exit = %v, want 7", view.ExitCode)
	}
	// drift-алерт (Reap создаёт при обнаружении мёртвого runtime)
	if c := driftAlertCount(t, e); c != 1 {
		t.Errorf("drift alerts = %d, want 1", c)
	}
	// событие session.failed (не session.stopped)
	found := false
	for _, et := range eventTypes {
		if et == "session.failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("no session.failed event, got %v", eventTypes)
	}
}

// T2: чистое завершение (exit 0, не user-stop) → stopped + drift-алерт + событие
// session.stopped.
func TestSessionDeadRuntimeCleanExit(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleID := e.teamWithRole(t, "s-dead-clean")

	bus := service.NewEventBus()
	var eventTypes []string
	bus.SetSink(func(ev service.WSEvent) { eventTypes = append(eventTypes, ev.Type) })
	e.ssvc.Bus = bus

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: "sh", Args: []string{"-c", "exit 0"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = e.ssvc.Reap(e.ctx)
		if v, err := e.ssvc.GetSessionView(e.ctx, sess.ID); err == nil && v.State == "stopped" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	view, _ := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if view.State != "stopped" {
		t.Fatalf("state = %s, want stopped", view.State)
	}
	if c := driftAlertCount(t, e); c != 1 {
		t.Errorf("drift alerts = %d, want 1", c)
	}
	found := false
	for _, et := range eventTypes {
		if et == "session.stopped" {
			found = true
		}
	}
	if !found {
		t.Errorf("no session.stopped event, got %v", eventTypes)
	}
}

// ---------- T2: transcript — pi DirectTTY (on-the-fly capture) ----------

// mockCaptureAdapter — тестовый адаптер: Capture возвращает заданные строки.
type mockCaptureAdapter struct{ lines []string }

func (m *mockCaptureAdapter) Name() string { return "mock" }
func (m *mockCaptureAdapter) Start(ctx context.Context, spec runtime.Spec) (string, error) {
	return "mock-ref", nil
}
func (m *mockCaptureAdapter) Stop(ctx context.Context, ref string) error { return nil }
func (m *mockCaptureAdapter) Status(ctx context.Context, ref string) (*runtime.Status, error) {
	return &runtime.Status{Alive: true, State: runtime.StateRunning}, nil
}
func (m *mockCaptureAdapter) Capture(ctx context.Context, ref string, lines int) ([]string, error) {
	return m.lines, nil
}
func (m *mockCaptureAdapter) Send(ctx context.Context, ref, text string, submit bool) (runtime.SendResult, error) {
	return runtime.SendResult{}, runtime.ErrNoScreen
}

// T2: пустой лог-файл (pi DirectTTY) → GetTranscript делает on-the-fly capture.
func TestTranscriptCaptureOnEmptyLog(t *testing.T) {
	e := setupTranscriptEnv(t, &mockCaptureAdapter{lines: []string{"screen-line-1", "screen-line-2"}})
	teamID, roleID := e.teamWithRole(t, "s-t2-capture")

	sess := &models.Session{
		TeamID: teamID, RoleID: roleID,
		RuntimeType: "other", RuntimeRef: "mock-ref",
		State: models.SessionRunning, WorkingDir: "/tmp",
	}
	if err := e.stores.Sessions.Create(e.ctx, e.db, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	tr, total, _, err := e.ssvc.GetTranscript(e.ctx, sess.ID, 100)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if total != 2 || len(tr) != 2 {
		t.Fatalf("total = %d (len %d), want 2 (captured lines)", total, len(tr))
	}
	if tr[0].Content != "screen-line-1" || tr[1].Content != "screen-line-2" {
		t.Errorf("captured lines = %q/%q, want screen-line-1/2", tr[0].Content, tr[1].Content)
	}
}

// T2: НЕ дублировать — если лог-файл уже есть, читать его (capture не вызывается).
func TestTranscriptLogFileTakesPrecedence(t *testing.T) {
	var captured bool
	e := setupTranscriptEnv(t, &captureRecordingAdapter{
		inner:    &mockCaptureAdapter{lines: []string{"SHOULD-NOT-APPEAR"}},
		captured: &captured,
	})
	teamID, roleID := e.teamWithRole(t, "s-t2-prec")

	sess := &models.Session{
		TeamID: teamID, RoleID: roleID,
		RuntimeType: "other", RuntimeRef: "mock-ref",
		State: models.SessionRunning, WorkingDir: "/tmp",
	}
	if err := e.stores.Sessions.Create(e.ctx, e.db, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// пишем непустой лог-файл
	if err := os.MkdirAll(e.ssvc.LogsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(e.ssvc.LogsDir, fmt.Sprintf("session-%d.log", sess.ID)),
		[]byte("log-line-1\nlog-line-2\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	tr, total, _, err := e.ssvc.GetTranscript(e.ctx, sess.ID, 100)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (log lines)", total)
	}
	if tr[0].Content != "log-line-1" {
		t.Errorf("line = %q, want log-line-1 (from log-file, not capture)", tr[0].Content)
	}
	if captured {
		t.Error("capture was called, want NOT called (log-file has content)")
	}
}

// setupTranscriptEnv — окружение с кастомным runtime-registry (мок-адаптер под
// тип "mock") для тестов transcript-capture.
func setupTranscriptEnv(t *testing.T, mock runtime.Adapter) *sessionEnv {
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
	runt := runtime.NewRegistry()
	runt.SetOverride("other", mock)
	ssvc := service.NewSessionService(db, stores, runt)
	logDir := t.TempDir()
	ssvc.LogsDir = filepath.Join(logDir, "logs")
	ssvc.ConfigsDir = filepath.Join(logDir, "configs")
	return &sessionEnv{
		svc: svc, tsvc: service.NewTaskService(db, stores), ssvc: ssvc,
		ctx: context.Background(), db: db, stores: stores,
	}
}

// captureRecordingAdapter — обёртка, которая фиксирует вызов Capture.
type captureRecordingAdapter struct {
	inner    *mockCaptureAdapter
	captured *bool
}

func (c *captureRecordingAdapter) Name() string { return c.inner.Name() }
func (c *captureRecordingAdapter) Start(ctx context.Context, spec runtime.Spec) (string, error) {
	return c.inner.Start(ctx, spec)
}
func (c *captureRecordingAdapter) Stop(ctx context.Context, ref string) error {
	return c.inner.Stop(ctx, ref)
}
func (c *captureRecordingAdapter) Status(ctx context.Context, ref string) (*runtime.Status, error) {
	return c.inner.Status(ctx, ref)
}
func (c *captureRecordingAdapter) Capture(ctx context.Context, ref string, lines int) ([]string, error) {
	*c.captured = true
	return c.inner.Capture(ctx, ref, lines)
}
func (c *captureRecordingAdapter) Send(ctx context.Context, ref, text string, submit bool) (runtime.SendResult, error) {
	return c.inner.Send(ctx, ref, text, submit)
}

// TestSessionLaunchAppliesRoleConfigOverride — T4: role.Config (pi_config override)
// переносится в session config при запуске (model/temperature/max_tokens/thinking),
// layout НЕ переносится. Merge runtime-agnostic (проверяем на process).
func TestSessionLaunchAppliesRoleConfigOverride(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "t4-rolecfg")

	_, err := e.svc.UpdateRoleConfig(e.ctx, service.UpdateRoleConfigRequest{
		ID: roleA,
		Config: &map[string]any{
			"model":       "test-model",
			"temperature": 0.42,
			"max_tokens":  1234,
			"thinking":    "high",
		},
	})
	if err != nil {
		t.Fatalf("UpdateRoleConfig: %v", err)
	}

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleA, Command: "sh", Args: []string{"-c", "echo hi"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err2 := e.ssvc.StopSession(e.ctx, sess.ID); err2 != nil {
		t.Fatalf("StopSession: %v", err2)
	}

	if got, _ := sess.Config["model"].(string); got != "test-model" {
		t.Errorf("session config model = %v, want test-model", sess.Config["model"])
	}
	if got, _ := sess.Config["thinking"].(string); got != "high" {
		t.Errorf("session config thinking = %v, want high", sess.Config["thinking"])
	}
	if got, ok := asFloat64(sess.Config["temperature"]); !ok || got != 0.42 {
		t.Errorf("session config temperature = %v, want 0.42", sess.Config["temperature"])
	}
	if got, ok := asFloat64(sess.Config["max_tokens"]); !ok || got != 1234 {
		t.Errorf("session config max_tokens = %v, want 1234", sess.Config["max_tokens"])
	}
	// layout НЕ переносится в session config (позиция на канвасе)
	if _, ok := sess.Config["layout"]; ok {
		t.Error("layout must not be merged into session config")
	}
}

// asFloat64 — int/float64 → float64 (JSON-число после DB roundtrip = float64).
func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// capturePiAdapter — детерминированный pi-адаптер: захватывает Spec (Pi-флаги) при Start.
type capturePiAdapter struct {
	mu   sync.Mutex
	spec *runtime.Spec
}

func (a *capturePiAdapter) Name() string { return "pi" }
func (a *capturePiAdapter) Start(ctx context.Context, spec runtime.Spec) (string, error) {
	a.mu.Lock()
	a.spec = &spec
	a.mu.Unlock()
	return "pi-ref", nil
}
func (a *capturePiAdapter) Stop(ctx context.Context, ref string) error { return nil }
func (a *capturePiAdapter) Status(ctx context.Context, ref string) (*runtime.Status, error) {
	return &runtime.Status{Alive: true}, nil
}
func (a *capturePiAdapter) Capture(ctx context.Context, ref string, lines int) ([]string, error) {
	return nil, runtime.ErrNoScreen
}
func (a *capturePiAdapter) Send(ctx context.Context, ref, text string, submit bool) (runtime.SendResult, error) {
	return runtime.SendResult{}, runtime.ErrNoScreen
}
func (a *capturePiAdapter) pi() *runtime.AgentSpecFields {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.spec == nil {
		return nil
	}
	return a.spec.Pi
}

// TestSessionLaunchMergesAgentResources — T5: base (spec) + override (role.Config[
// "agent_resources"]) → pi-флаги/конфиг. Новый скилл/плагин/startup-файл из override
// попадает в piFields (skills → --skill, startup orientation=system → system_prompt,
// plugins → конфиг сессии). Override перекрывает base по key.
func TestSessionLaunchMergesAgentResources(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "t5-agres")

	// base (spec) — через RoleSpecFunc (детерминированный)
	e.ssvc.RoleSpecFunc = func(specPath string) *service.AgentSpec {
		return &service.AgentSpec{
			Available: true,
			PiConfig: service.SpecPiConfig{
				Model:   "base-model",
				Skills:  []service.SpecSkill{{Path: "skills/base.md", Enabled: true}},
				Plugins: []service.SpecPlugin{{Name: "base-plugin", Enabled: true}},
			},
			Startup: service.SpecStartup{
				Files: []service.SpecStartupFile{{Path: "startup/base.md", Orientation: "context"}},
			},
		}
	}
	// детерминированный pi-адаптер (захват Spec)
	piMock := &capturePiAdapter{}
	e.ssvc.SetTestRuntimeOverride("pi", piMock)

	// override (agent_resources): новый скилл + disable base-скилл + новый плагин +
	// новый startup-файл (orientation=system)
	_, err := e.svc.UpdateRoleConfig(e.ctx, service.UpdateRoleConfigRequest{
		ID: roleA,
		Config: &map[string]any{
			"agent_resources": map[string]any{
				"skills": []any{
					map[string]any{"path": "skills/base.md", "enabled": false},
					map[string]any{"path": "skills/override.md", "enabled": true},
				},
				"plugins": []any{
					map[string]any{"name": "override-plugin", "enabled": true},
				},
				"startup_files": []any{
					map[string]any{"path": "startup/sys.md", "orientation": "system"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateRoleConfig: %v", err)
	}

	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleA, RuntimeType: "pi",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err2 := e.ssvc.StopSession(e.ctx, sess.ID); err2 != nil {
		t.Fatalf("StopSession: %v", err2)
	}

	pi := piMock.pi()
	if pi == nil {
		t.Fatal("pi adapter was not invoked (spec not captured)")
	}
	// skills: merged (base.md disabled by override) → enabled = [override.md]
	if len(pi.Skills) != 1 || pi.Skills[0] != "skills/override.md" {
		t.Errorf("merged skills = %v, want [skills/override.md]", pi.Skills)
	}
	// startup files: merged (base.md context + sys.md system); system → system_prompt
	if !strings.HasPrefix(pi.SystemPrompt, "## Startup files (system)") {
		t.Errorf("system_prompt must start with startup header, got %q", pi.SystemPrompt)
	}
	if !strings.Contains(pi.SystemPrompt, "startup/sys.md") {
		t.Errorf("system_prompt must contain startup/sys.md, got %q", pi.SystemPrompt)
	}
	if len(pi.StartupFiles) != 2 {
		t.Errorf("merged startup files = %d, want 2", len(pi.StartupFiles))
	}
	// plugins: merged (base-plugin + override-plugin) → в конфиг сессии
	ps, ok := sess.Config["plugins"].([]map[string]any)
	if !ok || len(ps) != 2 {
		// roundtrip: JSON → []any; проверяем длину через any
		psAny, _ := sess.Config["plugins"].([]any)
		if len(psAny) != 2 {
			t.Errorf("merged plugins = %v, want 2", sess.Config["plugins"])
		}
	} else {
		names := map[string]bool{}
		for _, p := range ps {
			names[p["name"].(string)] = true
		}
		if !names["base-plugin"] || !names["override-plugin"] {
			t.Errorf("merged plugins must include base + override, got %v", ps)
		}
	}
}
