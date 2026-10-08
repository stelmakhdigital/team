package httpapi

import (
	"net/http"
	"strconv"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- Sessions (slice 3) ----------

type createSessionBody struct {
	RoleID      int64          `json:"role_id"`
	QueueTaskID *int64         `json:"queue_task_id"`
	RuntimeType string         `json:"runtime_type"`
	Command     string         `json:"command"`
	Args        []string       `json:"args"`
	WorkingDir  string         `json:"working_dir"`
	Config      map[string]any `json:"config"`
}

// CreateSession — POST /api/v1/sessions (body: role_id, queue_task_id?, command?, runtime_type?, config?)
func (h *handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	teamID, err := parseIDParam(r.URL.Query().Get("team_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body createSessionBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	sess, err := h.ssvc.CreateSession(r.Context(), teamID, service.CreateSessionRequest{
		RoleID: body.RoleID, QueueTaskID: body.QueueTaskID,
		RuntimeType: body.RuntimeType, Command: body.Command, Args: body.Args,
		WorkingDir: body.WorkingDir, Config: body.Config,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": sess.ID, "state": string(sess.State), "status": "started",
	})
}

// ListSessions — GET /api/v1/sessions?team_id=&role_id=&state=
func (h *handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	f := repository.SessionFilter{}
	q := r.URL.Query()
	if v := q.Get("team_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.TeamID = &id
	}
	if v := q.Get("role_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.RoleID = &id
	}
	if v := q.Get("state"); v != "" {
		st := models.SessionState(v)
		if !st.Valid() {
			writeError(w, r, service.NewValidation("invalid state filter: "+v))
			return
		}
		f.State = &st
	}
	f.Active = q.Get("active") == "true"
	limit, offset := limitOffset(q)
	f.Limit, f.Offset = limit, offset

	sessions, total, err := h.ssvc.ListSessions(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "total": total})
}

// GetSession — GET /api/v1/sessions/{id}
func (h *handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	view, err := h.ssvc.GetSessionView(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// StopSession — DELETE /api/v1/sessions/{id}
func (h *handlers) StopSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	sess, err := h.ssvc.StopSession(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": sess.ID, "state": string(sess.State), "status": "stopped",
	})
}

// GetSessionHistory — GET /api/v1/sessions/{id}/history
func (h *handlers) GetSessionHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit, offset := limitOffset(r.URL.Query())
	history, total, err := h.ssvc.GetSessionHistory(r.Context(), id, limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history, "total": total})
}

// GetTranscript — GET /api/v1/sessions/{id}/transcript?limit=
func (h *handlers) GetTranscript(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		l, err := strconv.Atoi(v)
		if err != nil || l < 0 {
			writeError(w, r, service.NewValidation("invalid limit"))
			return
		}
		limit = l
	}
	entries, total, hasMore, err := h.ssvc.GetTranscript(r.Context(), id, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transcript": entries, "total": total, "has_more": hasMore})
}

// ---------- Dashboard: sessions & alerts (slice 3) ----------

// DashboardSessions — GET /api/v1/dashboard/sessions (активные сессии)
func (h *handlers) DashboardSessions(w http.ResponseWriter, r *http.Request) {
	sessions, total, err := h.ssvc.DashboardSessions(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "total": total})
}

// DashboardAlerts — GET /api/v1/dashboard/alerts
func (h *handlers) DashboardAlerts(w http.ResponseWriter, r *http.Request) {
	f := repository.AlertFilter{}
	if v := r.URL.Query().Get("team_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.TeamID = &id
	}
	limit, _ := limitOffset(r.URL.Query())
	f.Limit = limit
	alerts, total, err := h.alerts.ListAlerts(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts, "total": total})
}

// WatchdogEvents — GET /api/v1/watchdog/events (ТЗ 01)
func (h *handlers) WatchdogEvents(w http.ResponseWriter, r *http.Request) {
	h.DashboardAlerts(w, r)
}

// MarkAlertRead — POST /api/v1/watchdog/events/{id}/read
func (h *handlers) MarkAlertRead(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.alerts.MarkAlertRead(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "is_read": true, "status": "updated"})
}
