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

// UpdateWorkflow — PATCH /api/v1/workflows/{id} (U3-rename: name/description).
func (h *handlers) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body service.UpdateWorkflowRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	body.ID = id
	view, err := h.wsvc.UpdateWorkflow(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": view.ID, "name": view.Name, "status": "updated"})
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

// DeleteWorkflowBlock — DELETE /api/v1/workflows/{id}/blocks/{blockId} (контракт 20 §2.6).
// Удаляет блок и все его связи (каскад).
func (h *handlers) DeleteWorkflowBlock(w http.ResponseWriter, r *http.Request) {
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
	removed, err := h.wsvc.DeleteWorkflowBlock(r.Context(), id, blockID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": blockID, "status": "deleted", "removed_connections": removed,
	})
}

// DeleteWorkflowConnection — DELETE /api/v1/workflows/{id}/connections/{connectionId} (контракт 20 §2.7).
func (h *handlers) DeleteWorkflowConnection(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	connectionID, err := parseIDParam(r.PathValue("connectionId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.wsvc.DeleteWorkflowConnection(r.Context(), id, connectionID); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": connectionID, "status": "deleted"})
}

// DeleteWorkflow — DELETE /api/v1/workflows/{id} (контракт 20 §2.8).
// Удаляет workflow + все его блоки и связи.
func (h *handlers) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.wsvc.DeleteWorkflow(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "deleted"})
}

// ---------- P1.4: Workflow engine ----------

type runWorkflowRequest struct {
	RootObjective string `json:"root_objective"`
}

// RunWorkflow — POST /api/v1/workflows/{id}/run (P1.4): создаёт instance + первый шаг.
func (h *handlers) RunWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body runWorkflowRequest
	_ = decodeJSON(r, &body) // body опционален (root_objective пустой допустим)
	inst, err := h.engine.Run(r.Context(), id, body.RootObjective)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"instance": inst})
}

// ListWorkflowInstances — GET /api/v1/workflows/{id}/instances (P1.4).
func (h *handlers) ListWorkflowInstances(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	insts, err := h.engine.ListInstances(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": insts, "total": len(insts)})
}

// GetWorkflowInstance — GET /api/v1/workflow-instances/{iid} (P1.4: instance + steps).
func (h *handlers) GetWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("iid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.engine.GetInstance(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// GetWorkflowInstanceTrace — GET /api/v1/workflow-instances/{iid}/trace (P1.4, ASC).
func (h *handlers) GetWorkflowInstanceTrace(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("iid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	steps, err := h.engine.GetTrace(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": steps, "total": len(steps)})
}

type routeInstanceRequest struct {
	ToRoleID int64 `json:"to_role_id"`
}

// ResumeWorkflowInstance — POST /api/v1/workflow-instances/{iid}/resume (P1.4).
func (h *handlers) ResumeWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("iid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	inst, err := h.engine.Resume(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": inst})
}

// RouteWorkflowInstance — POST /api/v1/workflow-instances/{iid}/route (P1.4).
func (h *handlers) RouteWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("iid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body routeInstanceRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.ToRoleID == 0 {
		writeError(w, r, service.NewValidation("to_role_id is required", []service.FieldError{{Field: "to_role_id", Reason: "required"}}))
		return
	}
	inst, err := h.engine.Route(r.Context(), id, body.ToRoleID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": inst})
}

// AbortWorkflowInstance — POST /api/v1/workflow-instances/{iid}/abort (P1.4).
func (h *handlers) AbortWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("iid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	inst, err := h.engine.Abort(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": inst})
}
