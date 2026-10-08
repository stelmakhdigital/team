package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

func newTestService(t *testing.T) *service.TeamService {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := service.NewTeamService(db, repository.NewStores(db))
	svc.SpecsDir = t.TempDir()
	return svc
}

// specFile — создаёт agent_spec-файл в SpecsDir и возвращает его имя (для CreateRole, I4).
func specFile(t *testing.T, svc *service.TeamService, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(svc.SpecsDir, name), []byte("name: "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func ctx() context.Context { return context.Background() }

func isAppErr(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", code)
	}
	app, ok := err.(*service.AppError)
	if !ok {
		t.Fatalf("expected *service.AppError, got %T: %v", err, err)
	}
	if app.Code != code {
		t.Fatalf("expected code %q, got %q (%v)", code, app.Code, err)
	}
}

// ---------- CreateTeam ----------

func TestCreateTeamWithSpec(t *testing.T) {
	svc := newTestService(t)
	team, err := svc.CreateTeam(ctx(), service.CreateTeamRequest{
		Name: "dev-team",
		Spec: &service.TeamSpec{
			Segments: []service.SegmentSpec{
				{Name: "backend", Layout: &service.Layout{X: 10, Y: 20, Width: 100, Height: 80}},
				{Name: "review"},
			},
			Roles: []service.RoleSpec{
				{Segment: "backend", Name: "lead", AgentSpec: "agents/lead.yaml"},
				{Segment: "backend", Name: "worker", AgentSpec: "agents/worker.yaml"},
				{Segment: "review", Name: "reviewer", AgentSpec: "agents/reviewer.yaml"},
			},
			Relatives: []service.RelativeSpec{
				{From: "backend.lead", To: "backend.worker", Type: models.RelDelegatesTo},
				{From: "backend.worker", To: "review.reviewer", Type: models.RelCanObserve},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	detail, err := svc.GetTeam(ctx(), team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if len(detail.Segments) != 2 || len(detail.Roles) != 3 || len(detail.Relatives) != 2 {
		t.Fatalf("unexpected counts: seg=%d roles=%d rels=%d",
			len(detail.Segments), len(detail.Roles), len(detail.Relatives))
	}
	// address = team:segment.role
	lead := findRole(t, detail.Roles, "lead")
	if lead.Address != "dev-team:backend.lead" {
		t.Fatalf("bad address: %q", lead.Address)
	}
	// relative names
	rel := detail.Relatives[0]
	if rel.FromRoleName != "lead" || rel.ToRoleName != "worker" {
		t.Fatalf("bad relative names: %q -> %q", rel.FromRoleName, rel.ToRoleName)
	}
}

func TestCreateTeamDuplicate(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "a"})
	isAppErr(t, err, "conflict")
}

func TestCreateTeamSpecErrors(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: ""})
	isAppErr(t, err, "validation_failed")

	_, err = svc.CreateTeam(ctx(), service.CreateTeamRequest{
		Name: "t",
		Spec: &service.TeamSpec{Roles: []service.RoleSpec{{Segment: "nope", Name: "r", AgentSpec: "x"}}},
	})
	isAppErr(t, err, "validation_failed")

	_, err = svc.CreateTeam(ctx(), service.CreateTeamRequest{
		Name: "t",
		Spec: &service.TeamSpec{
			Segments: []service.SegmentSpec{{Name: "s"}},
			Roles:    []service.RoleSpec{{Segment: "s", Name: "r"}}, // нет agent_spec
		},
	})
	isAppErr(t, err, "validation_failed")
}

// ---------- Segments / Roles ----------

func TestCreateSegmentDuplicateAndArchivedTeam(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})

	seg, err := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "backend"})
	isAppErr(t, err, "conflict")

	if err := svc.ArchiveTeam(ctx(), team.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "other"})
	isAppErr(t, err, "conflict")
	_ = seg
}

func TestCreateRole(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "backend"})

	role, err := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "lead", AgentSpec: specFile(t, svc, "lead.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if role.Address != "t:backend.lead" {
		t.Fatalf("bad address %q", role.Address)
	}

	_, err = svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "lead", AgentSpec: specFile(t, svc, "x.yaml")})
	isAppErr(t, err, "conflict")

	_, err = svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "r2"})
	isAppErr(t, err, "validation_failed")

	// I4: несуществующий agent_spec → 404
	_, err = svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "r3", AgentSpec: "definitely-missing.yaml"})
	isAppErr(t, err, "not_found")

	_, err = svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: 9999, Name: "r4", AgentSpec: specFile(t, svc, "y.yaml")})
	isAppErr(t, err, "not_found")
}

// ---------- Relatives ----------

func TestCreateRelative(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "s"})
	r1, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "a", AgentSpec: specFile(t, svc, "x.yaml")})
	r2, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "b", AgentSpec: specFile(t, svc, "y.yaml")})

	rel, err := svc.CreateRelative(ctx(), service.CreateRelativeRequest{
		TeamID: team.ID, FromRoleID: r1.ID, ToRoleID: r2.ID, Type: models.RelDelegatesTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	// дубль
	_, err = svc.CreateRelative(ctx(), service.CreateRelativeRequest{
		TeamID: team.ID, FromRoleID: r1.ID, ToRoleID: r2.ID, Type: models.RelDelegatesTo,
	})
	isAppErr(t, err, "conflict")
	// self-loop
	_, err = svc.CreateRelative(ctx(), service.CreateRelativeRequest{
		TeamID: team.ID, FromRoleID: r1.ID, ToRoleID: r1.ID, Type: models.RelDelegatesTo,
	})
	isAppErr(t, err, "validation_failed")
	// чужая роль
	otherTeam, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "other"})
	oseg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: otherTeam.ID, Name: "s"})
	or, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: oseg.ID, Name: "c", AgentSpec: specFile(t, svc, "z.yaml")})
	_, err = svc.CreateRelative(ctx(), service.CreateRelativeRequest{
		TeamID: team.ID, FromRoleID: r1.ID, ToRoleID: or.ID, Type: models.RelDelegatesTo,
	})
	isAppErr(t, err, "validation_failed")

	// delete
	if _, err := svc.DeleteRelative(ctx(), rel.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.DeleteRelative(ctx(), rel.ID)
	isAppErr(t, err, "not_found")
}

// ---------- Update ----------

func TestUpdateRoleConfig(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "s"})
	role, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "a", AgentSpec: specFile(t, svc, "old.yaml")})

	profile := "debug"
	res, err := svc.UpdateRoleConfig(ctx(), service.UpdateRoleConfigRequest{
		ID: role.ID, Profile: &profile,
		Config: &map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Role.Profile != "debug" {
		t.Fatalf("profile not updated: %q", res.Role.Profile)
	}
	if res.PreviousConfig == nil || res.NewConfig["k"] != "v" {
		t.Fatalf("config result bad: prev=%v new=%v", res.PreviousConfig, res.NewConfig)
	}
	// пустое обновление
	_, err = svc.UpdateRoleConfig(ctx(), service.UpdateRoleConfigRequest{ID: role.ID})
	isAppErr(t, err, "validation_failed")
}

func TestUpdateLayouts(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "s"})
	role, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "a", AgentSpec: specFile(t, svc, "x.yaml")})

	// segment layout
	segRes, err := svc.UpdateSegmentLayout(ctx(), service.UpdateSegmentLayoutRequest{
		ID: seg.ID, Position: service.Position{X: 1, Y: 2}, Size: &service.Size{Width: 30, Height: 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if segRes.NewLayout.X != 1 || segRes.NewLayout.Width != 30 {
		t.Fatalf("segment layout bad: %+v", segRes.NewLayout)
	}

	// role layout
	roleRes, err := svc.UpdateRoleLayout(ctx(), service.UpdateRoleLayoutRequest{
		ID: role.ID, Position: service.Position{X: 5, Y: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if roleRes.NewPosition.X != 5 {
		t.Fatalf("role layout bad: %+v", roleRes.NewPosition)
	}

	// topology отражает layout
	topo, err := svc.GetTopology(ctx(), team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Layout.Segments) != 1 || topo.Layout.Segments[0].Position.X != 1 {
		t.Fatalf("topology segment layout bad: %+v", topo.Layout.Segments)
	}
	if len(topo.Layout.Roles) != 1 || topo.Layout.Roles[0].Position.Y != 6 {
		t.Fatalf("topology role layout bad: %+v", topo.Layout.Roles)
	}
}

// Регресс (отчёт frontend, slice 1):
// 1) PATCH segment layout: previous ≠ new (старое значение сохраняется);
// 2) topology layout.relatives содержит ВСЕ relatives (path опционально);
// 3) create-ответ: layout не null сразу после создания (без DB roundtrip).
func TestSlice1LayoutRegressions(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})

	// 3) create с layout → в-памяти layout доступен сразу
	seg, err := svc.CreateSegment(ctx(), service.CreateSegmentRequest{
		TeamID: team.ID, Name: "s",
		Layout: &service.Layout{X: 10, Y: 20, Width: 100, Height: 80},
	})
	if err != nil {
		t.Fatal(err)
	}
	if l := service.LayoutFromConfig(seg.Config); l == nil || l.X != 10 {
		t.Fatalf("segment create: layout missing in memory: %+v", seg.Config)
	}
	r1, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{
		SegmentID: seg.ID, Name: "a",
		AgentSpec: specFile(t, svc, "a.yaml"), Layout: &service.Layout{X: 1, Y: 2},
	})
	if l := service.LayoutFromConfig(r1.Config); l == nil || l.X != 1 {
		t.Fatalf("role create: layout missing in memory: %+v", r1.Config)
	}
	r2, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{
		SegmentID: seg.ID, Name: "b", AgentSpec: specFile(t, svc, "b.yaml"),
	})
	rel, err := svc.CreateRelative(ctx(), service.CreateRelativeRequest{
		TeamID: team.ID, FromRoleID: r1.ID, ToRoleID: r2.ID, Type: models.RelDelegatesTo,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2) topology: relative без path тоже в layout.relatives
	topo, err := svc.GetTopology(ctx(), team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Layout.Relatives) != 1 || topo.Layout.Relatives[0].RelativeID != rel.ID {
		t.Fatalf("layout.relatives: %+v", topo.Layout.Relatives)
	}
	if topo.Layout.Relatives[0].Path != nil {
		t.Fatalf("path should be nil/omitted: %+v", topo.Layout.Relatives[0].Path)
	}

	// 1) PATCH: previous ≠ new
	res, err := svc.UpdateSegmentLayout(ctx(), service.UpdateSegmentLayoutRequest{
		ID: seg.ID, Position: service.Position{X: 99, Y: 98},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.PreviousLayout == nil || res.PreviousLayout.X != 10 || res.PreviousLayout.Width != 100 {
		t.Fatalf("previous_layout wrong: %+v", res.PreviousLayout)
	}
	if res.NewLayout.X != 99 || res.NewLayout.Y != 98 {
		t.Fatalf("new_layout wrong: %+v", res.NewLayout)
	}
	// повторный PATCH: previous = предыдущее new
	res2, err := svc.UpdateSegmentLayout(ctx(), service.UpdateSegmentLayoutRequest{
		ID: seg.ID, Position: service.Position{X: 1, Y: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.PreviousLayout.X != 99 {
		t.Fatalf("previous after second patch = %+v, want X=99", res2.PreviousLayout)
	}
}

// ---------- Validate ----------

func TestValidateTopology(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{
		Name: "t",
		Spec: &service.TeamSpec{
			Segments: []service.SegmentSpec{{Name: "s"}},
			Roles: []service.RoleSpec{
				{Segment: "s", Name: "lead", AgentSpec: "a.yaml"},
				{Segment: "s", Name: "w1", AgentSpec: "b.yaml"},
				{Segment: "s", Name: "w2", AgentSpec: "c.yaml"},
				{Segment: "s", Name: "orphan", AgentSpec: "d.yaml"}, // без рёбер
			},
			Relatives: []service.RelativeSpec{
				{From: "s.lead", To: "s.w1", Type: models.RelDelegatesTo},
				{From: "s.lead", To: "s.w2", Type: models.RelDelegatesTo},
			},
		},
	})

	resp, err := svc.ValidateTopology(ctx(), service.ValidateTopologyRequest{TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.IsValid {
		t.Fatalf("expected invalid (orphan role)")
	}
	found := map[string]bool{}
	for _, e := range resp.Errors {
		found[e.Code] = true
	}
	if !found["ORPHAN_ROLE"] {
		t.Fatalf("expected ORPHAN_ROLE error, got %+v", resp.Errors)
	}

	// цикличность
	lead := findRole(t, mustGetTeam(t, svc, team.ID).Roles, "lead")
	w1 := findRole(t, mustGetTeam(t, svc, team.ID).Roles, "w1")
	if _, err := svc.CreateRelative(ctx(), service.CreateRelativeRequest{
		TeamID: team.ID, FromRoleID: w1.ID, ToRoleID: lead.ID, Type: models.RelDelegatesTo,
	}); err != nil {
		t.Fatal(err)
	}
	resp, err = svc.ValidateTopology(ctx(), service.ValidateTopologyRequest{TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	cyc := false
	for _, e := range resp.Errors {
		if e.Code == "CIRCULAR_DEPENDENCY" {
			cyc = true
		}
	}
	if !cyc {
		t.Fatalf("expected CIRCULAR_DEPENDENCY, got %+v", resp.Errors)
	}

	// пустой agent_spec запрещён
	noSpec := findRole(t, mustGetTeam(t, svc, team.ID).Roles, "w2")
	empty := ""
	_, err = svc.UpdateRoleConfig(ctx(), service.UpdateRoleConfigRequest{
		ID: noSpec.ID, AgentSpec: &empty,
	})
	isAppErr(t, err, "validation_failed")
}

func TestValidateTopologyValid(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{
		Name: "t",
		Spec: &service.TeamSpec{
			Segments: []service.SegmentSpec{{Name: "s"}},
			Roles: []service.RoleSpec{
				{Segment: "s", Name: "lead", AgentSpec: "a.yaml"},
				{Segment: "s", Name: "w1", AgentSpec: "b.yaml"},
			},
			Relatives: []service.RelativeSpec{
				{From: "s.lead", To: "s.w1", Type: models.RelDelegatesTo},
			},
		},
	})
	resp, err := svc.ValidateTopology(ctx(), service.ValidateTopologyRequest{TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IsValid || !resp.Valid {
		t.Fatalf("expected valid (is_valid+valid), got errors=%+v", resp.Errors)
	}
}

// I2: пустая команда → is_valid=false, valid=false + NOT_EMPTY.
func TestValidateTopologyEmpty(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "empty"})
	resp, err := svc.ValidateTopology(ctx(), service.ValidateTopologyRequest{TeamID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.IsValid || resp.Valid {
		t.Fatalf("expected invalid, got %+v", resp)
	}
	found := false
	for _, e := range resp.Errors {
		if e.Code == "NOT_EMPTY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected NOT_EMPTY error, got %+v", resp.Errors)
	}
}

// I1: save с неизвестными сегментом/ролью → 400 validation_failed + details.
func TestSaveTopologyUnknownNodes(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "core"})
	_, _ = svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "lead", AgentSpec: specFile(t, svc, "lead.yaml")})

	res, err := svc.SaveTopology(ctx(), service.SaveTopologyRequest{TeamID: team.ID, Segments: []string{"core"}, Roles: []string{"lead"}})
	if err != nil {
		t.Fatalf("save known nodes: %v", err)
	}
	if res == nil || res.Validation == nil {
		t.Fatal("bad save result")
	}

	_, err = svc.SaveTopology(ctx(), service.SaveTopologyRequest{
		TeamID: team.ID, Segments: []string{"ghost-seg"}, Roles: []string{"ghost-role"},
	})
	if err == nil {
		t.Fatal("expected error for unknown nodes")
	}
	app, ok := err.(*service.AppError)
	if !ok || app.Code != "validation_failed" || app.Status != 400 {
		t.Fatalf("bad error: %+v", err)
	}
	details, ok := app.Details["errors"].([]service.FieldError)
	if !ok || len(details) != 2 {
		t.Fatalf("expected 2 field errors, got %+v", app.Details)
	}
}

// I4: CreateRole с несуществующим agent_spec → 404 not_found.
func TestCreateRoleMissingSpec(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "s"})
	_, err := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "lead", AgentSpec: "agents/definitely-missing.yaml"})
	isAppErr(t, err, "not_found")
}

// ---------- Archive / 404 ----------

func TestGetRoleConfigParsesYaml(t *testing.T) {
	svc := newTestService(t)
	dir := t.TempDir()
	t.Setenv("DAEMON_AGENT_SPECS_DIR", dir)
	svc.SpecsDir = dir

	specContent := `name: pi-go-backend
version: 0.1.0
runtime:
  type: pi
  version: ">=0.5.0"
pi_config:
  model: claude-3-7-sonnet
  temperature: 0.7
  max_tokens: 4096
  plugins:
    - name: git
      enabled: true
    - name: http
      enabled: false
  skills:
    - path: skills/tdd
      enabled: true
  profiles:
    - name: project-x
    - name: debug
      description: debug profile
resources:
  cpu: "1.0"
  memory: 1G
  gpu: false
`
	specPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatal(err)
	}

	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "s"})
	role, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "a", AgentSpec: specPath})

	r, spec, profiles, err := svc.GetRoleConfig(ctx(), role.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.Available || spec.Name != "pi-go-backend" || spec.PiConfig.Model != "claude-3-7-sonnet" {
		t.Fatalf("bad spec: %+v", spec)
	}
	if len(spec.PiConfig.Plugins) != 2 || !spec.PiConfig.Plugins[0].Enabled || spec.PiConfig.Plugins[1].Enabled {
		t.Fatalf("bad plugins: %+v", spec.PiConfig.Plugins)
	}
	if len(profiles) != 2 || profiles[0].Name != "project-x" || profiles[1].Description != "debug profile" {
		t.Fatalf("bad profiles: %+v", profiles)
	}
	_ = r

	// path traversal: файл вне разрешённых корней → stub
	role2, _ := svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "b", AgentSpec: "/etc/hostname"})
	_, spec2, _, err := svc.GetRoleConfig(ctx(), role2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if spec2.Available {
		t.Fatalf("expected unavailable stub for /etc/hostname, got %+v", spec2)
	}
}

func TestNotFoundAndArchive(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.GetTeam(ctx(), 42)
	isAppErr(t, err, "not_found")

	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	if err := svc.ArchiveTeam(ctx(), team.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveTeam(ctx(), team.ID); err == nil {
		t.Fatal("expected conflict on second archive")
	}
	// archived команда читается
	if _, err := svc.GetTeam(ctx(), team.ID); err != nil {
		t.Fatal(err)
	}
}

func TestListTeams(t *testing.T) {
	svc := newTestService(t)
	team, _ := svc.CreateTeam(ctx(), service.CreateTeamRequest{Name: "t"})
	seg, _ := svc.CreateSegment(ctx(), service.CreateSegmentRequest{TeamID: team.ID, Name: "s"})
	_, _ = svc.CreateRole(ctx(), service.CreateRoleRequest{SegmentID: seg.ID, Name: "a", AgentSpec: specFile(t, svc, "x.yaml")})

	teams, total, err := svc.ListTeams(ctx(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(teams) != 1 {
		t.Fatalf("bad total: %d", total)
	}
	if teams[0].SegmentsCount != 1 || teams[0].RolesCount != 1 {
		t.Fatalf("bad counts: %+v", teams[0])
	}
}

// ---------- helpers ----------

func findRole(t *testing.T, roles []*models.Role, name string) *models.Role {
	t.Helper()
	for _, r := range roles {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("role %q not found", name)
	return nil
}

func mustGetTeam(t *testing.T, svc *service.TeamService, teamID int64) *service.TeamDetail {
	t.Helper()
	d, err := svc.GetTeam(ctx(), teamID)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func strPtr(s string) *string { return &s }
