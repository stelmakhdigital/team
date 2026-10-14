package service

// C1 / P4.3 — Team bundle: перенос команды (топология + конфиг ролей + layout +
// воркфлоу команды) между даemons/проектами как один JSON-файл.
//
// Формат v1 — docs/contracts/api-decisions.md §P4.3:
//   - переносится: team meta (name/description/posture), segments, roles
//     (name/profile/agent_spec/config), relatives (адреса "Segment.Role"),
//     layout (та же форма, что контракт TopologyLayout, но *_id — 0-based
//     индексы в массивы bundle), workflows команды (name/description/
//     engine_enabled + blocks/connections в config).
//   - НЕ переносится: project (dir — локальная сущность; при импорте
//     project_id = null), sessions/tasks/messages/history/attention
//     (состояние, а не топология).
//   - воркфлоу при импорте — state начальный (draft), не активируются.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

const (
	// BundleKindTeam — kind bundle v1.
	BundleKindTeam = "team-bundle"
	// BundleVersion — поддерживаемая версия формата (import: только 1).
	BundleVersion = 1
)

// ---------- Формат bundle (v1) ----------

// TeamBundle — корневой объект bundle.
type TeamBundle struct {
	Version    int              `json:"version"`
	Kind       string           `json:"kind"`
	ExportedAt string           `json:"exported_at,omitempty"`
	Team       *BundleTeam      `json:"team"`
	Workflows  []BundleWorkflow `json:"workflows"`
}

type BundleTeam struct {
	Name        string           `json:"name"`
	Description *string          `json:"description"`
	Posture     *string          `json:"posture"`
	Segments    []BundleSegment  `json:"segments"`
	Relatives   []BundleRelative `json:"relatives"`
	Layout      *BundleLayout    `json:"layout,omitempty"`
}

type BundleSegment struct {
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	Config      map[string]any `json:"config,omitempty"` // config сегмента без layout
	Roles       []BundleRole   `json:"roles"`
}

type BundleRole struct {
	Name      string         `json:"name"`
	Role      *string        `json:"role"` // profile роли ("" → null)
	AgentSpec string         `json:"agent_spec"`
	Config    map[string]any `json:"config,omitempty"` // config роли без layout
}

type BundleRelative struct {
	From   string         `json:"from"` // "Segment.Role"
	To     string         `json:"to"`   // "Segment.Role"
	Kind   string         `json:"kind"`
	Label  *BundlePoint   `json:"label"` // позиция label (null — нет)
	Config map[string]any `json:"config,omitempty"`
}

type BundlePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// BundleLayout — та же форма, что контракт TopologyLayout
// (docs/contracts/openapi.yaml), но segment_id/role_id/relative_id/from_role_id/
// to_role_id — 0-based ИНДЕКСЫ в массивы bundle (team.segments / flat-роли в
// порядке сегментов / team.relatives), а не DB-идентификаторы.
type BundleLayout struct {
	Segments  []BundleSegmentLayout  `json:"segments"`
	Roles     []BundleRoleLayout     `json:"roles"`
	Relatives []BundleRelativeLayout `json:"relatives"`
}

type BundleSegmentLayout struct {
	SegmentID int64         `json:"segment_id"`
	Position  BundlePosSize `json:"position"`
	Collapsed bool          `json:"collapsed"`
}

type BundlePosSize struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type BundleRoleLayout struct {
	RoleID    int64       `json:"role_id"`
	SegmentID int64       `json:"segment_id"`
	Position  BundlePoint `json:"position"`
}

type BundleRelativeLayout struct {
	RelativeID int64   `json:"relative_id"`
	FromRoleID int64   `json:"from_role_id"`
	ToRoleID   int64   `json:"to_role_id"`
	Path       []Point `json:"path,omitempty"`
}

// BundleWorkflow — воркфлоу команды. Config несёт структуру воркфлоу
// (блоки/связи) — те же структуры, что CreateWorkflowRequest:
//
//	{"blocks": [{"type","position":{"x","y"},"config","label"}],
//	 "connections": [{"from","to","condition"}]} — from/to — индексы blocks.
type BundleWorkflow struct {
	Name          string         `json:"name"`
	Description   *string        `json:"description"`
	Config        map[string]any `json:"config"`
	EngineEnabled bool           `json:"engine_enabled"`
}

// bundleBlock / bundleConnection — элементы workflow.config (внутренний разбор).
type bundleBlock struct {
	Type     string         `json:"type"`
	Position *BundlePoint   `json:"position"`
	Config   map[string]any `json:"config"`
	Label    *string        `json:"label"`
}

type bundleConnection struct {
	From      int64   `json:"from"`
	To        int64   `json:"to"`
	Condition *string `json:"condition"`
}

// ---------- Export ----------

// ExportTeam — собирает TeamBundle команды (RBAC teams.read, handler).
func (s *TeamService) ExportTeam(ctx context.Context, teamID int64) (*TeamBundle, error) {
	detail, err := s.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	team := detail.Team

	bundle := &TeamBundle{
		Version:    BundleVersion,
		Kind:       BundleKindTeam,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Team: &BundleTeam{
			Name:        team.Name,
			Description: optionalString(team.Description),
			Posture:     ptr(string(team.Posture)),
			Segments:    []BundleSegment{},
			Relatives:   []BundleRelative{},
		},
		Workflows: []BundleWorkflow{},
	}

	segNameByID := make(map[int64]string, len(detail.Segments))
	segIndexByID := make(map[int64]int, len(detail.Segments))
	for i, sw := range detail.Segments {
		segNameByID[sw.ID] = sw.Name
		segIndexByID[sw.ID] = i
	}
	// flat-порядок ролей (сегменты в порядке списка, внутри — по id):
	// index — для layout.roles.role_id / layout.relatives.*_role_id.
	roleIndexByID := make(map[int64]int, len(detail.Roles))
	flatRoles := []*models.Role{}
	for _, sw := range detail.Segments {
		for _, r := range detail.Roles {
			if r.SegmentID == sw.ID {
				roleIndexByID[r.ID] = len(flatRoles)
				flatRoles = append(flatRoles, r)
			}
		}
	}

	for _, sw := range detail.Segments {
		seg := &BundleSegment{
			Name:        sw.Name,
			Description: optionalString(sw.Description),
			Config:      configWithoutLayout(sw.Config),
			Roles:       []BundleRole{},
		}
		for _, r := range detail.Roles {
			if r.SegmentID != sw.ID {
				continue
			}
			seg.Roles = append(seg.Roles, BundleRole{
				Name:      r.Name,
				Role:      optionalString(r.Profile),
				AgentSpec: r.AgentSpec,
				Config:    configWithoutLayout(r.Config),
			})
		}
		bundle.Team.Segments = append(bundle.Team.Segments, *seg)
	}

	roleSeg := make(map[int64]string, len(detail.Roles))
	for _, r := range detail.Roles {
		roleSeg[r.ID] = segNameByID[r.SegmentID]
	}
	for _, rw := range detail.Relatives {
		rel := BundleRelative{
			From:   roleSeg[rw.FromRoleID] + "." + rw.FromRoleName,
			To:     roleSeg[rw.ToRoleID] + "." + rw.ToRoleName,
			Kind:   string(rw.Type),
			Label:  nil,
			Config: configWithoutLayout(rw.Config),
		}
		if l := getLayout(rw.Config); l != nil && l.HasLabel {
			rel.Label = &BundlePoint{X: l.LabelX, Y: l.LabelY}
		}
		bundle.Team.Relatives = append(bundle.Team.Relatives, rel)
	}

	layout := &BundleLayout{Segments: []BundleSegmentLayout{}, Roles: []BundleRoleLayout{}, Relatives: []BundleRelativeLayout{}}
	for i, sw := range detail.Segments {
		l := getLayout(sw.Config)
		if l == nil {
			continue
		}
		collapsed := false
		if l.Collapsed != nil {
			collapsed = *l.Collapsed
		}
		layout.Segments = append(layout.Segments, BundleSegmentLayout{
			SegmentID: int64(i),
			Position:  BundlePosSize{X: l.X, Y: l.Y, Width: l.Width, Height: l.Height},
			Collapsed: collapsed,
		})
	}
	for i, r := range flatRoles {
		l := getLayout(r.Config)
		if l == nil {
			continue
		}
		layout.Roles = append(layout.Roles, BundleRoleLayout{
			RoleID:    int64(i),
			SegmentID: int64(segIndexByID[r.SegmentID]),
			Position:  BundlePoint{X: l.X, Y: l.Y},
		})
	}
	for i, rw := range detail.Relatives {
		entry := BundleRelativeLayout{
			RelativeID: int64(i),
			FromRoleID: int64(roleIndexByID[rw.FromRoleID]),
			ToRoleID:   int64(roleIndexByID[rw.ToRoleID]),
		}
		if l := getLayout(rw.Config); l != nil && len(l.Path) > 0 {
			entry.Path = l.Path
		}
		layout.Relatives = append(layout.Relatives, entry)
	}
	if len(layout.Segments) > 0 || len(layout.Roles) > 0 || len(layout.Relatives) > 0 {
		bundle.Team.Layout = layout
	}

	// workflows команды (только team_id = экспортируемой).
	wfs, _, err := s.workflows.List(ctx, s.db, repository.WorkflowFilter{TeamID: &teamID})
	if err != nil {
		return nil, err
	}
	for _, w := range wfs {
		bw := BundleWorkflow{
			Name:          w.Name,
			Description:   w.Description,
			Config:        map[string]any{},
			EngineEnabled: w.EngineEnabled,
		}
		blocks, err := s.wfBlocks.ListByWorkflow(ctx, s.db, w.ID)
		if err != nil {
			return nil, err
		}
		blockIndex := make(map[int64]int, len(blocks))
		bb := make([]bundleBlock, 0, len(blocks))
		for i, b := range blocks {
			blockIndex[b.ID] = i
			bb = append(bb, bundleBlock{
				Type:     string(b.Type),
				Position: &BundlePoint{X: float64(b.PositionX), Y: float64(b.PositionY)},
				Config:   cloneConfig(b.Config),
				Label:    b.Label,
			})
		}
		conns, err := s.wfConns.ListByWorkflow(ctx, s.db, w.ID)
		if err != nil {
			return nil, err
		}
		bc := make([]bundleConnection, 0, len(conns))
		for _, c := range conns {
			bc = append(bc, bundleConnection{
				From:      int64(blockIndex[c.FromBlockID]),
				To:        int64(blockIndex[c.ToBlockID]),
				Condition: c.Condition,
			})
		}
		bw.Config["blocks"] = bb
		bw.Config["connections"] = bc
		bundle.Workflows = append(bundle.Workflows, bw)
	}
	return bundle, nil
}

// optionalString — "" → nil (bundle-поля description/role — nullable).
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// configWithoutLayout — config без ключа "layout" (layout живёт в bundle.layout).
// Пустой результат → nil (JSON-поле omit).
func configWithoutLayout(cfg map[string]any) map[string]any {
	if cfg == nil {
		return nil
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if k == "layout" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ---------- Import ----------

// ImportedWorkflow — воркфлоу, созданный при импорте (имя с учётом auto-suffix).
type ImportedWorkflow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ImportTeamResult — результат импорта (команда + counts + созданные воркфлоу).
type ImportTeamResult struct {
	Team          *models.Team
	SegmentsCount int
	RolesCount    int
	Workflows     []ImportedWorkflow
}

// ImportTeam — импорт TeamBundle: команда со всей топологией + layout (одна
// транзакция, как CreateTeam) + воркфлоу команды (state draft).
// Конфликты имён — auto-suffix ("name (2)", "name (3)", ...), без 409.
func (s *TeamService) ImportTeam(ctx context.Context, b *TeamBundle) (*ImportTeamResult, error) {
	if b.Version != BundleVersion {
		return nil, &AppError{
			Code:    "unsupported_version",
			Status:  400,
			Message: fmt.Sprintf("unsupported bundle version %d (only %d)", b.Version, BundleVersion),
		}
	}
	if b.Kind != BundleKindTeam {
		return nil, &AppError{
			Code:    "invalid_bundle",
			Status:  400,
			Message: fmt.Sprintf("unsupported bundle kind %q (want %q)", b.Kind, BundleKindTeam),
		}
	}
	var fieldErrs []FieldError
	addErr := func(field, reason string) {
		fieldErrs = append(fieldErrs, FieldError{Field: field, Reason: reason})
	}

	if b.Team == nil {
		addErr("team", "required")
	} else {
		if strings.TrimSpace(b.Team.Name) == "" {
			addErr("team.name", "required")
		}
		if b.Team.Posture != nil && !models.Posture(*b.Team.Posture).Valid() {
			addErr("team.posture", fmt.Sprintf("invalid value %q (want delegated|human_led)", *b.Team.Posture))
		}
		seenSegs := map[string]bool{}
		roleAddrs := map[string]bool{}
		totalRoles := 0
		for i, seg := range b.Team.Segments {
			if strings.TrimSpace(seg.Name) == "" {
				addErr(fmt.Sprintf("team.segments[%d].name", i), "required")
				continue
			}
			if seenSegs[seg.Name] {
				addErr(fmt.Sprintf("team.segments[%d].name", i), fmt.Sprintf("duplicate segment %q", seg.Name))
			}
			seenSegs[seg.Name] = true
			if len(seg.Roles) == 0 {
				addErr(fmt.Sprintf("team.segments[%d].roles", i), "segment must contain at least 1 role")
			}
			seenRoleNames := map[string]bool{}
			for j, r := range seg.Roles {
				if strings.TrimSpace(r.Name) == "" {
					addErr(fmt.Sprintf("team.segments[%d].roles[%d].name", i, j), "required")
				}
				if strings.TrimSpace(r.AgentSpec) == "" {
					addErr(fmt.Sprintf("team.segments[%d].roles[%d].agent_spec", i, j), "required")
				}
				if seenRoleNames[r.Name] {
					addErr(fmt.Sprintf("team.segments[%d].roles[%d].name", i, j),
						fmt.Sprintf("duplicate role %q in segment %q", r.Name, seg.Name))
				}
				seenRoleNames[r.Name] = true
				totalRoles++
				roleAddrs[seg.Name+"."+r.Name] = true
			}
		}
		if len(b.Team.Segments) == 0 {
			addErr("team.segments", "at least 1 segment required")
		}
		if totalRoles == 0 {
			addErr("team.roles", "at least 1 role required")
		}
		seenRels := map[string]bool{}
		for i, rel := range b.Team.Relatives {
			if !roleAddrs[rel.From] {
				addErr(fmt.Sprintf("team.relatives[%d].from", i), fmt.Sprintf("unknown role address %q", rel.From))
			}
			if !roleAddrs[rel.To] {
				addErr(fmt.Sprintf("team.relatives[%d].to", i), fmt.Sprintf("unknown role address %q", rel.To))
			}
			if !models.RelativeType(rel.Kind).Valid() {
				addErr(fmt.Sprintf("team.relatives[%d].kind", i), fmt.Sprintf("invalid value %q", rel.Kind))
			}
			dupKey := rel.From + "→" + rel.To + "→" + rel.Kind
			if seenRels[dupKey] {
				addErr(fmt.Sprintf("team.relatives[%d]", i), "duplicate relative (same from/to/kind)")
			}
			seenRels[dupKey] = true
		}
	}
	for i, wf := range b.Workflows {
		if strings.TrimSpace(wf.Name) == "" {
			addErr(fmt.Sprintf("workflows[%d].name", i), "required")
			continue
		}
		if err := validateBundleWorkflowConfig(wf.Config, func(field, reason string) {
			addErr(fmt.Sprintf("workflows[%d].%s", i, field), reason)
		}); err != nil {
			return nil, err
		}
	}
	if len(fieldErrs) > 0 {
		return nil, NewValidation("invalid team bundle", fieldErrs)
	}

	// auto-suffix: имя команды (глобальное unique) и имён воркфлоу (unique в команде).
	teamName, err := s.uniqueTeamName(ctx, b.Team.Name)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	posture := models.PostureDelegated
	if b.Team.Posture != nil {
		posture = models.Posture(*b.Team.Posture)
	}
	team := &models.Team{Name: teamName, Description: derefString(b.Team.Description), Posture: posture}
	if err := s.teams.Create(ctx, tx, team); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("team %q already exists", team.Name))
		}
		return nil, err
	}
	// Slice 4: team-level chatroom (как в CreateTeam).
	if err := s.chatRooms.Create(ctx, tx, &models.Chatroom{
		TeamID: team.ID, Name: team.Name, Topic: ptr("Whole team"),
	}); err != nil {
		return nil, err
	}

	// segments + layout.
	segIDs := make(map[string]int64, len(b.Team.Segments))
	for i, seg := range b.Team.Segments {
		cfg := cloneConfig(seg.Config)
		if l := bundleSegmentLayout(b.Team.Layout, i); l != nil {
			cfg = setLayout(cfg, l)
		}
		segModel := &models.Segment{TeamID: team.ID, Name: seg.Name, Description: derefString(seg.Description), Config: cfg}
		if err := s.segments.Create(ctx, tx, segModel); err != nil {
			return nil, err
		}
		if err := s.chatRooms.Create(ctx, tx, &models.Chatroom{
			TeamID: team.ID, SegmentID: &segModel.ID, Name: seg.Name + "-general",
			Topic: ptr(capFirst(seg.Name) + " channel"),
		}); err != nil {
			return nil, err
		}
		segIDs[seg.Name] = segModel.ID
	}

	// roles (flat-порядок = порядок сегментов × ролей внутри) + layout.
	roleIDByAddr := make(map[string]int64)
	flatRoleCount := 0
	for _, seg := range b.Team.Segments {
		for _, r := range seg.Roles {
			cfg := cloneConfig(r.Config)
			if l := bundleRoleLayout(b.Team.Layout, flatRoleCount); l != nil {
				cfg = setLayout(cfg, l)
			}
			role := &models.Role{
				TeamID:    team.ID,
				SegmentID: segIDs[seg.Name],
				Name:      r.Name,
				Address:   team.Name + ":" + seg.Name + "." + r.Name,
				AgentSpec: r.AgentSpec,
				Profile:   derefString(r.Role),
				Config:    cfg,
			}
			if err := s.roles.Create(ctx, tx, role); err != nil {
				return nil, err
			}
			roleIDByAddr[seg.Name+"."+r.Name] = role.ID
			flatRoleCount++
		}
	}

	// relatives + layout (path/label).
	for i, rel := range b.Team.Relatives {
		cfg := cloneConfig(rel.Config)
		layoutL := &Layout{}
		hasLayout := false
		if bl := bundleRelativeLayout(b.Team.Layout, i); bl != nil {
			layoutL.Path = bl.Path
			hasLayout = len(bl.Path) > 0
		}
		if rel.Label != nil {
			layoutL.LabelX, layoutL.LabelY = rel.Label.X, rel.Label.Y
			layoutL.HasLabel = true
			hasLayout = true
		}
		if hasLayout {
			cfg = setLayout(cfg, layoutL)
		}
		if err := s.createRelativeOnTx(ctx, tx, &models.Relative{
			TeamID: team.ID, FromRoleID: roleIDByAddr[rel.From], ToRoleID: roleIDByAddr[rel.To],
			Type: models.RelativeType(rel.Kind), Config: cfg,
		}); err != nil {
			return nil, err
		}
	}

	// workflows (state draft — не активируем при импорте; auto-suffix имён).
	takenWFNames := map[string]bool{}
	createdWFs := []ImportedWorkflow{}
	for _, bw := range b.Workflows {
		name := uniqueNameInSet(bw.Name, takenWFNames)
		takenWFNames[name] = true
		w := &models.Workflow{
			TeamID: team.ID, Name: name, Description: bw.Description,
			State: models.WorkflowDraft, Config: map[string]any{}, EngineEnabled: bw.EngineEnabled,
		}
		if err := s.workflows.Create(ctx, tx, w); err != nil {
			return nil, err
		}
		blocks := bundleBlocks(bw.Config)
		created := make([]*models.WorkflowBlock, 0, len(blocks))
		for _, bb := range blocks {
			b := &models.WorkflowBlock{
				WorkflowID: w.ID, Type: models.WorkflowBlockType(bb.Type),
				Config: cloneConfig(bb.Config), Label: bb.Label,
			}
			if bb.Position != nil {
				b.PositionX = int(bb.Position.X)
				b.PositionY = int(bb.Position.Y)
			}
			if err := s.wfBlocks.Create(ctx, tx, b); err != nil {
				return nil, err
			}
			created = append(created, b)
		}
		for _, bc := range bundleConnections(bw.Config) {
			if bc.From < 0 || bc.From >= int64(len(created)) || bc.To < 0 || bc.To >= int64(len(created)) {
				return nil, NewValidation("invalid request", []FieldError{{
					Field: "workflows.connections", Reason: "from/to must be indices into blocks",
				}})
			}
			if err := s.wfConns.Create(ctx, tx, &models.WorkflowConnection{
				WorkflowID: w.ID, FromBlockID: created[bc.From].ID, ToBlockID: created[bc.To].ID,
				Condition: bc.Condition,
			}); err != nil {
				return nil, err
			}
		}
		createdWFs = append(createdWFs, ImportedWorkflow{ID: w.ID, Name: name})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rolesCount := 0
	for _, seg := range b.Team.Segments {
		rolesCount += len(seg.Roles)
	}
	return &ImportTeamResult{
		Team: team, Workflows: createdWFs,
		SegmentsCount: len(b.Team.Segments), RolesCount: rolesCount,
	}, nil
}

// validateBundleWorkflowConfig — разбор workflow.config (blocks/connections)
// + валидация типов/индексов.
func validateBundleWorkflowConfig(cfg map[string]any, addErr func(field, reason string)) error {
	blocks := bundleBlocks(cfg)
	for i, b := range blocks {
		if !models.WorkflowBlockType(b.Type).Valid() {
			addErr(fmt.Sprintf("config.blocks[%d].type", i), fmt.Sprintf("invalid value %q", b.Type))
		}
	}
	for i, c := range bundleConnections(cfg) {
		if c.From < 0 || c.From >= int64(len(blocks)) {
			addErr(fmt.Sprintf("config.connections[%d].from", i), "must be an index into blocks")
		}
		if c.To < 0 || c.To >= int64(len(blocks)) {
			addErr(fmt.Sprintf("config.connections[%d].to", i), "must be an index into blocks")
		}
	}
	return nil
}

func bundleBlocks(cfg map[string]any) []bundleBlock {
	out := []bundleBlock{}
	raw, _ := cfg["blocks"].([]any)
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		b := bundleBlock{Config: map[string]any{}}
		if t, ok := m["type"].(string); ok {
			b.Type = t
		}
		if c, ok := m["config"].(map[string]any); ok {
			b.Config = c
		}
		if l, ok := m["label"].(string); ok {
			b.Label = &l
		}
		if p, ok := m["position"].(map[string]any); ok {
			pt := &BundlePoint{}
			if x, ok := p["x"].(float64); ok {
				pt.X = x
			}
			if y, ok := p["y"].(float64); ok {
				pt.Y = y
			}
			b.Position = pt
		}
		out = append(out, b)
	}
	return out
}

func bundleConnections(cfg map[string]any) []bundleConnection {
	out := []bundleConnection{}
	raw, _ := cfg["connections"].([]any)
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		c := bundleConnection{}
		if f, ok := m["from"].(float64); ok {
			c.From = int64(f)
		}
		if t, ok := m["to"].(float64); ok {
			c.To = int64(t)
		}
		if cond, ok := m["condition"].(string); ok {
			c.Condition = &cond
		}
		out = append(out, c)
	}
	return out
}

// bundleSegmentLayout — layout сегмента по bundle-индексу (или nil).
func bundleSegmentLayout(l *BundleLayout, segIndex int) *Layout {
	if l == nil {
		return nil
	}
	for _, e := range l.Segments {
		if e.SegmentID == int64(segIndex) {
			collapsed := e.Collapsed
			return &Layout{
				X: e.Position.X, Y: e.Position.Y, Width: e.Position.Width, Height: e.Position.Height,
				Collapsed: &collapsed,
			}
		}
	}
	return nil
}

// bundleRoleLayout — layout роли по flat-индексу (или nil).
func bundleRoleLayout(l *BundleLayout, roleIndex int) *Layout {
	if l == nil {
		return nil
	}
	for _, e := range l.Roles {
		if e.RoleID == int64(roleIndex) {
			return &Layout{X: e.Position.X, Y: e.Position.Y}
		}
	}
	return nil
}

// bundleRelativeLayout — layout relative по bundle-индексу (path; или nil).
func bundleRelativeLayout(l *BundleLayout, relIndex int) *BundleRelativeLayout {
	if l == nil {
		return nil
	}
	for i := range l.Relatives {
		if l.Relatives[i].RelativeID == int64(relIndex) {
			return &l.Relatives[i]
		}
	}
	return nil
}

// uniqueTeamName — base или "base (2)", "base (3)", ... — первое свободное
// (teams.name UNIQUE, глобально).
func (s *TeamService) uniqueTeamName(ctx context.Context, base string) (string, error) {
	if _, err := s.teams.GetByName(ctx, s.db, base); errors.Is(err, repository.ErrNotFound) {
		return base, nil
	} else if err != nil {
		return "", err
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", base, n)
		if _, err := s.teams.GetByName(ctx, s.db, candidate); errors.Is(err, repository.ErrNotFound) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
		if n > 10_000 {
			return "", NewConflict(fmt.Sprintf("cannot find a free name for team %q", base))
		}
	}
}

// uniqueNameInSet — base или "base (2)", ... — первое имя, отсутствующее в set
// (set не мутируется; вызывающий добавляет результат сам).
func uniqueNameInSet(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", base, n)
		if !taken[candidate] {
			return candidate
		}
		if n > 10_000 {
			return base + " (max)"
		}
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
