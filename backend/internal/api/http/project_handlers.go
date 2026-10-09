package httpapi

import (
	"net/http"

	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

type createProjectBody struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`        // existing | new
	Path        string `json:"path"`        // kind=existing
	ParentPath  string `json:"parent_path"` // kind=new
	Description string `json:"description"`
}

type updateProjectBody struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Path        *string `json:"path"` // если задан → 409 (path immutable)
}

func projectView(p *models.Project) map[string]any {
	return map[string]any{
		"id": p.ID, "name": p.Name, "path": p.Path, "kind": string(p.Kind),
		"git": p.Git, "description": p.Description, "archived": p.Archived,
		"created_at": p.CreatedAt.Format(rfc3339), "updated_at": p.UpdatedAt.Format(rfc3339),
	}
}

// ListProjects — GET /api/v1/projects
func (h *handlers) ListProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := limitOffset(q)
	f := repository.ProjectFilter{Name: q.Get("name")}
	switch {
	case q.Get("archived") == "true":
		f.ArchivedOnly = true
	case q.Get("include_archived") == "true":
		f.IncludeArch = true
	}
	projects, total, err := h.projects.ListProjects(r.Context(), f, limit, offset)
	if err != nil {
		writeError(w, r, err)
		return
	}
	views := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		views = append(views, projectView(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": views, "total": total})
}

// CreateProject — POST /api/v1/projects
func (h *handlers) CreateProject(w http.ResponseWriter, r *http.Request) {
	var body createProjectBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	res, err := h.projects.CreateProject(r.Context(), service.CreateProjectRequest{
		Name: body.Name, Kind: body.Kind, Path: body.Path,
		ParentPath: body.ParentPath, Description: body.Description,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	resp := map[string]any{"project": projectView(res.Project)}
	if res.Warning != "" {
		resp["warning"] = res.Warning
	}
	writeJSON(w, http.StatusCreated, resp)
}

// GetProject — GET /api/v1/projects/{id} (project + счётчики)
func (h *handlers) GetProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	summary, err := h.projects.GetProject(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project": projectView(&summary.Project),
		"counts": map[string]any{
			"teams": summary.Counts.Teams, "tasks": summary.Counts.Tasks,
			"active_sessions": summary.Counts.ActiveSessions,
		},
	})
}

// UpdateProject — PATCH /api/v1/projects/{id} (name/description; path → 409)
func (h *handlers) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body updateProjectBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.Path != nil {
		writeError(w, r, service.NewConflict("project path is immutable after creation"))
		return
	}
	p, err := h.projects.UpdateProject(r.Context(), id, body.Name, body.Description)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": projectView(p)})
}

// ArchiveProject — DELETE /api/v1/projects/{id} (архив, идемпотентно)
func (h *handlers) ArchiveProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, err := h.projects.ArchiveProject(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": projectView(p)})
}
