package service_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P2.8: Capture (service) ----------

// mkCapSession — сессия с заданным runtime_type/ref.
func mkCapSession(t *testing.T, e *sessionEnv, teamID, roleID int64, rt, ref string, state models.SessionState) int64 {
	t.Helper()
	sess := &models.Session{TeamID: teamID, RoleID: roleID, RuntimeType: rt, RuntimeRef: ref, State: state}
	if err := e.stores.Sessions.Create(e.ctx, e.db, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

func TestCaptureProcess(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "cap-team")
	s := mkCapSession(t, e, teamID, roleA, "process", "", models.SessionRunning)
	mkTranscript(t, e.ssvc.LogsDir, s, []string{"l1", "l2", "l3", "l4", "l5"})

	res, err := e.ssvc.Capture(e.ctx, s, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "log-tail" {
		t.Errorf("source = %q, want log-tail", res.Source)
	}
	if len(res.Lines) != 3 || res.Lines[0] != "l3" || res.Lines[2] != "l5" {
		t.Errorf("lines = %v, want [l3 l4 l5]", res.Lines)
	}
	if res.SavedPath == "" {
		t.Fatal("no saved_path")
	}
	if _, err := os.Stat(res.SavedPath); err != nil {
		t.Errorf("saved file missing: %v", err)
	}
	if res.CapturedAt == "" {
		t.Error("no captured_at")
	}
}

func TestCaptureProcessNoLog(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "cap-team-nolog")
	s := mkCapSession(t, e, teamID, roleA, "process", "", models.SessionRunning)
	// transcript-файла нет → 200 с пустыми lines (честность, не ошибка)
	res, err := e.ssvc.Capture(e.ctx, s, 10)
	if err != nil {
		t.Fatalf("no-log capture should succeed, got %v", err)
	}
	if res.Source != "log-tail" {
		t.Errorf("source = %q, want log-tail", res.Source)
	}
	if len(res.Lines) != 0 {
		t.Errorf("expected empty lines, got %v", res.Lines)
	}
}

func TestCaptureNotFound(t *testing.T) {
	e := setupSessionEnv(t)
	_, err := e.ssvc.Capture(e.ctx, 999999, 10)
	var appErr *service.AppError
	if !errors.As(err, &appErr) || appErr.Status != 404 {
		t.Errorf("want 404, got %v", err)
	}
}

func TestCaptureTmuxDead(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "cap-team-dead")
	// tmux-сессия мертва (ref не существует) → 409
	s := mkCapSession(t, e, teamID, roleA, "tmux", "daemon-session-99986", models.SessionRunning)
	_, err := e.ssvc.Capture(e.ctx, s, 10)
	var appErr *service.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Errorf("want 409 (dead tmux), got %v", err)
	}
	if appErr != nil && !strings.Contains(strings.ToLower(appErr.Error()), "screen") {
		t.Errorf("409 reason should be clear (screen), got %q", appErr.Error())
	}
}

func TestCaptureLinesClamp(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "cap-team-clamp")
	s := mkCapSession(t, e, teamID, roleA, "process", "", models.SessionRunning)
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, fmt.Sprintf("line-%d", i))
	}
	mkTranscript(t, e.ssvc.LogsDir, s, lines)
	// lines=5000 → clamp к 200
	res, err := e.ssvc.Capture(e.ctx, s, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 200 {
		t.Errorf("clamped lines = %d, want 200", len(res.Lines))
	}
	// lines=0 → default 20
	res2, err := e.ssvc.Capture(e.ctx, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Lines) != 20 {
		t.Errorf("default lines = %d, want 20", len(res2.Lines))
	}
}

func TestCaptureBoundedCleanup(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "cap-team-cleanup")
	s := mkCapSession(t, e, teamID, roleA, "process", "", models.SessionRunning)
	mkTranscript(t, e.ssvc.LogsDir, s, []string{"a", "b", "c"})
	// 25 capture → держать ≤ 20 на сессию
	for i := 0; i < 25; i++ {
		if _, err := e.ssvc.Capture(e.ctx, s, 5); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	dir := filepath.Join(e.ssvc.LogsDir, "captures")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "session-" + fmt.Sprint(s) + "-"
	count := 0
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), prefix) {
			count++
		}
	}
	if count > 20 {
		t.Errorf("bounded cleanup: kept %d captures, want <= 20", count)
	}
}
