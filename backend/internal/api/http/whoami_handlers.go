package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"daemon/internal/service"
)

// GetWhoami — GET /api/v1/whoami?session_id=&role_id=&team_id= (P2.15).
// «Кто я и что мне делать» — компактный срез для агента. RBAC: read (viewer-ok).
func (h *handlers) GetWhoami(w http.ResponseWriter, r *http.Request) {
	if h.whoami == nil {
		writeError(w, r, &service.AppError{Code: "internal", Status: 503, Message: "whoami unavailable"})
		return
	}
	req := service.WhoamiRequest{}
	if id := parseQueryID(r, "session_id"); id != nil {
		req.SessionID = id
	}
	if id := parseQueryID(r, "role_id"); id != nil {
		req.RoleID = id
	}
	if id := parseQueryID(r, "team_id"); id != nil {
		req.TeamID = id
	}
	res, err := h.whoami.Whoami(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// parseQueryID — query-параметр как *int64 (пустой/не-число → nil).
func parseQueryID(r *http.Request, name string) *int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
