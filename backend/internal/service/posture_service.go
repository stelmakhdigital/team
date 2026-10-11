package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ---------- P3.16: PostureService (scope resolution + Check + Decide) ----------
//
// Posture (delegated/human_led) на уровне команды (база) + сессии (override).
// Effective posture = session.posture если задано, иначе team.posture; без
// team/session → daemon default (config posture.default). human_led +
// sensitive-операция → park (human_request pending) → approve/deny → replay.

// requestScope — scope запроса (из path).
type requestScope struct {
	kind string // "session" | "team" | "task" | "global"
	id   int64
}

func parseScope(path string) requestScope {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "v1" {
		return requestScope{kind: "global"}
	}
	res := parts[2]
	if len(parts) > 3 && parts[3] != "" {
		if id, err := strconv.ParseInt(parts[3], 10, 64); err == nil {
			switch res {
			case "sessions":
				return requestScope{kind: "session", id: id}
			case "teams":
				return requestScope{kind: "team", id: id}
			case "tasks":
				return requestScope{kind: "task", id: id}
			}
		}
	}
	return requestScope{kind: "global"}
}

// scopeInfo — resolved effective posture + scope (для human_request).
type scopeInfo struct {
	posture   models.Posture
	scopeType string
	scopeID   *int64
	teamID    *int64
}

// PostureService — scope resolution + Check (park) + Decide (approve/deny).
type PostureService struct {
	db       *sql.DB
	hr       *repository.HumanRequestRepo
	teams    *repository.TeamRepo
	sessions *repository.SessionRepo
	tasks    *repository.TaskRepo
	config   *ConfigService
	audit    *AuditService
	bus      *EventBus // P3.16: human_request.parked / human_request.decided (live Attention)
}

// NewPostureService — конструктор.
func NewPostureService(db *sql.DB, stores *repository.Stores, config *ConfigService, audit *AuditService) *PostureService {
	return &PostureService{db: db, hr: stores.HumanRequests, teams: stores.Teams,
		sessions: stores.Sessions, tasks: stores.Tasks, config: config, audit: audit}
}

// SetBus — EventBus (Pulse: human_request.parked / human_request.decided), nil-safe.
func (s *PostureService) SetBus(b *EventBus) { s.bus = b }

// resolve — effective posture + scope из request path.
func (s *PostureService) resolve(ctx context.Context, sc requestScope) (scopeInfo, error) {
	switch sc.kind {
	case "session":
		sess, err := s.sessions.GetByID(ctx, s.db, sc.id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return scopeInfo{posture: models.PostureDelegated, scopeType: "global"}, nil
			}
			return scopeInfo{}, err
		}
		posture := models.PostureDelegated
		if sess.Posture != nil && sess.Posture.Valid() {
			posture = *sess.Posture
		} else if team, terr := s.teams.GetByID(ctx, s.db, sess.TeamID); terr == nil && team.Posture.Valid() {
			posture = team.Posture
		}
		sid, tid := sc.id, sess.TeamID
		return scopeInfo{posture: posture, scopeType: "session", scopeID: &sid, teamID: &tid}, nil
	case "team":
		team, err := s.teams.GetByID(ctx, s.db, sc.id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return scopeInfo{posture: models.PostureDelegated, scopeType: "global"}, nil
			}
			return scopeInfo{}, err
		}
		tid := sc.id
		p := team.Posture
		if !p.Valid() {
			p = models.PostureDelegated
		}
		return scopeInfo{posture: p, scopeType: "team", scopeID: &tid, teamID: &tid}, nil
	case "task":
		task, err := s.tasks.GetByID(ctx, s.db, sc.id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return scopeInfo{posture: models.PostureDelegated, scopeType: "global"}, nil
			}
			return scopeInfo{}, err
		}
		posture := models.PostureDelegated
		if team, terr := s.teams.GetByID(ctx, s.db, task.TeamID); terr == nil && team.Posture.Valid() {
			posture = team.Posture
		}
		tid := task.TeamID
		return scopeInfo{posture: posture, scopeType: "team", scopeID: &tid, teamID: &tid}, nil
	default:
		p, _ := s.defaultPosture(ctx)
		return scopeInfo{posture: p, scopeType: "global"}, nil
	}
}

// defaultPosture — config posture.default (default delegated).
func (s *PostureService) defaultPosture(ctx context.Context) (models.Posture, error) {
	if s.config == nil {
		return models.PostureDelegated, nil
	}
	v, err := s.config.ResolveString(ctx, "posture.default")
	if err != nil {
		return models.PostureDelegated, nil
	}
	p := models.Posture(v)
	if !p.Valid() {
		return models.PostureDelegated, nil
	}
	return p, nil
}

// EffectivePosture — effective posture для request path (badge, публичное).
func (s *PostureService) EffectivePosture(ctx context.Context, path string) models.Posture {
	info, err := s.resolve(ctx, parseScope(path))
	if err != nil {
		return models.PostureDelegated
	}
	return info.posture
}

// Check — паркуем ли запрос (interceptor). park=true → операция НЕ выполняется.
// method/path/body/apiKey — из запроса (body уже прочитан middleware'ом, bounded).
func (s *PostureService) Check(ctx context.Context, method, path, body, apiKey string) (park bool, reqID int64, operation string, err error) {
	operation, reversible, ok := MatchSensitive(method, path)
	if !ok {
		return false, 0, "", nil // не sensitive → не паркуем
	}
	info, err := s.resolve(ctx, parseScope(path))
	if err != nil {
		return false, 0, "", err
	}
	if info.posture != models.PostureHumanLed {
		return false, 0, "", nil // delegated → не паркуем
	}
	// human_led + sensitive → park. Дедуп: если уже есть pending на ту же операцию → вернуть.
	if existing, derr := s.hr.ListPendingByOperation(ctx, s.db, method, path); derr == nil && len(existing) > 0 {
		return true, existing[0].ID, operation, nil
	}
	var bodyPtr, apiKeyPtr *string
	if body != "" {
		bodyPtr = &body
	}
	if apiKey != "" {
		apiKeyPtr = &apiKey
	}
	hr := &models.HumanRequest{
		ScopeType: info.scopeType, ScopeID: info.scopeID, TeamID: info.teamID,
		Method: method, Path: path, Body: bodyPtr, AuthKey: apiKeyPtr,
		Operation: operation, Reversible: reversible,
	}
	if err := s.hr.Create(ctx, s.db, hr); err != nil {
		return false, 0, "", err
	}
	s.publishParked(hr)
	return true, hr.ID, operation, nil
}

// Decide — approve/deny pending-запрос. Только из pending (иначе 409). 404.
func (s *PostureService) Decide(ctx context.Context, id int64, decision, reason, decidedBy string) (*models.HumanRequest, error) {
	hr, err := s.hr.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("human_request")
		}
		return nil, err
	}
	if hr.Status != models.HRPending {
		return nil, NewConflict(fmt.Sprintf("human_request is %s, cannot decide", hr.Status))
	}
	var status models.HumanRequestStatus
	switch decision {
	case "approve":
		status = models.HRApproved
	case "deny":
		status = models.HRDenied
	default:
		return nil, NewValidation("decision must be 'approve' or 'deny'")
	}
	now := time.Now().UTC()
	reasonPtr := strOrNil(reason)
	by := decidedBy
	if err := s.hr.SetDecision(ctx, s.db, id, status, reasonPtr, &by, now); err != nil {
		return nil, err
	}
	// audit: human_request.decided (атрибуция обязательна).
	res := "human_request"
	_ = s.audit.Record(ctx, models.AuditEntry{
		Action:   "human_request.decided",
		Resource: &res,
		UserName: &by,
		Details:  map[string]any{"decision": decision, "operation": hr.Operation, "reason": reason},
	})
	hr.Status = status
	hr.DecidedBy = &by
	hr.DecidedAt = &now
	hr.DecisionReason = reasonPtr
	s.publishDecided(hr, decision)
	return hr, nil
}

// Get / List — для UI (деталка + Attention).
func (s *PostureService) Get(ctx context.Context, id int64) (*models.HumanRequest, error) {
	hr, err := s.hr.GetByID(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, notFound("human_request")
		}
		return nil, err
	}
	return hr, nil
}

func (s *PostureService) ListPending(ctx context.Context) ([]*models.HumanRequest, error) {
	return s.hr.ListPending(ctx, s.db, 100)
}

// SetTeamPosture — PATCH /teams/{id}/posture.
func (s *PostureService) SetTeamPosture(ctx context.Context, teamID int64, posture models.Posture) error {
	if !posture.Valid() {
		return NewValidation("posture must be 'delegated' or 'human_led'")
	}
	if _, err := s.teams.GetByID(ctx, s.db, teamID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return notFound("team")
		}
		return err
	}
	return s.teams.SetPosture(ctx, s.db, teamID, posture)
}

// SetSessionPosture — PATCH /sessions/{id}/posture (NULL = наследует от команды).
func (s *PostureService) SetSessionPosture(ctx context.Context, sessionID int64, posture *models.Posture) error {
	if posture != nil && !posture.Valid() {
		return NewValidation("posture must be 'delegated' or 'human_led'")
	}
	if _, err := s.sessions.GetByID(ctx, s.db, sessionID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return notFound("session")
		}
		return err
	}
	return s.sessions.SetPosture(ctx, s.db, sessionID, posture)
}

// MarkReplayed — one-time bypass (approved → replayed). true = удалось.
func (s *PostureService) MarkReplayed(ctx context.Context, id int64) (bool, error) {
	return s.hr.MarkReplayed(ctx, s.db, id)
}

// SetReplayStatus — после replay: HTTP-статус replay.
func (s *PostureService) SetReplayStatus(ctx context.Context, id int64, status int) error {
	return s.hr.SetReplayStatus(ctx, s.db, id, status)
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// publishParked — P3.16: human_request.parked (live Attention).
func (s *PostureService) publishParked(hr *models.HumanRequest) {
	if s.bus == nil {
		return
	}
	data := map[string]any{"human_request_id": hr.ID, "operation": hr.Operation, "scope_type": hr.ScopeType}
	chans := []string{}
	if hr.TeamID != nil {
		chans = append(chans, fmt.Sprintf("team:%d", *hr.TeamID))
	}
	s.bus.Publish(newEvent("human_request.parked", data, chans...))
}

// publishDecided — P3.16: human_request.decided (live Attention).
func (s *PostureService) publishDecided(hr *models.HumanRequest, decision string) {
	if s.bus == nil {
		return
	}
	data := map[string]any{"human_request_id": hr.ID, "operation": hr.Operation, "decision": decision}
	chans := []string{}
	if hr.TeamID != nil {
		chans = append(chans, fmt.Sprintf("team:%d", *hr.TeamID))
	}
	s.bus.Publish(newEvent("human_request.decided", data, chans...))
}
