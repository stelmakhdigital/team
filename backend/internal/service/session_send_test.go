package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"daemon/internal/models"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P2.11: Send to session + verify (service) ----------

// mockSendAdapter — управляемый адаптер (Capture+Send) для детерминированных
// service-тестов (без реального tmux). Capture ДО send → guard; ПОСЛЕ → verify.
type mockSendAdapter struct {
	name    string
	guard   []string
	verify  []string
	sendErr error
	sent    bool
}

func (m *mockSendAdapter) Name() string { return m.name }
func (m *mockSendAdapter) Start(context.Context, runtime.Spec) (string, error) {
	return "ref", nil
}
func (m *mockSendAdapter) Stop(context.Context, string) error { return nil }
func (m *mockSendAdapter) Status(context.Context, string) (*runtime.Status, error) {
	return &runtime.Status{Alive: true, State: runtime.StateRunning}, nil
}
func (m *mockSendAdapter) Capture(context.Context, string, int) ([]string, error) {
	if m.sent {
		return m.verify, nil
	}
	return m.guard, nil
}
func (m *mockSendAdapter) Send(ctx context.Context, ref, text string, submit bool) (runtime.SendResult, error) {
	if m.sendErr != nil {
		return runtime.SendResult{}, m.sendErr
	}
	m.sent = true
	return runtime.SendResult{Sent: true, Submitted: submit}, nil
}

// mkSendSvc — SessionService с контролируемым реестром (mock для tmux/pi).
func mkSendSvc(t *testing.T, e *sessionEnv, mock *mockSendAdapter) *service.SessionService {
	t.Helper()
	reg := runtime.NewRegistry()
	reg.SetOverride("tmux", mock)
	reg.SetOverride("pi", mock)
	svc := service.NewSessionService(e.db, e.stores, reg)
	svc.LogsDir = e.ssvc.LogsDir
	return svc
}

func TestSendDelivered(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "send-delivered")
	mock := &mockSendAdapter{
		name:   "tmux",
		guard:  []string{"$ ready", "idle"},
		verify: []string{"$ ready", "idle", "typed HELLO-VERIFY-TXT"},
	}
	svc := mkSendSvc(t, e, mock)
	s := mkCapSession(t, e, teamID, roleA, "tmux", "daemon-session-77001", models.SessionRunning)

	res, err := svc.Send(e.ctx, s, "HELLO-VERIFY-TXT", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != service.SendStatusDelivered {
		t.Errorf("status = %q, want delivered", res.Status)
	}
	if !res.Sent || !res.Submitted || !res.Verified {
		t.Errorf("sent/submitted/verified = %v/%v/%v, want true", res.Sent, res.Submitted, res.Verified)
	}
	if len(res.Tail) == 0 {
		t.Error("verified but no tail")
	}
}

func TestSendStaged(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "send-staged")
	// verify НЕ содержит текст → staged (введено, не подтверждено)
	mock := &mockSendAdapter{
		name:   "tmux",
		guard:  []string{"$ ready", "idle"},
		verify: []string{"$ ready", "idle", "(still rendering…)"},
	}
	svc := mkSendSvc(t, e, mock)
	s := mkCapSession(t, e, teamID, roleA, "pi", "daemon-session-77002", models.SessionRunning)

	res, err := svc.Send(e.ctx, s, "STAGED-UNSEEN-TEXT", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != service.SendStatusStaged {
		t.Errorf("status = %q, want staged", res.Status)
	}
	if !res.Sent || res.Verified {
		t.Errorf("sent/verified = %v/%v, want true/false", res.Sent, res.Verified)
	}
	if res.Tail != nil {
		t.Error("staged should have no tail")
	}
}

func TestSendGuardRetained(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "send-guard")
	// guard: последняя строка — интерактивный prompt → не отправляем
	mock := &mockSendAdapter{
		name:   "tmux",
		guard:  []string{"Do you want to continue? (y/n)"},
		verify: []string{"should not be reached"},
	}
	svc := mkSendSvc(t, e, mock)
	s := mkCapSession(t, e, teamID, roleA, "tmux", "daemon-session-77003", models.SessionRunning)

	res, err := svc.Send(e.ctx, s, "DO-NOT-SEND", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != service.SendStatusRetained {
		t.Errorf("status = %q, want retained", res.Status)
	}
	if res.Sent {
		t.Error("guard fired but sent=true")
	}
	if res.Guard != "interactive-prompt" {
		t.Errorf("guard = %q, want interactive-prompt", res.Guard)
	}
}

func TestSendProcessNoScreen(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "send-process")
	mock := &mockSendAdapter{name: "tmux", guard: []string{"x"}, verify: []string{"x"}}
	svc := mkSendSvc(t, e, mock)
	// process → нет экрана → 409
	s := mkCapSession(t, e, teamID, roleA, "process", "", models.SessionRunning)
	_, err := svc.Send(e.ctx, s, "hello", true)
	var appErr *service.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Errorf("want 409 (process no screen), got %v", err)
	}
}

func TestSendNotFoundAndValidation(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "send-404")
	mock := &mockSendAdapter{name: "tmux", guard: []string{"x"}, verify: []string{"x"}}
	svc := mkSendSvc(t, e, mock)
	s := mkCapSession(t, e, teamID, roleA, "tmux", "daemon-session-77004", models.SessionRunning)

	// 404 — сессия нет
	_, err := svc.Send(e.ctx, 999999, "hello", true)
	var appErr *service.AppError
	if !errors.As(err, &appErr) || appErr.Status != 404 {
		t.Errorf("want 404, got %v", err)
	}
	// 400 — text пуст
	if _, err := svc.Send(e.ctx, s, "   ", true); !isStatus(err, 400) {
		t.Errorf("empty text: want 400, got %v", err)
	}
	// 400 — text длиннее 2000
	if _, err := svc.Send(e.ctx, s, strings.Repeat("a", 2001), true); !isStatus(err, 400) {
		t.Errorf("long text: want 400, got %v", err)
	}
}

// TestSendEvent — событие session.send в events (P2.9).
func TestSendEvent(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "send-event")
	bus := service.NewEventBus()
	evs := service.NewEventService(e.db, e.stores)
	evs.SetSink(bus)
	mock := &mockSendAdapter{
		name:   "tmux",
		guard:  []string{"$ ready"},
		verify: []string{"$ ready", "typed EVT-TEXT"},
	}
	svc := mkSendSvc(t, e, mock)
	svc.Bus = bus
	s := mkCapSession(t, e, teamID, roleA, "tmux", "daemon-session-77005", models.SessionRunning)

	if _, err := svc.Send(e.ctx, s, "EVT-TEXT", true); err != nil {
		t.Fatal(err)
	}
	page, err := evs.ListEvents(e.ctx, service.EventFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range page.Events {
		if ev.Type == "session.send" && ev.EntityKind == "session" && ev.EntityID == s {
			found = true
			if !strings.Contains(ev.Summary, "Send to session") {
				t.Errorf("summary = %q, want 'Send to session…'", ev.Summary)
			}
		}
	}
	if !found {
		t.Errorf("no session.send event found (got %d events)", len(page.Events))
	}
}

// isStatus — err есть ли AppError с этим HTTP-статусом.
func isStatus(err error, want int) bool {
	var appErr *service.AppError
	return errors.As(err, &appErr) && appErr.Status == want
}
