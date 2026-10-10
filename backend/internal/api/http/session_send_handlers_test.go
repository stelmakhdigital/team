package httpapi_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P2.11: send to session (HTTP: /sessions/{id}/send, RBAC operator+) ----------

// httpMockAdapter — управляемый адаптер (Capture+Send) для HTTP-тестов P2.11.
type httpMockAdapter struct {
	name       string
	guard      []string
	verify     []string
	sent       bool
	captureErr error
	sendErr    error
}

func (m *httpMockAdapter) Name() string { return m.name }
func (m *httpMockAdapter) Start(context.Context, runtime.Spec) (string, error) {
	return "ref", nil
}
func (m *httpMockAdapter) Stop(context.Context, string) error { return nil }
func (m *httpMockAdapter) Status(context.Context, string) (*runtime.Status, error) {
	return &runtime.Status{Alive: true, State: runtime.StateRunning}, nil
}
func (m *httpMockAdapter) Capture(context.Context, string, int) ([]string, error) {
	if m.captureErr != nil {
		return nil, m.captureErr
	}
	if m.sent {
		return m.verify, nil
	}
	return m.guard, nil
}
func (m *httpMockAdapter) Send(context.Context, string, string, bool) (runtime.SendResult, error) {
	if m.sendErr != nil {
		return runtime.SendResult{}, m.sendErr
	}
	m.sent = true
	return runtime.SendResult{Sent: true, Submitted: true}, nil
}

// newSendMockServer — test-сервер с mock-адаптером (tmux/pi).
func newSendMockServer(t *testing.T, mock *httpMockAdapter) *searchServer {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := runtime.NewRegistry()
	if mock != nil {
		reg.SetOverride("tmux", mock)
		reg.SetOverride("pi", mock)
	}
	ssvc := service.NewSessionService(db, stores, reg)
	ssvc.LogsDir = t.TempDir() + "/logs"
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, service.NewTaskService(db, stores), httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Sessions: ssvc,
		Auth:     auth,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &searchServer{srv: srv, auth: auth, db: db, stores: stores, svc: svc, ssvc: ssvc}
}

func (s *searchServer) mkTmuxSession(t *testing.T, teamID, roleID int64) int64 {
	t.Helper()
	sess := &models.Session{TeamID: teamID, RoleID: roleID, RuntimeType: "tmux", RuntimeRef: "daemon-session-x", State: models.SessionRunning}
	if err := s.stores.Sessions.Create(context.Background(), s.db, sess); err != nil {
		t.Fatal(err)
	}
	return sess.ID
}

func TestSendHTTPDelivered(t *testing.T) {
	mock := &httpMockAdapter{
		name:   "tmux",
		guard:  []string{"$ ready", "idle"},
		verify: []string{"$ ready", "idle", "typed SENT-HTTP-TXT"},
	}
	s := newSendMockServer(t, mock)
	teamID, roleA, _ := s.mkTeamRoles(t, "send-http-delivered")
	sid := s.mkTmuxSession(t, teamID, roleA)
	admin := map[string]string{"X-API-Key": "env-admin"}

	st, out, _ := do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, sid),
		map[string]any{"text": "SENT-HTTP-TXT", "submit": true}, admin)
	if st != http.StatusOK {
		t.Fatalf("delivered: want 200, got %d %v", st, out)
	}
	if out["status"] != "delivered" {
		t.Errorf("status = %v, want delivered", out["status"])
	}
	if out["verified"] != true || out["sent"] != true {
		t.Errorf("sent/verified = %v/%v, want true", out["sent"], out["verified"])
	}
	if out["tail"] == nil {
		t.Error("delivered but no tail")
	}
}

func TestSendHTTPRetainedGuard(t *testing.T) {
	mock := &httpMockAdapter{
		name:   "tmux",
		guard:  []string{"Do you want to continue? (y/n)"},
		verify: []string{"unreachable"},
	}
	s := newSendMockServer(t, mock)
	teamID, roleA, _ := s.mkTeamRoles(t, "send-http-guard")
	sid := s.mkTmuxSession(t, teamID, roleA)
	admin := map[string]string{"X-API-Key": "env-admin"}

	st, out, _ := do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, sid),
		map[string]any{"text": "DO-NOT-SEND"}, admin)
	if st != http.StatusOK {
		t.Fatalf("retained: want 200, got %d %v", st, out)
	}
	if out["status"] != "retained" {
		t.Errorf("status = %v, want retained", out["status"])
	}
	if out["guard"] != "interactive-prompt" {
		t.Errorf("guard = %v, want interactive-prompt", out["guard"])
	}
}

func TestSendHTTPErrors(t *testing.T) {
	mock := &httpMockAdapter{name: "tmux", captureErr: runtime.ErrTmuxSessionDead}
	s := newSendMockServer(t, mock)
	teamID, roleA, roleB := s.mkTeamRoles(t, "send-http-err")
	admin := map[string]string{"X-API-Key": "env-admin"}
	body := map[string]any{"text": "hello"}

	// 404: сессия нет
	st, _, _ := do(t, "POST", s.srv.URL+"/api/v1/sessions/999999/send", body, admin)
	if st != http.StatusNotFound {
		t.Fatalf("not found: want 404, got %d", st)
	}
	// 400: text пуст
	procSess := s.mkSession(t, teamID, roleA, models.SessionRunning) // process
	st, _, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, procSess),
		map[string]any{"text": "   "}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("empty text: want 400, got %d", st)
	}
	// 409: process (нет экрана)
	st, _, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, procSess), body, admin)
	if st != http.StatusConflict {
		t.Fatalf("process: want 409, got %d", st)
	}
	// 409: tmux мертва (mock captureErr)
	tmuxSess := s.mkTmuxSession(t, teamID, roleB)
	st, _, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, tmuxSess), body, admin)
	if st != http.StatusConflict {
		t.Fatalf("tmux dead: want 409, got %d", st)
	}
	// 401: без ключа
	st, _, _ = do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, procSess), body, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}
}

func TestSendHTTPRBAC(t *testing.T) {
	mock := &httpMockAdapter{name: "tmux", guard: []string{"$ ready"}, verify: []string{"$ ready", "typed RBAC-TXT"}}
	s := newSendMockServer(t, mock)
	teamID, roleA, _ := s.mkTeamRoles(t, "send-http-rbac")
	sid := s.mkTmuxSession(t, teamID, roleA)
	body := map[string]any{"text": "RBAC-TXT"}

	// viewer: 403 (sessions.update — operator+)
	_, viewerKey := mustCreateKey(t, s.auth, "send-viewer", "viewer", "v")
	st, _, _ := do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, sid),
		body, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer send: want 403, got %d", st)
	}
	// operator: 200 (delivered) — RBAC пройден
	_, opKey := mustCreateKey(t, s.auth, "send-operator", "operator", "o")
	st, out, _ := do(t, "POST", fmt.Sprintf("%s/api/v1/sessions/%d/send", s.srv.URL, sid),
		body, map[string]string{"X-API-Key": opKey})
	if st != http.StatusOK {
		t.Fatalf("operator send: want 200, got %d %v", st, out)
	}
	if out["status"] != "delivered" {
		t.Errorf("operator status = %v, want delivered", out["status"])
	}
}
