package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"daemon/internal/models"
	"daemon/internal/service"
)

const rfc3339 = time.RFC3339

type handlers struct {
	svc *service.TeamService
}

func limitOffset(q url.Values) (int, int) {
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// ---------- Teams ----------

// ListTeams — GET /api/v1/teams
func (h *handlers) ListTeams(w http.ResponseWriter, r *http.Request) {
	limit, offset := limitOffset(r.URL.Query())
	teams, total, err := h.svc.ListTeams(r.Context(), limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	type teamView struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description,omitempty"`
		State         string `json:"state"`
		SegmentsCount int    `json:"segments_count"`
		RolesCount    int    `json:"roles_count"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
	}
	out := make([]teamView, 0, len(teams))
	for _, t := range teams {
		out = append(out, teamView{
			ID: t.ID, Name: t.Name, Description: t.Description, State: string(t.State),
			SegmentsCount: t.SegmentsCount, RolesCount: t.RolesCount,
			CreatedAt: t.CreatedAt.Format(rfc3339), UpdatedAt: t.UpdatedAt.Format(rfc3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": out, "total": total})
}

type createTeamBody struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Spec        *specJSON `json:"spec"`
}

type specJSON struct {
	Segments  []segmentSpecJSON  `json:"segments"`
	Roles     []roleSpecJSON     `json:"roles"`
	Relatives []relativeSpecJSON `json:"relatives"`
}

type segmentSpecJSON struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
	Layout      *layoutJSON    `json:"layout"`
}

type roleSpecJSON struct {
	Segment   string         `json:"segment"`
	Name      string         `json:"name"`
	AgentSpec string         `json:"agent_spec"`
	Profile   string         `json:"profile"`
	Config    map[string]any `json:"config"`
	Layout    *layoutJSON    `json:"layout"`
}

type relativeSpecJSON struct {
	From   string         `json:"from"`
	To     string         `json:"to"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

// CreateTeam — POST /api/v1/teams
func (h *handlers) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var body createTeamBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	req := service.CreateTeamRequest{Name: body.Name, Description: body.Description}
	if body.Spec != nil {
		spec := &service.TeamSpec{}
		for _, s := range body.Spec.Segments {
			spec.Segments = append(spec.Segments, service.SegmentSpec{
				Name: s.Name, Description: s.Description, Config: s.Config, Layout: toLayout(s.Layout),
			})
		}
		for _, r2 := range body.Spec.Roles {
			spec.Roles = append(spec.Roles, service.RoleSpec{
				Segment: r2.Segment, Name: r2.Name, AgentSpec: r2.AgentSpec, Profile: r2.Profile,
				Config: r2.Config, Layout: toLayout(r2.Layout),
			})
		}
		for _, r3 := range body.Spec.Relatives {
			spec.Relatives = append(spec.Relatives, service.RelativeSpec{
				From: r3.From, To: r3.To, Type: models.RelativeType(r3.Type), Config: r3.Config,
			})
		}
		req.Spec = spec
	}

	team, err := h.svc.CreateTeam(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": team.ID, "name": team.Name, "status": "created",
	})
}

// GetTeam — GET /api/v1/teams/{id}
func (h *handlers) GetTeam(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.svc.GetTeam(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	segViews := make([]map[string]any, 0, len(detail.Segments))
	for _, s := range detail.Segments {
		segViews = append(segViews, map[string]any{
			"id": s.ID, "team_id": s.TeamID, "name": s.Name,
			"description": s.Description, "config": s.Config,
			"roles_count": s.RolesCount,
			"created_at":  s.CreatedAt.Format(rfc3339), "updated_at": s.UpdatedAt.Format(rfc3339),
		})
	}
	roleViews := make([]map[string]any, 0, len(detail.Roles))
	for _, role := range detail.Roles {
		roleViews = append(roleViews, map[string]any{
			"id": role.ID, "team_id": role.TeamID, "segment_id": role.SegmentID,
			"segment_name": segmentName(detail, role.SegmentID),
			"name":         role.Name, "address": role.Address,
			"agent_spec": role.AgentSpec, "profile": role.Profile,
			"state":      string(role.State),
			"created_at": role.CreatedAt.Format(rfc3339), "updated_at": role.UpdatedAt.Format(rfc3339),
		})
	}
	relViews := make([]map[string]any, 0, len(detail.Relatives))
	for _, rel := range detail.Relatives {
		relViews = append(relViews, map[string]any{
			"id": rel.ID, "team_id": rel.TeamID,
			"from_role_id": rel.FromRoleID, "from_role_name": rel.FromRoleName,
			"to_role_id": rel.ToRoleID, "to_role_name": rel.ToRoleName,
			"type": string(rel.Type), "config": rel.Config,
			"created_at": rel.CreatedAt.Format(rfc3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"team": map[string]any{
			"id": detail.Team.ID, "name": detail.Team.Name,
			"description": detail.Team.Description, "state": string(detail.Team.State),
			"segments_count": len(detail.Segments), "roles_count": len(detail.Roles),
			"created_at": detail.Team.CreatedAt.Format(rfc3339),
			"updated_at": detail.Team.UpdatedAt.Format(rfc3339),
		},
		"segments": segViews, "roles": roleViews, "relatives": relViews,
	})
}

// ArchiveTeam — DELETE /api/v1/teams/{id}
func (h *handlers) ArchiveTeam(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.svc.ArchiveTeam(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "archived"})
}

// ---------- Segments / Roles / Relatives ----------

type createSegmentBody struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
	Layout      *layoutJSON    `json:"layout"`
}

// CreateSegment — POST /api/v1/teams/{teamId}/segments
func (h *handlers) CreateSegment(w http.ResponseWriter, r *http.Request) {
	teamID, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body createSegmentBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	seg, err := h.svc.CreateSegment(r.Context(), service.CreateSegmentRequest{
		TeamID: teamID, Name: body.Name, Description: body.Description,
		Config: body.Config, Layout: toLayout(body.Layout),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": seg.ID, "team_id": seg.TeamID, "name": seg.Name,
		"layout": segLayout(seg.Config), "status": "created",
	})
}

type createRoleBody struct {
	Name      string         `json:"name"`
	AgentSpec string         `json:"agent_spec"`
	Profile   string         `json:"profile"`
	Config    map[string]any `json:"config"`
	Layout    *layoutJSON    `json:"layout"`
}

// CreateRole — POST /api/v1/segments/{segmentId}/roles
func (h *handlers) CreateRole(w http.ResponseWriter, r *http.Request) {
	segmentID, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body createRoleBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	role, err := h.svc.CreateRole(r.Context(), service.CreateRoleRequest{
		SegmentID: segmentID, Name: body.Name, AgentSpec: body.AgentSpec,
		Profile: body.Profile, Config: body.Config, Layout: toLayout(body.Layout),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": role.ID, "segment_id": role.SegmentID, "name": role.Name,
		"address": role.Address, "layout": segLayout(role.Config), "status": "created",
	})
}

type createRelativeBody struct {
	FromRoleID int64          `json:"from_role_id"`
	ToRoleID   int64          `json:"to_role_id"`
	Type       string         `json:"type"`
	Config     map[string]any `json:"config"`
	Layout     *layoutJSON    `json:"layout"`
}

// CreateRelative — POST /api/v1/teams/{teamId}/relatives
func (h *handlers) CreateRelative(w http.ResponseWriter, r *http.Request) {
	teamID, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body createRelativeBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	rel, err := h.svc.CreateRelative(r.Context(), service.CreateRelativeRequest{
		TeamID: teamID, FromRoleID: body.FromRoleID, ToRoleID: body.ToRoleID,
		Type: models.RelativeType(body.Type), Config: body.Config, Layout: toLayout(body.Layout),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	// имена ролей для ответа (контракт: from_role_name/to_role_name)
	names := map[int64]string{}
	if detail, err := h.svc.GetTeam(r.Context(), teamID); err == nil {
		for _, role := range detail.Roles {
			names[role.ID] = role.Name
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             rel.ID,
		"from_role_name": names[rel.FromRoleID],
		"to_role_name":   names[rel.ToRoleID],
		"type":           string(rel.Type),
		"status":         "created",
	})
}

// DeleteRelative — DELETE /api/v1/relatives/{id}
func (h *handlers) DeleteRelative(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	rel, err := h.svc.DeleteRelative(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	names := map[int64]string{}
	if detail, err := h.svc.GetTeam(r.Context(), rel.TeamID); err == nil {
		for _, role := range detail.Roles {
			names[role.ID] = role.Name
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "status": "deleted",
		"from_role_name": names[rel.FromRoleID],
		"to_role_name":   names[rel.ToRoleID],
	})
}

// ---------- Обновление конфигурации и layout ----------

type updateRoleConfigBody struct {
	AgentSpec *string         `json:"agent_spec"`
	Profile   *string         `json:"profile"`
	Config    *map[string]any `json:"config"`
}

// UpdateRoleConfig — PATCH /api/v1/roles/{id}/config
func (h *handlers) UpdateRoleConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body updateRoleConfigBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.svc.UpdateRoleConfig(r.Context(), service.UpdateRoleConfigRequest{
		ID: id, AgentSpec: body.AgentSpec, Profile: body.Profile, Config: body.Config,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "status": "updated",
		"previous_config": res.PreviousConfig, "new_config": res.NewConfig,
	})
}

type layoutPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type updateSegmentLayoutBody struct {
	Position layoutPosition `json:"position"`
	Size     *struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"size"`
	Collapsed *bool `json:"collapsed"`
}

// UpdateSegmentLayout — PATCH /api/v1/segments/{id}/layout
func (h *handlers) UpdateSegmentLayout(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body updateSegmentLayoutBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	var size *service.Size
	if body.Size != nil {
		size = &service.Size{Width: body.Size.Width, Height: body.Size.Height}
	}
	res, err := h.svc.UpdateSegmentLayout(r.Context(), service.UpdateSegmentLayoutRequest{
		ID:        id,
		Position:  service.Position{X: body.Position.X, Y: body.Position.Y},
		Size:      size,
		Collapsed: body.Collapsed,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "status": "updated",
		"previous_layout": res.PreviousLayout, "new_layout": res.NewLayout,
	})
}

type updateRoleLayoutBody struct {
	Position layoutPosition `json:"position"`
}

// UpdateRoleLayout — PATCH /api/v1/roles/{id}/layout
func (h *handlers) UpdateRoleLayout(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body updateRoleLayoutBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.svc.UpdateRoleLayout(r.Context(), service.UpdateRoleLayoutRequest{
		ID: id, Position: service.Position{X: body.Position.X, Y: body.Position.Y},
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "status": "updated",
		"previous_position": res.PreviousPosition, "new_position": res.NewPosition,
	})
}

type updateRelativeLayoutBody struct {
	Path          []service.Point `json:"path"`
	LabelPosition *layoutPosition `json:"label_position"`
}

// UpdateRelativeLayout — PATCH /api/v1/relatives/{id}/layout
func (h *handlers) UpdateRelativeLayout(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body updateRelativeLayoutBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	var labelPos *service.Position
	if body.LabelPosition != nil {
		labelPos = &service.Position{X: body.LabelPosition.X, Y: body.LabelPosition.Y}
	}
	if _, err := h.svc.UpdateRelativeLayout(r.Context(), service.UpdateRelativeLayoutRequest{
		ID: id, Path: body.Path, LabelPosition: labelPos,
	}); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "updated"})
}

// ---------- Topology ----------

// GetTopology — GET /api/v1/teams/{id}/topology
func (h *handlers) GetTopology(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	topo, err := h.svc.GetTopology(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, topologyView(topo))
}

// topologyView — контрактная (snake_case) форма GET /teams/{id}/topology.
// Служебная *service.Topology встраивает models без json-тегов, поэтому
// сериализуем явно (по образцу GetTeam), дополняя полями контракта:
// team.segments_count/roles_count, segment.roles_count, role.segment_name,
// relative.from_role_name/to_role_name, layout segment position width/height.
func topologyView(t *service.Topology) map[string]any {
	segName := make(map[int64]string, len(t.Segments))
	segRoles := make(map[int64]int, len(t.Segments))
	for _, s := range t.Segments {
		segName[s.ID] = s.Name
	}
	roleName := make(map[int64]string, len(t.Roles))
	for _, role := range t.Roles {
		roleName[role.ID] = role.Name
		segRoles[role.SegmentID]++
	}

	segViews := make([]map[string]any, 0, len(t.Segments))
	for _, s := range t.Segments {
		segViews = append(segViews, map[string]any{
			"id": s.ID, "team_id": s.TeamID, "name": s.Name,
			"description": s.Description, "config": s.Config,
			"roles_count": segRoles[s.ID],
			"created_at":  s.CreatedAt.Format(rfc3339), "updated_at": s.UpdatedAt.Format(rfc3339),
		})
	}
	roleViews := make([]map[string]any, 0, len(t.Roles))
	for _, role := range t.Roles {
		roleViews = append(roleViews, map[string]any{
			"id": role.ID, "team_id": role.TeamID, "segment_id": role.SegmentID,
			"segment_name": segName[role.SegmentID],
			"name":         role.Name, "address": role.Address,
			"agent_spec": role.AgentSpec, "profile": role.Profile,
			"state":      string(role.State),
			"created_at": role.CreatedAt.Format(rfc3339), "updated_at": role.UpdatedAt.Format(rfc3339),
		})
	}
	relViews := make([]map[string]any, 0, len(t.Relatives))
	for _, rel := range t.Relatives {
		relViews = append(relViews, map[string]any{
			"id": rel.ID, "team_id": rel.TeamID,
			"from_role_id": rel.FromRoleID, "from_role_name": roleName[rel.FromRoleID],
			"to_role_id": rel.ToRoleID, "to_role_name": roleName[rel.ToRoleID],
			"type": string(rel.Type), "config": rel.Config,
			"created_at": rel.CreatedAt.Format(rfc3339),
		})
	}

	out := map[string]any{
		"team": map[string]any{
			"id": t.Team.ID, "name": t.Team.Name,
			"description": t.Team.Description, "state": string(t.Team.State),
			"segments_count": len(t.Segments), "roles_count": len(t.Roles),
			"created_at": t.Team.CreatedAt.Format(rfc3339),
			"updated_at": t.Team.UpdatedAt.Format(rfc3339),
		},
		"segments": segViews, "roles": roleViews, "relatives": relViews,
	}

	if t.Layout != nil {
		segSize := make(map[int64]*service.Layout, len(t.Segments))
		for _, s := range t.Segments {
			if l := service.LayoutFromConfig(s.Config); l != nil {
				segSize[s.ID] = l
			}
		}
		lvSegs := []any{}
		for _, l := range t.Layout.Segments {
			pos := map[string]any{"x": l.Position.X, "y": l.Position.Y}
			if fl := segSize[l.SegmentID]; fl != nil && (fl.Width > 0 || fl.Height > 0) {
				pos["width"] = fl.Width
				pos["height"] = fl.Height
			}
			lvSegs = append(lvSegs, map[string]any{
				"segment_id": l.SegmentID, "position": pos, "collapsed": l.Collapsed,
			})
		}
		lvRoles := []any{}
		for _, l := range t.Layout.Roles {
			lvRoles = append(lvRoles, map[string]any{
				"role_id": l.RoleID, "segment_id": l.SegmentID,
				"position": map[string]any{"x": l.Position.X, "y": l.Position.Y},
			})
		}
		lvRels := []any{}
		for _, l := range t.Layout.Relatives {
			entry := map[string]any{
				"relative_id": l.RelativeID, "from_role_id": l.FromRoleID, "to_role_id": l.ToRoleID,
			}
			if len(l.Path) > 0 {
				path := make([]map[string]any, 0, len(l.Path))
				for _, p := range l.Path {
					path = append(path, map[string]any{"x": p.X, "y": p.Y})
				}
				entry["path"] = path
			}
			lvRels = append(lvRels, entry)
		}
		lv := map[string]any{"segments": lvSegs, "roles": lvRoles, "relatives": lvRels}
		out["layout"] = lv
	}
	return out
}

type validateBody struct {
	CheckCircular       *bool `json:"check_circular"`
	CheckOrphans        *bool `json:"check_orphans"`
	CheckRequiredFields *bool `json:"check_required_fields"`
}

// ValidateTopology — POST /api/v1/teams/{id}/validate
func (h *handlers) ValidateTopology(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body validateBody
	// body опционален
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, r, err)
			return
		}
	}
	resp, err := h.svc.ValidateTopology(r.Context(), service.ValidateTopologyRequest{
		TeamID:              id,
		CheckCircular:       body.CheckCircular,
		CheckOrphans:        body.CheckOrphans,
		CheckRequiredFields: body.CheckRequiredFields,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetRoleConfig — GET /api/v1/roles/{id}/config (контракт 21 §10)
func (h *handlers) GetRoleConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	role, spec, profiles, err := h.svc.GetRoleConfig(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	view := service.RoleConfigView{
		AgentSpec:         spec,
		AvailableProfiles: profiles,
		AvailableSkills:   spec.PiConfig.Skills,
		AvailablePlugins:  spec.PiConfig.Plugins,
	}
	writeJSON(w, http.StatusOK, map[string]any{"role": roleView(role, ""), "agent_spec": view.AgentSpec,
		"available_profiles": view.AvailableProfiles, "available_skills": view.AvailableSkills,
		"available_plugins": view.AvailablePlugins})
}

// SaveTopology — POST /api/v1/teams/{id}/save (контракт 21 §9)
func (h *handlers) SaveTopology(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		SaveToLibrary bool   `json:"save_to_library"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, r, err)
			return
		}
	}
	res, err := h.svc.SaveTopology(r.Context(), service.SaveTopologyRequest{
		TeamID: id, Name: body.Name, Description: body.Description, SaveToLibrary: body.SaveToLibrary,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"team_id": res.TeamID, "status": "saved", "validation": res.Validation,
	})
}

func roleView(role *models.Role, segmentName string) map[string]any {
	return map[string]any{
		"id": role.ID, "team_id": role.TeamID, "segment_id": role.SegmentID,
		"segment_name": segmentName,
		"name":         role.Name, "address": role.Address,
		"agent_spec": role.AgentSpec, "profile": role.Profile,
		"state":      string(role.State),
		"created_at": role.CreatedAt.Format(rfc3339), "updated_at": role.UpdatedAt.Format(rfc3339),
	}
}

// ---------- helpers ----------

type layoutJSON struct {
	X         float64     `json:"x"`
	Y         float64     `json:"y"`
	Width     float64     `json:"width"`
	Height    float64     `json:"height"`
	Collapsed *bool       `json:"collapsed"`
	Path      []pointJSON `json:"path"`
}

type pointJSON struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func toLayout(l *layoutJSON) *service.Layout {
	if l == nil {
		return nil
	}
	out := &service.Layout{
		X: l.X, Y: l.Y, Width: l.Width, Height: l.Height, Collapsed: l.Collapsed,
	}
	for _, p := range l.Path {
		out.Path = append(out.Path, service.Point{X: p.X, Y: p.Y})
	}
	return out
}

// segLayout — layout из config (или nil).
func segLayout(cfg map[string]any) *layoutJSON {
	l := service.LayoutFromConfig(cfg)
	if l == nil {
		return nil
	}
	out := &layoutJSON{
		X: l.X, Y: l.Y, Width: l.Width, Height: l.Height, Collapsed: l.Collapsed,
	}
	for _, p := range l.Path {
		out.Path = append(out.Path, pointJSON{X: p.X, Y: p.Y})
	}
	return out
}

func segmentName(detail *service.TeamDetail, segmentID int64) string {
	for _, s := range detail.Segments {
		if s.ID == segmentID {
			return s.Name
		}
	}
	return ""
}
