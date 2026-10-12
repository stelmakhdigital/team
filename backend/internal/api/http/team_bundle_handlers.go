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
// 201: { team: <TeamView>, workflows: [{id, name}] }.
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"team": map[string]any{
			"id":             res.Team.ID,
			"name":           res.Team.Name,
			"description":    res.Team.Description,
			"state":          string(res.Team.State),
			"posture":        string(res.Team.Posture),
			"segments_count": res.SegmentsCount,
			"roles_count":    res.RolesCount,
			"created_at":     res.Team.CreatedAt.Format(rfc3339),
			"updated_at":     res.Team.UpdatedAt.Format(rfc3339),
		},
		"workflows": res.Workflows,
	})
}
