package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// LibraryService — Library: save/get/apply (контракт 20 §5).
type LibraryService struct {
	db       *sql.DB
	teams    teamStore
	teamSvc  *TeamService
	roles    roleLister
	segments segmentLister
	wf       *repository.WorkflowRepo
	wfBlocks *repository.WorkflowBlockRepo
	wfConns  *repository.WorkflowConnectionRepo
	items    *repository.LibraryRepo
	versions *repository.LibraryVersionRepo
}

// segmentLister — часть SegmentRepo, нужна LibraryService (снапшот/apply сегментов).
type segmentLister interface {
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Segment, error)
	GetByTeamAndName(ctx context.Context, tx repository.DBTX, teamID int64, name string) (*models.Segment, error)
	ListByTeam(ctx context.Context, tx repository.DBTX, teamID int64) ([]*models.Segment, error)
}

func NewLibraryService(db *sql.DB, s *repository.Stores, teamSvc *TeamService) *LibraryService {
	return &LibraryService{
		db: db, teams: s.Teams, teamSvc: teamSvc, roles: s.Roles, segments: s.Segments,
		wf: s.Workflows, wfBlocks: s.WorkflowBlocks, wfConns: s.WorkflowConnections,
		items: s.Library, versions: s.LibraryVersions,
	}
}

// ---------- Views (контракт 20 §5) ----------

// LibraryItemView — элемент библиотеки (контракт: LibraryItem).
type LibraryItemView struct {
	ID             int64    `json:"id"`
	Type           string   `json:"type"`
	Name           string   `json:"name"`
	Description    *string  `json:"description,omitempty"`
	Group          *string  `json:"group,omitempty"`
	Version        string   `json:"version"`
	Author         *string  `json:"author,omitempty"`
	IsPublic       bool     `json:"is_public"`
	DownloadsCount int      `json:"downloads_count"`
	Tags           []string `json:"tags,omitempty"`
	Thumbnail      *string  `json:"thumbnail,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func libraryItemView(it *models.LibraryItem) *LibraryItemView {
	group := ""
	if it.Group != "" && it.Group != "general" {
		group = it.Group
	}
	var groupPtr *string
	if group != "" {
		groupPtr = &group
	}
	return &LibraryItemView{
		ID: it.ID, Type: string(it.Type), Name: it.Name, Description: it.Description,
		Group: groupPtr, Version: it.Version, Author: it.Author,
		IsPublic: it.IsPublic, DownloadsCount: it.DownloadsCount, Tags: it.Tags,
		Thumbnail: it.Thumbnail,
		CreatedAt: it.CreatedAt.Format(rfc3339), UpdatedAt: it.UpdatedAt.Format(rfc3339),
	}
}

type libraryGroupView struct {
	Name       string `json:"name"`
	ItemsCount int    `json:"items_count"`
}

// LibraryVersionView — версия (контракт: LibraryVersion).
type LibraryVersionView struct {
	Version   string  `json:"version"`
	CreatedAt string  `json:"created_at"`
	Changes   *string `json:"changes,omitempty"`
	Author    *string `json:"author,omitempty"`
}

// ---------- Requests ----------

type SaveToLibraryRequest struct {
	Type        models.LibraryItemType `json:"type"`
	SourceID    int64                  `json:"source_id"`
	Name        string                 `json:"name"`
	Description *string                `json:"description"`
	Group       *string                `json:"group"`
	IsPublic    *bool                  `json:"is_public"`
	Tags        []string               `json:"tags"`
}

type ApplyLibraryItemRequest struct {
	TargetTeamID *int64         `json:"target_team_id"`
	Overrides    map[string]any `json:"overrides"` // опц.: name для новой команды
}

type ApplyResult struct {
	Status           string            `json:"status"`
	CreatedResources CreatedResources  `json:"created_resources,omitempty"`
	UpdatedResources *UpdatedResources `json:"updated_resources,omitempty"`
}

type CreatedResources struct {
	Teams    []int64 `json:"teams,omitempty"`
	Segments []int64 `json:"segments,omitempty"`
	Roles    []int64 `json:"roles,omitempty"`
}

type UpdatedResources struct {
	Teams []int64 `json:"teams,omitempty"`
}

// ---------- Use cases ----------

// List — GET /api/v1/library (контракт 20 §5.1).
func (s *LibraryService) List(ctx context.Context, f repository.LibraryFilter) ([]*LibraryItemView, int, []libraryGroupView, error) {
	items, total, err := s.items.List(ctx, s.db, f)
	if err != nil {
		return nil, 0, nil, err
	}
	counts, err := s.items.GroupCounts(ctx, s.db, f)
	if err != nil {
		return nil, 0, nil, err
	}
	views := make([]*LibraryItemView, 0, len(items))
	for _, it := range items {
		views = append(views, libraryItemView(it))
	}
	groups := make([]libraryGroupView, 0, len(counts))
	for name, count := range counts {
		groups = append(groups, libraryGroupView{Name: name, ItemsCount: count})
	}
	return views, total, groups, nil
}

// Get — GET /api/v1/library/{id} (контракт 20 §5.3).
func (s *LibraryService) Get(ctx context.Context, id int64) (*LibraryItemView, map[string]any, []*LibraryVersionView, error) {
	it, err := s.items.GetByID(ctx, s.db, id)
	if err != nil {
		return nil, nil, nil, wrapRepositoryNotFound(err, "library item")
	}
	versions, err := s.versions.ListByItem(ctx, s.db, id)
	if err != nil {
		return nil, nil, nil, err
	}
	vv := make([]*LibraryVersionView, 0, len(versions))
	for _, v := range versions {
		vv = append(vv, &LibraryVersionView{
			Version: v.Version, CreatedAt: v.CreatedAt.Format(rfc3339),
			Changes: v.Changes, Author: v.Author,
		})
	}
	return libraryItemView(it), it.Spec, vv, nil
}

// Save — POST /api/v1/library (контракт 20 §5.2): снапшот spec источника.
func (s *LibraryService) Save(ctx context.Context, req SaveToLibraryRequest) (*models.LibraryItem, error) {
	if req.Name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "required"}})
	}
	if !req.Type.Valid() {
		return nil, NewValidation("invalid request", []FieldError{{Field: "type", Reason: "invalid"}})
	}
	spec, sourceType, err := s.snapshotSpec(ctx, req.Type, req.SourceID)
	if err != nil {
		return nil, err
	}

	group := "general"
	if req.Group != nil && *req.Group != "" {
		group = *req.Group
	}
	item := &models.LibraryItem{
		Type: req.Type, Name: req.Name, Description: req.Description,
		Group: group, Spec: spec, SourceType: strPtr(string(sourceType)), SourceID: &req.SourceID,
	}
	if req.IsPublic != nil {
		item.IsPublic = *req.IsPublic
	}
	item.Tags = req.Tags

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.items.Create(ctx, tx, item); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("library item %s:%q already exists", req.Type, req.Name))
		}
		return nil, err
	}
	changes := "initial snapshot"
	if err := s.versions.Create(ctx, tx, &models.LibraryVersion{
		ItemID: item.ID, Version: item.Version, Changes: &changes,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

// Apply — POST /api/v1/library/{id}/apply (контракт 20 §5.4).
func (s *LibraryService) Apply(ctx context.Context, id int64, req ApplyLibraryItemRequest) (*ApplyResult, error) {
	it, err := s.items.GetByID(ctx, s.db, id)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "library item")
	}

	switch it.Type {
	case models.LibTeam:
		res, err := s.applyTeam(ctx, it, req)
		if err != nil {
			return nil, err
		}
		s.bumpDownloads(ctx, id)
		return res, nil
	case models.LibWorkflow:
		if req.TargetTeamID == nil {
			return nil, NewValidation("target_team_id is required for workflow apply")
		}
		if _, err := s.teams.GetByID(ctx, s.db, *req.TargetTeamID); err != nil {
			return nil, wrapRepositoryNotFound(err, "team")
		}
		// workflow spec: name + blocks + connections (индексы)
		spec := it.Spec
		name, _ := spec["name"].(string)
		wf := &models.Workflow{TeamID: *req.TargetTeamID, Name: name, Description: ptr(strFromAny(spec["description"]))}
		if wf.Name == "" {
			wf.Name = it.Name
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()
		if err := s.wf.Create(ctx, tx, wf); err != nil {
			if repository.IsUniqueViolation(err) {
				return nil, NewConflict(fmt.Sprintf("workflow %q already exists in team", wf.Name))
			}
			return nil, err
		}
		// blocks
		var blockIDs []int64
		if rawBlocks, ok := spec["blocks"].([]any); ok {
			for _, rb := range rawBlocks {
				bm, _ := rb.(map[string]any)
				pos := mapFromAny(bm["position"])
				b := &models.WorkflowBlock{
					WorkflowID: wf.ID, Type: models.WorkflowBlockType(strFromAny(bm["type"])),
					Config:    mapFromAny(bm["config"]),
					PositionX: intFromAny(pos["x"]), PositionY: intFromAny(pos["y"]),
				}
				if l, ok := bm["label"].(string); ok {
					b.Label = &l
				}
				if err := s.wfBlocks.Create(ctx, tx, b); err != nil {
					return nil, err
				}
				blockIDs = append(blockIDs, b.ID)
			}
		}
		if rawConns, ok := spec["connections"].([]any); ok {
			for _, rc := range rawConns {
				cm, _ := rc.(map[string]any)
				fromIdx := intFromAny(cm["from_block_id"])
				toIdx := intFromAny(cm["to_block_id"])
				if fromIdx < 0 || fromIdx >= len(blockIDs) || toIdx < 0 || toIdx >= len(blockIDs) {
					return nil, NewValidation("invalid workflow spec: bad connection index")
				}
				c := &models.WorkflowConnection{
					WorkflowID: wf.ID, FromBlockID: blockIDs[fromIdx], ToBlockID: blockIDs[toIdx],
				}
				if cond, ok := cm["condition"].(string); ok {
					c.Condition = &cond
				}
				if err := s.wfConns.Create(ctx, tx, c); err != nil {
					return nil, err
				}
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		s.bumpDownloads(ctx, id)
		return &ApplyResult{Status: "applied", CreatedResources: CreatedResources{Teams: []int64{*req.TargetTeamID}}}, nil
	case models.LibSegment:
		res, err := s.applySegment(ctx, it, req)
		if err != nil {
			return nil, err
		}
		s.bumpDownloads(ctx, id)
		return res, nil
	case models.LibRole:
		res, err := s.applyRole(ctx, it, req)
		if err != nil {
			return nil, err
		}
		s.bumpDownloads(ctx, id)
		return res, nil
	default:
		return nil, NewValidation(fmt.Sprintf("apply for library type %q is not supported", it.Type))
	}
}

// applySegment — снапшот сегмента (с его ролями) в команду (контракт 20 §5.4).
// target_team_id обязателен; семантика — merge (существующие по имени пропускаются).
func (s *LibraryService) applySegment(ctx context.Context, it *models.LibraryItem, req ApplyLibraryItemRequest) (*ApplyResult, error) {
	if req.TargetTeamID == nil {
		return nil, NewValidation("target_team_id is required for segment apply")
	}
	segName := strFromAny(it.Spec["name"])
	if segName == "" {
		return nil, NewValidation("invalid segment spec in library item")
	}
	spec := &TeamSpec{Segments: []SegmentSpec{{Name: segName, Description: strFromAny(it.Spec["description"])}}}
	if rawRoles, ok := it.Spec["roles"].([]any); ok {
		for _, rr := range rawRoles {
			rm, _ := rr.(map[string]any)
			spec.Roles = append(spec.Roles, RoleSpec{
				Segment:   segName,
				Name:      strFromAny(rm["name"]),
				AgentSpec: strFromAny(rm["agent_spec"]),
				Profile:   strFromAny(rm["profile"]),
			})
		}
	}
	merged, err := s.teamSvc.MergeSpec(ctx, MergeSpecRequest{TeamID: *req.TargetTeamID, Spec: spec})
	if err != nil {
		return nil, err
	}
	return &ApplyResult{
		Status:           "merged",
		CreatedResources: CreatedResources{Segments: merged.Segments, Roles: merged.Roles},
		UpdatedResources: &UpdatedResources{Teams: []int64{*req.TargetTeamID}},
	}, nil
}

// applyRole — снапшот роли в команду (контракт 20 §5.4). target_team_id обязателен.
// Сегмент: overrides.segment_id | overrides.segment (имя) | имя из снапшота;
// отсутствующий сегмент создаётся. Роль с тем же именем в сегменте → 409.
func (s *LibraryService) applyRole(ctx context.Context, it *models.LibraryItem, req ApplyLibraryItemRequest) (*ApplyResult, error) {
	if req.TargetTeamID == nil {
		return nil, NewValidation("target_team_id is required for role apply")
	}
	roleName := strFromAny(it.Spec["name"])
	agentSpec := strFromAny(it.Spec["agent_spec"])
	if roleName == "" || agentSpec == "" {
		return nil, NewValidation("invalid role spec in library item")
	}
	profile := strFromAny(it.Spec["profile"])
	snapshotSeg := strFromAny(it.Spec["segment"])

	team, err := s.teamSvc.GetTeam(ctx, *req.TargetTeamID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "team")
	}
	t := team.Team

	// 1. выбор/создание целевого сегмента
	segID, createdSeg, err := s.resolveRoleSegment(ctx, t, req.Overrides, snapshotSeg)
	if err != nil {
		return nil, err
	}
	// 2. роль (CreateRole: валидация agent_spec-файла, unique в сегменте → 409)
	role, err := s.teamSvc.CreateRole(ctx, CreateRoleRequest{
		SegmentID: segID, Name: roleName, AgentSpec: agentSpec, Profile: profile,
	})
	if err != nil {
		return nil, err
	}
	created := CreatedResources{Roles: []int64{role.ID}}
	if createdSeg != 0 {
		created.Segments = []int64{createdSeg}
	}
	return &ApplyResult{
		Status:           "applied",
		CreatedResources: created,
		UpdatedResources: &UpdatedResources{Teams: []int64{t.ID}},
	}, nil
}

// resolveRoleSegment — сегмент для role-apply: overrides.segment_id →
// overrides.segment (имя) → имя из снапшота → единственный сегмент команды;
// отсутствующий сегмент (по имени) создаётся. Валидация team: archived → 409.
func (s *LibraryService) resolveRoleSegment(ctx context.Context, team *models.Team, overrides map[string]any, snapshotSeg string) (int64, int64, error) {
	if overrides != nil {
		if v, ok := overrides["segment_id"]; ok {
			id := int64(intFromAny(v))
			if id <= 0 {
				return 0, 0, NewValidation("overrides.segment_id must be a positive integer")
			}
			seg, err := s.segments.GetByID(ctx, s.db, id)
			if err != nil {
				return 0, 0, wrapRepositoryNotFound(err, "segment")
			}
			if seg.TeamID != team.ID {
				return 0, 0, NewValidation("segment does not belong to target team")
			}
			return id, 0, nil
		}
		if v, ok := overrides["segment"].(string); ok && v != "" {
			return s.segmentByNameOrCreate(ctx, team, v)
		}
	}
	if snapshotSeg != "" {
		if seg, err := s.segments.GetByTeamAndName(ctx, s.db, team.ID, snapshotSeg); err == nil {
			return seg.ID, 0, nil
		}
		return s.segmentByNameOrCreate(ctx, team, snapshotSeg)
	}
	segs, err := s.segments.ListByTeam(ctx, s.db, team.ID)
	if err != nil {
		return 0, 0, err
	}
	if len(segs) == 1 {
		return segs[0].ID, 0, nil
	}
	if len(segs) == 0 {
		return s.segmentByNameOrCreate(ctx, team, "general")
	}
	return 0, 0, NewValidation("team has multiple segments: specify overrides.segment or overrides.segment_id")
}

func (s *LibraryService) segmentByNameOrCreate(ctx context.Context, team *models.Team, name string) (int64, int64, error) {
	if seg, err := s.segments.GetByTeamAndName(ctx, s.db, team.ID, name); err == nil {
		return seg.ID, 0, nil
	}
	seg, err := s.teamSvc.CreateSegment(ctx, CreateSegmentRequest{TeamID: team.ID, Name: name})
	if err != nil {
		return 0, 0, err
	}
	return seg.ID, seg.ID, nil
}

// applyTeam — новое создание команды или merge в существующую.
func (s *LibraryService) applyTeam(ctx context.Context, it *models.LibraryItem, req ApplyLibraryItemRequest) (*ApplyResult, error) {
	withName, ok := teamSpecFromJSON(it.Spec)
	if !ok {
		return nil, NewValidation("invalid team spec in library item")
	}
	if req.Overrides != nil {
		if n, ok := req.Overrides["name"].(string); ok && n != "" {
			withName.Name = n
		}
	}
	if withName.Name == "" {
		withName.Name = it.Name
	}
	spec := &withName.TeamSpec
	desc := ""
	if withName.Description != nil {
		desc = *withName.Description
	}

	if req.TargetTeamID == nil {
		team, err := s.teamSvc.CreateTeam(ctx, CreateTeamRequest{Name: withName.Name, Description: desc, Spec: spec})
		if err != nil {
			return nil, err
		}
		return &ApplyResult{
			Status:           "applied",
			CreatedResources: CreatedResources{Teams: []int64{team.ID}},
		}, nil
	}

	// merge в существующую команду
	merged, err := s.teamSvc.MergeSpec(ctx, MergeSpecRequest{TeamID: *req.TargetTeamID, Spec: spec})
	if err != nil {
		return nil, err
	}
	return &ApplyResult{
		Status:           "merged",
		CreatedResources: CreatedResources{Segments: merged.Segments, Roles: merged.Roles},
		UpdatedResources: &UpdatedResources{Teams: []int64{*req.TargetTeamID}},
	}, nil
}

func (s *LibraryService) bumpDownloads(ctx context.Context, id int64) {
	_ = s.items.IncrementDownloads(ctx, s.db, id)
}

// snapshotSpec — снимок spec источника (team/workflow/segment/role).
func (s *LibraryService) snapshotSpec(ctx context.Context, t models.LibraryItemType, sourceID int64) (map[string]any, models.LibraryItemType, error) {
	switch t {
	case models.LibTeam:
		detail, err := s.teamSvc.GetTeam(ctx, sourceID)
		if err != nil {
			return nil, t, err
		}
		spec := &TeamSpec{}
		for _, seg := range detail.Segments {
			spec.Segments = append(spec.Segments, SegmentSpec{
				Name: seg.Name, Description: seg.Description,
			})
		}
		// роли: segment-имя из detail.Segments
		segName := map[int64]string{}
		for _, seg := range detail.Segments {
			segName[seg.ID] = seg.Name
		}
		for _, role := range detail.Roles {
			spec.Roles = append(spec.Roles, RoleSpec{
				Segment:   segName[role.SegmentID],
				Name:      role.Name,
				AgentSpec: role.AgentSpec,
				Profile:   role.Profile,
			})
		}
		for _, rel := range detail.Relatives {
			fromSeg, fromName, err := s.roleSegment(ctx, rel.FromRoleID)
			if err != nil {
				return nil, t, err
			}
			toSeg, toName, err := s.roleSegment(ctx, rel.ToRoleID)
			if err != nil {
				return nil, t, err
			}
			spec.Relatives = append(spec.Relatives, RelativeSpec{
				From: segName[fromSeg] + "." + fromName,
				To:   segName[toSeg] + "." + toName,
				Type: rel.Type,
			})
		}
		return teamSpecToJSON(spec), t, nil
	case models.LibWorkflow:
		wf, err := s.wf.GetByID(ctx, s.db, sourceID)
		if err != nil {
			return nil, t, wrapRepositoryNotFound(err, "workflow")
		}
		blocks, err := s.wfBlocks.ListByWorkflow(ctx, s.db, sourceID)
		if err != nil {
			return nil, t, err
		}
		conns, err := s.wfConns.ListByWorkflow(ctx, s.db, sourceID)
		if err != nil {
			return nil, t, err
		}
		// индексы блоков
		idx := map[int64]int{}
		rawBlocks := make([]any, 0, len(blocks))
		for i, b := range blocks {
			idx[b.ID] = i
			m := map[string]any{"type": string(b.Type), "config": b.Config,
				"position": map[string]any{"x": b.PositionX, "y": b.PositionY}}
			if b.Label != nil {
				m["label"] = *b.Label
			}
			rawBlocks = append(rawBlocks, m)
		}
		rawConns := make([]any, 0, len(conns))
		for _, c := range conns {
			m := map[string]any{"from_block_id": idx[c.FromBlockID], "to_block_id": idx[c.ToBlockID]}
			if c.Condition != nil {
				m["condition"] = *c.Condition
			}
			rawConns = append(rawConns, m)
		}
		spec := map[string]any{
			"name": wf.Name, "description": wf.Description,
			"blocks": rawBlocks, "connections": rawConns,
		}
		return spec, t, nil
	case models.LibSegment:
		seg, err := s.segments.GetByID(ctx, s.db, sourceID)
		if err != nil {
			return nil, t, wrapRepositoryNotFound(err, "segment")
		}
		roles, err := s.roles.ListBySegment(ctx, s.db, sourceID)
		if err != nil {
			return nil, t, err
		}
		rawRoles := make([]any, 0, len(roles))
		for _, r := range roles {
			m := map[string]any{"name": r.Name, "agent_spec": r.AgentSpec}
			if r.Profile != "" {
				m["profile"] = r.Profile
			}
			rawRoles = append(rawRoles, m)
		}
		desc := seg.Description
		return map[string]any{"name": seg.Name, "description": desc, "roles": rawRoles}, t, nil
	case models.LibRole:
		role, err := s.roles.GetByID(ctx, s.db, sourceID)
		if err != nil {
			return nil, t, wrapRepositoryNotFound(err, "role")
		}
		seg, err := s.segments.GetByID(ctx, s.db, role.SegmentID)
		if err != nil {
			return nil, t, wrapRepositoryNotFound(err, "segment")
		}
		spec := map[string]any{"segment": seg.Name, "name": role.Name, "agent_spec": role.AgentSpec}
		if role.Profile != "" {
			spec["profile"] = role.Profile
		}
		return spec, t, nil
	default:
		return nil, t, NewValidation(fmt.Sprintf("library type %q is not supported yet", t))
	}
}

// ---------- helpers ----------

func (s *LibraryService) roleSegment(ctx context.Context, roleID int64) (int64, string, error) {
	role, err := s.roles.GetByID(ctx, s.db, roleID)
	if err != nil {
		return 0, "", wrapRepositoryNotFound(err, "role")
	}
	return role.SegmentID, role.Name, nil
}

// teamSpecToJSON / teamSpecFromJSON — TeamSpec ↔ map[string]any (для spec в БД).
func teamSpecToJSON(spec *TeamSpec) map[string]any {
	b, err := json.Marshal(spec)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// TeamSpecWithName — TeamSpec + name/description для library-снапшотов.
type TeamSpecWithName struct {
	TeamSpec
	Name        string  `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func teamSpecFromJSON(m map[string]any) (*TeamSpecWithName, bool) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	var spec TeamSpecWithName
	if err := json.Unmarshal(b, &spec); err != nil {
		return nil, false
	}
	return &spec, true
}

func strFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func strPtr(s string) *string { return &s }
