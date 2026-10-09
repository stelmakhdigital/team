package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ---------- P1.4: Workflow engine (исполняемые воркфлоу) ----------
//
// «the runtime is the scribe, not the gate»: engine создаёт задачи по блокам и
// проецирует закрытие шага на следующий. Блоки v1: task, agent, decision,
// parallel, loop, manual. Закрытие задачи (done/canceled) → projection в той же tx.

// WorkflowEngine — runtime исполняемых воркфлоу.
type WorkflowEngine struct {
	db        *sql.DB
	teams     teamStore
	roles     roleStore
	tasks     taskStore
	history   historyStore
	wf        *repository.WorkflowRepo
	blocks    *repository.WorkflowBlockRepo
	conns     *repository.WorkflowConnectionRepo
	instances *repository.WorkflowInstanceRepo
	steps     *repository.WorkflowStepRepo
	// Bus — real-time события (workflow.instance_updated); nil = без событий.
	Bus *EventBus
}

func NewWorkflowEngine(db *sql.DB, s *repository.Stores) *WorkflowEngine {
	return &WorkflowEngine{
		db: db, teams: s.Teams, roles: s.Roles, tasks: s.Tasks, history: s.History,
		wf: s.Workflows, blocks: s.WorkflowBlocks, conns: s.WorkflowConnections,
		instances: s.WorkflowInstances, steps: s.WorkflowSteps,
	}
}

// wfGraph — граф воркфлоу (блоки + исходящие связи) для быстрых lookups.
type wfGraph struct {
	blocks map[int64]*models.WorkflowBlock
	conns  map[int64][]*models.WorkflowConnection
}

func (g *wfGraph) block(id int64) *models.WorkflowBlock { return g.blocks[id] }
func (g *wfGraph) outgoing(blockID int64) []*models.WorkflowConnection {
	return g.conns[blockID]
}

func buildGraph(blocks []*models.WorkflowBlock, conns []*models.WorkflowConnection) *wfGraph {
	g := &wfGraph{blocks: map[int64]*models.WorkflowBlock{}, conns: map[int64][]*models.WorkflowConnection{}}
	for _, b := range blocks {
		g.blocks[b.ID] = b
	}
	for _, c := range conns {
		g.conns[c.FromBlockID] = append(g.conns[c.FromBlockID], c)
	}
	return g
}

// entryBlocks — блоки без входящих connection'ов.
func entryBlocks(blocks []*models.WorkflowBlock, conns []*models.WorkflowConnection) []*models.WorkflowBlock {
	incoming := map[int64]bool{}
	for _, c := range conns {
		incoming[c.ToBlockID] = true
	}
	var entries []*models.WorkflowBlock
	for _, b := range blocks {
		if !incoming[b.ID] {
			entries = append(entries, b)
		}
	}
	return entries
}

// ---------- Views ----------

// InstanceView — инстанс в формате контракта.
type InstanceView struct {
	ID             int64   `json:"id"`
	WorkflowID     int64   `json:"workflow_id"`
	State          string  `json:"state"`
	RootObjective  string  `json:"root_objective"`
	CurrentBlockID *int64  `json:"current_block_id,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	FinishedAt     *string `json:"finished_at,omitempty"`
}

func instanceView(i *models.WorkflowInstance) *InstanceView {
	v := &InstanceView{
		ID: i.ID, WorkflowID: i.WorkflowID, State: string(i.State), RootObjective: i.RootObjective,
		CurrentBlockID: i.CurrentBlockID,
		CreatedAt:      i.CreatedAt.Format(rfc3339), UpdatedAt: i.UpdatedAt.Format(rfc3339),
	}
	if i.FinishedAt != nil {
		v.FinishedAt = ptr(i.FinishedAt.Format(rfc3339))
	}
	return v
}

// StepView — шаг в формате контракта (trail).
type StepView struct {
	ID         int64   `json:"id"`
	BlockID    int64   `json:"block_id"`
	BlockType  string  `json:"block_type"`
	BlockLabel *string `json:"block_label,omitempty"`
	TaskID     *int64  `json:"task_id,omitempty"`
	TaskTitle  *string `json:"task_title,omitempty"`
	State      string  `json:"state"`
	ExitKind   *string `json:"exit_kind,omitempty"`
	Actor      *int64  `json:"actor,omitempty"`
	ActorName  *string `json:"actor_name,omitempty"`
	ResultNote *string `json:"result_note,omitempty"`
	EnteredAt  *string `json:"entered_at,omitempty"`
	ExitedAt   *string `json:"exited_at,omitempty"`
}

// InstanceDetailView — инстанс + trail + активные шаги (для canvas-подсветки).
type InstanceDetailView struct {
	Instance      *InstanceView `json:"instance"`
	Steps         []*StepView   `json:"steps"`
	ActiveStepIDs []int64       `json:"active_step_ids"`
}

// ---------- Run ----------

// Run — POST /workflows/{id}/run: создаёт instance + первый шаг (entry-блок).
func (e *WorkflowEngine) Run(ctx context.Context, workflowID int64, rootObjective string) (*InstanceView, error) {
	wf, err := e.wf.GetByID(ctx, e.db, workflowID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow")
	}
	if !wf.EngineEnabled {
		return nil, NewConflict("workflow is not engine-enabled; enable the engine to run it")
	}
	team, err := e.teams.GetByID(ctx, e.db, wf.TeamID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "team")
	}
	if team.State != models.TeamActive {
		return nil, NewConflict(fmt.Sprintf("team %q is %s", team.Name, team.State))
	}
	blocks, err := e.blocks.ListByWorkflow(ctx, e.db, wf.ID)
	if err != nil {
		return nil, err
	}
	conns, err := e.conns.ListByWorkflow(ctx, e.db, wf.ID)
	if err != nil {
		return nil, err
	}
	entries := entryBlocks(blocks, conns)
	if len(entries) == 0 {
		return nil, NewValidation("workflow has no entry block (a block with no incoming connections)",
			[]FieldError{{Field: "workflow", Reason: "no entry block"}})
	}
	if len(entries) > 1 {
		return nil, NewValidation(fmt.Sprintf("workflow has %d entry blocks; exactly one is required", len(entries)),
			[]FieldError{{Field: "workflow", Reason: "multiple entry blocks"}})
	}

	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	inst := &models.WorkflowInstance{
		WorkflowID: wf.ID, State: models.WfInstanceRunning, RootObjective: rootObjective,
		CurrentBlockID: &entries[0].ID,
	}
	if err := e.instances.Create(ctx, tx, inst); err != nil {
		return nil, err
	}
	graph := buildGraph(blocks, conns)
	if err := e.stepForBlock(ctx, tx, inst, team, graph, entries[0], ""); err != nil {
		return nil, err
	}
	if err := e.instances.Update(ctx, tx, inst); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	e.publishInstance(inst, team.ID)
	return instanceView(inst), nil
}

// ---------- Projection (hook из TaskService) ----------

// ProjectStepClose — вызывается TaskService внутри tx смены состояния задачи.
// Возвращает события для публикации после commit. no-op, если задача не
// workflow-шаг / инстанс завершён (идемпотентность).
func (e *WorkflowEngine) ProjectStepClose(ctx context.Context, tx repository.DBTX, task *models.Task, from, to models.TaskState, note string) ([]WSEvent, error) {
	if to != models.TaskDone && to != models.TaskCanceled && to != models.TaskBlocked && to != models.TaskPending {
		return nil, nil
	}
	step, err := e.steps.GetActiveByTask(ctx, tx, task.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil // не workflow-задача (или шаг уже закрыт)
		}
		return nil, err
	}
	inst, err := e.instances.GetByID(ctx, tx, step.InstanceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if inst.State.Finished() {
		return nil, nil // идемпотентность: инстанс уже завершён
	}

	switch to {
	case models.TaskDone:
		exit := mapDoneExit(task.ClosureReason)
		if exit == models.WfExitWaiting { // blocked_on → ждём (wake/зависимость)
			inst.State = models.WfInstanceWaiting
			if err := e.instances.Update(ctx, tx, inst); err != nil {
				return nil, err
			}
			return e.instanceEvents(inst, task.TeamID), nil
		}
		stepState := models.WfStepDone
		if exit == models.WfExitFailed {
			stepState = models.WfStepFailed
		}
		if err := e.closeStep(ctx, tx, step, stepState, exit, note); err != nil {
			return nil, err
		}
		if exit == models.WfExitFailed { // escalation → instance failed
			e.failInstance(inst)
			if err := e.instances.Update(ctx, tx, inst); err != nil {
				return nil, err
			}
			return e.instanceEvents(inst, task.TeamID), nil
		}
		// handoff/done → projection следующего шага
		team, err := e.teams.GetByID(ctx, tx, task.TeamID)
		if err != nil {
			return nil, err
		}
		graph, err := e.loadGraph(ctx, tx, inst.WorkflowID)
		if err != nil {
			return nil, err
		}
		if closedBlock := graph.block(step.BlockID); closedBlock != nil {
			if err := e.followOutgoing(ctx, tx, inst, team, graph, closedBlock, note); err != nil {
				return nil, err
			}
		} else {
			e.finishInstance(inst) // блок удалён → завершение
		}
		if err := e.instances.Update(ctx, tx, inst); err != nil {
			return nil, err
		}
		return e.instanceEvents(inst, task.TeamID), nil

	case models.TaskCanceled:
		if err := e.closeStep(ctx, tx, step, models.WfStepFailed, models.WfExitFailed, note); err != nil {
			return nil, err
		}
		e.failInstance(inst)
		if err := e.instances.Update(ctx, tx, inst); err != nil {
			return nil, err
		}
		return e.instanceEvents(inst, task.TeamID), nil

	case models.TaskBlocked:
		// шаг остаётся active; instance → waiting (wake-механика P0.3 без изменений)
		inst.State = models.WfInstanceWaiting
		if err := e.instances.Update(ctx, tx, inst); err != nil {
			return nil, err
		}
		return e.instanceEvents(inst, task.TeamID), nil

	case models.TaskPending:
		if from == models.TaskBlocked && inst.State == models.WfInstanceWaiting {
			inst.State = models.WfInstanceRunning
			if err := e.instances.Update(ctx, tx, inst); err != nil {
				return nil, err
			}
			return e.instanceEvents(inst, task.TeamID), nil
		}
		return nil, nil
	}
	return nil, nil
}

// stepForBlock — «достигнут» блок: создаёт active-шаг (task/agent) или
// транзитно проходит control-блок (decision/parallel/loop/manual).
func (e *WorkflowEngine) stepForBlock(ctx context.Context, tx repository.DBTX, inst *models.WorkflowInstance, team *models.Team, g *wfGraph, block *models.WorkflowBlock, note string) error {
	if block == nil {
		e.finishInstance(inst)
		return nil
	}
	switch block.Type {
	case models.BlockTask, models.BlockAgent:
		roleID := cfgInt64(block.Config, "role_id")
		if roleID == 0 {
			// agent без role_id → skipped с записью (spec); task без role — defensive skip.
			e.recordControlStep(ctx, tx, inst, block, models.WfStepSkipped, nil, "block without role_id — skipped")
			return e.followOutgoing(ctx, tx, inst, team, g, block, note)
		}
		title := block.Label
		if title == nil || strings.TrimSpace(*title) == "" {
			t := string(block.Type) + " step"
			title = &t
		}
		task, err := e.createStepTask(ctx, tx, team.ID, roleID, *title, team.ProjectID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := e.steps.Create(ctx, tx, &models.WorkflowStep{
			InstanceID: inst.ID, BlockID: block.ID, TaskID: &task.ID,
			State: models.WfStepActive, Actor: &roleID, ResultNote: ptr(""), EnteredAt: &now,
		}); err != nil {
			return err
		}
		inst.State = models.WfInstanceRunning
		inst.CurrentBlockID = &block.ID
		return nil

	case models.BlockDecision:
		conns := g.outgoing(block.ID)
		chosen, cond := pickDecisionBranch(conns, note)
		if chosen == nil {
			e.recordControlStep(ctx, tx, inst, block, models.WfStepRouted, nil, "decision: no matching branch — completes")
			e.finishInstance(inst)
			return nil
		}
		e.recordControlStep(ctx, tx, inst, block, models.WfStepRouted, nil, "decision: "+cond)
		return e.stepForBlock(ctx, tx, inst, team, g, g.block(chosen.ToBlockID), note)

	case models.BlockParallel:
		conns := g.outgoing(block.ID)
		e.recordControlStep(ctx, tx, inst, block, models.WfStepRouted, nil, fmt.Sprintf("parallel: fan-out to %d branch(es)", len(conns)))
		for _, c := range conns {
			if err := e.stepForBlock(ctx, tx, inst, team, g, g.block(c.ToBlockID), note); err != nil {
				return err
			}
		}
		if len(conns) == 0 {
			e.finishInstance(inst)
		}
		return nil

	case models.BlockLoop:
		// тело цикла: задача для role_id (одна итерация). Повтор/выход — в followOutgoing.
		roleID := cfgInt64(block.Config, "role_id")
		if roleID == 0 {
			e.recordControlStep(ctx, tx, inst, block, models.WfStepRouted, nil, "loop: no body — pass through")
			return e.followOutgoing(ctx, tx, inst, team, g, block, note)
		}
		title := block.Label
		if title == nil || strings.TrimSpace(*title) == "" {
			tt := "loop body"
			title = &tt
		}
		task, err := e.createStepTask(ctx, tx, team.ID, roleID, *title, team.ProjectID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := e.steps.Create(ctx, tx, &models.WorkflowStep{
			InstanceID: inst.ID, BlockID: block.ID, TaskID: &task.ID,
			State: models.WfStepActive, Actor: &roleID, ResultNote: ptr(""), EnteredAt: &now,
		}); err != nil {
			return err
		}
		inst.State = models.WfInstanceRunning
		inst.CurrentBlockID = &block.ID
		return nil

	case models.BlockManual:
		now := time.Now().UTC()
		if err := e.steps.Create(ctx, tx, &models.WorkflowStep{
			InstanceID: inst.ID, BlockID: block.ID,
			State: models.WfStepActive, ResultNote: ptr("manual — waiting for operator"), EnteredAt: &now,
		}); err != nil {
			return err
		}
		inst.State = models.WfInstanceWaiting
		inst.CurrentBlockID = &block.ID
		return nil
	}
	e.finishInstance(inst)
	return nil
}

// followOutgoing — следует за исходящими связями блока (обычно одна).
// Для loop-блока: повтор тела, пока note без "exit"; при "exit" — выход по связи.
func (e *WorkflowEngine) followOutgoing(ctx context.Context, tx repository.DBTX, inst *models.WorkflowInstance, team *models.Team, g *wfGraph, block *models.WorkflowBlock, note string) error {
	if block.Type == models.BlockLoop {
		if !strings.Contains(note, "exit") {
			e.recordControlStep(ctx, tx, inst, block, models.WfStepRouted, nil, "loop: repeat")
			return e.stepForBlock(ctx, tx, inst, team, g, block, note)
		}
		e.recordControlStep(ctx, tx, inst, block, models.WfStepRouted, nil, "loop: exit")
	}
	conns := g.outgoing(block.ID)
	if len(conns) == 0 {
		e.finishInstance(inst)
		return nil
	}
	for _, c := range conns {
		if err := e.stepForBlock(ctx, tx, inst, team, g, g.block(c.ToBlockID), note); err != nil {
			return err
		}
	}
	return nil
}

// createStepTask — создаёт задачу для task/agent-блока (within tx) + history.
func (e *WorkflowEngine) createStepTask(ctx context.Context, tx repository.DBTX, teamID, roleID int64, title string, projectID *int64) (*models.Task, error) {
	role, err := e.roles.GetByID(ctx, tx, roleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NewConflict(fmt.Sprintf("role %d not found", roleID))
		}
		return nil, err
	}
	if role.TeamID != teamID {
		return nil, NewConflict(fmt.Sprintf("role %d does not belong to team %d", roleID, teamID))
	}
	task := &models.Task{
		TeamID: teamID, ProjectID: projectID,
		DestinationRoleID: roleID, Title: title, State: models.TaskPending,
	}
	if err := e.tasks.Create(ctx, tx, task); err != nil {
		return nil, err
	}
	if err := e.history.Create(ctx, tx, &models.HistoryEntry{
		QueueTaskID: task.ID, ToState: string(models.TaskPending),
		ActorType: "daemon", Comment: ptr("created by workflow engine"),
		Metadata: map[string]any{"type": "workflow_step"},
	}); err != nil {
		return nil, err
	}
	return task, nil
}

// recordControlStep — транзитная запись trail для control-блока.
func (e *WorkflowEngine) recordControlStep(ctx context.Context, tx repository.DBTX, inst *models.WorkflowInstance, block *models.WorkflowBlock, state models.WorkflowStepState, actor *int64, note string) error {
	now := time.Now().UTC()
	return e.steps.Create(ctx, tx, &models.WorkflowStep{
		InstanceID: inst.ID, BlockID: block.ID, State: state, Actor: actor,
		ResultNote: ptr(note), EnteredAt: &now, ExitedAt: &now,
	})
}

// closeStep — закрывает активный шаг (state + exit_kind + note + exited_at).
func (e *WorkflowEngine) closeStep(ctx context.Context, tx repository.DBTX, step *models.WorkflowStep, state models.WorkflowStepState, exit models.WorkflowStepExitKind, note string) error {
	now := time.Now().UTC()
	step.State = state
	step.ExitKind = &exit
	step.ResultNote = ptr(note)
	step.ExitedAt = &now
	return e.steps.Update(ctx, tx, step)
}

func (e *WorkflowEngine) finishInstance(inst *models.WorkflowInstance) {
	now := time.Now().UTC()
	inst.State = models.WfInstanceCompleted
	inst.FinishedAt = &now
	inst.CurrentBlockID = nil
}

func (e *WorkflowEngine) failInstance(inst *models.WorkflowInstance) {
	now := time.Now().UTC()
	inst.State = models.WfInstanceFailed
	inst.FinishedAt = &now
	inst.CurrentBlockID = nil
}

// mapDoneExit — closure_reason при done → exit_kind.
func mapDoneExit(closureReason *models.ClosureReason) models.WorkflowStepExitKind {
	if closureReason == nil {
		return models.WfExitDone
	}
	switch *closureReason {
	case models.ClosureHandedOff:
		return models.WfExitHandoff
	case models.ClosureBlockedOn:
		return models.WfExitWaiting
	case models.ClosureEscalation:
		return models.WfExitFailed
	default: // no_follow_on, denied, canceled → done
		return models.WfExitDone
	}
}

// pickDecisionBranch — ветка decision по note (closure comment).
func pickDecisionBranch(conns []*models.WorkflowConnection, note string) (*models.WorkflowConnection, string) {
	if len(conns) == 0 {
		return nil, ""
	}
	n := strings.ToLower(strings.TrimSpace(note))
	for _, c := range conns {
		if c.Condition != nil {
			cond := strings.ToLower(strings.TrimSpace(*c.Condition))
			if cond != "" && (cond == n || (n != "" && strings.Contains(n, cond))) {
				return c, strings.TrimSpace(*c.Condition)
			}
		}
	}
	for _, c := range conns {
		if c.Condition == nil || strings.TrimSpace(*c.Condition) == "" {
			return c, "default"
		}
	}
	cond := "first"
	if conns[0].Condition != nil {
		cond = *conns[0].Condition
	}
	return conns[0], cond
}

// loadGraph — граф воркфлоу (within tx).
func (e *WorkflowEngine) loadGraph(ctx context.Context, tx repository.DBTX, workflowID int64) (*wfGraph, error) {
	blocks, err := e.blocks.ListByWorkflow(ctx, tx, workflowID)
	if err != nil {
		return nil, err
	}
	conns, err := e.conns.ListByWorkflow(ctx, tx, workflowID)
	if err != nil {
		return nil, err
	}
	return buildGraph(blocks, conns), nil
}

func (e *WorkflowEngine) publishInstance(inst *models.WorkflowInstance, teamID int64) {
	if e.Bus == nil {
		return
	}
	e.Bus.Publish(newEvent("workflow.instance_updated", map[string]any{
		"instance_id": inst.ID, "workflow_id": inst.WorkflowID,
		"team_id": teamID, "state": string(inst.State),
	}, fmt.Sprintf("team:%d", teamID), fmt.Sprintf("workflow:%d", inst.WorkflowID)))
}

func (e *WorkflowEngine) instanceEvents(inst *models.WorkflowInstance, teamID int64) []WSEvent {
	return []WSEvent{newEvent("workflow.instance_updated", map[string]any{
		"instance_id": inst.ID, "workflow_id": inst.WorkflowID,
		"team_id": teamID, "state": string(inst.State),
	}, fmt.Sprintf("team:%d", teamID), fmt.Sprintf("workflow:%d", inst.WorkflowID))}
}

// ---------- Запросы ----------

// ListInstances — GET /workflows/{id}/instances.
func (e *WorkflowEngine) ListInstances(ctx context.Context, workflowID int64) ([]*InstanceView, error) {
	if _, err := e.wf.GetByID(ctx, e.db, workflowID); err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow")
	}
	insts, err := e.instances.ListByWorkflow(ctx, e.db, workflowID)
	if err != nil {
		return nil, err
	}
	out := make([]*InstanceView, 0, len(insts))
	for _, i := range insts {
		out = append(out, instanceView(i))
	}
	return out, nil
}

// GetInstance — GET /workflow-instances/{iid} (instance + steps + active).
func (e *WorkflowEngine) GetInstance(ctx context.Context, iid int64) (*InstanceDetailView, error) {
	inst, err := e.instances.GetByID(ctx, e.db, iid)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow instance")
	}
	steps, err := e.steps.ListByInstance(ctx, e.db, iid)
	if err != nil {
		return nil, err
	}
	sv, err := e.stepViews(ctx, e.db, inst.WorkflowID, steps)
	if err != nil {
		return nil, err
	}
	var active []int64
	for _, s := range steps {
		if s.State == models.WfStepActive {
			active = append(active, s.ID)
		}
	}
	if active == nil {
		active = []int64{}
	}
	return &InstanceDetailView{Instance: instanceView(inst), Steps: sv, ActiveStepIDs: active}, nil
}

// GetTrace — GET /workflow-instances/{iid}/trace (append-only trail, ASC).
func (e *WorkflowEngine) GetTrace(ctx context.Context, iid int64) ([]*StepView, error) {
	inst, err := e.instances.GetByID(ctx, e.db, iid)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow instance")
	}
	steps, err := e.steps.ListByInstance(ctx, e.db, iid)
	if err != nil {
		return nil, err
	}
	return e.stepViews(ctx, e.db, inst.WorkflowID, steps)
}

// stepViews — обогащение шагов (block type/label, task title, actor name) + кэш.
func (e *WorkflowEngine) stepViews(ctx context.Context, tx repository.DBTX, workflowID int64, steps []*models.WorkflowStep) ([]*StepView, error) {
	blocks, err := e.blocks.ListByWorkflow(ctx, tx, workflowID)
	if err != nil {
		return nil, err
	}
	blockMap := map[int64]*models.WorkflowBlock{}
	for _, b := range blocks {
		blockMap[b.ID] = b
	}
	roleName := map[int64]*string{}
	taskTitle := map[int64]*string{}
	out := make([]*StepView, 0, len(steps))
	for _, s := range steps {
		v := &StepView{ID: s.ID, BlockID: s.BlockID, TaskID: s.TaskID, State: string(s.State),
			ExitKind: exitKindStr(s.ExitKind), Actor: s.Actor, ResultNote: s.ResultNote}
		if b, ok := blockMap[s.BlockID]; ok {
			v.BlockType = string(b.Type)
			v.BlockLabel = b.Label
		}
		if s.EnteredAt != nil {
			v.EnteredAt = ptr(s.EnteredAt.Format(rfc3339))
		}
		if s.ExitedAt != nil {
			v.ExitedAt = ptr(s.ExitedAt.Format(rfc3339))
		}
		if s.Actor != nil {
			if name, ok := roleName[*s.Actor]; ok {
				v.ActorName = name
			} else {
				name := e.roleName(ctx, tx, *s.Actor)
				roleName[*s.Actor] = name
				v.ActorName = name
			}
		}
		if s.TaskID != nil {
			if title, ok := taskTitle[*s.TaskID]; ok {
				v.TaskTitle = title
			} else {
				title := e.taskTitle(ctx, tx, *s.TaskID)
				taskTitle[*s.TaskID] = title
				v.TaskTitle = title
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (e *WorkflowEngine) roleName(ctx context.Context, tx repository.DBTX, roleID int64) *string {
	r, err := e.roles.GetByID(ctx, tx, roleID)
	if err != nil || r == nil {
		return nil
	}
	return &r.Name
}

func (e *WorkflowEngine) taskTitle(ctx context.Context, tx repository.DBTX, taskID int64) *string {
	t, err := e.tasks.GetByID(ctx, tx, taskID)
	if err != nil || t == nil {
		return nil
	}
	return &t.Title
}

func exitKindStr(k *models.WorkflowStepExitKind) *string {
	if k == nil {
		return nil
	}
	s := string(*k)
	return &s
}

// ---------- Actions: resume / route / abort ----------

// Resume — POST /workflow-instances/{iid}/resume: failed → re-arm failed-шага.
func (e *WorkflowEngine) Resume(ctx context.Context, iid int64) (*InstanceView, error) {
	inst, err := e.instances.GetByID(ctx, e.db, iid)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow instance")
	}
	if inst.State != models.WfInstanceFailed {
		return nil, NewConflict("only failed instances can be resumed")
	}
	lastFailed, err := e.steps.LastFailedByInstance(ctx, e.db, iid)
	if err != nil {
		return nil, err
	}
	if lastFailed == nil {
		return nil, NewConflict("no failed step to resume")
	}
	wf, err := e.wf.GetByID(ctx, e.db, inst.WorkflowID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow")
	}
	team, err := e.teams.GetByID(ctx, e.db, wf.TeamID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "team")
	}
	blocks, err := e.blocks.ListByWorkflow(ctx, e.db, wf.ID)
	if err != nil {
		return nil, err
	}
	conns, err := e.conns.ListByWorkflow(ctx, e.db, wf.ID)
	if err != nil {
		return nil, err
	}

	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// re-arm: новый step того же блока (completed-шаги НЕ перезапускаются)
	graph := buildGraph(blocks, conns)
	if err := e.stepForBlock(ctx, tx, inst, team, graph, graph.block(lastFailed.BlockID), ""); err != nil {
		return nil, err
	}
	inst.State = models.WfInstanceRunning
	inst.FinishedAt = nil
	if err := e.instances.Update(ctx, tx, inst); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	e.publishInstance(inst, team.ID)
	return instanceView(inst), nil
}

// Route — POST /workflow-instances/{iid}/route: handoff active-шага на новую роль.
func (e *WorkflowEngine) Route(ctx context.Context, iid int64, toRoleID int64) (*InstanceView, error) {
	inst, err := e.instances.GetByID(ctx, e.db, iid)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow instance")
	}
	if inst.State != models.WfInstanceRunning {
		return nil, NewConflict("only running instances can be routed")
	}
	role, err := e.roles.GetByID(ctx, e.db, toRoleID)
	if err != nil {
		return nil, NewValidation(fmt.Sprintf("role %d not found", toRoleID),
			[]FieldError{{Field: "to_role_id", Reason: "role not found"}})
	}
	activeSteps, err := e.steps.ListActiveByInstance(ctx, e.db, iid)
	if err != nil {
		return nil, err
	}
	var target *models.WorkflowStep
	for _, s := range activeSteps {
		if s.TaskID != nil {
			target = s
			break
		}
	}
	if target == nil {
		return nil, NewConflict("no active step with a task to route")
	}

	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// handoff-задача на новую роль (шаг остаётся тем же, actor обновляется)
	if err := e.tasks.UpdateDestinationRole(ctx, tx, *target.TaskID, toRoleID); err != nil {
		return nil, err
	}
	target.Actor = &toRoleID
	if err := e.steps.Update(ctx, tx, target); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := e.steps.Create(ctx, tx, &models.WorkflowStep{
		InstanceID: iid, BlockID: target.BlockID, TaskID: target.TaskID,
		State: models.WfStepRouted, Actor: &toRoleID,
		ResultNote: ptr("routed to role " + itoa(toRoleID) + " (" + role.Name + ")"),
		EnteredAt:  &now, ExitedAt: &now,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	e.publishInstance(inst, role.TeamID)
	return instanceView(inst), nil
}

// Abort — POST /workflow-instances/{iid}/abort: все active задачи → canceled.
func (e *WorkflowEngine) Abort(ctx context.Context, iid int64) (*InstanceView, error) {
	inst, err := e.instances.GetByID(ctx, e.db, iid)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow instance")
	}
	if inst.State.Finished() {
		return nil, NewConflict("instance already finished")
	}
	wf, err := e.wf.GetByID(ctx, e.db, inst.WorkflowID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow")
	}

	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	activeSteps, err := e.steps.ListActiveByInstance(ctx, tx, iid)
	if err != nil {
		return nil, err
	}
	for _, s := range activeSteps {
		if s.TaskID == nil {
			continue
		}
		if t, err := e.tasks.GetByID(ctx, tx, *s.TaskID); err == nil && !t.State.Terminal() {
			from := string(t.State)
			now := time.Now().UTC()
			t.State = models.TaskCanceled
			t.CompletedAt = &now
			if err := e.tasks.UpdateState(ctx, tx, t.ID, t); err != nil {
				return nil, err
			}
			if err := e.history.Create(ctx, tx, &models.HistoryEntry{
				QueueTaskID: t.ID, FromState: &from, ToState: string(models.TaskCanceled),
				ActorType: "daemon", Comment: ptr("aborted by workflow instance"),
				Metadata: map[string]any{"type": "workflow_abort", "instance_id": inst.ID},
			}); err != nil {
				return nil, err
			}
		}
		now := time.Now().UTC()
		s.State = models.WfStepFailed
		failed := models.WfExitFailed
		s.ExitKind = &failed
		s.ExitedAt = &now
		if err := e.steps.Update(ctx, tx, s); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	inst.State = models.WfInstanceAborted
	inst.FinishedAt = &now
	inst.CurrentBlockID = nil
	if err := e.instances.Update(ctx, tx, inst); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	e.publishInstance(inst, wf.TeamID)
	return instanceView(inst), nil
}

// ---------- helpers ----------

func cfgInt64(cfg map[string]any, key string) int64 {
	if cfg == nil {
		return 0
	}
	switch v := cfg[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	}
	return 0
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
