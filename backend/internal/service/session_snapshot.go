package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// P3.12 — Agent images / session snapshots.
// Снимок продуктивного состояния сессии (конфиг агента + накопленное резюме)
// как Library-элемент type=agent_image; launch создаёт сессию из снимка.

// agentImageSpec — структура spec (JSON в library_items.spec).
//
//	{ "source_session_id": 12, "role_name": "go-dev", "runtime_type": "pi",
//	  "agent": { "name","model","thinking","system_prompt","skills","tools" },
//	  "summary": "...", "working_dir": "/abs" }
type agentImageSpec struct {
	SourceSessionID int64          `json:"source_session_id"`
	RoleName        string         `json:"role_name"`
	RuntimeType     string         `json:"runtime_type"`
	Command         string         `json:"command,omitempty"`
	Agent           map[string]any `json:"agent"`
	Summary         string         `json:"summary"`
	WorkingDir      string         `json:"working_dir,omitempty"`
}

// SnapshotRequest — POST /sessions/{id}/snapshot.
type SnapshotRequest struct {
	Name        string  `json:"name"`
	Summary     *string `json:"summary"`
	Description *string `json:"description"`
}

// SnapshotSession — захват продуктивного состояния сессии как agent_image.
func (s *SessionService) SnapshotSession(ctx context.Context, sessionID int64, req SnapshotRequest) (*models.LibraryItem, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, NewValidation("invalid request", []FieldError{{Field: "name", Reason: "name is required"}})
	}
	if s.library == nil {
		return nil, NewNotFound("library")
	}
	sess, err := s.sessions.GetByID(ctx, s.db, sessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NewNotFound("session")
		}
		return nil, err
	}
	role, err := s.roles.GetByID(ctx, s.db, sess.RoleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			role = &models.Role{Name: ""}
		} else {
			return nil, err
		}
	}
	agent := s.sessionAgentFields(sess, role)

	summary := ""
	if req.Summary != nil {
		summary = strings.TrimSpace(*req.Summary)
	}
	if summary == "" {
		// авто-резюме из transcript-tail (best-effort; см. api-decisions §P3.12).
		summary = s.autoSummaryFromTranscript(sess)
	}

	spec := map[string]any{
		"source_session_id": sess.ID,
		"role_name":         role.Name,
		"runtime_type":      sess.RuntimeType,
		"agent":             agent,
		"summary":           summary,
	}
	if sess.Command != "" {
		spec["command"] = sess.Command
	}
	if sess.WorkingDir != "" {
		spec["working_dir"] = sess.WorkingDir
	}
	srcID := sess.ID
	srcType := "session"
	it := &models.LibraryItem{
		Type:        models.LibAgentImage,
		Name:        name,
		Description: req.Description,
		Group:       "agent-images",
		Version:     "1.0.0",
		IsPublic:    false,
		SourceType:  &srcType,
		SourceID:    &srcID,
		Spec:        spec,
	}
	if err := s.library.Create(ctx, s.db, it); err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, NewConflict(fmt.Sprintf("agent image %q already exists", name))
		}
		return nil, err
	}
	// версия (append-only)
	if s.libraryVersions != nil {
		_ = s.libraryVersions.Create(ctx, s.db, &models.LibraryVersion{
			ItemID: it.ID, Version: "1.0.0",
		})
	}
	return it, nil
}

// sessionAgentFields — (model/system_prompt/skills/tools/thinking) из сессии.
func (s *SessionService) sessionAgentFields(sess *models.Session, role *models.Role) map[string]any {
	cfg := map[string]any{}
	if sess.Config != nil {
		for k, v := range sess.Config {
			cfg[k] = v
		}
	}
	agent := map[string]any{"name": ""}
	if role != nil {
		agent["name"] = role.Name
	}
	// model: из config, иначе из pi-конфига сессии.
	if m, ok := cfg["model"].(string); ok && m != "" {
		agent["model"] = m
	} else if m := s.sessionModel(sess); m != "" {
		agent["model"] = m
	}
	if p, ok := cfg["system_prompt"].(string); ok && p != "" {
		agent["system_prompt"] = p
	}
	if t, ok := cfg["thinking"].(string); ok && t != "" {
		agent["thinking"] = t
	}
	if sk := stringSliceFromAny(cfg["skills"]); len(sk) > 0 {
		agent["skills"] = sk
	}
	if tl := stringSliceFromAny(cfg["tools"]); len(tl) > 0 {
		agent["tools"] = tl
	}
	return agent
}

// autoSummaryFromTranscript — best-effort: последние не-пустые «текстовые» строки
// transcript-лога (bounded ~300 символов). Если лог пуст/JSONL без текста — "".
func (s *SessionService) autoSummaryFromTranscript(sess *models.Session) string {
	path := s.sessionLogPath(sess.ID)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > 64<<10 {
		data = data[len(data)-64<<10:]
	}
	var texts []string
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "{") {
			continue
		}
		texts = append(texts, t)
		if len(texts) >= 3 {
			break
		}
	}
	if len(texts) == 0 {
		return ""
	}
	out := strings.Join(texts, "\n")
	if len(out) > 300 {
		out = out[:300] + "…"
	}
	return out
}

// LaunchRequest — POST /library/{id}/launch.
type LaunchRequest struct {
	TeamID    int64  `json:"team_id"`
	ProjectID *int64 `json:"project_id"`
}

// LaunchFromSnapshot — создаёт сессию из agent_image (переиспользует CreateSession).
// config (model/system_prompt/skills/tools/thinking) + working_dir — из снимка;
// summary подмешивается в system_prompt как накопленный контекст.
func (s *SessionService) LaunchFromSnapshot(ctx context.Context, libraryID int64, req LaunchRequest) (*models.Session, error) {
	if req.TeamID == 0 {
		return nil, NewValidation("invalid request", []FieldError{{Field: "team_id", Reason: "team_id is required"}})
	}
	if s.library == nil {
		return nil, NewNotFound("library")
	}
	it, err := s.library.GetByID(ctx, s.db, libraryID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NewNotFound("library item")
		}
		return nil, err
	}
	if it.Type != models.LibAgentImage {
		return nil, &AppError{Code: "validation_failed", Status: 400,
			Message: fmt.Sprintf("launch is only supported for agent_image (got %s)", it.Type)}
	}
	spec, err := parseAgentImageSpec(it.Spec)
	if err != nil {
		return nil, NewValidation("agent image spec is invalid")
	}
	// роль в целевой команде по имени из снимка
	roles, err := s.roles.ListByTeam(ctx, s.db, req.TeamID)
	if err != nil {
		return nil, err
	}
	var roleID int64
	for _, r := range roles {
		if r.Name == spec.RoleName {
			roleID = r.ID
			break
		}
	}
	if roleID == 0 {
		return nil, NewNotFound(fmt.Sprintf("role %q in team", spec.RoleName))
	}

	config := map[string]any{}
	for k, v := range spec.Agent {
		if k == "name" {
			continue
		}
		config[k] = v
	}
	// summary → system_prompt (накопленный контекст; решение: в system_prompt, см. §P3.12)
	if strings.TrimSpace(spec.Summary) != "" {
		ctxBlock := "## Session context (from agent image " + it.Name + ")\n" + strings.TrimSpace(spec.Summary)
		if sp, ok := config["system_prompt"].(string); ok && strings.TrimSpace(sp) != "" {
			config["system_prompt"] = ctxBlock + "\n\n" + sp
		} else {
			config["system_prompt"] = ctxBlock
		}
	}
	rtType := spec.RuntimeType
	creq := CreateSessionRequest{
		RoleID:      roleID,
		RuntimeType: rtType,
		Config:      config,
		ProjectID:   req.ProjectID,
	}
	if spec.Command != "" {
		creq.Command = spec.Command
	}
	if spec.WorkingDir != "" {
		creq.WorkingDir = spec.WorkingDir
	}
	sess, err := s.CreateSession(ctx, req.TeamID, creq)
	if err != nil {
		return nil, err
	}
	// провенанс: downloads_count +1 (снимок «использован»)
	_ = s.library.IncrementDownloads(ctx, s.db, it.ID)
	return sess, nil
}

func parseAgentImageSpec(spec map[string]any) (*agentImageSpec, error) {
	out := &agentImageSpec{Agent: map[string]any{}}
	if v, ok := spec["source_session_id"].(float64); ok {
		out.SourceSessionID = int64(v)
	}
	if v, ok := spec["role_name"].(string); ok {
		out.RoleName = v
	}
	if v, ok := spec["runtime_type"].(string); ok {
		out.RuntimeType = v
	}
	if v, ok := spec["command"].(string); ok {
		out.Command = v
	}
	if v, ok := spec["summary"].(string); ok {
		out.Summary = v
	}
	if v, ok := spec["working_dir"].(string); ok {
		out.WorkingDir = v
	}
	if a, ok := spec["agent"].(map[string]any); ok {
		out.Agent = a
	}
	return out, nil
}

// stringSliceFromAny — []string из any (JSON: []any / []string / string).
func stringSliceFromAny(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	}
	return nil
}
