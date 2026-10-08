package service_test

import (
	"context"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

func setupWorkflowEnv(t *testing.T) (*service.TeamService, *service.WorkflowService, context.Context) {
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
	wsvc := service.NewWorkflowService(db, stores)
	return teamSvc, wsvc, context.Background()
}

func makeWfTeam(t *testing.T, svc *service.TeamService, name string) *models.Team {
	t.Helper()
	team, err := svc.CreateTeam(context.Background(), service.CreateTeamRequest{Name: name})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	return team
}

func TestWorkflowFull(t *testing.T) {
	teamSvc, wsvc, ctx := setupWorkflowEnv(t)
	team := makeWfTeam(t, teamSvc, "wf-team")

	// создание с блоками и connections (ссылки = индексы blocks)
	wf, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{
		TeamID: team.ID,
		Name:   "deploy-flow",
		Blocks: []service.WorkflowBlockInput{
			{Type: "task", Position: &service.WorkflowPosition{X: 0, Y: 0}, Label: strPtr("Start")},
			{Type: "decision", Position: &service.WorkflowPosition{X: 100, Y: 0}},
			{Type: "agent", Position: &service.WorkflowPosition{X: 200, Y: 50}},
		},
		Connections: []service.WorkflowConnectionInput{
			{FromBlockID: 0, ToBlockID: 1},
			{FromBlockID: 1, ToBlockID: 2, Condition: strPtr("yes")},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// GET
	got, blocks, conns, err := wsvc.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.Name != "deploy-flow" || got.State != "draft" {
		t.Fatalf("workflow = %+v", got)
	}
	if len(blocks) != 3 || len(conns) != 2 {
		t.Fatalf("blocks=%d conns=%d, want 3/2", len(blocks), len(conns))
	}
	if blocks[0].Position.X != 0 || blocks[1].Type != "decision" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].Label == nil || *blocks[0].Label != "Start" {
		t.Fatalf("block label = %v", blocks[0].Label)
	}
	// connection: condition "yes", id-ы = реальные блоки
	if conns[1].Condition == nil || *conns[1].Condition != "yes" {
		t.Fatalf("connection condition = %v", conns[1].Condition)
	}
	if conns[0].FromBlockID != blocks[0].ID || conns[0].ToBlockID != blocks[1].ID {
		t.Fatalf("connection ids: %+v (blocks %d,%d)", conns[0], blocks[0].ID, blocks[1].ID)
	}

	// LIST: фильтры
	state := models.WorkflowDraft
	ws, total, err := wsvc.ListWorkflows(ctx, &team.ID, &state)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if total != 1 || len(ws) != 1 {
		t.Fatalf("list: total=%d, want 1", total)
	}

	// add block
	b, err := wsvc.CreateBlock(ctx, wf.ID, service.WorkflowBlockInput{
		Type: "manual", Position: &service.WorkflowPosition{X: 5, Y: 6},
	})
	if err != nil {
		t.Fatalf("CreateBlock: %v", err)
	}

	// add connection между старым и новым блоком
	c, err := wsvc.CreateConnection(ctx, wf.ID, service.WorkflowConnectionInput{
		FromBlockID: blocks[2].ID, ToBlockID: b.ID,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	if c.FromBlockID != blocks[2].ID {
		t.Fatal("connection wrong")
	}

	// update block (drag&drop)
	posChanged, cfgChanged, _, newPos, _, _, err := wsvc.UpdateBlock(ctx, wf.ID, b.ID, service.UpdateBlockRequest{
		Position: &service.WorkflowPosition{X: 10, Y: 20},
	})
	if err != nil {
		t.Fatalf("UpdateBlock: %v", err)
	}
	if !posChanged || cfgChanged {
		t.Fatalf("changes: pos=%v cfg=%v, want true/false", posChanged, cfgChanged)
	}
	if np, ok := newPos.(service.WorkflowPosition); !ok || np.X != 10 {
		t.Fatalf("newPos = %v", newPos)
	}
	// перепроверка: позиция обновилась
	_, blocks2, _, err := wsvc.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, bb := range blocks2 {
		if bb.ID == b.ID {
			found = true
			if bb.Position.X != 10 || bb.Position.Y != 20 {
				t.Fatalf("block position = %+v, want 10/20", bb.Position)
			}
		}
	}
	if !found {
		t.Fatal("block not found after update")
	}

	// errors: unknown workflow, invalid type, connection из другого workflow
	if _, _, _, err := wsvc.GetWorkflow(ctx, 999); err == nil {
		t.Fatal("want not_found for unknown workflow")
	}
	if _, err := wsvc.CreateBlock(ctx, wf.ID, service.WorkflowBlockInput{Type: "nope"}); err == nil {
		t.Fatal("want validation error for invalid block type")
	}
	wf2, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{TeamID: team.ID, Name: "wf2"})
	if err != nil {
		t.Fatal(err)
	}
	b2, err := wsvc.CreateBlock(ctx, wf2.ID, service.WorkflowBlockInput{Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wsvc.CreateConnection(ctx, wf.ID, service.WorkflowConnectionInput{
		FromBlockID: blocks[0].ID, ToBlockID: b2.ID,
	}); err == nil {
		t.Fatal("want validation error for cross-workflow connection")
	}
	// update блока из другого workflow
	if _, _, _, _, _, _, err := wsvc.UpdateBlock(ctx, wf2.ID, b.ID, service.UpdateBlockRequest{}); err == nil {
		t.Fatal("want validation error for cross-workflow block update")
	}
}

func TestWorkflowValidation(t *testing.T) {
	teamSvc, wsvc, ctx := setupWorkflowEnv(t)
	team := makeWfTeam(t, teamSvc, "wf-val")

	// name обязателен
	if _, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{TeamID: team.ID}); err == nil {
		t.Fatal("want validation error for empty name")
	}
	// неизвестная команда
	if _, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{TeamID: 999, Name: "x"}); err == nil {
		t.Fatal("want not_found for unknown team")
	}
	// archived команда
	arch := makeWfTeam(t, teamSvc, "wf-val-arch")
	if err := teamSvc.ArchiveTeam(ctx, arch.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{TeamID: arch.ID, Name: "x"}); err == nil {
		t.Fatal("want conflict for archived team")
	}
	// дубликат имени
	if _, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{TeamID: team.ID, Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{TeamID: team.ID, Name: "dup"}); err == nil {
		t.Fatal("want conflict for duplicate workflow name")
	}
	// connection: индексы вне диапазона blocks
	if _, err := wsvc.CreateWorkflow(ctx, service.CreateWorkflowRequest{
		TeamID: team.ID, Name: "bad-conn",
		Blocks:      []service.WorkflowBlockInput{{Type: "task"}},
		Connections: []service.WorkflowConnectionInput{{FromBlockID: 0, ToBlockID: 5}},
	}); err == nil {
		t.Fatal("want validation error for out-of-range connection index")
	}
}
