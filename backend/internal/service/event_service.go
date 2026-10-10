package service

import (
	"context"
	"database/sql"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ---------- P2.9: EventService (GET /events + sink) ----------
//
// EventService: (1) подключает sink к EventBus (каждое событие → events-таблица),
// (2) отдаёт ленту GET /events (фильтры + limit/offset + enrichment имён).

type EventService struct {
	repo *repository.EventRepo
	db   repository.DBTX
}

func NewEventService(db *sql.DB, stores *repository.Stores) *EventService {
	return &EventService{repo: stores.Events, db: db}
}

// SetSink — подключить sink к EventBus: каждое событие (кроме session.output)
// синхронно пишется в events (bounded). Additive — WS-семантика не меняется.
func (s *EventService) SetSink(bus *EventBus) {
	if s == nil || bus == nil || s.repo == nil {
		return
	}
	ctx := context.Background()
	bus.SetSink(func(e WSEvent) {
		ev, ok := parseEvent(e)
		if !ok {
			return
		}
		// best-effort: ошибка записи не ломает publish (событие уже в WS ушло)
		_ = s.repo.Record(ctx, s.db, ev)
	})
}

// EventFilter — фильтры GET /events (0/"" = не задан).
type EventFilter struct {
	Type       string
	EntityKind string
	TeamID     int64
	RoleID     int64
	EntityID   int64
	Limit      int
	Offset     int
}

// EventPage — ответ GET /events.
type EventPage struct {
	Events  []models.EnrichedEvent `json:"events"`
	Total   int                    `json:"total"`
	HasMore bool                   `json:"has_more"`
}

// ListEvents — лента событий (created_at desc) + enrichment + total/has_more.
func (s *EventService) ListEvents(ctx context.Context, f EventFilter) (*EventPage, error) {
	rf := repository.EventListFilter{Limit: f.Limit, Offset: f.Offset}
	if f.Type != "" {
		v := f.Type
		rf.Type = &v
	}
	if f.EntityKind != "" {
		v := f.EntityKind
		rf.EntityKind = &v
	}
	if f.TeamID != 0 {
		v := f.TeamID
		rf.TeamID = &v
	}
	if f.RoleID != 0 {
		v := f.RoleID
		rf.RoleID = &v
	}
	if f.EntityID != 0 {
		v := f.EntityID
		rf.EntityID = &v
	}
	events, total, err := s.repo.List(ctx, s.db, rf)
	if err != nil {
		return nil, err
	}
	enriched := s.enrich(ctx, events)
	fetched := f.Offset + len(enriched)
	if fetched < 0 {
		fetched = f.Offset
	}
	return &EventPage{
		Events:  enriched,
		Total:   total,
		HasMore: fetched < total,
	}, nil
}

// enrich — bounded-подборка имён (team/role/entity_title): 1 запрос на тип.
func (s *EventService) enrich(ctx context.Context, events []*repository.EventOut) []models.EnrichedEvent {
	var teamIDs, roleIDs, taskIDs []int64
	for _, e := range events {
		if e.TeamID != nil {
			teamIDs = append(teamIDs, *e.TeamID)
		}
		if e.RoleID != nil {
			roleIDs = append(roleIDs, *e.RoleID)
		}
		if e.EntityKind == "task" && e.EntityID != 0 {
			taskIDs = append(taskIDs, e.EntityID)
		}
	}
	teamNames, _ := s.repo.TeamNames(ctx, s.db, distinctInts(teamIDs))
	roleNames, _ := s.repo.RoleNames(ctx, s.db, distinctInts(roleIDs))
	taskTitles, _ := s.repo.TaskTitles(ctx, s.db, distinctInts(taskIDs))

	out := make([]models.EnrichedEvent, 0, len(events))
	for _, e := range events {
		ee := models.EnrichedEvent{
			ID:         e.ID,
			Type:       e.Type,
			TeamID:     e.TeamID,
			RoleID:     e.RoleID,
			EntityKind: e.EntityKind,
			EntityID:   e.EntityID,
			Summary:    e.Summary,
			CreatedAt:  e.CreatedAtStr,
		}
		if e.TeamID != nil {
			if n, ok := teamNames[*e.TeamID]; ok {
				ee.TeamName = &n
			}
		}
		if e.RoleID != nil {
			if n, ok := roleNames[*e.RoleID]; ok {
				ee.RoleName = &n
			}
		}
		if e.EntityKind == "task" {
			if t, ok := taskTitles[e.EntityID]; ok {
				ee.EntityTitle = &t
			}
		}
		out = append(out, ee)
	}
	return out
}

// --- helpers ---

func distinctInts(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
