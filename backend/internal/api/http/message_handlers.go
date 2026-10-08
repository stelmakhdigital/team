package httpapi

import (
	"net/http"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- Message Center (slice 4, контракт 20 §4) ----------

// ListMessages — GET /api/v1/messages
// Query: team_id, queue_task_id, from_role_id, to_role_id, type, limit, offset.
func (h *handlers) ListMessages(w http.ResponseWriter, r *http.Request) {
	f := repository.MessageFilter{}
	q := r.URL.Query()
	if v := q.Get("team_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.TeamID = &id
	}
	if v := q.Get("queue_task_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.QueueTaskID = &id
	}
	if v := q.Get("from_role_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.FromRoleID = &id
	}
	if v := q.Get("to_role_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.ToRoleID = &id
	}
	if v := q.Get("type"); v != "" {
		t := models.MessageType(v)
		if !t.Valid() {
			writeError(w, r, service.NewValidation("invalid type filter: "+v))
			return
		}
		f.Type = &t
	}
	limit, offset := limitOffset(q)
	f.Limit, f.Offset = limit, offset

	messages, total, err := h.msvc.ListMessages(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages, "total": total, "has_more": offset+limit < total,
	})
}

type sendMessageBody struct {
	TeamID      int64          `json:"team_id"`
	QueueTaskID *int64         `json:"queue_task_id"`
	FromRoleID  *int64         `json:"from_role_id"`
	ToRoleID    *int64         `json:"to_role_id"`
	Type        string         `json:"type"`
	Body        string         `json:"body"`
	Metadata    map[string]any `json:"metadata"`
}

// SendMessage — POST /api/v1/messages
func (h *handlers) SendMessage(w http.ResponseWriter, r *http.Request) {
	var body sendMessageBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.msvc.SendMessage(r.Context(), service.SendMessageRequest{
		TeamID: body.TeamID, QueueTaskID: body.QueueTaskID,
		FromRoleID: body.FromRoleID, ToRoleID: body.ToRoleID,
		Type: body.Type, Body: body.Body, Metadata: body.Metadata,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// ListChatrooms — GET /api/v1/chatrooms (опц. ?team_id=)
func (h *handlers) ListChatrooms(w http.ResponseWriter, r *http.Request) {
	var teamID *int64
	if v := r.URL.Query().Get("team_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		teamID = &id
	}
	var userID *int64
	if ac := authContext(r.Context()); ac != nil {
		userID = ac.UserID
	}
	rooms, err := h.msvc.ListChatrooms(r.Context(), teamID, userID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chatrooms": rooms})
}

// GetChatroomMessages — GET /api/v1/chatrooms/{id}/messages?limit=&offset=
func (h *handlers) GetChatroomMessages(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var userID *int64
	if ac := authContext(r.Context()); ac != nil {
		userID = ac.UserID
	}
	limit, offset := limitOffset(r.URL.Query())
	messages, total, err := h.msvc.GetChatroomMessages(r.Context(), id, limit, offset, userID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages, "has_more": offset+limit < total,
	})
}

type sendChatroomMessageBody struct {
	Body       string `json:"body"`
	FromRoleID *int64 `json:"from_role_id"`
}

// SendChatroomMessage — POST /api/v1/chatrooms/{id}/messages
func (h *handlers) SendChatroomMessage(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body sendChatroomMessageBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	view, err := h.msvc.SendChatroomMessage(r.Context(), id, struct {
		Body       string `json:"body"`
		FromRoleID *int64 `json:"from_role_id"`
	}{Body: body.Body, FromRoleID: body.FromRoleID})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": view.ID, "status": "sent"})
}
