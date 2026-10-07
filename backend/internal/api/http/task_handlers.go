package httpapi

import (
	"net/http"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- Tasks (slice 2) ----------

// ListTasks — GET /api/v1/tasks?team_id=&state=&destination_role_id=&limit=&offset=
func (h *handlers) ListTasks(w http.ResponseWriter, r *http.Request) {
	f := repository.TaskFilter{}
	q := r.URL.Query()
	if v := q.Get("team_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.TeamID = &id
	}
	if v := q.Get("state"); v != "" {
		st := models.TaskState(v)
		if !st.Valid() {
			writeError(w, r, service.NewValidation("invalid state filter: "+v))
			return
		}
		f.State = &st
	}
	if v := q.Get("destination_role_id"); v != "" {
		id, err := parseIDParam(v)
		if err != nil {
			writeError(w, r, err)
			return
		}
		f.DestinationRole = &id
	}
	limit, offset := limitOffset(q)
	f.Limit, f.Offset = limit, offset

	tasks, total, err := h.tsvc.ListTasks(r.Context(), f)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "total": total})
}

type createTaskBody struct {
	TeamID            int64          `json:"team_id"`
	ParentTaskID      *int64         `json:"parent_task_id"`
	DestinationRoleID int64          `json:"destination_role_id"`
	SourceRoleID      *int64         `json:"source_role_id"`
	Title             string         `json:"title"`
	Body              string         `json:"body"`
	BodyContext       map[string]any `json:"body_context"`
	Priority          int            `json:"priority"`
}

// CreateTask — POST /api/v1/tasks
func (h *handlers) CreateTask(w http.ResponseWriter, r *http.Request) {
	var body createTaskBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	task, err := h.tsvc.CreateTask(r.Context(), service.CreateTaskRequest{
		TeamID: body.TeamID, ParentTaskID: body.ParentTaskID,
		DestinationRoleID: body.DestinationRoleID, SourceRoleID: body.SourceRoleID,
		Title: body.Title, Body: body.Body, BodyContext: body.BodyContext, Priority: body.Priority,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": task.ID, "state": string(task.State), "status": "created"})
}

// GetTask — GET /api/v1/tasks/{id}
func (h *handlers) GetTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.tsvc.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type updateTaskStateBody struct {
	State           string  `json:"state"`
	ClosureReason   *string `json:"closure_reason"`
	ClosureTargetID *int64  `json:"closure_target_id"`
	Comment         string  `json:"comment"`
}

// UpdateTaskState — PATCH /api/v1/tasks/{id}/state
func (h *handlers) UpdateTaskState(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body updateTaskStateBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	req := service.UpdateTaskStateRequest{
		ID: id, State: models.TaskState(body.State),
		ClosureTargetID: body.ClosureTargetID, Comment: body.Comment, ActorType: "human",
	}
	if body.ClosureReason != nil {
		cr := models.ClosureReason(*body.ClosureReason)
		req.ClosureReason = &cr
	}
	task, err := h.tsvc.UpdateTaskState(r.Context(), req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	// ответ в контрактном виде
	detail, err := h.tsvc.GetTask(r.Context(), task.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail.Task)
}

type handoffBody struct {
	ToRoleID int64  `json:"to_role_id"`
	Comment  string `json:"comment"`
}

// HandoffTask — POST /api/v1/tasks/{id}/handoff
func (h *handlers) HandoffTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body handoffBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.tsvc.HandoffTask(r.Context(), service.HandoffTaskRequest{ID: id, ToRoleID: body.ToRoleID, Comment: body.Comment})
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.tsvc.GetTask(r.Context(), res.NewTask.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"new_task_id": res.NewTask.ID, "closed_task_id": res.ClosedTask.ID,
		"task":   detail.Task,
		"status": "handed_off",
	})
}

// GetTaskHistory — GET /api/v1/tasks/{id}/history?limit=&offset=
func (h *handlers) GetTaskHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit, offset := limitOffset(r.URL.Query())
	entries, total, err := h.tsvc.GetTaskHistory(r.Context(), id, limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": entries, "total": total})
}

// ---------- Dashboard (slice 2) ----------

// DashboardSummary — GET /api/v1/dashboard/summary
func (h *handlers) DashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.tsvc.DashboardSummary(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// DashboardTasks — GET /api/v1/dashboard/tasks
func (h *handlers) DashboardTasks(w http.ResponseWriter, r *http.Request) {
	tasks, total, err := h.tsvc.DashboardTasks(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "total": total})
}
