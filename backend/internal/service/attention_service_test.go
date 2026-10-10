package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

func setupAttentionEnv(t *testing.T) (db *sql.DB, svc *service.TeamService, tsvc *service.TaskService, att *service.AttentionService, watchdog *repository.WatchdogRepo, ctx context.Context) {
	t.Helper()
	var err error
	db, err = database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err = database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stores := repository.NewStores(db)
	svc = service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err = os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc = service.NewTaskService(db, stores)
	att = service.NewAttentionService(db, stores)
	return db, svc, tsvc, att, stores.Watchdog, context.Background()
}

func attMakeTeam(t *testing.T, svc *service.TeamService, name string) (teamID, roleA int64) {
	t.Helper()
	team, err := svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: name})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	seg, err := svc.CreateSegment(context.Background(), service.CreateSegmentRequest{
		TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	a, err := svc.CreateRole(context.Background(), service.CreateRoleRequest{
		SegmentID: seg.ID, Name: "role_a", AgentSpec: "agents/role_a.yaml", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	return team.ID, a.ID
}

func attMakeTask(t *testing.T, tsvc *service.TaskService, ctx context.Context, teamID, roleID int64, title string) int64 {
	t.Helper()
	task, err := tsvc.CreateTask(ctx, service.CreateTaskRequest{TeamID: teamID, DestinationRoleID: roleID, Title: title})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task.ID
}

func attSetBlocked(t *testing.T, tsvc *service.TaskService, ctx context.Context, id int64, escalate bool) {
	t.Helper()
	req := service.UpdateTaskStateRequest{ID: id, State: models.TaskBlocked}
	if escalate {
		r := models.ClosureEscalation
		req.ClosureReason = &r
	}
	if _, err := tsvc.UpdateTaskState(ctx, req); err != nil {
		t.Fatalf("UpdateTaskState(blocked,esc=%v): %v", escalate, err)
	}
}

func attMakeAlert(t *testing.T, db *sql.DB, watchdog *repository.WatchdogRepo, ctx context.Context, teamID int64, evType string, unread bool, createdAt time.Time) int64 {
	t.Helper()
	e := &models.WatchdogEvent{
		TeamID: teamID, EventType: evType, Severity: "high",
		Description: evType + " detected", IsRead: !unread, RequiresAction: unread,
		CreatedAt: createdAt,
	}
	if err := watchdog.Create(ctx, db, e); err != nil {
		t.Fatalf("Create watchdog event: %v", err)
	}
	return e.ID
}

func TestAttentionAggregation(t *testing.T) {
	db, svc, tsvc, att, watchdog, ctx := setupAttentionEnv(t)
	teamID, roleA := attMakeTeam(t, svc, "att-team")
	now := time.Now().UTC()

	// tasks: B1 blocked (no closure) -> task_blocked(high); B2 blocked+escalation -> task_escalated(critical, deduped from blocked)
	b1 := attMakeTask(t, tsvc, ctx, teamID, roleA, "B1 blocked")
	attSetBlocked(t, tsvc, ctx, b1, false)
	b2 := attMakeTask(t, tsvc, ctx, teamID, roleA, "B2 escalated")
	attSetBlocked(t, tsvc, ctx, b2, true)

	// alerts: A1 unread blocked (critical, now-2h); A2b unread stale (high, now-3h);
	// A2a unread stale (high, now-1h); A4 unread idle (normal, now-30m); A3 READ blocked (excluded)
	a1 := attMakeAlert(t, db, watchdog, ctx, teamID, "blocked", true, now.Add(-2*time.Hour))
	a2b := attMakeAlert(t, db, watchdog, ctx, teamID, "stale", true, now.Add(-3*time.Hour))
	a2a := attMakeAlert(t, db, watchdog, ctx, teamID, "stale", true, now.Add(-1*time.Hour))
	a4 := attMakeAlert(t, db, watchdog, ctx, teamID, "idle", true, now.Add(-30*time.Minute))
	a3 := attMakeAlert(t, db, watchdog, ctx, teamID, "blocked", false, now.Add(-40*time.Minute))

	res, err := att.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// 2 tasks + 4 unread alerts = 6 (A3 read excluded; B2 deduped, not double-counted)
	if res.Total != 6 {
		t.Fatalf("total = %d, want 6 (items=%d)", res.Total, len(res.Items))
	}
	if len(res.Items) != 6 {
		t.Fatalf("len(items) = %d, want 6", len(res.Items))
	}

	idx := map[string]int{}
	for i, it := range res.Items {
		key := it.Kind + ":" + it.ID
		if _, dup := idx[key]; dup {
			t.Fatalf("duplicate attention item %s", key)
		}
		idx[key] = i
	}

	// expected kinds present, correct severity
	want := map[string]string{
		"task_escalated:" + idStr(b2): "critical",
		"task_blocked:" + idStr(b1):   "high",
		"alert_unread:" + idStr(a1):   "critical",
		"alert_unread:" + idStr(a2b):  "high",
		"alert_unread:" + idStr(a2a):  "high",
		"alert_unread:" + idStr(a4):   "normal",
	}
	for k, sev := range want {
		i, ok := idx[k]
		if !ok {
			t.Fatalf("missing expected item %s (have %v)", k, idx)
		}
		if res.Items[i].Severity != sev {
			t.Errorf("%s severity = %s, want %s", k, res.Items[i].Severity, sev)
		}
	}
	// read alert A3 must be excluded
	if _, ok := idx["alert_unread:"+idStr(a3)]; ok {
		t.Errorf("read alert %d must be excluded from attention", a3)
	}

	// severity ordering: non-increasing rank (critical < high < normal)
	rank := map[string]int{"critical": 0, "high": 1, "normal": 2}
	for i := 1; i < len(res.Items); i++ {
		if rank[res.Items[i].Severity] < rank[res.Items[i-1].Severity] {
			t.Fatalf("severity order violated at %d: %s after %s", i, res.Items[i].Severity, res.Items[i-1].Severity)
		}
	}
	// within high severity, older first: A2b (now-3h) before A2a (now-1h)
	if idx["alert_unread:"+idStr(a2b)] > idx["alert_unread:"+idStr(a2a)] {
		t.Errorf("created_at asc violated: A2b (older) should precede A2a")
	}

	// limit + total
	paged, err := att.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List(limit=2): %v", err)
	}
	if len(paged.Items) != 2 || paged.Total != 6 {
		t.Fatalf("limit=2 -> len=%d total=%d, want 2/6", len(paged.Items), paged.Total)
	}
	// offset past the end -> empty items, total unchanged
	tail, err := att.List(ctx, 50, 100)
	if err != nil {
		t.Fatalf("List(offset=100): %v", err)
	}
	if tail.Total != 6 || len(tail.Items) != 0 {
		t.Fatalf("offset past end -> total=%d len=%d, want 6/0", tail.Total, len(tail.Items))
	}
}

func TestAttentionEmpty(t *testing.T) {
	_, _, _, att, _, ctx := setupAttentionEnv(t)
	res, err := att.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("total = %d, want 0", res.Total)
	}
	if res.Items == nil {
		// must be an empty (non-nil) slice so JSON renders [] not null
		t.Fatalf("items = nil, want empty slice")
	}
	if len(res.Items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(res.Items))
	}
}

func idStr(v int64) string {
	return strconv.FormatInt(v, 10)
}

// P1.4: workflow_attention (failed → critical, waiting → high) + link на деталку.
func TestAttentionWorkflow(t *testing.T) {
	db, svc, _, att, _, ctx := setupAttentionEnv(t)
	teamID, roleA := attMakeTeam(t, svc, "att-wf-team")

	stores := repository.NewStores(db)
	wfs := service.NewWorkflowService(db, stores)
	wf, err := wfs.CreateWorkflow(ctx, service.CreateWorkflowRequest{
		TeamID: teamID, Name: "att-wf",
		Blocks: []service.WorkflowBlockInput{{Type: "task", Config: map[string]any{"role_id": roleA}}},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	failed := &models.WorkflowInstance{WorkflowID: wf.ID, State: models.WfInstanceFailed, RootObjective: "o"}
	waiting := &models.WorkflowInstance{WorkflowID: wf.ID, State: models.WfInstanceWaiting, RootObjective: "o"}
	if err := stores.WorkflowInstances.Create(ctx, db, failed); err != nil {
		t.Fatalf("create failed instance: %v", err)
	}
	if err := stores.WorkflowInstances.Create(ctx, db, waiting); err != nil {
		t.Fatalf("create waiting instance: %v", err)
	}

	res, err := att.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]AttentionItemView{}
	for _, it := range res.Items {
		if it.Kind == "workflow_attention" {
			byID[it.ID] = AttentionItemView{Severity: it.Severity, LinkPath: it.Link.Path}
		}
	}
	if v, ok := byID["wf-"+idStr(failed.ID)]; !ok || v.Severity != "critical" ||
		v.LinkPath != fmt.Sprintf("/workflows/%d/instances/%d", wf.ID, failed.ID) {
		t.Fatalf("failed instance item wrong: %+v (want critical + instance link)", v)
	}
	if v, ok := byID["wf-"+idStr(waiting.ID)]; !ok || v.Severity != "high" {
		t.Fatalf("waiting instance item wrong: %+v (want high)", v)
	}
}

type AttentionItemView struct {
	Severity string
	LinkPath string
}

// P1.5: session_context_wall (high) — сессии у контекст-стены из compact-plan.
func TestAttentionSessionContextWall(t *testing.T) {
	_, _, _, att, _, ctx := setupAttentionEnv(t)
	att.SetContextWallFunc(func(ctx context.Context) ([]*service.CompactPlanEntry, error) {
		pct := func(p float64) *float64 { return &p }
		return []*service.CompactPlanEntry{
			{SessionID: 101, TeamID: 1, TeamName: "t", RoleID: 1, RoleName: "lead", RuntimeType: "pi", State: "idle", Model: "m", WorkingDir: "/w", ContextUsedPercentage: pct(92)},
			{SessionID: 102, TeamID: 1, TeamName: "t", RoleID: 2, RoleName: "worker", RuntimeType: "pi", State: "running", Model: "m", WorkingDir: "/w", ContextUsedPercentage: pct(85)},
		}, nil
	})
	res, err := att.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]AttentionItemView{}
	for _, it := range res.Items {
		if it.Kind == "session_context_wall" {
			byID[it.ID] = AttentionItemView{Severity: it.Severity, LinkPath: it.Link.Path}
		}
	}
	if v, ok := byID["ctxwall-101"]; !ok || v.Severity != "high" || v.LinkPath != "/sessions/101" {
		t.Fatalf("session 101 item wrong: %+v (want high + /sessions/101)", v)
	}
	if v, ok := byID["ctxwall-102"]; !ok || v.Severity != "high" {
		t.Fatalf("session 102 item wrong: %+v (want high)", v)
	}
}

// P1.8: diagnostics_fail (fail-чеги Health/Doctor) → critical + link на /diagnostics.
func TestAttentionDiagnosticsFail(t *testing.T) {
	db, svc, _, att, watchdog, ctx := setupAttentionEnv(t)
	teamID, _ := attMakeTeam(t, svc, "att-diag")
	// unread diagnostics_fail-событие (TeamID обязателен; link → /diagnostics)
	attMakeAlert(t, db, watchdog, ctx, teamID, "diagnostics_fail", true, time.Now().UTC().Add(-5*time.Minute))
	res, err := att.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *AttentionItemView
	for i := range res.Items {
		if res.Items[i].Kind == "diagnostics_fail" {
			found = &AttentionItemView{Severity: res.Items[i].Severity, LinkPath: res.Items[i].Link.Path}
		}
	}
	if found == nil {
		t.Fatalf("no diagnostics_fail attention item")
	}
	if found.Severity != "critical" || found.LinkPath != "/diagnostics" {
		t.Fatalf("diagnostics_fail item = %+v (want critical + /diagnostics)", found)
	}
}
