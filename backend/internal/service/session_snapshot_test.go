package service_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P3.12: agent_image / session snapshots ----------

func setupSnapshotEnv(t *testing.T) *sessionEnv {
	t.Helper()
	e := setupSessionEnv(t)
	e.ssvc.SetLibraryRepo(e.stores.Library)
	e.ssvc.SetLibraryVersionRepo(e.stores.LibraryVersions)
	return e
}

func lsvc(e *sessionEnv) *service.LibraryService {
	return service.NewLibraryService(e.db, e.stores, e.svc)
}

func appErrStatus(err error) int {
	var ae *service.AppError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// mkSnapshotSession — сессия с agent-конфигом (process, живёт ~1с).
func mkSnapshotSession(t *testing.T, e *sessionEnv, teamID, roleID int64) *models.Session {
	t.Helper()
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID:      roleID,
		RuntimeType: "process",
		Command:     "sh",
		Args:        []string{"-c", "echo snapshot-hello; sleep 1"},
		Config: map[string]any{
			"model":         "claude-test",
			"system_prompt": "be a good dev",
			"skills":        []string{"skill-1"},
			"tools":         []string{"tool-1"},
			"thinking":      "medium",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess
}

func TestSnapshotCreated(t *testing.T) {
	e := setupSnapshotEnv(t)
	teamID, roleA := e.teamWithRole(t, "snap-1")
	sess := mkSnapshotSession(t, e, teamID, roleA)

	it, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "dev-snap"})
	if err != nil {
		t.Fatalf("SnapshotSession: %v", err)
	}
	if it.Type != models.LibAgentImage {
		t.Errorf("type = %s, want agent_image", it.Type)
	}
	if it.Group != "agent-images" {
		t.Errorf("group = %s, want agent-images", it.Group)
	}
	agent, _ := it.Spec["agent"].(map[string]any)
	if agent == nil {
		t.Fatalf("spec.agent missing: %v", it.Spec)
	}
	if agent["model"] != "claude-test" || agent["system_prompt"] != "be a good dev" {
		t.Errorf("agent model/system_prompt = %v", agent)
	}
	if it.Spec["role_name"] != "role_a" {
		t.Errorf("role_name = %v, want role_a", it.Spec["role_name"])
	}
	if it.Spec["source_session_id"] != float64(sess.ID) {
		if int64Val, ok := it.Spec["source_session_id"].(int64); !ok || int64Val != sess.ID {
			t.Errorf("source_session_id = %v (%T), want %d", it.Spec["source_session_id"], it.Spec["source_session_id"], sess.ID)
		}
	}
	if it.SourceID == nil || *it.SourceID != sess.ID {
		t.Errorf("SourceID = %v, want %d", it.SourceID, sess.ID)
	}
}

func TestSnapshot404Session(t *testing.T) {
	e := setupSnapshotEnv(t)
	_, err := e.ssvc.SnapshotSession(e.ctx, 999999, service.SnapshotRequest{Name: "x"})
	if appErrStatus(err) != 404 {
		t.Fatalf("want 404, got %v (status %d)", err, appErrStatus(err))
	}
}

func TestSnapshot409DuplicateName(t *testing.T) {
	e := setupSnapshotEnv(t)
	teamID, roleA := e.teamWithRole(t, "snap-2")
	sess := mkSnapshotSession(t, e, teamID, roleA)
	if _, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "dup"}); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	_, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "dup"})
	if appErrStatus(err) != 409 {
		t.Fatalf("duplicate: want 409, got %v (status %d)", err, appErrStatus(err))
	}
}

func TestSnapshotSummaryFromBodyAndAuto(t *testing.T) {
	e := setupSnapshotEnv(t)
	teamID, roleA := e.teamWithRole(t, "snap-3")
	sess := mkSnapshotSession(t, e, teamID, roleA)

	// из body
	sum := "my explicit summary"
	it, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "s-body", Summary: &sum})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if it.Spec["summary"] != "my explicit summary" {
		t.Errorf("summary = %v, want body value", it.Spec["summary"])
	}

	// auto: пишем текст в transcript-лог, снапшот без summary
	logPath := filepath.Join(e.logDir, "logs", fmt.Sprintf("session-%d.log", sess.ID))
	if err := os.WriteFile(logPath, []byte("first line of work\nsecond line of work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	it2, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "s-auto"})
	if err != nil {
		t.Fatalf("snapshot auto: %v", err)
	}
	if got, _ := it2.Spec["summary"].(string); !strings.Contains(got, "line of work") {
		t.Errorf("auto summary = %q, want to contain transcript text", got)
	}
}

func TestLaunchFromSnapshot(t *testing.T) {
	e := setupSnapshotEnv(t)
	teamID, roleA := e.teamWithRole(t, "snap-4")
	sess := mkSnapshotSession(t, e, teamID, roleA)
	sum := "previous context here"
	it, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "launch-me", Summary: &sum})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// роль role_a занята активной сессией → стопнем её (иначе 409 active-session)
	if _, err := e.ssvc.StopSession(e.ctx, sess.ID); err != nil {
		t.Fatalf("stop source: %v", err)
	}
	newSess, err := e.ssvc.LaunchFromSnapshot(e.ctx, it.ID, service.LaunchRequest{TeamID: teamID})
	if err != nil {
		t.Fatalf("LaunchFromSnapshot: %v", err)
	}
	if newSess.ID == sess.ID {
		t.Fatalf("launch created the same session")
	}
	if m, _ := newSess.Config["model"].(string); m != "claude-test" {
		t.Errorf("model = %v, want claude-test", newSess.Config["model"])
	}
	if sp, _ := newSess.Config["system_prompt"].(string); !strings.Contains(sp, "previous context here") || !strings.Contains(sp, "be a good dev") {
		t.Errorf("system_prompt = %q, want summary + original", sp)
	}
}

func TestLaunch404And400(t *testing.T) {
	e := setupSnapshotEnv(t)
	// 404 снапшот
	_, err := e.ssvc.LaunchFromSnapshot(e.ctx, 999999, service.LaunchRequest{TeamID: 1})
	if appErrStatus(err) != 404 {
		t.Fatalf("launch missing: want 404, got %v (%d)", err, appErrStatus(err))
	}
	// 400 не-agent_image: сохраним team-снапшот (не agent_image)
	teamID, _ := e.teamWithRole(t, "snap-5")
	teamItem, err := lsvc(e).Save(e.ctx, service.SaveToLibraryRequest{
		Type: models.LibTeam, SourceID: teamID, Name: "team-snap",
	})
	if err != nil {
		t.Fatalf("save team: %v", err)
	}
	_, err = e.ssvc.LaunchFromSnapshot(e.ctx, teamItem.ID, service.LaunchRequest{TeamID: teamID})
	if appErrStatus(err) != 400 {
		t.Fatalf("launch non-agent_image: want 400, got %v (%d)", err, appErrStatus(err))
	}
}

func TestLaunch404RoleInTeam(t *testing.T) {
	e := setupSnapshotEnv(t)
	teamID, roleA := e.teamWithRole(t, "snap-6-src")
	sess := mkSnapshotSession(t, e, teamID, roleA)
	it, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "no-role-target"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// целевая команда без ролей
	emptyTeam, err := e.svc.CreateTeam(e.ctx, service.CreateTeamRequest{Name: "empty-team"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.ssvc.LaunchFromSnapshot(e.ctx, it.ID, service.LaunchRequest{TeamID: emptyTeam.ID})
	if appErrStatus(err) != 404 {
		t.Fatalf("launch role-not-in-team: want 404, got %v (%d)", err, appErrStatus(err))
	}
}

func TestPinUnpinPrune(t *testing.T) {
	e := setupSnapshotEnv(t)
	ls := lsvc(e)
	teamID, roleA := e.teamWithRole(t, "snap-7")
	sess := mkSnapshotSession(t, e, teamID, roleA)
	it, err := e.ssvc.SnapshotSession(e.ctx, sess.ID, service.SnapshotRequest{Name: "pin-me"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	view, err := ls.Pin(e.ctx, it.ID)
	if err != nil || !view.IsPinned {
		t.Fatalf("pin: view=%v err=%v", view, err)
	}
	if _, err := ls.Unpin(e.ctx, it.ID); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if _, err := ls.Pin(e.ctx, it.ID); err != nil {
		t.Fatalf("pin2: %v", err)
	}
	if _, err := ls.Prune(e.ctx, it.ID); appErrStatus(err) != 409 {
		t.Fatalf("prune pinned: want 409, got %v (%d)", err, appErrStatus(err))
	}
	if _, err := ls.Unpin(e.ctx, it.ID); err != nil {
		t.Fatalf("unpin2: %v", err)
	}
	if _, err := ls.Prune(e.ctx, it.ID); err != nil {
		t.Fatalf("prune non-pinned: %v", err)
	}
	if _, err := ls.Prune(e.ctx, it.ID); appErrStatus(err) != 404 {
		t.Fatalf("prune again: want 404, got %v (%d)", err, appErrStatus(err))
	}
}

// prune не-agent_image → 400
func TestPruneNonAgentImage400(t *testing.T) {
	e := setupSnapshotEnv(t)
	ls := lsvc(e)
	teamID, _ := e.teamWithRole(t, "snap-8")
	teamItem, err := ls.Save(e.ctx, service.SaveToLibraryRequest{Type: models.LibTeam, SourceID: teamID, Name: "prune-me"})
	if err != nil {
		t.Fatalf("save team: %v", err)
	}
	_, err = ls.Prune(e.ctx, teamItem.ID)
	if appErrStatus(err) != 400 {
		t.Fatalf("prune non-agent_image: want 400, got %v (%d)", err, appErrStatus(err))
	}
}
