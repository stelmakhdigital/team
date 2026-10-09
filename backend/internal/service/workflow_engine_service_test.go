package service_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- P1.4: Workflow engine (service) ----------

type engineEnv struct {
	t      *testing.T
	ctx    context.Context
	teams  *service.TeamService
	tasks  *service.TaskService
	wfs    *service.WorkflowService
	engine *service.WorkflowEngine
}

func newEngineEnv(t *testing.T) *engineEnv {
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
	teamSvc := service.NewTeamService(db, stores)
	teamSvc.SpecsDir = t.TempDir()
	// agent_spec файл для ролей (CreateRole валидирует существование)
	if err := os.MkdirAll(filepath.Join(teamSvc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamSvc.SpecsDir, "agents", "x.yaml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskSvc := service.NewTaskService(db, stores)
	wfSvc := service.NewWorkflowService(db, stores)
	engine := service.NewWorkflowEngine(db, stores)
	taskSvc.ProjectionHook = engine.ProjectStepClose // P1.4 projection
	return &engineEnv{t: t, ctx: context.Background(), teams: teamSvc, tasks: taskSvc, wfs: wfSvc, engine: engine}
}

// teamWithRoles — команда с n ролями.
func (e *engineEnv) teamWithRoles(n int) (teamID int64, roleIDs []int64) {
	e.t.Helper()
	team, err := e.teams.CreateTeam(e.ctx, service.CreateTeamRequest{Name: "wf-engine-team"})
	if err != nil {
		e.t.Fatalf("CreateTeam: %v", err)
	}
	seg, err := e.teams.CreateSegment(e.ctx, service.CreateSegmentRequest{TeamID: team.ID, Name: "core", Layout: &service.Layout{X: 0, Y: 0}})
	if err != nil {
		e.t.Fatalf("CreateSegment: %v", err)
	}
	roleIDs = make([]int64, 0, n)
	for i := 0; i < n; i++ {
		name := "role_" + string(rune('a'+i))
		r, err := e.teams.CreateRole(e.ctx, service.CreateRoleRequest{SegmentID: seg.ID, Name: name, AgentSpec: "agents/x.yaml", Layout: &service.Layout{X: 0, Y: float64(i)}})
		if err != nil {
			e.t.Fatalf("CreateRole %s: %v", name, err)
		}
		roleIDs = append(roleIDs, r.ID)
	}
	return team.ID, roleIDs
}

var wfNameSeq int64

// wf — создаёт исполняемый воркфлоу (engine_enabled=true).
func (e *engineEnv) wf(teamID int64, blocks []service.WorkflowBlockInput, conns []service.WorkflowConnectionInput) int64 {
	e.t.Helper()
	wfNameSeq++
	name := fmt.Sprintf("engine-flow-%d-%d", wfNameSeq, time.Now().UnixNano())
	wf, err := e.wfs.CreateWorkflow(e.ctx, service.CreateWorkflowRequest{TeamID: teamID, Name: name, Blocks: blocks, Connections: conns})
	if err != nil {
		e.t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := e.wfs.UpdateWorkflow(e.ctx, service.UpdateWorkflowRequest{ID: wf.ID, Name: name, EngineEnabled: boolPtr(true)}); err != nil {
		e.t.Fatalf("enable engine: %v", err)
	}
	return wf.ID
}

func boolPtr(b bool) *bool { return &b }

func (e *engineEnv) taskBlock(id int64, label string) service.WorkflowBlockInput {
	return service.WorkflowBlockInput{Type: "task", Label: strPtr(label), Config: map[string]any{"role_id": id}}
}

// activeTaskID — task_id активного шага инстанса (первый с задачей).
func (e *engineEnv) activeTaskID(instanceID int64) int64 {
	e.t.Helper()
	detail, err := e.engine.GetInstance(e.ctx, instanceID)
	if err != nil {
		e.t.Fatalf("GetInstance: %v", err)
	}
	for _, s := range detail.Steps {
		if s.State == "active" && s.TaskID != nil {
			return *s.TaskID
		}
	}
	e.t.Fatalf("no active task step in instance %d", instanceID)
	return 0
}

func (e *engineEnv) instanceState(instanceID int64) string {
	e.t.Helper()
	detail, err := e.engine.GetInstance(e.ctx, instanceID)
	if err != nil {
		e.t.Fatalf("GetInstance: %v", err)
	}
	return detail.Instance.State
}

// closeTask — доводит задачу до done с reason + comment (projection-триггер).
func (e *engineEnv) closeTask(taskID int64, reason models.ClosureReason, comment string) {
	e.t.Helper()
	// до in_progress (чтобы был реалистичный переход), затем done
	if _, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: taskID, State: models.TaskInProgress}); err != nil {
		e.t.Fatalf("->in_progress: %v", err)
	}
	_, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: taskID, State: models.TaskDone, ClosureReason: &reason, Comment: comment})
	if err != nil {
		e.t.Fatalf("close task %d (%s): %v", taskID, reason, err)
	}
}

func TestEngineRunAndChain(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(3)
	ra, rb, rc := roles[0], roles[1], roles[2]
	// A → B → C (линейная цепочка)
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B"), e.taskBlock(rc, "C")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}, {FromBlockID: 1, ToBlockID: 2}},
	)
	inst, err := e.engine.Run(e.ctx, wfID, "build & ship")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if inst.State != "running" {
		t.Fatalf("instance state = %s, want running", inst.State)
	}
	// первый шаг: задача A (roleA)
	taskA := e.activeTaskID(inst.ID)
	e.closeTask(taskA, models.ClosureNoFollowOn, "")
	if st := e.instanceState(inst.ID); st != "running" {
		t.Fatalf("after A close: instance = %s, want running", st)
	}
	// projection: задача B (roleB)
	taskB := e.activeTaskID(inst.ID)
	if taskB == taskA {
		t.Fatalf("projection did not create a new task (still %d)", taskA)
	}
	e.closeTask(taskB, models.ClosureHandedOff, "pass to C")
	taskC := e.activeTaskID(inst.ID)
	e.closeTask(taskC, models.ClosureNoFollowOn, "")
	if st := e.instanceState(inst.ID); st != "completed" {
		t.Fatalf("after C close: instance = %s, want completed", st)
	}
}

func TestEngineDecisionBranch(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(3)
	ra, rb, rc := roles[0], roles[1], roles[2]
	// A → D(decision) → B[yes] / C[no]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{
			e.taskBlock(ra, "A"),
			{Type: "decision", Label: strPtr("gate")},
			e.taskBlock(rb, "B-yes"),
			e.taskBlock(rc, "C-no"),
		},
		[]service.WorkflowConnectionInput{
			{FromBlockID: 0, ToBlockID: 1},
			{FromBlockID: 1, ToBlockID: 2, Condition: strPtr("yes")},
			{FromBlockID: 1, ToBlockID: 3, Condition: strPtr("no")},
		},
	)
	// "yes" ветка
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	taskA := e.activeTaskID(inst.ID)
	e.closeTask(taskA, models.ClosureNoFollowOn, "yes")
	detail, _ := e.engine.GetInstance(e.ctx, inst.ID)
	// должен быть активный шаг B-yes (roleB)
	var activeTitle string
	for _, s := range detail.Steps {
		if s.State == "active" && s.TaskID != nil {
			activeTitle = *s.TaskTitle
		}
	}
	if activeTitle != "B-yes" {
		t.Fatalf("decision 'yes' → active task = %q, want B-yes", activeTitle)
	}

	// "no" ветка (новый запуск)
	inst2, _ := e.engine.Run(e.ctx, wfID, "")
	taskA2 := e.activeTaskID(inst2.ID)
	e.closeTask(taskA2, models.ClosureNoFollowOn, "no")
	detail2, _ := e.engine.GetInstance(e.ctx, inst2.ID)
	var activeTitle2 string
	for _, s := range detail2.Steps {
		if s.State == "active" && s.TaskID != nil {
			activeTitle2 = *s.TaskTitle
		}
	}
	if activeTitle2 != "C-no" {
		t.Fatalf("decision 'no' → active task = %q, want C-no", activeTitle2)
	}
}

func TestEngineParallelFanout(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(3)
	ra, rb, rc := roles[0], roles[1], roles[2]
	// A → P(parallel) → B + C
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{
			e.taskBlock(ra, "A"),
			{Type: "parallel", Label: strPtr("P")},
			e.taskBlock(rb, "B"),
			e.taskBlock(rc, "C"),
		},
		[]service.WorkflowConnectionInput{
			{FromBlockID: 0, ToBlockID: 1},
			{FromBlockID: 1, ToBlockID: 2},
			{FromBlockID: 1, ToBlockID: 3},
		},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	e.closeTask(e.activeTaskID(inst.ID), models.ClosureNoFollowOn, "")
	detail, _ := e.engine.GetInstance(e.ctx, inst.ID)
	activeCount := 0
	for _, s := range detail.Steps {
		if s.State == "active" && s.TaskID != nil {
			activeCount++
		}
	}
	if activeCount != 2 {
		t.Fatalf("parallel fan-out: active tasks = %d, want 2", activeCount)
	}
	// закрываем обе → completed
	var activeTasks []int64
	for _, s := range detail.Steps {
		if s.State == "active" && s.TaskID != nil {
			activeTasks = append(activeTasks, *s.TaskID)
		}
	}
	for _, id := range activeTasks {
		_, _ = e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: id, State: models.TaskInProgress})
		_, _ = e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: id, State: models.TaskDone, ClosureReason: ptrReason(models.ClosureNoFollowOn)})
	}
	if st := e.instanceState(inst.ID); st != "completed" {
		t.Fatalf("after closing both parallel tasks: instance = %s, want completed", st)
	}
}

func TestEngineLoop(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(3)
	ra, rx, rb := roles[0], roles[1], roles[2]
	// A → L(loop) → B ; L повторяет себя, пока note без "exit"
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{
			e.taskBlock(ra, "A"),
			{Type: "loop", Label: strPtr("L"), Config: map[string]any{"role_id": rx}},
			e.taskBlock(rb, "B"),
		},
		[]service.WorkflowConnectionInput{
			{FromBlockID: 0, ToBlockID: 1},
			{FromBlockID: 1, ToBlockID: 2},
		},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	// закрываем A → reach L → нет "exit" → repeat L (задача roleX)
	e.closeTask(e.activeTaskID(inst.ID), models.ClosureNoFollowOn, "")
	// закрываем L-итерацию 1 (без exit) → repeat L
	_, _ = e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: e.activeTaskID(inst.ID), State: models.TaskInProgress})
	_, _ = e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: e.activeTaskID(inst.ID), State: models.TaskDone, ClosureReason: ptrReason(models.ClosureNoFollowOn), Comment: "keep looping"})
	// закрываем L-итерацию 2 С exit → выход в B
	_, _ = e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: e.activeTaskID(inst.ID), State: models.TaskInProgress})
	_, _ = e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: e.activeTaskID(inst.ID), State: models.TaskDone, ClosureReason: ptrReason(models.ClosureNoFollowOn), Comment: "exit"})
	detail, _ := e.engine.GetInstance(e.ctx, inst.ID)
	var activeTitle string
	for _, s := range detail.Steps {
		if s.State == "active" && s.TaskID != nil {
			activeTitle = *s.TaskTitle
		}
	}
	if activeTitle != "B" {
		t.Fatalf("loop exit → active task = %q, want B", activeTitle)
	}
}

func TestEngineAgentWithoutRoleSkipped(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(1)
	rb := roles[0]
	// A(agent, без role_id) → B(task)
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{
			{Type: "agent", Label: strPtr("no-role-agent")},
			e.taskBlock(rb, "B"),
		},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, err := e.engine.Run(e.ctx, wfID, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// agent без role → skipped; следующий шаг B активен
	detail, _ := e.engine.GetInstance(e.ctx, inst.ID)
	var sawSkipped, activeB bool
	for _, s := range detail.Steps {
		if s.State == "skipped" {
			sawSkipped = true
		}
		if s.State == "active" && s.TaskID != nil && *s.TaskTitle == "B" {
			activeB = true
		}
	}
	if !sawSkipped || !activeB {
		t.Fatalf("agent without role: skipped=%v activeB=%v, want true/true", sawSkipped, activeB)
	}
}

func TestEngineBlockedWaiting(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	taskA := e.activeTaskID(inst.ID)
	// block → instance waiting
	if _, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: taskA, State: models.TaskInProgress}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: taskA, State: models.TaskBlocked}); err != nil {
		t.Fatal(err)
	}
	if st := e.instanceState(inst.ID); st != "waiting" {
		t.Fatalf("blocked → instance = %s, want waiting", st)
	}
	// unblock → running
	if _, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: taskA, State: models.TaskPending}); err != nil {
		t.Fatal(err)
	}
	if st := e.instanceState(inst.ID); st != "running" {
		t.Fatalf("unblock → instance = %s, want running", st)
	}
}

func TestEngineFailed(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	// escalation → failed
	taskA := e.activeTaskID(inst.ID)
	e.closeTask(taskA, models.ClosureEscalation, "needs human")
	if st := e.instanceState(inst.ID); st != "failed" {
		t.Fatalf("escalation → instance = %s, want failed", st)
	}
}

func TestEngineResume(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	// A → done (no_follow_on) → B; затем B → escalation → failed
	e.closeTask(e.activeTaskID(inst.ID), models.ClosureNoFollowOn, "")
	e.closeTask(e.activeTaskID(inst.ID), models.ClosureEscalation, "B failed")
	if st := e.instanceState(inst.ID); st != "failed" {
		t.Fatalf("B escalation → instance = %s, want failed", st)
	}
	// resume → re-arm B (новый step того же блока); A НЕ перезапускается
	_, err := e.engine.Resume(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if st := e.instanceState(inst.ID); st != "running" {
		t.Fatalf("after resume: instance = %s, want running", st)
	}
	detail, _ := e.engine.GetInstance(e.ctx, inst.ID)
	// активный шаг — блок B (roleB), есть новая задача
	var activeFound bool
	for _, s := range detail.Steps {
		if s.State == "active" && s.TaskID != nil && s.Actor != nil && *s.Actor == rb {
			activeFound = true
		}
	}
	if !activeFound {
		t.Fatalf("resume: no active step for block B (roleB)")
	}
	// completed-шаг A (done) остался в trail (не перезапущен)
	var aDone int
	for _, s := range detail.Steps {
		if s.State == "done" {
			aDone++
		}
	}
	if aDone < 1 {
		t.Fatalf("resume: completed step A missing from trail")
	}
}

func TestEngineRoute(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	taskA := e.activeTaskID(inst.ID)
	// route active-шага на roleB
	_, err := e.engine.Route(e.ctx, inst.ID, rb)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// задача переназначена на roleB
	tk, err := e.tasks.GetTask(e.ctx, taskA)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Task.DestinationRoleID != rb {
		t.Fatalf("route: task destination = %d, want %d", tk.Task.DestinationRoleID, rb)
	}
}

func TestEngineAbort(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	taskA := e.activeTaskID(inst.ID)
	_, err := e.engine.Abort(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if st := e.instanceState(inst.ID); st != "aborted" {
		t.Fatalf("abort → instance = %s, want aborted", st)
	}
	// задача отменена
	tk, _ := e.tasks.GetTask(e.ctx, taskA)
	if tk.Task.State != "canceled" {
		t.Fatalf("abort: task state = %s, want canceled", tk.Task.State)
	}
}

func TestEngineIdempotentProjection(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	taskA := e.activeTaskID(inst.ID)
	e.closeTask(taskA, models.ClosureNoFollowOn, "")
	// повторный «close» A (no-op: уже done → CanTransition false → но projection не создаст дубль)
	// проверим: в trail ровно один done-шаг A + один активный B (нет дублей)
	detail, _ := e.engine.GetInstance(e.ctx, inst.ID)
	var doneA, activeB int
	for _, s := range detail.Steps {
		if s.State == "done" {
			doneA++
		}
		if s.State == "active" && s.TaskID != nil {
			activeB++
		}
	}
	if doneA != 1 {
		t.Fatalf("idempotency: done steps = %d, want 1", doneA)
	}
	if activeB != 1 {
		t.Fatalf("idempotency: active B steps = %d, want 1", activeB)
	}
}

func TestEngineTransactionality(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(1)
	ra := roles[0]
	// A → B, но B ссылается на несуществующую роль 99999 → projection падает →
	// весь tx откатывается (задача A НЕ закрывается, step A остаётся active).
	wfID := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(99999, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}},
	)
	inst, _ := e.engine.Run(e.ctx, wfID, "")
	taskA := e.activeTaskID(inst.ID)
	// доводим A до in_progress (отдельный tx, успешный)
	if _, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{ID: taskA, State: models.TaskInProgress}); err != nil {
		t.Fatalf("->in_progress: %v", err)
	}
	// close A (done) → projection B с invalid role → ошибка → rollback
	_, err := e.tasks.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: taskA, State: models.TaskDone, ClosureReason: ptrReason(models.ClosureNoFollowOn)})
	if err == nil {
		t.Fatalf("expected error from projection (invalid role), got nil")
	}
	// A осталась in_progress (откат), не done
	tk, _ := e.tasks.GetTask(e.ctx, taskA)
	if tk.Task.State != "in_progress" {
		t.Fatalf("transactionality: task A state = %s, want in_progress (rolled back)", tk.Task.State)
	}
	// step A остался active (не закрыт)
	if st := e.instanceState(inst.ID); st != "running" {
		t.Fatalf("transactionality: instance = %s, want running", st)
	}
}

func TestEngineRunValidation(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(2)
	ra, rb := roles[0], roles[1]

	// engine не включён → 409
	wfOff, err := e.wfs.CreateWorkflow(e.ctx, service.CreateWorkflowRequest{
		TeamID: teamID, Name: "off",
		Blocks: []service.WorkflowBlockInput{e.taskBlock(ra, "A")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.engine.Run(e.ctx, wfOff.ID, ""); err == nil {
		t.Fatalf("Run on non-enabled workflow: expected error")
	}

	// несколько entry-блоков → 400 (два блока без входящих связей)
	wfMulti := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{})
	if _, err := e.engine.Run(e.ctx, wfMulti, ""); err == nil {
		t.Fatalf("Run with multiple entry blocks: expected error")
	}

	// нет entry (цикл) → 400
	wfCycle := e.wf(teamID,
		[]service.WorkflowBlockInput{e.taskBlock(ra, "A"), e.taskBlock(rb, "B")},
		[]service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 1}, {FromBlockID: 1, ToBlockID: 0}})
	if _, err := e.engine.Run(e.ctx, wfCycle, ""); err == nil {
		t.Fatalf("Run with no entry (cycle): expected error")
	}
}

func TestEngineCompatOldWorkflow(t *testing.T) {
	e := newEngineEnv(t)
	teamID, roles := e.teamWithRoles(1)
	ra := roles[0]
	// старый workflow без engine — CRUD как раньше
	wf, err := e.wfs.CreateWorkflow(e.ctx, service.CreateWorkflowRequest{
		TeamID: teamID, Name: "legacy", Blocks: []service.WorkflowBlockInput{e.taskBlock(ra, "A")}})
	if err != nil {
		t.Fatal(err)
	}
	got, blocks, conns, err := e.wfs.GetWorkflow(e.ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineEnabled {
		t.Fatalf("legacy workflow engine_enabled = true, want false")
	}
	if len(blocks) != 1 || len(conns) != 0 {
		t.Fatalf("legacy CRUD: blocks=%d conns=%d", len(blocks), len(conns))
	}
	// список инстансов пуст
	insts, err := e.engine.ListInstances(e.ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 0 {
		t.Fatalf("legacy workflow instances = %d, want 0", len(insts))
	}
}

func ptrReason(r models.ClosureReason) *models.ClosureReason { return &r }
