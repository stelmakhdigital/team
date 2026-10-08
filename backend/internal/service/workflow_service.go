package service

import (
	"context"
	"database/sql"
	"fmt"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// WorkflowService — Workflow Editor (контракт 20 §2).
type WorkflowService struct {
	db     *sql.DB
	teams  teamStore
	wf     *repository.WorkflowRepo
	blocks *repository.WorkflowBlockRepo
	conns  *repository.WorkflowConnectionRepo
}

func NewWorkflowService(db *sql.DB, s *repository.Stores) *WorkflowService {
	return &WorkflowService{db: db, teams: s.Teams,
		wf: s.Workflows, blocks: s.WorkflowBlocks, conns: s.WorkflowConnections}
}

// ---------- Views (контракт 20 §2) ----------

// WorkflowView — workflow (контракт: Workflow).
type WorkflowView struct {
	ID          int64   `json:"id"`
	TeamID      int64   `json:"team_id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	State       string  `json:"state"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func workflowView(w *models.Workflow) *WorkflowView {
	return &WorkflowView{
		ID: w.ID, TeamID: w.TeamID, Name: w.Name, Description: w.Description,
		State:     string(w.State),
		CreatedAt: w.CreatedAt.Format(rfc3339), UpdatedAt: w.UpdatedAt.Format(rfc3339),
	}
}

type WorkflowPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// WorkflowBlockView — блок (контракт: WorkflowBlock).
type WorkflowBlockView struct {
	ID         int64            `json:"id"`
	WorkflowID int64            `json:"workflow_id"`
	Type       string           `json:"type"`
	Position   WorkflowPosition `json:"position"`
	Config     map[string]any   `json:"config"`
	Label      *string          `json:"label,omitempty"`
	CreatedAt  string           `json:"created_at"`
}

func workflowBlockView(b *models.WorkflowBlock) *WorkflowBlockView {
	return &WorkflowBlockView{
		ID: b.ID, WorkflowID: b.WorkflowID, Type: string(b.Type),
		Position: WorkflowPosition{X: b.PositionX, Y: b.PositionY},
		Config:   b.Config, Label: b.Label,
		CreatedAt: b.CreatedAt.Format(rfc3339),
	}
}

// WorkflowConnectionView — связь (контракт: WorkflowConnection).
type WorkflowConnectionView struct {
	ID          int64   `json:"id"`
	WorkflowID  int64   `json:"workflow_id"`
	FromBlockID int64   `json:"from_block_id"`
	ToBlockID   int64   `json:"to_block_id"`
	Condition   *string `json:"condition,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func workflowConnectionView(c *models.WorkflowConnection) *WorkflowConnectionView {
	return &WorkflowConnectionView{
		ID: c.ID, WorkflowID: c.WorkflowID, FromBlockID: c.FromBlockID,
		ToBlockID: c.ToBlockID, Condition: c.Condition,
		CreatedAt: c.CreatedAt.Format(rfc3339),
	}
}

// ---------- Requests ----------

type CreateWorkflowRequest struct {
	TeamID      int64                     `json:"team_id"`
	Name        string                    `json:"name"`
	Description *string                   `json:"description"`
	Blocks      []WorkflowBlockInput      `json:"blocks"`
	Connections []WorkflowConnectionInput `json:"connections"`
}

type WorkflowBlockInput struct {
	Type     string            `json:"type"`
	Position *WorkflowPosition `json:"position"`
	Config   map[string]any    `json:"config"`
	Label    *string           `json:"label"`
}

type WorkflowConnectionInput struct {
	FromBlockID int64   `json:"from_block_id"`
	ToBlockID   int64   `json:"to_block_id"`
	Condition   *string `json:"condition"`
}

type UpdateBlockRequest struct {
	Position *WorkflowPosition `json:"position"`
	Config   *map[string]any   `json:"config"`
	Label    *string           `json:"label"`
}

// ---------- Use cases ----------

// ListWorkflows — GET /api/v1/workflows (контракт 20 §2.0).
func (s *WorkflowService) ListWorkflows(ctx context.Context, teamID *int64, state *models.WorkflowState) ([]*WorkflowView, int, error) {
	if teamID != nil {
		if _, err := s.teams.GetByID(ctx, s.db, *teamID); err != nil {
			return nil, 0, wrapRepositoryNotFound(err, "team")
		}
	}
	ws, total, err := s.wf.List(ctx, s.db, repository.WorkflowFilter{TeamID: teamID, State: state})
	if err != nil {
		return nil, 0, err
	}
	out := make([]*WorkflowView, 0, len(ws))
	for _, w := range ws {
		out = append(out, workflowView(w))
	}
	return out, total, nil
}

// GetWorkflow — GET /api/v1/workflows/{id} (контракт 20 §2.1).
func (s *WorkflowService) GetWorkflow(ctx context.Context, id int64) (*WorkflowView, []*WorkflowBlockView, []*WorkflowConnectionView, error) {
	w, err := s.wf.GetByID(ctx, s.db, id)
	if err != nil {
		return nil, nil, nil, wrapRepositoryNotFound(err, "workflow")
	}
	blocks, err := s.blocks.ListByWorkflow(ctx, s.db, id)
	if err != nil {
		return nil, nil, nil, err
	}
	conns, err := s.conns.ListByWorkflow(ctx, s.db, id)
	if err != nil {
		return nil, nil, nil, err
	}
	bv := make([]*WorkflowBlockView, 0, len(blocks))
	for _, b := range blocks {
		bv = append(bv, workflowBlockView(b))
	}
	cv := make([]*WorkflowConnectionView, 0, len(conns))
	for _, c := range conns {
		cv = append(cv, workflowConnectionView(c))
	}
	return workflowView(w), bv, cv, nil
}

// CreateWorkflow — POST /api/v1/workflows (контракт 20 §2.2).
func (s *WorkflowService) CreateWorkflow(ctx context.Context, req CreateWorkflowRequest) (*models.Workflow, error) {
	if req.Name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "required"}})
	}
	team, err := s.teams.GetByID(ctx, s.db, req.TeamID)
	if err != nil {
		return nil, wrapRepositoryNotFound(err, "team")
	}
	if team.State != models.TeamActive {
		return nil, NewConflict(fmt.Sprintf("team %q is %s", team.Name, team.State))
	}
	for i, b := range req.Blocks {
		if !models.WorkflowBlockType(b.Type).Valid() {
			return nil, NewValidation("invalid request", []FieldError{{Field: fmt.Sprintf("blocks[%d].type", i), Reason: "invalid"}})
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	w := &models.Workflow{TeamID: team.ID, Name: req.Name, Description: req.Description}
	if err := s.wf.Create(ctx, tx, w); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("workflow %q already exists in team %q", req.Name, team.Name))
		}
		return nil, err
	}

	// blocks + connections.
	// Ссылки connections: index в массиве blocks этого же body (0-based) — блоки
	// создаются в той же транзакции и у них ещё нет глобальных id (зафиксировано
	// в docs/contracts/api-decisions.md, slice 5).
	created := make([]*models.WorkflowBlock, 0, len(req.Blocks))
	for _, b := range req.Blocks {
		bb := &models.WorkflowBlock{
			WorkflowID: w.ID, Type: models.WorkflowBlockType(b.Type), Config: b.Config, Label: b.Label,
		}
		if b.Position != nil {
			bb.PositionX, bb.PositionY = b.Position.X, b.Position.Y
		}
		if err := s.blocks.Create(ctx, tx, bb); err != nil {
			return nil, err
		}
		created = append(created, bb)
	}
	for i, c := range req.Connections {
		if c.FromBlockID < 0 || c.FromBlockID >= int64(len(created)) ||
			c.ToBlockID < 0 || c.ToBlockID >= int64(len(created)) {
			return nil, NewValidation("invalid request", []FieldError{
				{Field: fmt.Sprintf("connections[%d]", i), Reason: "from_block_id/to_block_id must be indices into blocks"}})
		}
		cc := &models.WorkflowConnection{
			WorkflowID: w.ID, FromBlockID: created[c.FromBlockID].ID, ToBlockID: created[c.ToBlockID].ID,
			Condition: c.Condition,
		}
		if err := s.conns.Create(ctx, tx, cc); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// CreateBlock — POST /api/v1/workflows/{id}/blocks (контракт 20 §2.3).
func (s *WorkflowService) CreateBlock(ctx context.Context, workflowID int64, in WorkflowBlockInput) (*models.WorkflowBlock, error) {
	if _, err := s.wf.GetByID(ctx, s.db, workflowID); err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow")
	}
	if !models.WorkflowBlockType(in.Type).Valid() {
		return nil, NewValidation("invalid request", []FieldError{{Field: "type", Reason: "invalid"}})
	}
	b := &models.WorkflowBlock{
		WorkflowID: workflowID, Type: models.WorkflowBlockType(in.Type), Config: in.Config, Label: in.Label,
	}
	if in.Position != nil {
		b.PositionX, b.PositionY = in.Position.X, in.Position.Y
	}
	if err := s.blocks.Create(ctx, s.db, b); err != nil {
		return nil, err
	}
	return b, nil
}

// CreateConnection — POST /api/v1/workflows/{id}/connections (контракт 20 §2.4).
func (s *WorkflowService) CreateConnection(ctx context.Context, workflowID int64, in WorkflowConnectionInput) (*models.WorkflowConnection, error) {
	if _, err := s.wf.GetByID(ctx, s.db, workflowID); err != nil {
		return nil, wrapRepositoryNotFound(err, "workflow")
	}
	for _, id := range []int64{in.FromBlockID, in.ToBlockID} {
		b, err := s.blocks.GetByID(ctx, s.db, id)
		if err != nil {
			return nil, NewValidation("invalid request", []FieldError{{Field: "block_id", Reason: fmt.Sprintf("block %d not in workflow", id)}})
		}
		if b.WorkflowID != workflowID {
			return nil, NewValidation("invalid request", []FieldError{{Field: "block_id", Reason: fmt.Sprintf("block %d belongs to another workflow", id)}})
		}
	}
	c := &models.WorkflowConnection{WorkflowID: workflowID, FromBlockID: in.FromBlockID, ToBlockID: in.ToBlockID, Condition: in.Condition}
	if err := s.conns.Create(ctx, s.db, c); err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateBlock — PATCH /api/v1/workflows/{id}/blocks/{blockId} (контракт 20 §2.5).
// Возвращает изменения (position/config old/new).
func (s *WorkflowService) UpdateBlock(ctx context.Context, workflowID, blockID int64, req UpdateBlockRequest) (positionChanged, configChanged bool, oldPos, newPos, oldCfg, newCfg any, err error) {
	b, err := s.blocks.GetByID(ctx, s.db, blockID)
	if err != nil {
		return false, false, nil, nil, nil, nil, wrapRepositoryNotFound(err, "block")
	}
	if b.WorkflowID != workflowID {
		return false, false, nil, nil, nil, nil, NewValidation("invalid request", []FieldError{{Field: "block_id", Reason: "block belongs to another workflow"}})
	}
	updated := *b
	if req.Position != nil {
		updated.PositionX, updated.PositionY = req.Position.X, req.Position.Y
	}
	if req.Config != nil {
		updated.Config = *req.Config
	}
	if req.Label != nil {
		updated.Label = req.Label
	}
	old, err := s.blocks.Update(ctx, s.db, &updated)
	if err != nil {
		return false, false, nil, nil, nil, nil, err
	}
	positionChanged = req.Position != nil && (req.Position.X != old.PositionX || req.Position.Y != old.PositionY)
	configChanged = req.Config != nil
	_ = old
	return positionChanged, configChanged,
		WorkflowPosition{X: old.PositionX, Y: old.PositionY},
		WorkflowPosition{X: updated.PositionX, Y: updated.PositionY},
		old.Config, updated.Config, nil
}

// wrapRepositoryNotFound — ErrNotFound из репозитория → AppError not_found.
func wrapRepositoryNotFound(err error, what string) error {
	if err == repository.ErrNotFound {
		return NewNotFound(what)
	}
	return err
}
