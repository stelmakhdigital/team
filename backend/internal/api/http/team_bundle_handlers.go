package httpapi

// C1 / P4.3 — Team bundle: GET /teams/{id}/export + POST /teams/import.

import (
	"net/http"

	"daemon/internal/service"
)

// ExportTeam — GET /api/v1/teams/{id}/export → 200 TeamBundle (RBAC teams.read).
func (h *handlers) ExportTeam(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	bundle, err := h.svc.ExportTeam(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

// ImportTeam — POST /api/v1/teams/import, body = bundle JSON (RBAC teams.update).
// 201: полный TeamView (team + segments + roles + relatives, как GET /teams/{id})
// + workflows: [{id, name}] (созданные воркфлоу, имена с auto-suffix).
func (h *handlers) ImportTeam(w http.ResponseWriter, r *http.Request) {
	var bundle service.TeamBundle
	if err := decodeJSON(r, &bundle); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.svc.ImportTeam(r.Context(), &bundle)
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.svc.GetTeam(r.Context(), res.Team.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	resp := teamDetailResponse(detail)
	resp["workflows"] = res.Workflows
	writeJSON(w, http.StatusCreated, resp)
}
