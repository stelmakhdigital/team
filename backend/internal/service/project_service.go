package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ProjectService — P1.0: Projects (мультипроектность).
type ProjectService struct {
	db       *sql.DB
	projects *repository.ProjectRepo
	// GitProbe — проверяет доступность git (exported для тестов). Default: exec.LookPath("git").
	GitProbe func() bool
}

func NewProjectService(db *sql.DB, s *repository.Stores) *ProjectService {
	return &ProjectService{
		db: db, projects: s.Projects,
		GitProbe: func() bool { _, err := exec.LookPath("git"); return err == nil },
	}
}

func (s *ProjectService) hasGit() bool {
	if s.GitProbe == nil {
		return false
	}
	return s.GitProbe()
}

// CreateProjectRequest — body для POST /projects.
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`                  // existing | new
	Path        string `json:"path,omitempty"`        // kind=existing: абсолютный путь (должен существовать)
	ParentPath  string `json:"parent_path,omitempty"` // kind=new: parent-каталог + name
	Description string `json:"description,omitempty"`
}

// CreateProjectResult — ответ (project + необязательный warning).
type CreateProjectResult struct {
	Project *models.Project `json:"project"`
	Warning string          `json:"warning,omitempty"`
}

// ProjectCounts — счётчики для сводки.
type ProjectCounts struct {
	Teams          int `json:"teams"`
	Tasks          int `json:"tasks"`
	ActiveSessions int `json:"active_sessions"`
}

// ProjectSummary — project + счётчики (GET /projects/{id}).
type ProjectSummary struct {
	models.Project
	Counts ProjectCounts `json:"counts"`
}

// CreateProject — создание проекта (existing/new) с валидацией файловой системы.
func (s *ProjectService) CreateProject(ctx context.Context, req CreateProjectRequest) (*CreateProjectResult, error) {
	if req.Name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "required"}})
	}
	if !models.ProjectKind(req.Kind).Valid() {
		return nil, NewValidation("invalid request", []FieldError{{Field: "kind", Reason: "must be 'existing' or 'new'"}})
	}

	var path, warning string
	switch models.ProjectKind(req.Kind) {
	case models.ProjectExisting:
		if req.Path == "" {
			return nil, NewValidation("invalid request", []FieldError{{Field: "path", Reason: "required for kind=existing"}})
		}
		abs, err := filepath.Abs(req.Path)
		if err != nil {
			return nil, NewValidation("invalid path", []FieldError{{Field: "path", Reason: err.Error()}})
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, NewValidation("path not found", []FieldError{{Field: "path", Reason: fmt.Sprintf("directory does not exist: %s", abs)}})
		}
		if !st.IsDir() {
			return nil, NewValidation("path not a directory", []FieldError{{Field: "path", Reason: abs}})
		}
		if err := checkWritable(abs); err != nil {
			return nil, NewValidation("path not writable", []FieldError{{Field: "path", Reason: err.Error()}})
		}
		path = abs
	case models.ProjectNew:
		if req.ParentPath == "" {
			return nil, NewValidation("invalid request", []FieldError{{Field: "parent_path", Reason: "required for kind=new"}})
		}
		absParent, err := filepath.Abs(req.ParentPath)
		if err != nil {
			return nil, NewValidation("invalid parent_path", []FieldError{{Field: "parent_path", Reason: err.Error()}})
		}
		st, err := os.Stat(absParent)
		if err != nil {
			return nil, NewValidation("parent_path not found", []FieldError{{Field: "parent_path", Reason: fmt.Sprintf("parent directory does not exist: %s", absParent)}})
		}
		if !st.IsDir() {
			return nil, NewValidation("parent_path not a directory", []FieldError{{Field: "parent_path", Reason: absParent}})
		}
		target := filepath.Join(absParent, req.Name)
		if _, err := os.Stat(target); err == nil {
			return nil, NewConflict(fmt.Sprintf("directory already exists: %s", target))
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, fmt.Errorf("create project dir: %w", err)
		}
		path = target
	}

	// unique по path + name (до insert)
	if existing, err := s.projects.GetByPath(ctx, s.db, path); err == nil && existing != nil {
		return nil, NewConflict(fmt.Sprintf("project with path already exists: %s", path))
	}
	if existing, err := s.projects.GetByName(ctx, s.db, req.Name); err == nil && existing != nil {
		return nil, NewConflict(fmt.Sprintf("project with name already exists: %s", req.Name))
	}

	p := &models.Project{
		Name:        req.Name,
		Path:        path,
		Kind:        models.ProjectKind(req.Kind),
		Description: req.Description,
	}
	if models.ProjectKind(req.Kind) == models.ProjectExisting {
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			p.Git = true
		}
	} else {
		if s.hasGit() {
			if err := exec.CommandContext(ctx, "git", "-C", path, "init", "-q").Run(); err == nil {
				p.Git = true
			} else {
				warning = "git init failed; project created without git"
			}
		} else {
			warning = "git not available; project created without git"
		}
		// README-заготовка, если каталог пуст
		writeReadmeStub(path, req.Name)
	}

	if err := s.projects.Create(ctx, s.db, p); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict("project name or path already exists")
		}
		return nil, err
	}
	return &CreateProjectResult{Project: p, Warning: warning}, nil
}

// ListProjects — список (по умолчанию только активные).
func (s *ProjectService) ListProjects(ctx context.Context, f repository.ProjectFilter, limit, offset int) ([]*models.Project, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return s.projects.List(ctx, s.db, f, limit, offset)
}

// GetProject — project + счётчики.
func (s *ProjectService) GetProject(ctx context.Context, id int64) (*ProjectSummary, error) {
	p, err := s.projects.GetByID(ctx, s.db, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, notFound("project")
		}
		return nil, err
	}
	teams, _ := s.projects.CountTeams(ctx, s.db, id)
	tasks, _ := s.projects.CountTasks(ctx, s.db, id)
	active, _ := s.projects.CountActiveSessions(ctx, s.db, id)
	return &ProjectSummary{
		Project: *p,
		Counts:  ProjectCounts{Teams: teams, Tasks: tasks, ActiveSessions: active},
	}, nil
}

// UpdateProject — name/description (path immutable → 409).
func (s *ProjectService) UpdateProject(ctx context.Context, id int64, name, description *string) (*models.Project, error) {
	p, err := s.projects.GetByID(ctx, s.db, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, notFound("project")
		}
		return nil, err
	}
	if p.Archived {
		return nil, NewConflict("project is archived")
	}
	if name == nil && description == nil {
		return p, nil
	}
	newName := p.Name
	if name != nil {
		if *name == "" {
			return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "must not be empty"}})
		}
		newName = *name
	}
	newDesc := p.Description
	if description != nil {
		newDesc = *description
	}
	if err := s.projects.UpdateMeta(ctx, s.db, id, newName, newDesc); err != nil {
		if err == repository.ErrNotFound {
			return nil, notFound("project")
		}
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict("project name already exists")
		}
		return nil, err
	}
	return s.projects.GetByID(ctx, s.db, id)
}

// ArchiveProject — DELETE /projects/{id} = архив (идемпотентно).
func (s *ProjectService) ArchiveProject(ctx context.Context, id int64) (*models.Project, error) {
	p, err := s.projects.GetByID(ctx, s.db, id)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, notFound("project")
		}
		return nil, err
	}
	if !p.Archived {
		if err := s.projects.Archive(ctx, s.db, id); err != nil {
			return nil, err
		}
	}
	return s.projects.GetByID(ctx, s.db, id)
}

// ResolveWorkingDir — P1.0 layered разрешение working_dir для сессии:
//
//	explicit working_dir > task.project > team.project > (explicit/empty).
//
// Возвращает (working_dir, resolved_project_id, error).
//   - 400 — если референсированный проект не существует;
//   - 409 — если проект archived.
//   - если проект нигде не задан → working_dir = explicit (или empty) — backward
//     compatibility: сессии/команды/задачи без project_id работают как раньше (нет 400).
func (s *ProjectService) ResolveWorkingDir(ctx context.Context, explicit string, explicitProjectID *int64,
	task *models.Task, team *models.Team) (string, *int64, error) {
	return resolveWorkingDir(ctx, s.db, s.projects, explicit, explicitProjectID, task, team)
}

// resolveWorkingDir — P1.0 layered разрешение working_dir (общий для Project/Session).
//
//	expllicit working_dir > task.project > team.project > (explicit/empty).
//
// Возвращает (working_dir, resolved_project_id, error).
//   - 400 — референсированный проект не существует;
//   - 409 — проект archived;
//   - если проект нигде не задан → working_dir = explicit (или empty) — backward
//     compatibility (нет 400).
func resolveWorkingDir(ctx context.Context, db *sql.DB, projects *repository.ProjectRepo,
	explicit string, explicitProjectID *int64, task *models.Task, team *models.Team) (string, *int64, error) {

	var pid *int64
	switch {
	case explicitProjectID != nil:
		pid = explicitProjectID
	case task != nil && task.ProjectID != nil:
		pid = task.ProjectID
	case team != nil && team.ProjectID != nil:
		pid = team.ProjectID
	}

	var projectPath string
	var resolved *int64
	if pid != nil && projects != nil {
		p, err := projects.GetByID(ctx, db, *pid)
		if err != nil {
			if err == repository.ErrNotFound {
				return "", nil, NewValidation("project not found", []FieldError{{Field: "project_id", Reason: "project does not exist"}})
			}
			return "", nil, err
		}
		if p.Archived {
			return "", nil, NewConflict(fmt.Sprintf("project %d (%s) is archived and cannot be used", p.ID, p.Name))
		}
		projectPath = p.Path
		resolved = &p.ID
	}

	wd := explicit
	if wd == "" {
		wd = projectPath
	}
	return wd, resolved, nil
}

// checkWritable — проверка, что в каталог можно писать.
func checkWritable(dir string) error {
	tmp, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(name)
	return nil
}

// writeReadmeStub — README-заготовка, если каталог пуст.
func writeReadmeStub(dir, name string) {
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); err == nil {
		return // уже есть
	}
	_ = os.WriteFile(readme, []byte("# "+name+"\n\nПроект создан daemon (P1.0 Projects).\n"), 0o644)
}
