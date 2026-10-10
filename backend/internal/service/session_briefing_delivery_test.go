package service_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daemon/internal/models"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P1.7: briefing delivery (CreateSession + handover) ----------

func TestBriefingProcessDelivery(t *testing.T) {
	e := setupSessionEnv(t)
	e.ssvc.BriefingFunc = service.NewBriefingService(e.db, e.stores).Build
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-deliver")
	cmd := sleepScript(t)
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{RoleID: roleA, Command: cmd})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// config: briefing_applied=builtin + path
	if got := cfgString(sess.Config, "briefing_applied"); got != "builtin" {
		t.Fatalf("briefing_applied = %q, want builtin", got)
	}
	bp := cfgString(sess.Config, "briefing_path")
	if bp == "" {
		t.Fatal("briefing_path missing")
	}
	data, err := os.ReadFile(bp)
	if err != nil {
		t.Fatalf("briefing file not written: %v", err)
	}
	if !strings.Contains(string(data), "Session briefing") {
		t.Fatalf("briefing file content wrong:\n%s", data)
	}
	// env has BRIEFING_FILE
	env := cfgStringSlice(sess.Config, "env")
	found := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "BRIEFING_FILE=") {
			found = true
		}
	}
	if !found {
		t.Fatalf("env missing BRIEFING_FILE: %v", env)
	}
	// view exposes it
	view, _ := e.ssvc.GetSessionView(e.ctx, sess.ID)
	if view.BriefingApplied != "builtin" || view.BriefingPath == "" {
		t.Fatalf("view briefing = %q/%q", view.BriefingApplied, view.BriefingPath)
	}
	if view.BriefingText == "" {
		t.Fatal("view.BriefingText empty (detail)")
	}
}

func TestBriefingOverride(t *testing.T) {
	e := setupSessionEnv(t)
	e.ssvc.BriefingFunc = service.NewBriefingService(e.db, e.stores).Build
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-override")
	cmd := sleepScript(t)
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleA, Command: cmd,
		Config: map[string]any{"briefing_override": "MY CUSTOM BRIEFING"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if got := cfgString(sess.Config, "briefing_applied"); got != "override" {
		t.Fatalf("briefing_applied = %q, want override", got)
	}
	if got := cfgString(sess.Config, "briefing_text"); !strings.Contains(got, "MY CUSTOM BRIEFING") {
		t.Fatalf("briefing_text = %q, want contains override", got)
	}
}

func TestBriefingDisabled(t *testing.T) {
	e := setupSessionEnv(t)
	e.ssvc.BriefingFunc = service.NewBriefingService(e.db, e.stores).Build
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-disabled")
	cmd := sleepScript(t)
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleA, Command: cmd, Config: map[string]any{"briefing": "none"},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if got := cfgString(sess.Config, "briefing_applied"); got != "" {
		t.Fatalf("briefing_applied = %q, want empty (disabled)", got)
	}
	if _, ok := sess.Config["briefing_text"]; ok {
		t.Fatal("briefing_text should not be set (disabled)")
	}
}

// TestBriefingPiConfigFormat — pi-доставка: briefing → system_prompt → pi-конфиг (yaml).
func TestBriefingPiConfigFormat(t *testing.T) {
	e, _ := newBriefingEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "bf-pi")
	b := service.NewBriefingService(e.db, e.stores)
	task, _ := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleA, Title: "pi task"})
	brief := b.Build(e.ctx, service.BriefingParams{TeamID: teamID, RoleID: roleA, QueueTaskID: &task.ID})
	if brief == nil || brief.Text == "" {
		t.Fatal("briefing empty")
	}
	// CreateSession (pi) ставит briefing в system_prompt; пишем конфиг тем же путём (runtime.WriteSessionConfig).
	cfgFile := filepath.Join(t.TempDir(), "session-pi.yaml")
	if err := runtime.WriteSessionConfig(cfgFile, runtime.AgentSpecFields{Name: "role_a", SystemPrompt: brief.Text}); err != nil {
		t.Fatalf("WriteSessionConfig: %v", err)
	}
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "system_prompt: |-") {
		t.Fatalf("pi config missing system_prompt block:\n%s", s)
	}
	if !strings.Contains(s, "pi task") {
		t.Fatalf("pi config missing briefing task content:\n%s", s)
	}
}

// TestHandoverWithBriefing — P1.5 handover + P1.7: наследник получает briefing.
func TestHandoverWithBriefing(t *testing.T) {
	e, teamID, roleA, cmd := setupHandover(t)
	// P1.7: briefing builder подключён
	e.ssvc.BriefingFunc = service.NewBriefingService(e.db, e.stores).Build
	sessID, taskID := makeSessionWithTask(t, e, teamID, roleA, cmd)

	res, err := e.ssvc.Handover(e.ctx, sessID, service.HandoverRequest{Reason: "context wall"})
	if err != nil {
		t.Fatalf("Handover: %v", err)
	}
	if res.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", res.Status)
	}
	// наследник имеет briefing (builtin)
	newSess, _ := e.ssvc.GetSessionView(e.ctx, *res.NewSessionID)
	if newSess.BriefingApplied != "builtin" {
		t.Fatalf("successor briefing_applied = %q, want builtin (briefing in pack)", newSess.BriefingApplied)
	}
	if newSess.BriefingText == "" {
		t.Fatal("successor briefing_text empty")
	}
	_ = taskID
	_ = models.TaskDone
}

// ---------- test helpers ----------

func cfgString(cfg map[string]any, key string) string {
	if s, ok := cfg[key].(string); ok {
		return s
	}
	return ""
}

func cfgStringSlice(cfg map[string]any, key string) []string {
	var out []string
	switch v := cfg[key].(type) {
	case []string:
		out = v
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

var _ = context.Background
