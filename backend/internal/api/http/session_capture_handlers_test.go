package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"daemon/internal/models"
)

// ---------- P2.8: capture (HTTP: /sessions/{id}/capture, RBAC) ----------

func TestCaptureHTTP(t *testing.T) {
	s := newSearchTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	teamID, roleA, roleB := s.mkTeamRoles(t, "cap-http")
	// process-сессия + transcript
	s1 := s.mkSession(t, teamID, roleA, models.SessionRunning) // process
	s.mkTranscript(t, s1, "h1\nh2\nh3\nh4\nh5\n")
	// tmux-сессия (мертвый ref)
	tmuxSess := &models.Session{TeamID: teamID, RoleID: roleB, RuntimeType: "tmux", RuntimeRef: "daemon-session-99985", State: models.SessionRunning}
	if err := s.stores.Sessions.Create(context.Background(), s.db, tmuxSess); err != nil {
		t.Fatal(err)
	}

	// 200 process: source=log-tail + lines + saved_path
	st, out, _ := do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture", s.srv.URL, s1), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("capture: want 200, got %d %v", st, out)
	}
	if out["source"] != "log-tail" {
		t.Errorf("source = %v, want log-tail", out["source"])
	}
	if out["session_id"] == nil || out["runtime_type"] == nil || out["captured_at"] == nil {
		t.Errorf("missing fields: %v", out)
	}
	if out["saved_path"] == nil || out["saved_path"] == "" {
		t.Error("no saved_path")
	}
	lines, _ := out["lines"].([]any)
	if len(lines) != 5 { // default 20 > 5 строк → все 5
		t.Errorf("lines = %d, want 5 (default 20 clamp to file size)", len(lines))
	}

	// lines=3 → ровно 3 строки
	st, out, _ = do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture?lines=3", s.srv.URL, s1), nil, admin)
	if st != http.StatusOK {
		t.Fatalf("lines=3: want 200, got %d", st)
	}
	if l, _ := out["lines"].([]any); len(l) != 3 {
		t.Errorf("lines=3: got %d lines, want 3", len(l))
	}

	// 404: сессия не найдена
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/sessions/999999/capture", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("not found: want 404, got %d", st)
	}
	// 409: tmux мертва
	st, out, _ = do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture", s.srv.URL, tmuxSess.ID), nil, admin)
	if st != http.StatusConflict {
		t.Fatalf("tmux dead: want 409, got %d %v", st, out)
	}
	// 400: lines не число
	st, _, _ = do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture?lines=abc", s.srv.URL, s1), nil, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("invalid lines: want 400, got %d", st)
	}
	// 401: без ключа
	st, _, _ = do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture", s.srv.URL, s1), nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}
}

func TestCaptureHTTPRBAC(t *testing.T) {
	s := newSearchTestServer(t)
	teamID, roleA, _ := s.mkTeamRoles(t, "cap-rbac")
	sid := s.mkSession(t, teamID, roleA, models.SessionRunning) // process
	s.mkTranscript(t, sid, "a\nb\nc\n")

	// viewer: 200 (sessions.read — viewer-ok)
	_, viewerKey := mustCreateKey(t, s.auth, "cap-viewer", "viewer", "v")
	st, _, _ := do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture", s.srv.URL, sid), nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer capture: want 200, got %d", st)
	}
	// operator: 200
	_, opKey := mustCreateKey(t, s.auth, "cap-operator", "operator", "o")
	st, _, _ = do(t, "GET", fmt.Sprintf("%s/api/v1/sessions/%d/capture", s.srv.URL, sid), nil, map[string]string{"X-API-Key": opKey})
	if st != http.StatusOK {
		t.Fatalf("operator capture: want 200, got %d", st)
	}
}
