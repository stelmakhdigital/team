package httpapi

import (
	"net/http"
	"strconv"

	"daemon/internal/service"
)

// P1.6 — Watchdog-джобы (register, context-usage-threshold).

// RegisterWatchdogJob — POST /api/v1/watchdog/jobs (RBAC watchdog.update).
func (h *handlers) RegisterWatchdogJob(w http.ResponseWriter, r *http.Request) {
	var body service.RegisterWatchdogJobRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	createdBy := ""
	if ac := authContext(r.Context()); ac != nil {
		createdBy = ac.UserName
	}
	view, err := h.wdjobs.Register(r.Context(), &body, createdBy)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// ListWatchdogJobs — GET /api/v1/watchdog/jobs?all=&limit= (RBAC dashboard.read).
// Default: только enabled (compact); all=true — все.
func (h *handlers) ListWatchdogJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	all := q.Get("all") == "true" || q.Get("all") == "1"
	limit, _ := strconv.Atoi(q.Get("limit"))
	views, err := h.wdjobs.List(r.Context(), all, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": views, "total": len(views)})
}

// GetWatchdogJob — GET /api/v1/watchdog/jobs/{id} (RBAC dashboard.read): джоб + recent evaluations.
func (h *handlers) GetWatchdogJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.wdjobs.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// StopWatchdogJob — POST /api/v1/watchdog/jobs/{id}/stop (RBAC watchdog.update): enabled=false.
func (h *handlers) StopWatchdogJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	view, err := h.wdjobs.Stop(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// DeleteWatchdogJob — DELETE /api/v1/watchdog/jobs/{id} (RBAC watchdog.update).
// Если fired_count>0 → 409 (останься в истории; используй stop).
func (h *handlers) DeleteWatchdogJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.wdjobs.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
