package service_test

import (
	"context"
	"testing"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- P1.6: watchdog-джобы (service) ----------

// wdEnv — setupSessionEnv + подключённый watchdog-джоб loop + job-сервис.
type wdEnv struct {
	e    *sessionEnv
	jobs *service.WatchdogJobService
}

func setupWd(t *testing.T) *wdEnv {
	t.Helper()
	e := setupSessionEnv(t)
	// подключаем loop (context-threshold) + job-сервис
	e.wd.Jobs = e.stores.WatchdogJobs
	e.wd.JobEvents = e.stores.WatchdogJobEvents
	e.wd.LiveSession = e.ssvc.GetSessionView
	e.wd.FireWatchdogWakes = e.tsvc.FireArmedWatchdogWakes
	jobs := service.NewWatchdogJobService(e.db, e.stores)
	return &wdEnv{e: e, jobs: jobs}
}

// liveSessionAt — живая сессия с context = pct% (через transcript).
func (w *wdEnv) liveSessionAt(t *testing.T, teamID, roleID int64, pct int) int64 {
	t.Helper()
	sess, err := w.e.ssvc.CreateSession(w.e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleID, Command: sleepScript(t),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _, _ = w.e.ssvc.StopSession(context.Background(), sess.ID) })
	writeUsageTranscript(t, w.e.ssvc.LogsDir, sess.ID, pct*2000) // pct% от окна 200000
	return sess.ID
}

func intp(v int) *int { return &v }

// Test W1: register-валидация.
func TestWatchdogJobRegisterValidation(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-reg")
	sessID := w.liveSessionAt(t, teamID, roleA, 90)

	// name обязателен
	if _, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		TargetSessionID: &sessID, ThresholdPct: intp(80),
	}, "u"); err == nil {
		t.Fatal("empty name: want 400")
	}
	// неверный policy
	if _, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "x", Policy: "cpu-threshold", TargetSessionID: &sessID, ThresholdPct: intp(80),
	}, "u"); err == nil {
		t.Fatal("unsupported policy: want 400")
	}
	// нет target_session_id
	if _, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "x", ThresholdPct: intp(80),
	}, "u"); err == nil {
		t.Fatal("missing target_session_id: want 400")
	}
	// нет threshold_pct
	if _, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "x", TargetSessionID: &sessID,
	}, "u"); err == nil {
		t.Fatal("missing threshold_pct: want 400")
	}
	// threshold out of range
	if _, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "x", TargetSessionID: &sessID, ThresholdPct: intp(0),
	}, "u"); err == nil {
		t.Fatal("threshold 0: want 400")
	}
	// сессия не существует (404)
	missing := int64(99999)
	if _, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "x", TargetSessionID: &missing, ThresholdPct: intp(80),
	}, "u"); err == nil {
		t.Fatal("missing session: want 404")
	}
}

// Test W2: register-успех + поля.
func TestWatchdogJobRegisterSuccess(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-reg-ok")
	sessID := w.liveSessionAt(t, teamID, roleA, 90)
	view, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx-threshold", TargetSessionID: &sessID, ThresholdPct: intp(80), Message: "context high",
	}, "operator:abc")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if view.ID == 0 || !view.Enabled {
		t.Fatalf("view = %+v, want id>0 + enabled", view)
	}
	if view.Policy != models.WatchdogJobPolicyContextUsageThreshold {
		t.Fatalf("policy = %q", view.Policy)
	}
	if view.CreatedBy != "operator:abc" {
		t.Fatalf("created_by = %q", view.CreatedBy)
	}
}

// Test W3: loop-fire (порог) → алерт + fired_count + event.
func TestWatchdogJobLoopFire(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-fire")
	sessID := w.liveSessionAt(t, teamID, roleA, 90) // 90% >= 80
	job, err := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx", TargetSessionID: &sessID, ThresholdPct: intp(80), Message: "context high",
	}, "u")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	w.e.wd.Scan(w.e.ctx) // один проход

	detail, err := w.jobs.Get(w.e.ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.FiredCount != 1 {
		t.Fatalf("fired_count = %d, want 1", detail.FiredCount)
	}
	if detail.LastFiredPct == nil || *detail.LastFiredPct < 80 {
		t.Fatalf("last_fired_pct = %v, want >= 80", detail.LastFiredPct)
	}
	if len(detail.Recent) != 1 || detail.Recent[0].Outcome != "fired" {
		t.Fatalf("recent = %+v, want 1 fired", detail.Recent)
	}
	// алерт watchdog_job создан
	alerts, _, err := w.e.stores.Watchdog.List(w.e.ctx, w.e.db, repository.AlertFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	found := false
	for _, a := range alerts {
		if a.EventType == "watchdog_job" && a.SessionID != nil && *a.SessionID == sessID {
			found = true
		}
	}
	if !found {
		t.Fatalf("watchdog_job alert not created (alerts: %d)", len(alerts))
	}
}

// Test W4: дедуп — повторный scan без роста >5 п.т. не повторяет.
func TestWatchdogJobLoopDedup(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-dedup")
	sessID := w.liveSessionAt(t, teamID, roleA, 90)
	job, _ := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx", TargetSessionID: &sessID, ThresholdPct: intp(80),
	}, "u")

	w.e.wd.Scan(w.e.ctx) // fire (90%)
	// повторный scan: context всё ещё 90% (рост 0 <= 5) → quiet skip
	w.e.wd.Scan(w.e.ctx)
	d, _ := w.jobs.Get(w.e.ctx, job.ID)
	if d.FiredCount != 1 {
		t.Fatalf("fired_count = %d, want 1 (dedup: no >5pt growth)", d.FiredCount)
	}
	// рост >5 п.т. (90 → 96) → повторяется
	writeUsageTranscript(t, w.e.ssvc.LogsDir, sessID, 192000) // 96%
	w.e.wd.Scan(w.e.ctx)
	d, _ = w.jobs.Get(w.e.ctx, job.ID)
	if d.FiredCount != 2 {
		t.Fatalf("fired_count = %d, want 2 (growth 90→96 > 5)", d.FiredCount)
	}
}

// Test W5: dead-target — сессия остановлена → джоб enabled=false + event.
func TestWatchdogJobDeadTarget(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-dead")
	sess, err := w.e.ssvc.CreateSession(w.e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleA, Command: sleepScript(t),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, _ := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx", TargetSessionID: &sess.ID, ThresholdPct: intp(80),
	}, "u")

	// останавливаем сессию → dead-target
	if _, err := w.e.ssvc.StopSession(w.e.ctx, sess.ID); err != nil {
		t.Fatalf("stop session: %v", err)
	}
	w.e.wd.Scan(w.e.ctx)

	d, _ := w.jobs.Get(w.e.ctx, job.ID)
	if d.Enabled {
		t.Fatal("job still enabled after dead target")
	}
	if len(d.Recent) != 1 || d.Recent[0].Outcome != "dead-target" {
		t.Fatalf("recent = %+v, want 1 dead-target", d.Recent)
	}
}

// Test W6: stop → enabled=false (scheduler пропускает).
func TestWatchdogJobStop(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-stop")
	sessID := w.liveSessionAt(t, teamID, roleA, 90)
	job, _ := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx", TargetSessionID: &sessID, ThresholdPct: intp(80),
	}, "u")
	view, err := w.jobs.Stop(w.e.ctx, job.ID)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if view.Enabled {
		t.Fatal("job still enabled after stop")
	}
	// scheduler пропускает stopped-джоб: scan не создаёт алерт
	w.e.wd.Scan(w.e.ctx)
	d, _ := w.jobs.Get(w.e.ctx, job.ID)
	if d.FiredCount != 0 {
		t.Fatalf("stopped job fired (count=%d), want 0", d.FiredCount)
	}
}

// Test W7: delete — fired_count=0 → ок; fired_count>0 → 409.
func TestWatchdogJobDelete(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-del")
	sessID := w.liveSessionAt(t, teamID, roleA, 30) // ниже порога — не fires
	job, _ := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx", TargetSessionID: &sessID, ThresholdPct: intp(80),
	}, "u")
	// fired_count=0 → delete ок
	if err := w.jobs.Delete(w.e.ctx, job.ID); err != nil {
		t.Fatalf("delete (fired=0): %v", err)
	}
	if _, err := w.jobs.Get(w.e.ctx, job.ID); err == nil {
		t.Fatal("job still exists after delete")
	}
	// fired_count>0 → 409
	job2, _ := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx2", TargetSessionID: &sessID, ThresholdPct: intp(10),
	}, "u")
	w.e.wd.Scan(w.e.ctx) // fires (30% >= 10%)
	if err := w.jobs.Delete(w.e.ctx, job2.ID); err == nil {
		t.Fatal("delete (fired>0): want 409")
	}
}

// Test W8: fire → parked-задача с watchdog-wake разблокирована (P0.3-интеграция).
func TestWatchdogJobFireUnblocksWake(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-wake")
	sessID := w.liveSessionAt(t, teamID, roleA, 90)
	job, _ := w.jobs.Register(w.e.ctx, &service.RegisterWatchdogJobRequest{
		Name: "ctx", TargetSessionID: &sessID, ThresholdPct: intp(80),
	}, "u")
	// parked-задача с watchdog-wake на этот джоб
	task, err := w.e.tsvc.CreateTask(w.e.ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "parked"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := w.e.tsvc.UpdateTaskState(w.e.ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake:      &service.WakeSpec{Kind: "watchdog", JobID: &job.ID, Continuation: "resume after job fires"},
		ActorType: "human",
	}); err != nil {
		t.Fatalf("block task: %v", err)
	}

	w.e.wd.Scan(w.e.ctx) // job fires → fire watchdog-wakes

	got, err := w.e.tsvc.GetTask(w.e.ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Task.State != "pending" {
		t.Fatalf("task state = %s, want pending (unblocked by watchdog job)", got.Task.State)
	}
}

// Test W9: validation kind=watchdog без job_id → 400 (previously 400 on the kind itself).
func TestWatchdogJobWakeKindValidation(t *testing.T) {
	w := setupWd(t)
	teamID, roleA, _ := makeTeamWithRoles(t, w.e.svc, "wd-wakeval")
	task, _ := w.e.tsvc.CreateTask(w.e.ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "t"})
	// kind=watchdog без job_id → 400
	_, err := w.e.tsvc.UpdateTaskState(w.e.ctx, service.UpdateTaskStateRequest{
		ID: task.ID, State: models.TaskBlocked,
		Wake: &service.WakeSpec{Kind: "watchdog", Continuation: "x"}, ActorType: "human",
	})
	if err == nil {
		t.Fatal("kind=watchdog без job_id: want 400")
	}
}
