package httpapi

import (
	"net/http"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- Workflow Editor (slice 5, контракт 20 §2) ----------

// ListWorkflows — GET /api/v1/workflows?team_id=&state= (контракт 20 §2.0).
func (h *handlers) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	var teamID *int64
	if v := r.URL.Query().Get("team_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		teamID = &id
	}
	var state *models.WorkflowState
	if v := r.URL.Query().Get("state"); v != "" {
		s := models.WorkflowState(v)
		if !s.Valid() {
			writeError(w, r, service.NewValidation("invalid state filter: "+v))
			return
		}
		state = &s
	}
	ws, total, err := h.wsvc.ListWorkflows(r.Context(), teamID, state)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": ws, "total": total})
}

// GetWorkflow — GET /api/v1/workflows/{id} (контракт 20 §2.1).
func (h *handlers) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	wf, blocks, conns, err := h.wsvc.GetWorkflow(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workflow": wf, "blocks": blocks, "connections": conns,
	})
}

// CreateWorkflow — POST /api/v1/workflows (контракт 20 §2.2).
func (h *handlers) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var body service.CreateWorkflowRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	wf, err := h.wsvc.CreateWorkflow(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": wf.ID, "name": wf.Name, "status": "created",
	})
}

// CreateWorkflowBlock — POST /api/v1/workflows/{id}/blocks (контракт 20 §2.3).
func (h *handlers) CreateWorkflowBlock(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body service.WorkflowBlockInput
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	b, err := h.wsvc.CreateBlock(r.Context(), id, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": b.ID, "workflow_id": b.WorkflowID, "type": string(b.Type), "status": "created",
	})
}

// CreateWorkflowConnection — POST /api/v1/workflows/{id}/connections (контракт 20 §2.4).
func (h *handlers) CreateWorkflowConnection(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body service.WorkflowConnectionInput
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := h.wsvc.CreateConnection(r.Context(), id, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID, "status": "created"})
}

// UpdateWorkflowBlock — PATCH /api/v1/workflows/{id}/blocks/{blockId} (контракт 20 §2.5).
func (h *handlers) UpdateWorkflowBlock(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	blockID, err := parseIDParam(r.PathValue("blockId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body service.UpdateBlockRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	posChanged, cfgChanged, oldPos, newPos, oldCfg, newCfg, err :=
		h.wsvc.UpdateBlock(r.Context(), id, blockID, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	changes := map[string]any{}
	if posChanged {
		changes["position"] = map[string]any{"old": oldPos, "new": newPos}
	}
	if cfgChanged {
		changes["config"] = map[string]any{"old": oldCfg, "new": newCfg}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": blockID, "status": "updated", "changes": changes})
}
