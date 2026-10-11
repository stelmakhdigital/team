package httpapi

import (
	"net/http"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- Library (slice 5b, контракт 20 §5) ----------

// ListLibrary — GET /api/v1/library?type=&group=&search=&limit=&offset=
func (h *handlers) ListLibrary(w http.ResponseWriter, r *http.Request) {
	f := repository.LibraryFilter{}
	q := r.URL.Query()
	if v := q.Get("type"); v != "" {
		t := models.LibraryItemType(v)
		if !t.Valid() {
			writeError(w, r, service.NewValidation("invalid type filter: "+v))
			return
		}
		f.Type = &t
	}
	if v := q.Get("group"); v != "" {
		f.Group = &v
	}
	if v := q.Get("search"); v != "" {
		f.Search = &v
	}
	limit, offset := limitOffset(q)
	f.Limit, f.Offset = limit, offset

	items, total, groups, err := h.lsvc.List(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "groups": groups,
	})
}

// SaveToLibrary — POST /api/v1/library
func (h *handlers) SaveToLibrary(w http.ResponseWriter, r *http.Request) {
	var body service.SaveToLibraryRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	item, err := h.lsvc.Save(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": item.ID, "status": "saved", "library_item_id": item.ID,
	})
}

// GetLibraryItem — GET /api/v1/library/{id}
func (h *handlers) GetLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, spec, versions, err := h.lsvc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item": item, "spec": spec, "versions": versions,
	})
}

// UpdateLibraryItem — PATCH /api/v1/library/{id} (U3-rename: name/description).
func (h *handlers) UpdateLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body service.UpdateLibraryRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	body.ID = id
	view, err := h.lsvc.Update(r.Context(), body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": view.ID, "name": view.Name, "status": "updated"})
}

// ApplyLibraryItem — POST /api/v1/library/{id}/apply
func (h *handlers) ApplyLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body service.ApplyLibraryItemRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.lsvc.Apply(r.Context(), id, body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------- P3.12: agent_image — pin / unpin / prune / launch ----------

// PinLibraryItem — POST /api/v1/library/{id}/pin (is_pinned = true; agent_image).
func (h *handlers) PinLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	view, err := h.lsvc.Pin(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": view.ID, "is_pinned": view.IsPinned, "status": "pinned"})
}

// UnpinLibraryItem — POST /api/v1/library/{id}/unpin (is_pinned = false).
func (h *handlers) UnpinLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	view, err := h.lsvc.Unpin(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": view.ID, "is_pinned": view.IsPinned, "status": "unpinned"})
}

// DeleteLibraryItem — DELETE /api/v1/library/{id} (prune; только agent_image, не-pinned).
func (h *handlers) DeleteLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := h.lsvc.Prune(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "deleted"})
}

// LaunchLibraryItem — POST /api/v1/library/{id}/launch (создать сессию из agent_image).
// Body: {team_id (обязат.), project_id?}. Только type=agent_image.
func (h *handlers) LaunchLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		TeamID    int64  `json:"team_id"`
		ProjectID *int64 `json:"project_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, service.NewValidation("invalid body"))
		return
	}
	sess, err := h.ssvc.LaunchFromSnapshot(r.Context(), id, service.LaunchRequest{
		TeamID: body.TeamID, ProjectID: body.ProjectID,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": sess.ID, "state": string(sess.State), "status": "started",
	})
}

// ---------- Audit log (slice 5b, контракт 20 §6.3) ----------

// ListAudit — GET /api/v1/audit
func (h *handlers) ListAudit(w http.ResponseWriter, r *http.Request) {
	f := repository.AuditFilter{}
	q := r.URL.Query()
	if v := q.Get("user_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.UserID = &id
	}
	if v := q.Get("action"); v != "" {
		f.Action = &v
	}
	if v := q.Get("resource"); v != "" {
		f.Resource = &v
	}
	if v := q.Get("start_time"); v != "" {
		f.StartTime = &v
	}
	if v := q.Get("end_time"); v != "" {
		f.EndTime = &v
	}
	limit, offset := limitOffset(q)
	f.Limit, f.Offset = limit, offset

	entries, total, err := h.asvc.List(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	views := make([]*AuditEntryView, 0, len(entries))
	for _, e := range entries {
		views = append(views, auditEntryView(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": views, "total": total})
}

// AuditEntryView — запись audit log (контракт: AuditEntry).
type AuditEntryView struct {
	ID        int64          `json:"id"`
	Timestamp string         `json:"timestamp"`
	UserID    *int64         `json:"user_id,omitempty"`
	UserName  *string        `json:"user_name,omitempty"`
	APIKeyID  *int64         `json:"api_key_id,omitempty"`
	Action    string         `json:"action"`
	Resource  *string        `json:"resource,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	IPAddress *string        `json:"ip_address,omitempty"`
	UserAgent *string        `json:"user_agent,omitempty"`
	Severity  string         `json:"severity"`
}

func auditEntryView(e *models.AuditEntry) *AuditEntryView {
	return &AuditEntryView{
		ID: e.ID, Timestamp: e.CreatedAt.Format(rfc3339),
		UserID: e.UserID, UserName: e.UserName, APIKeyID: e.APIKeyID,
		Action: e.Action, Resource: e.Resource,
		Details: e.Details, IPAddress: e.IPAddress, UserAgent: e.UserAgent,
		Severity: "info",
	}
}

// ---------- Dashboard metrics (slice 5b, контракт 20 §3.5) ----------

// DashboardMetrics — GET /api/v1/dashboard/metrics?range=1h|24h|7d
func (h *handlers) DashboardMetrics(w http.ResponseWriter, r *http.Request) {
	rng := service.MetricsRange(r.URL.Query().Get("range"))
	if rng == "" {
		rng = service.MetricsRange24H
	}
	res, err := h.msvcM.Get(r.Context(), rng)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
