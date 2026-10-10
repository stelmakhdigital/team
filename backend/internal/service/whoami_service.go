package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// WhoamiService — P2.15: «кто я и что мне делать» (компактный срез для агента).
// Identity: API-ключ определяет кто запрашивает (auth); query идентифицирует
// какого агента описать (session_id предпочтительно, либо role_id+team_id).
type WhoamiService struct {
	db     *sql.DB
	stores *repository.Stores
	teams  *TeamService
	tsvc   *TaskService
	ssvc   *SessionService
	msvc   *MessageService
}

// whoamiListLimit — bounded-срез списков (задокументировано): кто-у-кого —
// оператору/агенту нужна первая горсть, не всё.
const whoamiListLimit = 20

func NewWhoamiService(db *sql.DB, stores *repository.Stores, teams *TeamService, tsvc *TaskService, ssvc *SessionService, msvc *MessageService) *WhoamiService {
	return &WhoamiService{db: db, stores: stores, teams: teams, tsvc: tsvc, ssvc: ssvc, msvc: msvc}
}

// WhoamiRequest — запрос whoami (query-параметры).
type WhoamiRequest struct {
	SessionID *int64
	RoleID    *int64
	TeamID    *int64
}

type WhoamiAgent struct {
	RoleID   int64  `json:"role_id"`
	RoleName string `json:"role_name"`
	TeamID   int64  `json:"team_id"`
	TeamName string `json:"team_name"`
	Address  string `json:"address,omitempty"`
}

type WhoamiSession struct {
	ID             int64    `json:"id"`
	State          string   `json:"state"`
	Model          string   `json:"model,omitempty"`
	ContextUsedPct *float64 `json:"context_used_pct,omitempty"`
}

type WhoamiTaskPending struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Priority  int    `json:"priority"`
	ProjectID *int64 `json:"project_id,omitempty"`
}

type WhoamiTaskInProgress struct {
	ID                int64   `json:"id"`
	Title             string  `json:"title"`
	ClosureRequiredAt *string `json:"closure_required_at,omitempty"`
	IsOverdue         bool    `json:"is_overdue"`
}

type WhoamiWake struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}

type WhoamiTaskBlocked struct {
	ID    int64       `json:"id"`
	Title string      `json:"title"`
	Wake  *WhoamiWake `json:"wake,omitempty"`
}

type WhoamiMyTasks struct {
	Pending    []WhoamiTaskPending    `json:"pending"`
	InProgress []WhoamiTaskInProgress `json:"in_progress"`
	Blocked    []WhoamiTaskBlocked    `json:"blocked"`
}

type WhoamiWaiting struct {
	ID           int64   `json:"id"`
	FromRoleName *string `json:"from_role_name,omitempty"`
	Type         string  `json:"type"`
	Summary      string  `json:"summary"`
}

type WhoamiRelative struct {
	RoleName string `json:"role_name"`
	Via      string `json:"via"`
}

type WhoamiResponse struct {
	Agent        WhoamiAgent      `json:"agent"`
	Session      *WhoamiSession   `json:"session,omitempty"`
	MyTasks      WhoamiMyTasks    `json:"my_tasks"`
	WaitingForMe []WhoamiWaiting  `json:"waiting_for_me"`
	Upstream     []WhoamiRelative `json:"upstream"`
	Downstream   []WhoamiRelative `json:"downstream"`
}

// Whoami — собирает компактный срез агента (bounded).
// 400 (нет параметров / role-team mismatch), 404 (нет сессии/роли/команды).
func (s *WhoamiService) Whoami(ctx context.Context, req WhoamiRequest) (*WhoamiResponse, error) {
	var (
		roleID   int64
		teamID   int64
		sessView *SessionView
	)

	switch {
	case req.SessionID != nil:
		// предпочтительно: агент знает свой session id → role+team+статус сессии
		v, err := s.ssvc.GetSessionView(ctx, *req.SessionID)
		if err != nil {
			return nil, err // 404 (нет сессии)
		}
		roleID, teamID, sessView = v.RoleID, v.TeamID, v
		if req.TeamID != nil && *req.TeamID != teamID {
			return nil, NewValidation(fmt.Sprintf("session %d belongs to team %d, not %d", *req.SessionID, teamID, *req.TeamID))
		}
	case req.RoleID != nil:
		role, err := s.stores.Roles.GetByID(ctx, s.db, *req.RoleID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, NewNotFound("role")
			}
			return nil, err
		}
		roleID, teamID = role.ID, role.TeamID
		if req.TeamID != nil && *req.TeamID != teamID {
			return nil, NewValidation(fmt.Sprintf("role %d belongs to team %d, not %d", *req.RoleID, teamID, *req.TeamID))
		}
	default:
		return nil, NewValidation("укажите session_id или role_id")
	}

	// team + роли/связи (upstream/downstream, address)
	detail, err := s.teams.GetTeam(ctx, teamID)
	if err != nil {
		return nil, err // 404 (нет команды)
	}
	role, err := s.stores.Roles.GetByID(ctx, s.db, roleID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, NewNotFound("role")
		}
		return nil, err
	}

	resp := &WhoamiResponse{
		Agent: WhoamiAgent{
			RoleID: role.ID, RoleName: role.Name,
			TeamID: detail.Team.ID, TeamName: detail.Team.Name,
			Address: role.Address,
		},
		MyTasks:      WhoamiMyTasks{Pending: []WhoamiTaskPending{}, InProgress: []WhoamiTaskInProgress{}, Blocked: []WhoamiTaskBlocked{}},
		WaitingForMe: []WhoamiWaiting{},
		Upstream:     []WhoamiRelative{},
		Downstream:   []WhoamiRelative{},
	}

	if sessView != nil {
		resp.Session = &WhoamiSession{
			ID: sessView.ID, State: sessView.State, Model: sessView.Model,
			ContextUsedPct: sessView.ContextUsedPercentage,
		}
	}

	// my_tasks: destination_role_id = role, по состоянию (bounded)
	if err := s.fillMyTasks(ctx, resp, roleID); err != nil {
		return nil, err
	}
	// waiting_for_me: unread входящие к роли (bounded)
	if err := s.fillWaiting(ctx, resp, roleID); err != nil {
		return nil, err
	}
	// upstream/downstream: relatives in/out (bounded)
	resp.Upstream, resp.Downstream = fillRelatives(detail.Relatives, roleID)

	return resp, nil
}

func (s *WhoamiService) fillMyTasks(ctx context.Context, resp *WhoamiResponse, roleID int64) error {
	rid := roleID
	f := func(states ...models.TaskState) repository.TaskFilter {
		return repository.TaskFilter{
			DestinationRole: &rid,
			States:          states,
			Limit:           whoamiListLimit,
		}
	}

	// pending
	if views, _, err := s.tsvc.ListTasks(ctx, f(models.TaskPending)); err != nil {
		return err
	} else {
		for _, v := range views {
			resp.MyTasks.Pending = append(resp.MyTasks.Pending, WhoamiTaskPending{
				ID: v.ID, Title: v.Title, Priority: v.Priority, ProjectID: v.ProjectID,
			})
		}
	}
	// in_progress
	if views, _, err := s.tsvc.ListTasks(ctx, f(models.TaskInProgress)); err != nil {
		return err
	} else {
		for _, v := range views {
			resp.MyTasks.InProgress = append(resp.MyTasks.InProgress, WhoamiTaskInProgress{
				ID: v.ID, Title: v.Title,
				ClosureRequiredAt: v.ClosureRequiredAt,
				IsOverdue:         v.IsOverdue,
			})
		}
	}
	// blocked (+ wake)
	if views, _, err := s.tsvc.ListTasks(ctx, f(models.TaskBlocked)); err != nil {
		return err
	} else {
		for _, v := range views {
			b := WhoamiTaskBlocked{ID: v.ID, Title: v.Title}
			if w, werr := s.stores.Wakes.GetActiveByTask(ctx, s.db, v.ID); werr == nil && w != nil {
				b.Wake = &WhoamiWake{Kind: w.Kind, Ref: w.Ref}
			}
			resp.MyTasks.Blocked = append(resp.MyTasks.Blocked, b)
		}
	}
	return nil
}

func (s *WhoamiService) fillWaiting(ctx context.Context, resp *WhoamiResponse, roleID int64) error {
	rid := roleID
	views, _, err := s.msvc.ListMessages(ctx, repository.MessageFilter{
		ToRoleID:   &rid,
		UnreadOnly: true,
		Limit:      whoamiListLimit,
	})
	if err != nil {
		return err
	}
	for _, v := range views {
		resp.WaitingForMe = append(resp.WaitingForMe, WhoamiWaiting{
			ID: v.ID, FromRoleName: v.FromRoleName, Type: v.Type,
			Summary: summarizeBody(v.Body),
		})
	}
	return nil
}

// fillRelatives — relatives in (upstream: кто мне передаёт) / out (downstream).
func fillRelatives(rels []*RelativeWithNames, roleID int64) (up, down []WhoamiRelative) {
	up, down = []WhoamiRelative{}, []WhoamiRelative{}
	for _, r := range rels {
		via := string(r.Type)
		if via == "" {
			via = "relative"
		}
		if r.ToRoleID == roleID {
			if len(up) < whoamiListLimit {
				up = append(up, WhoamiRelative{RoleName: r.FromRoleName, Via: via})
			}
		}
		if r.FromRoleID == roleID {
			if len(down) < whoamiListLimit {
				down = append(down, WhoamiRelative{RoleName: r.ToRoleName, Via: via})
			}
		}
	}
	return up, down
}

// summarizeBody — короткая строка-сводка сообщения (≤120 символов, 1 строка).
func summarizeBody(body string) string {
	b := strings.ReplaceAll(strings.ReplaceAll(body, "\r", " "), "\n", " ")
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	if len(b) > 120 {
		return b[:117] + "…"
	}
	return b
}
