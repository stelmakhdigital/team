package service_test

import (
	"context"
	"testing"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P2.15: Whoami (service) ----------

type whoamiEnv struct {
	*sessionEnv
	msvc   *service.MessageService
	whoami *service.WhoamiService
}

func setupWhoamiEnv(t *testing.T) *whoamiEnv {
	t.Helper()
	e := setupSessionEnv(t)
	msvc := service.NewMessageService(e.db, e.stores)
	whoami := service.NewWhoamiService(e.db, e.stores, e.svc, e.tsvc, e.ssvc, msvc)
	return &whoamiEnv{sessionEnv: e, msvc: msvc, whoami: whoami}
}

func mkWhoamiSession(t *testing.T, e *whoamiEnv, teamID, roleID int64) int64 {
	t.Helper()
	s := &models.Session{TeamID: teamID, RoleID: roleID, RuntimeType: "tmux", RuntimeRef: "whoami-1", State: models.SessionRunning}
	if err := e.stores.Sessions.Create(e.ctx, e.db, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s.ID
}

func TestWhoamiBySession(t *testing.T) {
	e := setupWhoamiEnv(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "whoami-team")

	// relative: roleB delegates_to roleA → roleA.upstream=[role_b], roleB.downstream=[role_a]
	if _, err := e.svc.CreateRelative(e.ctx, service.CreateRelativeRequest{
		TeamID: teamID, FromRoleID: roleB, ToRoleID: roleA, Type: models.RelDelegatesTo,
	}); err != nil {
		t.Fatalf("CreateRelative: %v", err)
	}

	// tasks for roleA: 1 pending (stays), 1 in_progress, 1 blocked (wake → roleB task)
	refB := makeTaskFor(t, e.tsvc, e.ctx, teamID, roleB, "upstream work")
	makeTaskFor(t, e.tsvc, e.ctx, teamID, roleA, "P pending") // остаётся pending
	ip := makeTaskFor(t, e.tsvc, e.ctx, teamID, roleA, "I in_progress")
	if _, err := e.tsvc.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: ip, State: models.TaskInProgress}); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	b := makeTaskFor(t, e.tsvc, e.ctx, teamID, roleA, "B blocked")
	ref := refB
	if _, err := e.tsvc.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: b, State: models.TaskBlocked, Wake: &service.WakeSpec{Kind: "task", TaskID: &ref},
	}); err != nil {
		t.Fatalf("blocked+wake: %v", err)
	}

	// unread incoming to roleA (from roleB)
	if _, err := e.msvc.SendMessage(e.ctx, service.SendMessageRequest{
		TeamID: teamID, FromRoleID: &roleB, ToRoleID: &roleA, Type: "direct",
		Body: "please review my change\nwhen you get a chance",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	sid := mkWhoamiSession(t, e, teamID, roleA)

	res, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{SessionID: &sid})
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}

	// agent
	if res.Agent.RoleID != roleA || res.Agent.RoleName != "role_a" {
		t.Errorf("agent = %+v, want role_a/roleA", res.Agent)
	}
	if res.Agent.TeamName != "whoami-team" || res.Agent.Address == "" {
		t.Errorf("agent.team/address = %q/%q", res.Agent.TeamName, res.Agent.Address)
	}
	// session
	if res.Session == nil || res.Session.ID != sid || res.Session.State != "running" {
		t.Errorf("session = %+v, want id=%d running", res.Session, sid)
	}
	// my_tasks
	if len(res.MyTasks.Pending) != 1 || res.MyTasks.Pending[0].Title != "P pending" {
		t.Errorf("pending = %+v", res.MyTasks.Pending)
	}
	if len(res.MyTasks.InProgress) != 1 || res.MyTasks.InProgress[0].Title != "I in_progress" {
		t.Errorf("in_progress = %+v", res.MyTasks.InProgress)
	}
	if ip := res.MyTasks.InProgress[0]; ip.ClosureRequiredAt == nil || ip.IsOverdue {
		t.Errorf("in_progress closure = %v / overdue=%v, want deadline+false", ip.ClosureRequiredAt, ip.IsOverdue)
	}
	if len(res.MyTasks.Blocked) != 1 || res.MyTasks.Blocked[0].Title != "B blocked" {
		t.Errorf("blocked = %+v", res.MyTasks.Blocked)
	}
	if w := res.MyTasks.Blocked[0].Wake; w == nil || w.Kind != "task" {
		t.Errorf("blocked.wake = %+v, want kind=task", w)
	}
	// waiting_for_me (unread from role_b)
	if len(res.WaitingForMe) != 1 || *res.WaitingForMe[0].FromRoleName != "role_b" {
		t.Errorf("waiting = %+v", res.WaitingForMe)
	}
	if got := res.WaitingForMe[0].Summary; got != "please review my change when you get a chance" {
		t.Errorf("waiting summary = %q", got)
	}
	// upstream (role_b delegates_to role_a) / downstream (none)
	if len(res.Upstream) != 1 || res.Upstream[0].RoleName != "role_b" || res.Upstream[0].Via != "delegates_to" {
		t.Errorf("upstream = %+v", res.Upstream)
	}
	if len(res.Downstream) != 0 {
		t.Errorf("downstream = %+v, want empty", res.Downstream)
	}
}

func TestWhoamiByRole(t *testing.T) {
	e := setupWhoamiEnv(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "whoami-role")
	if _, err := e.svc.CreateRelative(e.ctx, service.CreateRelativeRequest{
		TeamID: teamID, FromRoleID: roleB, ToRoleID: roleA, Type: models.RelCollaborates,
	}); err != nil {
		t.Fatalf("CreateRelative: %v", err)
	}
	makeTaskFor(t, e.tsvc, e.ctx, teamID, roleA, "only pending")

	res, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{RoleID: &roleA})
	if err != nil {
		t.Fatalf("Whoami(role): %v", err)
	}
	if res.Session != nil {
		t.Errorf("session should be nil when by role, got %+v", res.Session)
	}
	if len(res.MyTasks.Pending) != 1 || res.MyTasks.Pending[0].Title != "only pending" {
		t.Errorf("pending = %+v", res.MyTasks.Pending)
	}
	if len(res.Upstream) != 1 || res.Upstream[0].RoleName != "role_b" {
		t.Errorf("upstream = %+v", res.Upstream)
	}
}

func TestWhoamiValidationAnd404(t *testing.T) {
	e := setupWhoamiEnv(t)
	// 400 — нет параметров
	if _, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{}); err == nil {
		t.Fatal("want 400 (no params), got nil")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 400 {
		t.Errorf("want AppError 400, got %v", err)
	}
	// 404 — нет сессии
	if _, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{SessionID: int64p(999999)}); err == nil {
		t.Fatal("want 404 (no session), got nil")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 404 {
		t.Errorf("want AppError 404 (session), got %v", err)
	}
	// 404 — нет роли
	if _, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{RoleID: int64p(999999)}); err == nil {
		t.Fatal("want 404 (no role), got nil")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 404 {
		t.Errorf("want AppError 404 (role), got %v", err)
	}
}

func TestWhoamiRoleTeamMismatch(t *testing.T) {
	e := setupWhoamiEnv(t)
	team1, roleA1, _ := makeTeamWithRoles(t, e.svc, "mismatch-1")
	team2, _, _ := makeTeamWithRoles(t, e.svc, "mismatch-2")
	_ = team1
	// role из team1, но team_id=team2 → 400 (честность)
	if _, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{RoleID: &roleA1, TeamID: &team2}); err == nil {
		t.Fatal("want 400 (role-team mismatch), got nil")
	} else if ae, ok := err.(*service.AppError); !ok || ae.Status != 400 {
		t.Errorf("want AppError 400 (mismatch), got %v", err)
	}
}

func TestWhoamiBounded(t *testing.T) {
	e := setupWhoamiEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "whoami-bounded")
	// 25 pending → my_tasks.pending ≤ 20
	for i := 0; i < 25; i++ {
		makeTaskFor(t, e.tsvc, e.ctx, teamID, roleA, "t")
	}
	res, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{RoleID: &roleA})
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if len(res.MyTasks.Pending) > 20 {
		t.Errorf("pending = %d, want <=20 (bounded)", len(res.MyTasks.Pending))
	}
	if len(res.MyTasks.Pending) != 20 {
		t.Errorf("pending = %d, want 20", len(res.MyTasks.Pending))
	}
}

func TestWhoamiDifferentRoles(t *testing.T) {
	e := setupWhoamiEnv(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "whoami-roles")
	// roleA: 1 pending; roleB: 3 pending
	makeTaskFor(t, e.tsvc, e.ctx, teamID, roleA, "a1")
	for i := 0; i < 3; i++ {
		makeTaskFor(t, e.tsvc, e.ctx, teamID, roleB, "b")
	}
	resA, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{RoleID: &roleA})
	if err != nil {
		t.Fatalf("Whoami A: %v", err)
	}
	resB, err := e.whoami.Whoami(e.ctx, service.WhoamiRequest{RoleID: &roleB})
	if err != nil {
		t.Fatalf("Whoami B: %v", err)
	}
	if len(resA.MyTasks.Pending) != 1 || len(resB.MyTasks.Pending) != 3 {
		t.Errorf("A.pending=%d B.pending=%d, want 1/3", len(resA.MyTasks.Pending), len(resB.MyTasks.Pending))
	}
}

// ---------- helpers ----------

func int64p(n int64) *int64 { return &n }

func makeTaskFor(t *testing.T, tsvc *service.TaskService, ctx context.Context, teamID, roleID int64, title string) int64 {
	t.Helper()
	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleID, Title: title})
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", title, err)
	}
	return task.ID
}
