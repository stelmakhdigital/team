package service_test

import (
	"context"
	"errors"
	"testing"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// findStarter — находит seed-стартер по имени (post-migrate seed).
func findStarter(t *testing.T, lsvc *service.LibraryService, ctx context.Context, name string) *service.LibraryItemView {
	t.Helper()
	items, _, _, err := lsvc.List(ctx, repository.LibraryFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, it := range items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("starter %q not found (items=%d)", name, len(items))
	return nil
}

func countStarters(t *testing.T, lsvc *service.LibraryService, ctx context.Context) int {
	t.Helper()
	items, _, _, err := lsvc.List(ctx, repository.LibraryFilter{Limit: 200})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	n := 0
	for _, it := range items {
		if it.IsStarter {
			n++
		}
	}
	return n
}

func assertConflict(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want conflict (409), got nil", msg)
	}
	var appErr *service.AppError
	if errors.As(err, &appErr) {
		if appErr.Status != 409 {
			t.Fatalf("%s: want 409, got %d (%v)", msg, appErr.Status, err)
		}
		return
	}
	t.Fatalf("%s: want *AppError(409), got %v", msg, err)
}

// TestSeedStarters — seed создаёт 2 стартера (is_starter=1, group=starters).
func TestSeedStarters(t *testing.T) {
	lsvc, _, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	devTeam := findStarter(t, lsvc, ctx, "dev-team")
	if !devTeam.IsStarter {
		t.Fatalf("dev-team: want is_starter=true")
	}
	if devTeam.Type != "team" || devTeam.Group == nil || *devTeam.Group != "starters" {
		t.Fatalf("dev-team: want type=team group=starters, got %s/%v", devTeam.Type, devTeam.Group)
	}
	rw := findStarter(t, lsvc, ctx, "review-workflow")
	if !rw.IsStarter || rw.Type != "workflow" {
		t.Fatalf("review-workflow: want is_starter=true type=workflow, got %v/%s", rw.IsStarter, rw.Type)
	}
}

// TestSeedStartersIdempotent — повторный seed не дублирует.
func TestSeedStartersIdempotent(t *testing.T) {
	lsvc, _, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed1: %v", err)
	}
	first := countStarters(t, lsvc, ctx)
	if first != 2 {
		t.Fatalf("after first seed: want 2 starters, got %d", first)
	}
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed2: %v", err)
	}
	if n := countStarters(t, lsvc, ctx); n != 2 {
		t.Fatalf("after second seed: want still 2 starters, got %d (duplicated)", n)
	}
}

// TestApplyTeamStarter — apply dev-team создаёт команду с ролями lead/worker/reviewer.
func TestApplyTeamStarter(t *testing.T) {
	lsvc, stores, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	devTeam := findStarter(t, lsvc, ctx, "dev-team")
	res, err := lsvc.Apply(ctx, devTeam.ID, service.ApplyLibraryItemRequest{
		Overrides: map[string]any{"name": "dev-team-applied"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.CreatedResources.Teams) != 1 {
		t.Fatalf("want 1 team created, got %v", res.CreatedResources.Teams)
	}
	teamID := res.CreatedResources.Teams[0]
	// верифицируем роли через team service
	teamSvc := service.NewTeamService(db, stores)
	detail, err := teamSvc.GetTeam(ctx, teamID)
	if err != nil {
		t.Fatalf("get team: %v", err)
	}
	roles := map[string]bool{}
	for _, r := range detail.Roles {
		roles[r.Name] = true
	}
	for _, want := range []string{"lead", "worker", "reviewer"} {
		if !roles[want] {
			t.Fatalf("applied team missing role %q (has %v)", want, roles)
		}
	}
}

// TestApplyTeamStarterWithProject — apply dev-team с project_id привязывает команду к проекту.
func TestApplyTeamStarterWithProject(t *testing.T) {
	lsvc, stores, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// создаём проект (kind=new)
	parent := t.TempDir()
	pvc := service.NewProjectService(db, stores)
	res, err := pvc.CreateProject(ctx, service.CreateProjectRequest{Name: "proj-x", Kind: "new", ParentPath: parent})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projID := res.Project.ID

	devTeam := findStarter(t, lsvc, ctx, "dev-team")
	ares, err := lsvc.Apply(ctx, devTeam.ID, service.ApplyLibraryItemRequest{
		ProjectID: &projID,
		Overrides: map[string]any{"name": "dev-team-proj"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	teamID := ares.CreatedResources.Teams[0]
	teamSvc := service.NewTeamService(db, stores)
	detail, err := teamSvc.GetTeam(ctx, teamID)
	if err != nil {
		t.Fatalf("get team: %v", err)
	}
	if detail.Team.ProjectID == nil || *detail.Team.ProjectID != projID {
		t.Fatalf("want team.project_id=%d, got %v", projID, detail.Team.ProjectID)
	}
}

// TestApplyWorkflowStarter — apply review-workflow к команде создаёт воркфлоу с 3 блоками.
func TestApplyWorkflowStarter(t *testing.T) {
	lsvc, stores, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// целевая команда
	teamSvc := service.NewTeamService(db, stores)
	team, err := teamSvc.CreateTeam(ctx, service.CreateTeamRequest{Name: "wf-target"})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	rw := findStarter(t, lsvc, ctx, "review-workflow")
	res, err := lsvc.Apply(ctx, rw.ID, service.ApplyLibraryItemRequest{TargetTeamID: &team.ID})
	if err != nil {
		t.Fatalf("apply workflow: %v", err)
	}
	if res.Status != "applied" {
		t.Fatalf("want applied, got %s", res.Status)
	}
	// ищем созданный воркфлоу (по имени review-workflow в команде)
	wfs, _, err := stores.Workflows.List(ctx, db, repository.WorkflowFilter{TeamID: &team.ID})
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	var wf *models.Workflow
	for _, w := range wfs {
		if w.Name == "review-workflow" {
			wf = w
		}
	}
	if wf == nil {
		t.Fatalf("workflow review-workflow not created in team (wfs=%d)", len(wfs))
	}
	wblocks, err := stores.WorkflowBlocks.ListByWorkflow(ctx, db, wf.ID)
	if err != nil {
		t.Fatalf("list blocks: %v", err)
	}
	if len(wblocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(wblocks))
	}
}

// TestStarterPinUpdate409 — rename/обновление стартера → 409.
func TestStarterPinUpdate409(t *testing.T) {
	lsvc, _, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	devTeam := findStarter(t, lsvc, ctx, "dev-team")
	_, err := lsvc.Update(ctx, service.UpdateLibraryRequest{ID: devTeam.ID, Name: "hacked"})
	assertConflict(t, err, "update starter")
}

// TestSaveStarterNameConflict409 — save с именем стартера → 409 (UNIQUE type,name).
func TestSaveStarterNameConflict409(t *testing.T) {
	lsvc, stores, db, ctx := setupLibraryEnv(t)
	if err := service.SeedStarters(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// валидный источник-команда (snapshotSpec тянет её до INSERT)
	teamSvc := service.NewTeamService(db, stores)
	team, err := teamSvc.CreateTeam(ctx, service.CreateTeamRequest{Name: "src-team"})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	_, err = lsvc.Save(ctx, service.SaveToLibraryRequest{
		Type: models.LibTeam, SourceID: team.ID, Name: "dev-team",
	})
	if err == nil {
		t.Fatalf("want conflict saving duplicate starter name, got nil")
	}
	var appErr *service.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Fatalf("want 409, got %v", err)
	}
}
