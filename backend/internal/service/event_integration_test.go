package service_test

import (
	"strconv"
	"testing"

	"daemon/internal/service"
)

// ---------- P2.9: integration — каждый Publish → строка в events ----------
//
// Sink (EventBus → events) проверяем: (A) напрямую publish 4 типов (+
// session.output — должен быть ПРОПУЩЕН); (B) реальный сервис (CreateTask →
// task.created) через wire-ованную шину.

func TestEventSinkDirectPublish(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, _, _ := makeTeamWithRoles(t, e.svc, "ev-direct")

	bus := service.NewEventBus()
	evs := service.NewEventService(e.db, e.stores)
	evs.SetSink(bus)

	tid := teamID
	// 4 типа (реальные формы Data) + session.output (пропустить)
	bus.Publish(service.WSEvent{
		Type:     "task.state_changed",
		Data:     map[string]any{"task_id": 4, "from_state": "pending", "to_state": "in_progress"},
		Channels: []string{fTeam(tid), "task:4"},
	})
	bus.Publish(service.WSEvent{
		Type:     "session.started",
		Data:     map[string]any{"session_id": 5, "role_id": 2, "role_name": "role_a", "runtime_type": "pi"},
		Channels: []string{fTeam(tid), "session:5"},
	})
	bus.Publish(service.WSEvent{
		Type:     "message.sent",
		Data:     map[string]any{"message_id": 9, "team_id": tid, "from_role_name": "role_a", "to_role_name": "role_b", "type": "direct"},
		Channels: []string{fTeam(tid)},
	})
	bus.Publish(service.WSEvent{
		Type:     "alert.created",
		Data:     map[string]any{"alert_id": 3, "event_type": "stale", "severity": "critical", "description": "session stale"},
		Channels: []string{"watchdog:" + i64str(tid), fTeam(tid)},
	})
	// session.output — live-потоk, НЕ записываем
	bus.Publish(service.WSEvent{
		Type:     "session.output",
		Data:     map[string]any{"session_id": 5, "lines": []any{}},
		Channels: []string{fTeam(tid), "session:5"},
	})

	page, err := evs.ListEvents(e.ctx, service.EventFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 4 {
		t.Fatalf("events = %d, want 4 (session.output skipped)", len(page.Events))
	}
	seen := map[string]bool{}
	for _, ev := range page.Events {
		seen[ev.Type] = true
	}
	for _, wantType := range []string{"task.state_changed", "session.started", "message.sent", "alert.created"} {
		if !seen[wantType] {
			t.Errorf("missing event type %q (seen %v)", wantType, seen)
		}
	}
	// entity_kind + entity_id + team_name/summary enrichment
	for _, ev := range page.Events {
		if ev.TeamID == nil || *ev.TeamID != tid {
			t.Errorf("event %s team_id = %v, want %d", ev.Type, ev.TeamID, tid)
		}
		if ev.Summary == "" {
			t.Errorf("event %s has empty summary", ev.Type)
		}
	}
	// session.started: role_id + entity_kind=session + entity_id=5
	for _, ev := range page.Events {
		if ev.Type == "session.started" {
			if ev.EntityKind != "session" || ev.EntityID != 5 {
				t.Errorf("session.started entity = %s/%d, want session/5", ev.EntityKind, ev.EntityID)
			}
			if ev.RoleID == nil || *ev.RoleID != 2 {
				t.Errorf("session.started role_id = %v, want 2", ev.RoleID)
			}
		}
	}
}

func TestEventSinkRealService(t *testing.T) {
	e := setupSessionEnv(t)
	// реальный сервис → шина → sink → events
	bus := service.NewEventBus()
	evs := service.NewEventService(e.db, e.stores)
	evs.SetSink(bus)
	e.tsvc.Bus = bus

	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "ev-real")
	if _, err := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "ev real task",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	page, err := evs.ListEvents(e.ctx, service.EventFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 {
		t.Fatal("expected events from CreateTask, got none")
	}
	found := false
	for _, ev := range page.Events {
		if ev.Type == "task.created" && ev.EntityKind == "task" && ev.EntityID != 0 {
			found = true
			if ev.EntityTitle == nil {
				t.Error("task.created: entity_title (task title) не подбрано")
			}
		}
	}
	if !found {
		t.Errorf("no task.created event found (got %d events)", len(page.Events))
	}
}

// --- helpers ---

func fTeam(id int64) string { return "team:" + strconv.FormatInt(id, 10) }
func i64str(v int64) string { return strconv.FormatInt(v, 10) }
