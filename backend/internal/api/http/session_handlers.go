package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- Sessions (slice 3) ----------

type createSessionBody struct {
	RoleID      int64          `json:"role_id"`
	QueueTaskID *int64         `json:"queue_task_id"`
	ProjectID   *int64         `json:"project_id"` // P1.0: явный проект (опционально)
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
		RoleID: body.RoleID, QueueTaskID: body.QueueTaskID, ProjectID: body.ProjectID,
		RuntimeType: body.RuntimeType, Command: body.Command, Args: body.Args,
		WorkingDir: body.WorkingDir, Config: body.Config,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	briefingApplied, _ := sess.Config["briefing_applied"].(string)
	briefingPath, _ := sess.Config["briefing_path"].(string)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": sess.ID, "state": string(sess.State), "status": "started",
		// P1.7: briefing (что передано агенту)
		"briefing_applied": briefingApplied,
		"briefing_path":    briefingPath,
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
	if v := q.Get("project_id"); v != "" { // P1.0: фильтр по проекту
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.ProjectID = &id
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

// ---------- P2.8: capture (стл-кадр экрана сессии) ----------

// CaptureSession — GET /api/v1/sessions/{id}/capture?lines=N (RBAC sessions.read, viewer-ok).
// lines — default 20, max 200 (clamp). tmux/pi → capture-pane (мертвый tmux → 409);
// process → log-tail (файла нет → 200, пустые lines). Read-only.
func (h *handlers) CaptureSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	lines := 20
	if v := r.URL.Query().Get("lines"); v != "" {
		l, err := strconv.Atoi(v)
		if err != nil || l < 0 {
			writeError(w, r, service.NewValidation("invalid lines"))
			return
		}
		lines = l
	}
	res, err := h.ssvc.Capture(r.Context(), id, lines)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------- P2.11: send to session + verify ----------

// SendSession — POST /api/v1/sessions/{id}/send (RBAC sessions.update, operator+).
// Body: {text (non-empty, ≤2000), submit? bool default true}. Ввод в терминал
// живой сессии + верификация capture-м (delivered/staged/retained).
func (h *handlers) SendSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		Text   string `json:"text"`
		Submit *bool  `json:"submit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, service.NewValidation("invalid body"))
		return
	}
	submit := true
	if body.Submit != nil {
		submit = *body.Submit
	}
	res, err := h.ssvc.Send(r.Context(), id, body.Text, submit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------- P3.12: snapshot сессии (agent_image) ----------

// SnapshotSession — POST /api/v1/sessions/{id}/snapshot (RBAC sessions.update, operator+).
// Body: {name (обязат.), summary?, description?}. Захват продуктивного состояния
// сессии (агент-конфиг + резюме) → Library-элемент type=agent_image.
func (h *handlers) SnapshotSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		Name        string  `json:"name"`
		Summary     *string `json:"summary"`
		Description *string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, service.NewValidation("invalid body"))
		return
	}
	it, err := h.ssvc.SnapshotSession(r.Context(), id, service.SnapshotRequest{
		Name: body.Name, Summary: body.Summary, Description: body.Description,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": it.ID, "type": "agent_image", "name": it.Name, "status": "created",
	})
}

// ---------- P2.7: поиск по транскриптам ----------

// SearchSessions — GET /api/v1/sessions/search?q=&role_id=&team_id=&state=&limit=&offset=
// (RBAC sessions.read, viewer-ok). q — обязательный (min 2), фильтры сузят набор
// сканируемых файлов. Read-only, bounded, честный (файл-нет → сессия пропускается).
func (h *handlers) SearchSessions(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeError(w, r, service.NewValidation("q: обязательный, мин 2 символа"))
		return
	}
	var roleID, teamID *int64
	if v := r.URL.Query().Get("role_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			roleID = &n
		}
	}
	if v := r.URL.Query().Get("team_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			teamID = &n
		}
	}
	var state *models.SessionState
	if v := r.URL.Query().Get("state"); v != "" {
		sst := models.SessionState(v)
		if !sst.Valid() {
			writeError(w, r, service.NewValidation("state: невалидное состояние сессии"))
			return
		}
		state = &sst
	}
	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l >= 0 {
			limit = l
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			offset = o
		}
	}
	res, err := h.ssvc.SearchTranscripts(r.Context(), service.TranscriptSearchParams{
		Q: q, RoleID: roleID, TeamID: teamID, State: state, Limit: limit, Offset: offset,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------- P1.5: compact-plan + handover ----------

// CompactPlan — GET /api/v1/compact-plan (живые сессии у контекст-стены, desc).
func (h *handlers) CompactPlan(w http.ResponseWriter, r *http.Request) {
	entries, wall, err := h.ssvc.CompactPlan(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": entries, "total": len(entries), "wall_pct": wall})
}

// HandoverSession — POST /api/v1/sessions/{id}/handover (body: reason, target_working_dir?, dry_run).
type handoverBody struct {
	Reason           string `json:"reason"`
	TargetWorkingDir string `json:"target_working_dir"`
	DryRun           bool   `json:"dry_run"`
}

func (h *handlers) HandoverSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body handoverBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.ssvc.Handover(r.Context(), id, service.HandoverRequest{
		Reason: body.Reason, TargetWorkingDir: body.TargetWorkingDir, DryRun: body.DryRun,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// PreviewBriefing — POST /api/v1/teams/{id}/briefing/preview (P1.7, dry-run).
// Возвращает собранный брифинг (text + data) + как он будет передан (adapter-режим).
type briefingPreviewBody struct {
	RoleID      int64  `json:"role_id"`
	QueueTaskID *int64 `json:"queue_task_id"`
	Override    string `json:"override"`
	Append      string `json:"append"`
	RuntimeType string `json:"runtime_type"`
	WorkingDir  string `json:"working_dir"`
	ProjectID   *int64 `json:"project_id"`
}

func (h *handlers) PreviewBriefing(w http.ResponseWriter, r *http.Request) {
	if h.briefing == nil {
		writeError(w, r, &service.AppError{Code: "not_implemented", Status: http.StatusNotImplemented, Message: "briefing service not configured"})
		return
	}
	teamID, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	// team должен существовать (404)
	if _, err := h.svc.GetTeam(r.Context(), teamID); err != nil {
		writeError(w, r, err)
		return
	}
	var body briefingPreviewBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.RoleID == 0 {
		writeError(w, r, service.NewValidation("invalid request", []service.FieldError{{Field: "role_id", Reason: "required"}}))
		return
	}
	rtType := body.RuntimeType
	if rtType == "" {
		rtType = "pi"
	}
	var base *service.Briefing
	if strings.TrimSpace(body.Override) == "" {
		base = h.briefing.Build(r.Context(), service.BriefingParams{
			TeamID: teamID, RoleID: body.RoleID, QueueTaskID: body.QueueTaskID,
			WorkingDir: body.WorkingDir, ProjectID: body.ProjectID,
		})
	}
	text, applied := service.ApplyBriefingOverrides(base, body.Override, body.Append)
	writeJSON(w, http.StatusOK, map[string]any{
		"briefing_text":    text,
		"briefing_data":    briefDataOrEmpty(base),
		"briefing_applied": applied,
		"delivery":         service.BriefingDeliveryFor(rtType),
	})
}

func briefDataOrEmpty(base *service.Briefing) any {
	if base == nil {
		return service.BriefingData{}
	}
	return base.Data
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
