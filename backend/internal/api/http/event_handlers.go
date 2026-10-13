package httpapi

import (
	"net/http"
	"strconv"

	"daemon/internal/service"
)

// ---------- P2.9: Pulse feed — GET /api/v1/events (RBAC dashboard.read, viewer-ok) ----------

// ListEvents — единая лента событий (фильтры type/team/role/entity + q + limit/offset).
// q (W5.2) — substring, case-insensitive по summary.
func (h *handlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var teamID, roleID, entityID int64
	if v := q.Get("team_id"); v != "" {
		teamID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("role_id"); v != "" {
		roleID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("entity_id"); v != "" {
		entityID, _ = strconv.ParseInt(v, 10, 64)
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		l, _ := strconv.Atoi(v)
		if l > 0 {
			limit = l
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		o, _ := strconv.Atoi(v)
		if o > 0 {
			offset = o
		}
	}
	if h.evs == nil {
		writeError(w, r, service.NewValidation("events not enabled"))
		return
	}
	page, err := h.evs.ListEvents(r.Context(), service.EventFilter{
		Type: q.Get("type"), EntityKind: q.Get("entity_kind"), Q: q.Get("q"),
		TeamID: teamID, RoleID: roleID, EntityID: entityID,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
