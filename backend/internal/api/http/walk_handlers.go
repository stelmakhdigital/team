package httpapi

import (
	"net/http"

	"daemon/internal/service"
)

// ---------- P3.14: Walk (paced-доставка контекста в сессию) ----------

type createWalkBody struct {
	Name         string   `json:"name"`
	Description  *string  `json:"description"`
	Mode         string   `json:"mode"`
	IntervalSecs *int     `json:"interval_secs"`
	Steps        []string `json:"steps"`
}

// CreateWalk — POST /api/v1/sessions/{id}/walks (RBAC sessions.update, operator+).
// Создаёт именованную последовательность шагов + шаги (pending).
func (h *handlers) CreateWalk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body createWalkBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	v, err := h.walk.CreateWalk(r.Context(), id, service.CreateWalkRequest{
		Name: body.Name, Description: body.Description, Mode: body.Mode,
		IntervalSecs: body.IntervalSecs, Steps: body.Steps,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// ListWalks — GET /api/v1/sessions/{id}/walks (RBAC dashboard.read, viewer-ok).
func (h *handlers) ListWalks(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := h.walk.ListWalks(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"walks": items, "total": len(items)})
}

// GetWalk — GET /api/v1/walks/{id} (RBAC dashboard.read, viewer-ok). Walk + steps + progress.
func (h *handlers) GetWalk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := h.walk.GetWalk(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// AdvanceWalk — POST /api/v1/walks/{id}/advance (RBAC sessions.update, operator+).
// Отправляет следующий шаг (Send). retained → paused; failed → failed; все → completed.
func (h *handlers) AdvanceWalk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := h.walk.Advance(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// PauseWalk — POST /api/v1/walks/{id}/pause (RBAC sessions.update, operator+).
func (h *handlers) PauseWalk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := h.walk.Pause(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// ResumeWalk — POST /api/v1/walks/{id}/resume (RBAC sessions.update, operator+).
func (h *handlers) ResumeWalk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := h.walk.Resume(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// SkipWalk — POST /api/v1/walks/{id}/skip (RBAC sessions.update, operator+).
// Текущий шаг → skipped, current_seq++ (без Send).
func (h *handlers) SkipWalk(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	v, err := h.walk.Skip(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
