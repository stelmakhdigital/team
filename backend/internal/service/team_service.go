package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// Интерфейсы storage, которые использует сервис (маленькие, по потреблению).
type teamStore interface {
	Create(ctx context.Context, tx repository.DBTX, t *models.Team) error
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Team, error)
	GetByName(ctx context.Context, tx repository.DBTX, name string) (*models.Team, error)
	List(ctx context.Context, tx repository.DBTX, limit, offset int) ([]*models.Team, int, error)
	UpdateState(ctx context.Context, tx repository.DBTX, id int64, state models.TeamState) error
	UpdateMeta(ctx context.Context, tx repository.DBTX, id int64, name, description string) error
}

type segmentStore interface {
	Create(ctx context.Context, tx repository.DBTX, s *models.Segment) error
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Segment, error)
	GetByTeamAndName(ctx context.Context, tx repository.DBTX, teamID int64, name string) (*models.Segment, error)
	ListByTeam(ctx context.Context, tx repository.DBTX, teamID int64) ([]*models.Segment, error)
	CountRoles(ctx context.Context, tx repository.DBTX, segmentID int64) (int, error)
	UpdateConfig(ctx context.Context, tx repository.DBTX, id int64, cfg map[string]any) error
}

type roleStore interface {
	Create(ctx context.Context, tx repository.DBTX, r *models.Role) error
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Role, error)
	GetBySegmentAndName(ctx context.Context, tx repository.DBTX, segmentID int64, name string) (*models.Role, error)
	ListByTeam(ctx context.Context, tx repository.DBTX, teamID int64) ([]*models.Role, error)
	CountByTeam(ctx context.Context, tx repository.DBTX, teamID int64) (int, error)
	UpdateMutable(ctx context.Context, tx repository.DBTX, r *models.Role) error
	UpdateMutableLayout(ctx context.Context, tx repository.DBTX, r *models.Role, cfg map[string]any) error
}

type relativeStore interface {
	Create(ctx context.Context, tx repository.DBTX, r *models.Relative) error
	GetByID(ctx context.Context, tx repository.DBTX, id int64) (*models.Relative, error)
	ListByTeam(ctx context.Context, tx repository.DBTX, teamID int64) ([]*models.Relative, error)
	Delete(ctx context.Context, tx repository.DBTX, id int64) error
	UpdateConfig(ctx context.Context, tx repository.DBTX, id int64, cfg map[string]any) error
}

type TeamService struct {
	db        *sql.DB
	teams     teamStore
	segments  segmentStore
	roles     roleStore
	relatives relativeStore
	// SpecsDir — корень для чтения agent.yaml (GET /roles/{id}/config).
	SpecsDir string
}

func NewTeamService(db *sql.DB, s *repository.Stores) *TeamService {
	return &TeamService{
		db:        db,
		teams:     s.Teams,
		segments:  s.Segments,
		roles:     s.Roles,
		relatives: s.Relatives,
		SpecsDir:  "agents",
	}
}

// ---------- Запросы ----------

// TeamSummary — команда с counts (контракт: GetTeamsResponse).
type TeamSummary struct {
	*models.Team
	SegmentsCount int `json:"segments_count"`
	RolesCount    int `json:"roles_count"`
}

func (s *TeamService) ListTeams(ctx context.Context, limit, offset int) ([]*TeamSummary, int, error) {
	teams, total, err := s.teams.List(ctx, s.db, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*TeamSummary, 0, len(teams))
	for _, t := range teams {
		segments, err := s.segments.ListByTeam(ctx, s.db, t.ID)
		if err != nil {
			return nil, 0, err
		}
		roleCount, err := s.roles.CountByTeam(ctx, s.db, t.ID)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, &TeamSummary{Team: t, SegmentsCount: len(segments), RolesCount: roleCount})
	}
	return out, total, nil
}

// TeamDetail — team + segments + roles + relatives (контракт: GetTeamResponse).
type TeamDetail struct {
	Team      *models.Team         `json:"team"`
	Segments  []SegmentWithRoles   `json:"segments"`
	Roles     []*models.Role       `json:"roles"`
	Relatives []*RelativeWithNames `json:"relatives"`
}

type SegmentWithRoles struct {
	*models.Segment
	RolesCount int `json:"roles_count"`
}

type RelativeWithNames struct {
	*models.Relative
	FromRoleName string `json:"from_role_name"`
	ToRoleName   string `json:"to_role_name"`
}

func (s *TeamService) GetTeam(ctx context.Context, teamID int64) (*TeamDetail, error) {
	team, err := s.getTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	detail := &TeamDetail{
		Team:      team,
		Segments:  []SegmentWithRoles{},
		Roles:     []*models.Role{},
		Relatives: []*RelativeWithNames{},
	}

	segments, err := s.segments.ListByTeam(ctx, s.db, teamID)
	if err != nil {
		return nil, err
	}
	for _, seg := range segments {
		n, err := s.segments.CountRoles(ctx, s.db, seg.ID)
		if err != nil {
			return nil, err
		}
		detail.Segments = append(detail.Segments, SegmentWithRoles{Segment: seg, RolesCount: n})
	}

	roles, err := s.roles.ListByTeam(ctx, s.db, teamID)
	if err != nil {
		return nil, err
	}
	detail.Roles = roles

	rels, err := s.withRelativeNames(ctx, teamID, roles)
	if err != nil {
		return nil, err
	}
	detail.Relatives = rels
	return detail, nil
}

func (s *TeamService) ArchiveTeam(ctx context.Context, teamID int64) error {
	team, err := s.getTeam(ctx, teamID)
	if err != nil {
		return err
	}
	if team.State != models.TeamActive {
		return NewConflict(fmt.Sprintf("team %q is %s, only active teams can be archived", team.Name, team.State))
	}
	if err := s.teams.UpdateState(ctx, s.db, teamID, models.TeamArchived); err != nil {
		return err
	}
	return nil
}

// ---------- Создание ----------

// TeamSpec — спецификация команды при создании (контракт: TeamSpec, поля по 21).
type TeamSpec struct {
	Segments  []SegmentSpec  `json:"segments"`
	Roles     []RoleSpec     `json:"roles"`
	Relatives []RelativeSpec `json:"relatives"`
}

type SegmentSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
	Layout      *Layout        `json:"layout,omitempty"`
}

type RoleSpec struct {
	Segment   string         `json:"segment"` // имя сегмента из spec.segments
	Name      string         `json:"name"`
	AgentSpec string         `json:"agent_spec"`
	Profile   string         `json:"profile,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	Layout    *Layout        `json:"layout,omitempty"`
}

type RelativeSpec struct {
	From   string              `json:"from"` // "segment.role"
	To     string              `json:"to"`
	Type   models.RelativeType `json:"type"`
	Config map[string]any      `json:"config,omitempty"`
}

// Layout — координаты на холсте Team Builder (хранится в config.layout).
type Layout struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width,omitempty"`
	Height    float64 `json:"height,omitempty"`
	Collapsed *bool   `json:"collapsed,omitempty"`
	Path      []Point `json:"path,omitempty"`
	LabelX    float64 `json:"label_x,omitempty"`
	LabelY    float64 `json:"label_y,omitempty"`
	HasLabel  bool    `json:"-"`
}

type CreateTeamRequest struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Spec        *TeamSpec `json:"spec,omitempty"`
}

// CreateTeam — создание команды, опционально с полной топологией (одна транзакция).
func (s *TeamService) CreateTeam(ctx context.Context, req CreateTeamRequest) (*models.Team, error) {
	if req.Name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "required"}})
	}
	if req.Spec != nil {
		if err := validateSpec(req.Spec); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	team := &models.Team{Name: req.Name, Description: req.Description}
	if err := s.teams.Create(ctx, tx, team); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("team %q already exists", req.Name))
		}
		return nil, err
	}

	if req.Spec != nil {
		if err := s.applySpec(ctx, tx, team, req.Spec); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return team, nil
}

func (s *TeamService) applySpec(ctx context.Context, tx repository.DBTX, team *models.Team, spec *TeamSpec) error {
	segIDs := make(map[string]int64, len(spec.Segments))
	for i := range spec.Segments {
		ss := &spec.Segments[i]
		seg := &models.Segment{TeamID: team.ID, Name: ss.Name, Description: ss.Description, Config: ss.Config}
		if ss.Layout != nil {
			seg.Config = setLayout(seg.Config, ss.Layout)
		}
		if err := s.segments.Create(ctx, tx, seg); err != nil {
			if repository.IsUniqueViolation(err) {
				return NewConflict(fmt.Sprintf("segment %q already exists", ss.Name))
			}
			return err
		}
		segIDs[ss.Name] = seg.ID
	}

	roleIDs := make(map[string]int64, len(spec.Roles)) // "segment.role"
	for i := range spec.Roles {
		rs := &spec.Roles[i]
		segID, ok := segIDs[rs.Segment]
		if !ok {
			return NewValidation(fmt.Sprintf("role %q: segment %q not found in spec", rs.Name, rs.Segment))
		}
		role := &models.Role{
			TeamID: team.ID, SegmentID: segID,
			Name: rs.Name, Address: team.Name + ":" + rs.Segment + "." + rs.Name,
			AgentSpec: rs.AgentSpec, Profile: rs.Profile, Config: rs.Config,
		}
		if rs.Layout != nil {
			role.Config = setLayout(role.Config, rs.Layout)
		}
		if err := s.roles.Create(ctx, tx, role); err != nil {
			if repository.IsUniqueViolation(err) {
				return NewConflict(fmt.Sprintf("role %q already exists in segment %q", rs.Name, rs.Segment))
			}
			return err
		}
		roleIDs[rs.Segment+"."+rs.Name] = role.ID
	}

	for i := range spec.Relatives {
		rs := &spec.Relatives[i]
		fromID, ok := roleIDs[rs.From]
		if !ok {
			return NewValidation(fmt.Sprintf("relative: role %q not found in spec", rs.From))
		}
		toID, ok := roleIDs[rs.To]
		if !ok {
			return NewValidation(fmt.Sprintf("relative: role %q not found in spec", rs.To))
		}
		if err := s.createRelativeOnTx(ctx, tx, &models.Relative{
			TeamID: team.ID, FromRoleID: fromID, ToRoleID: toID, Type: rs.Type, Config: rs.Config,
		}); err != nil {
			return err
		}
	}
	return nil
}

func validateSpec(spec *TeamSpec) error {
	seenSegs := map[string]bool{}
	for _, ss := range spec.Segments {
		if ss.Name == "" {
			return NewValidation("invalid spec", []FieldError{{Field: "spec.segments.name", Reason: "required"}})
		}
		if seenSegs[ss.Name] {
			return NewValidation(fmt.Sprintf("duplicate segment %q in spec", ss.Name))
		}
		seenSegs[ss.Name] = true
	}
	seenRoles := map[string]bool{}
	for _, rs := range spec.Roles {
		if rs.Segment == "" || rs.Name == "" {
			return NewValidation("invalid spec", []FieldError{{Field: "spec.roles", Reason: "segment and name are required"}})
		}
		if rs.AgentSpec == "" {
			return NewValidation(fmt.Sprintf("role %q: agent_spec is required", rs.Name))
		}
		if !seenSegs[rs.Segment] {
			return NewValidation(fmt.Sprintf("role %q: segment %q not found in spec", rs.Name, rs.Segment))
		}
		key := rs.Segment + "." + rs.Name
		if seenRoles[key] {
			return NewValidation(fmt.Sprintf("duplicate role %q in spec", key))
		}
		seenRoles[key] = true
	}
	for _, rs := range spec.Relatives {
		if !rs.Type.Valid() {
			return NewValidation(fmt.Sprintf("invalid relative type %q", rs.Type))
		}
	}
	return nil
}

type CreateSegmentRequest struct {
	TeamID      int64          `json:"-"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
	Layout      *Layout        `json:"layout,omitempty"`
}

func (s *TeamService) CreateSegment(ctx context.Context, req CreateSegmentRequest) (*models.Segment, error) {
	if req.Name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "required"}})
	}
	team, err := s.requireActiveTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	seg := &models.Segment{TeamID: team.ID, Name: req.Name, Description: req.Description, Config: req.Config}
	if req.Layout != nil {
		seg.Config = setLayout(seg.Config, req.Layout)
	}
	if err := s.segments.Create(ctx, tx, seg); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("segment %q already exists in team %q", req.Name, team.Name))
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return seg, nil
}

type CreateRoleRequest struct {
	SegmentID int64          `json:"-"`
	Name      string         `json:"name"`
	AgentSpec string         `json:"agent_spec"`
	Profile   string         `json:"profile,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	Layout    *Layout        `json:"layout,omitempty"`
}

func (s *TeamService) CreateRole(ctx context.Context, req CreateRoleRequest) (*models.Role, error) {
	if req.Name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "required"}})
	}
	if req.AgentSpec == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "agent_spec", Reason: "required"}})
	}
	seg, err := s.segments.GetByID(ctx, s.db, req.SegmentID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("segment")
		}
		return nil, err
	}
	team, err := s.requireActiveTeam(ctx, seg.TeamID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	role := &models.Role{
		TeamID: team.ID, SegmentID: seg.ID,
		Name: req.Name, Address: team.Name + ":" + seg.Name + "." + req.Name,
		AgentSpec: req.AgentSpec, Profile: req.Profile, Config: req.Config,
	}
	if req.Layout != nil {
		role.Config = setLayout(role.Config, req.Layout)
	}
	if err := s.roles.Create(ctx, tx, role); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("role %q already exists in segment %q", req.Name, seg.Name))
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return role, nil
}

type CreateRelativeRequest struct {
	TeamID     int64               `json:"-"`
	FromRoleID int64               `json:"from_role_id"`
	ToRoleID   int64               `json:"to_role_id"`
	Type       models.RelativeType `json:"type"`
	Config     map[string]any      `json:"config,omitempty"`
	Layout     *Layout             `json:"layout,omitempty"`
}

func (s *TeamService) CreateRelative(ctx context.Context, req CreateRelativeRequest) (*models.Relative, error) {
	team, err := s.requireActiveTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}
	if !req.Type.Valid() {
		return nil, NewValidation(fmt.Sprintf("invalid relative type %q", req.Type))
	}
	if req.FromRoleID == req.ToRoleID {
		return nil, NewValidation("relative: from_role_id and to_role_id must differ")
	}
	from, err := s.requireRoleInTeam(ctx, req.FromRoleID, team.ID)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireRoleInTeam(ctx, req.ToRoleID, team.ID); err != nil {
		return nil, err
	}
	_ = from

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rel := &models.Relative{TeamID: team.ID, FromRoleID: req.FromRoleID, ToRoleID: req.ToRoleID, Type: req.Type, Config: req.Config}
	if req.Layout != nil {
		rel.Config = setLayout(rel.Config, req.Layout)
	}
	if err := s.createRelativeOnTx(ctx, tx, rel); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rel, nil
}

func (s *TeamService) createRelativeOnTx(ctx context.Context, tx repository.DBTX, rel *models.Relative) error {
	if err := s.relatives.Create(ctx, tx, rel); err != nil {
		if repository.IsUniqueViolation(err) {
			return NewConflict("relative already exists")
		}
		return err
	}
	return nil
}

// DeleteRelative — hard delete; возвращает удалённую связь (для имён ролей в ответе).
func (s *TeamService) DeleteRelative(ctx context.Context, id int64) (*models.Relative, error) {
	rel, err := s.relatives.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("relative")
		}
		return nil, err
	}
	if err := s.relatives.Delete(ctx, s.db, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("relative")
		}
		return nil, err
	}
	return rel, nil
}

// ---------- Обновление ----------

type UpdateRoleConfigRequest struct {
	ID        int64           `json:"-"`
	AgentSpec *string         `json:"agent_spec,omitempty"`
	Profile   *string         `json:"profile,omitempty"`
	Config    *map[string]any `json:"config,omitempty"`
}

type RoleConfigResult struct {
	Role           *models.Role   `json:"-"`
	PreviousConfig map[string]any `json:"previous_config"`
	NewConfig      map[string]any `json:"new_config"`
}

func (s *TeamService) UpdateRoleConfig(ctx context.Context, req UpdateRoleConfigRequest) (*RoleConfigResult, error) {
	if req.AgentSpec == nil && req.Profile == nil && req.Config == nil {
		return nil, NewValidation("invalid request", []FieldError{{Field: "body", Reason: "nothing to update"}})
	}
	role, err := s.roles.GetByID(ctx, s.db, req.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	previous := cloneConfig(role.Config)

	if req.AgentSpec != nil {
		if *req.AgentSpec == "" {
			return nil, NewValidation("invalid request", []FieldError{{Field: "agent_spec", Reason: "must not be empty"}})
		}
		role.AgentSpec = *req.AgentSpec
	}
	if req.Profile != nil {
		role.Profile = *req.Profile
	}
	if req.Config != nil {
		role.Config = *req.Config
	}

	if err := s.roles.UpdateMutable(ctx, s.db, role); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	return &RoleConfigResult{Role: role, PreviousConfig: previous, NewConfig: cloneConfig(role.Config)}, nil
}

type UpdateSegmentLayoutRequest struct {
	ID        int64    `json:"-"`
	Position  Position `json:"position"`
	Size      *Size    `json:"size,omitempty"`
	Collapsed *bool    `json:"collapsed,omitempty"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type SegmentLayoutResult struct {
	Segment        *models.Segment `json:"-"`
	PreviousLayout *Layout         `json:"previous_layout"`
	NewLayout      *Layout         `json:"new_layout"`
}

func (s *TeamService) UpdateSegmentLayout(ctx context.Context, req UpdateSegmentLayoutRequest) (*SegmentLayoutResult, error) {
	seg, err := s.segments.GetByID(ctx, s.db, req.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("segment")
		}
		return nil, err
	}
	previous := getLayout(seg.Config)
	next := previous
	if next == nil {
		next = &Layout{}
	}
	next.X, next.Y = req.Position.X, req.Position.Y
	if req.Size != nil {
		next.Width, next.Height = req.Size.Width, req.Size.Height
	}
	if req.Collapsed != nil {
		next.Collapsed = req.Collapsed
	}
	newCfg := setLayout(seg.Config, next)
	if err := s.segments.UpdateConfig(ctx, s.db, seg.ID, newCfg); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("segment")
		}
		return nil, err
	}
	seg.Config = newCfg
	return &SegmentLayoutResult{Segment: seg, PreviousLayout: previous, NewLayout: next}, nil
}

type UpdateRoleLayoutRequest struct {
	ID       int64    `json:"-"`
	Position Position `json:"position"`
}

type RoleLayoutResult struct {
	Role             *models.Role `json:"-"`
	PreviousPosition *Layout      `json:"previous_position"`
	NewPosition      *Layout      `json:"new_position"`
}

func (s *TeamService) UpdateRoleLayout(ctx context.Context, req UpdateRoleLayoutRequest) (*RoleLayoutResult, error) {
	role, err := s.roles.GetByID(ctx, s.db, req.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	previous := getLayout(role.Config)
	next := &Layout{X: req.Position.X, Y: req.Position.Y}
	if previous != nil {
		next.Width, next.Height = previous.Width, previous.Height
	}
	newCfg := setLayout(role.Config, next)
	if err := s.roles.UpdateMutableLayout(ctx, s.db, role, newCfg); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	role.Config = newCfg
	return &RoleLayoutResult{Role: role, PreviousPosition: previous, NewPosition: next}, nil
}

type UpdateRelativeLayoutRequest struct {
	ID            int64     `json:"-"`
	Path          []Point   `json:"path,omitempty"`
	LabelPosition *Position `json:"label_position,omitempty"`
}

func (s *TeamService) UpdateRelativeLayout(ctx context.Context, req UpdateRelativeLayoutRequest) (*models.Relative, error) {
	rel, err := s.relatives.GetByID(ctx, s.db, req.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("relative")
		}
		return nil, err
	}
	layout := getLayout(rel.Config)
	if layout == nil {
		layout = &Layout{}
	}
	if req.Path != nil {
		layout.Path = req.Path
	}
	if req.LabelPosition != nil {
		layout.LabelX, layout.LabelY = req.LabelPosition.X, req.LabelPosition.Y
		layout.HasLabel = true
	}
	newCfg := setLayout(rel.Config, layout)
	if err := s.relatives.UpdateConfig(ctx, s.db, rel.ID, newCfg); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("relative")
		}
		return nil, err
	}
	rel.Config = newCfg
	return rel, nil
}

// ---------- Topology ----------

type Topology struct {
	Team      *models.Team       `json:"team"`
	Segments  []*models.Segment  `json:"segments"`
	Roles     []*models.Role     `json:"roles"`
	Relatives []*models.Relative `json:"relatives"`
	Layout    *TopologyLayout    `json:"layout,omitempty"`
}

type TopologyLayout struct {
	Segments  []SegmentLayout  `json:"segments"`
	Roles     []RoleLayout     `json:"roles"`
	Relatives []RelativeLayout `json:"relatives"`
}

type SegmentLayout struct {
	SegmentID int64      `json:"segment_id"`
	Position  SegmentPos `json:"position"`
	Collapsed bool       `json:"collapsed"`
}

// SegmentPos — позиция сегмента на холсте (контракт 21: position несёт width/height).
type SegmentPos struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type RoleLayout struct {
	RoleID    int64    `json:"role_id"`
	SegmentID int64    `json:"segment_id"`
	Position  Position `json:"position"`
}

type RelativeLayout struct {
	RelativeID int64   `json:"relative_id"`
	FromRoleID int64   `json:"from_role_id"`
	ToRoleID   int64   `json:"to_role_id"`
	Path       []Point `json:"path,omitempty"`
}

func (s *TeamService) GetTopology(ctx context.Context, teamID int64) (*Topology, error) {
	detail, err := s.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	topo := &Topology{
		Team:      detail.Team,
		Segments:  []*models.Segment{},
		Roles:     []*models.Role{},
		Relatives: []*models.Relative{},
		Layout: &TopologyLayout{
			Segments:  []SegmentLayout{},
			Roles:     []RoleLayout{},
			Relatives: []RelativeLayout{},
		},
	}
	for _, sw := range detail.Segments {
		topo.Segments = append(topo.Segments, sw.Segment)
		if l := getLayout(sw.Segment.Config); l != nil {
			c := false
			if l.Collapsed != nil {
				c = *l.Collapsed
			}
			topo.Layout.Segments = append(topo.Layout.Segments, SegmentLayout{
				SegmentID: sw.ID,
				Position:  SegmentPos{X: l.X, Y: l.Y, Width: l.Width, Height: l.Height},
				Collapsed: c,
			})
		}
	}
	for _, r := range detail.Roles {
		topo.Roles = append(topo.Roles, r)
		if l := getLayout(r.Config); l != nil {
			topo.Layout.Roles = append(topo.Layout.Roles, RoleLayout{
				RoleID: r.ID, SegmentID: r.SegmentID, Position: Position{X: l.X, Y: l.Y},
			})
		}
	}
	for _, rw := range detail.Relatives {
		topo.Relatives = append(topo.Relatives, rw.Relative)
		if l := getLayout(rw.Relative.Config); l != nil && l.Path != nil {
			topo.Layout.Relatives = append(topo.Layout.Relatives, RelativeLayout{
				RelativeID: rw.ID, FromRoleID: rw.FromRoleID, ToRoleID: rw.ToRoleID, Path: l.Path,
			})
		}
	}
	return topo, nil
}

// ---------- GetRoleConfig / SaveTopology (контракт 21 §10 / §9) ----------

// GetRoleConfig — роль + распарсенный agent_spec + доступные профили/скиллы/плагины.
func (s *TeamService) GetRoleConfig(ctx context.Context, roleID int64) (*models.Role, *AgentSpec, []SpecProfile, error) {
	role, err := s.roles.GetByID(ctx, s.db, roleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, nil, notFound("role")
		}
		return nil, nil, nil, err
	}
	spec, profiles := s.LoadAgentSpec(role.AgentSpec)
	return role, spec, profiles, nil
}

type SaveTopologyRequest struct {
	TeamID        int64  `json:"-"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
	SaveToLibrary bool   `json:"save_to_library,omitempty"` // библиотека — slice 5; флаг принимается
}

type SaveTopologyResult struct {
	TeamID     int64                     `json:"team_id"`
	Validation *ValidateTopologyResponse `json:"validation"`
}

// SaveTopology — сохранение топологии: обновление meta команды (опц.) + валидация.
// Команда должна быть активной. Ошибки валидации не блокируют сохранение (контракт: 200 + validation).
func (s *TeamService) SaveTopology(ctx context.Context, req SaveTopologyRequest) (*SaveTopologyResult, error) {
	team, err := s.requireActiveTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" || req.Description != "" {
		name, desc := team.Name, team.Description
		if req.Name != "" {
			name = req.Name
		}
		if req.Description != "" {
			desc = req.Description
		}
		if err := s.teams.UpdateMeta(ctx, s.db, team.ID, name, desc); err != nil {
			if repository.IsUniqueViolation(err) {
				return nil, NewConflict(fmt.Sprintf("team name %q already exists", name))
			}
			if errors.Is(err, repository.ErrNotFound) {
				return nil, notFound("team")
			}
			return nil, err
		}
	}

	validation, err := s.ValidateTopology(ctx, ValidateTopologyRequest{TeamID: req.TeamID})
	if err != nil {
		return nil, err
	}
	return &SaveTopologyResult{TeamID: req.TeamID, Validation: validation}, nil
}

// ---------- Внутреннее ----------

func (s *TeamService) getTeam(ctx context.Context, id int64) (*models.Team, error) {
	team, err := s.teams.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("team")
		}
		return nil, err
	}
	return team, nil
}

func (s *TeamService) requireActiveTeam(ctx context.Context, teamID int64) (*models.Team, error) {
	team, err := s.getTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team.State != models.TeamActive {
		return nil, NewConflict(fmt.Sprintf("team %q is %s", team.Name, team.State))
	}
	return team, nil
}

func (s *TeamService) requireRoleInTeam(ctx context.Context, roleID, teamID int64) (*models.Role, error) {
	role, err := s.roles.GetByID(ctx, s.db, roleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("role")
		}
		return nil, err
	}
	if role.TeamID != teamID {
		return nil, NewValidation(fmt.Sprintf("role %d does not belong to team %d", roleID, teamID))
	}
	return role, nil
}

func (s *TeamService) withRelativeNames(ctx context.Context, teamID int64, roles []*models.Role) ([]*RelativeWithNames, error) {
	rels, err := s.relatives.ListByTeam(ctx, s.db, teamID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*models.Role, len(roles))
	for _, r := range roles {
		byID[r.ID] = r
	}
	out := make([]*RelativeWithNames, 0, len(rels))
	for _, rel := range rels {
		rw := &RelativeWithNames{Relative: rel}
		if from, ok := byID[rel.FromRoleID]; ok {
			rw.FromRoleName = from.Name
		}
		if to, ok := byID[rel.ToRoleID]; ok {
			rw.ToRoleName = to.Name
		}
		out = append(out, rw)
	}
	return out, nil
}

func cloneConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	return out
}
