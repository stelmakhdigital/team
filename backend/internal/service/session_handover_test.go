package service_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P1.5: compact-plan + handover (service) ----------

// writeUsageTranscript — пишет JSONL-transcript с usage (context_used_percentage).
func writeUsageTranscript(t *testing.T, logDir string, sessID int64, inputTokens int) {
	t.Helper()
	line := fmt.Sprintf("{\"usage\":{\"input_tokens\":%d,\"cache_read_input_tokens\":0,\"output_tokens\":100}}\n", inputTokens)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, fmt.Sprintf("session-%d.log", sessID)), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writePlainTranscript — несколько строк transcript (для контекст-пакета).
func writePlainTranscript(t *testing.T, logDir string, sessID int64, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	b := ""
	for _, l := range lines {
		b += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(logDir, fmt.Sprintf("session-%d.log", sessID)), []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sleepScript — no-arg executable (наследник использует sess.Command без args).
func sleepScript(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sleep.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompactPlanThresholdAndOmit(t *testing.T) {
	e := setupSessionEnv(t)
	e.ssvc.ContextWallPctFunc = func(ctx context.Context) (int, error) { return 80, nil }
	cmd := sleepScript(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "cp-team")
	logDir := e.ssvc.LogsDir

	// A: 90% (180000/200000) → в плане
	sa, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleA, Command: cmd})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	writeUsageTranscript(t, logDir, sa.ID, 180000)
	// B: без usage → omit
	if _, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleB, Command: cmd}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	entries, wall, err := e.ssvc.CompactPlan(e.ctx)
	if err != nil {
		t.Fatalf("CompactPlan: %v", err)
	}
	if wall != 80 {
		t.Fatalf("wall = %d, want 80", wall)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (only 90%% has usage above wall)", len(entries))
	}
	if entries[0].SessionID != sa.ID {
		t.Fatalf("plan session = %d, want %d", entries[0].SessionID, sa.ID)
	}
	if *entries[0].ContextUsedPercentage < 80 {
		t.Fatalf("pct = %v, want >= 80", *entries[0].ContextUsedPercentage)
	}
	if entries[0].SuggestedAction != "handover" && entries[0].SuggestedAction != "wait_for_turn" {
		t.Fatalf("suggested_action = %q", entries[0].SuggestedAction)
	}

	// порог 95 → A (90%) выпадает
	e.ssvc.ContextWallPctFunc = func(ctx context.Context) (int, error) { return 95, nil }
	entries2, _, _ := e.ssvc.CompactPlan(e.ctx)
	if len(entries2) != 0 {
		t.Fatalf("entries2 = %d, want 0 (wall 95 > 90)", len(entries2))
	}
}

func TestCompactPlanSorting(t *testing.T) {
	e := setupSessionEnv(t)
	e.ssvc.ContextWallPctFunc = func(ctx context.Context) (int, error) { return 80, nil }
	cmd := sleepScript(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "cp-sort")
	logDir := e.ssvc.LogsDir

	sa, _ := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleA, Command: cmd})
	sb, _ := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleB, Command: cmd})
	writeUsageTranscript(t, logDir, sa.ID, 160000) // 80%
	writeUsageTranscript(t, logDir, sb.ID, 190000) // 95%

	entries, _, err := e.ssvc.CompactPlan(e.ctx)
	if err != nil {
		t.Fatalf("CompactPlan: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// desc: 95% (sb) first
	if entries[0].SessionID != sb.ID || entries[1].SessionID != sa.ID {
		t.Fatalf("sort desc violated: [%d,%d] want [%d,%d]",
			entries[0].SessionID, entries[1].SessionID, sb.ID, sa.ID)
	}
}

func setupHandover(t *testing.T) (*sessionEnv, int64, int64, string) {
	t.Helper()
	e := setupSessionEnv(t)
	e.ssvc.ContextWallPctFunc = func(ctx context.Context) (int, error) { return 80, nil }
	e.ssvc.TaskHandoffFunc = func(ctx context.Context, req service.HandoffTaskRequest) (*service.HandoffTaskResult, error) {
		return e.tsvc.HandoffTask(ctx, req)
	}
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "ho-team")
	cmd := sleepScript(t)
	return e, teamID, roleA, cmd
}

// makeSessionWithTask — живая сессия (roleA) с привязанной pending-задачей + transcript.
func makeSessionWithTask(t *testing.T, e *sessionEnv, teamID, roleA int64, cmd string) (int64, int64) {
	t.Helper()
	task, err := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "ho task"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleA, QueueTaskID: &task.ID, Command: cmd})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	writePlainTranscript(t, e.ssvc.LogsDir, sess.ID, "line-1", "line-2", "line-3")
	return sess.ID, task.ID
}

func TestHandoverDryRunNoMutations(t *testing.T) {
	e, teamID, roleA, cmd := setupHandover(t)
	sessID, taskID := makeSessionWithTask(t, e, teamID, roleA, cmd)

	res, err := e.ssvc.Handover(e.ctx, sessID, service.HandoverRequest{Reason: "context wall", DryRun: true})
	if err != nil {
		t.Fatalf("Handover dry-run: %v", err)
	}
	if res.Status != "planned" || !res.DryRun {
		t.Fatalf("dry-run status = %q, want planned", res.Status)
	}
	if res.NewSessionID != nil {
		t.Fatalf("dry-run must not create a session (new=%v)", *res.NewSessionID)
	}
	// мутаций нет: сессия жива, задача не закрыта, контекст-файл не записан
	sess, _ := e.ssvc.GetSessionView(e.ctx, sessID)
	if !models.SessionState(sess.State).Active() {
		t.Fatalf("dry-run stopped the session (state=%s)", sess.State)
	}
	task, _ := e.tsvc.GetTask(e.ctx, taskID)
	if task.Task.State != string(models.TaskInProgress) {
		t.Fatalf("dry-run reassigned the task (state=%s)", task.Task.State)
	}
	if _, err := os.Stat(filepath.Join(e.ssvc.LogsDir, fmt.Sprintf("handover-%d.md", sessID))); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the context file")
	}
	// preview заполнен
	if res.SuccessorPreview == nil || res.SuccessorPreview.RoleID != roleA {
		t.Fatalf("dry-run successor preview missing/wrong: %+v", res.SuccessorPreview)
	}
}

func TestHandoverFullRun(t *testing.T) {
	e, teamID, roleA, cmd := setupHandover(t)
	sessID, taskID := makeSessionWithTask(t, e, teamID, roleA, cmd)

	res, err := e.ssvc.Handover(e.ctx, sessID, service.HandoverRequest{Reason: "context wall 95%"})
	if err != nil {
		t.Fatalf("Handover: %v", err)
	}
	if res.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded (steps: %+v)", res.Status, res)
	}
	if res.ContextPrepared.Status != "ok" || res.OldStopped.Status != "ok" ||
		res.TaskReassigned.Status != "ok" || res.SuccessorStarted.Status != "ok" {
		t.Fatalf("per-step not all ok: %+v", res)
	}
	if res.NewSessionID == nil {
		t.Fatal("new_session_id missing")
	}
	// old stopped
	old, _ := e.ssvc.GetSessionView(e.ctx, sessID)
	if old.State != string(models.SessionStopped) {
		t.Fatalf("old session state = %s, want stopped", old.State)
	}
	// old task closed (handed_off_to) → done; new task exists for the same role
	oldTask, _ := e.tsvc.GetTask(e.ctx, taskID)
	if oldTask.Task.State != string(models.TaskDone) {
		t.Fatalf("old task state = %s, want done", oldTask.Task.State)
	}
	newTaskID := res.ContextPackage.Task.NewID
	if newTaskID == nil {
		t.Fatal("new task id missing in context package")
	}
	// successor bound to the new task
	newSess, _ := e.ssvc.GetSessionView(e.ctx, *res.NewSessionID)
	if newSess.QueueTaskID == nil || *newSess.QueueTaskID != *newTaskID {
		t.Fatalf("successor queue_task_id = %v, want %d", newSess.QueueTaskID, *newTaskID)
	}
	if newSess.State != string(models.SessionRunning) {
		t.Fatalf("successor state = %s, want running", newSess.State)
	}
	// контекст-файл записан
	if _, err := os.Stat(res.ContextPackage.ContextFile); err != nil {
		t.Fatalf("context file not written: %v", err)
	}
}

func TestHandoverStoppedConflict(t *testing.T) {
	e, teamID, roleA, cmd := setupHandover(t)
	sess, _ := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleA, Command: cmd})
	if _, err := e.ssvc.StopSession(e.ctx, sess.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	_, err := e.ssvc.Handover(e.ctx, sess.ID, service.HandoverRequest{Reason: "x"})
	if err == nil {
		t.Fatal("handover of a stopped session must fail (409)")
	}
}

func TestHandoverRepeatIdempotent(t *testing.T) {
	e, teamID, roleA, cmd := setupHandover(t)
	sessID, _ := makeSessionWithTask(t, e, teamID, roleA, cmd)
	// первый прогон
	if _, err := e.ssvc.Handover(e.ctx, sessID, service.HandoverRequest{Reason: "first"}); err != nil {
		t.Fatalf("first handover: %v", err)
	}
	// повторный (old=stopped) → 409
	if _, err := e.ssvc.Handover(e.ctx, sessID, service.HandoverRequest{Reason: "again"}); err == nil {
		t.Fatal("repeat handover of the (now stopped) old session must fail")
	}
}

func TestHandoverNoTaskSkipped(t *testing.T) {
	e, teamID, roleA, cmd := setupHandover(t)
	sess, _ := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleA, Command: cmd})
	writePlainTranscript(t, e.ssvc.LogsDir, sess.ID, "line")
	res, err := e.ssvc.Handover(e.ctx, sess.ID, service.HandoverRequest{Reason: "no task"})
	if err != nil {
		t.Fatalf("Handover: %v", err)
	}
	if res.TaskReassigned.Status != "skipped" {
		t.Fatalf("task_reassigned = %q, want skipped (no task)", res.TaskReassigned.Status)
	}
	if res.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded (skipped task is not a failure)", res.Status)
	}
}
