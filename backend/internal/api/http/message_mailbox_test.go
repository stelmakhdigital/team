package httpapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- P3.15: Mailbox (inbox/outbox/ack, HTTP + RBAC) ----------

func newMailboxTestServer(t *testing.T) (*httptest.Server, *service.TeamService, *service.AuthService) {
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
	svc := service.NewTeamService(db, repository.NewStores(db))
	svc.SpecsDir = t.TempDir()
	for _, n := range []string{"a.yaml", "b.yaml"} {
		_ = os.WriteFile(filepath.Join(svc.SpecsDir, n), []byte("name: "+n+"\n"), 0o644)
	}
	msvc := service.NewMessageService(db, repository.NewStores(db))
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, service.NewTaskService(db, repository.NewStores(db)), httpapi.Options{
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:       db,
		Auth:     auth,
		Messages: msvc,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, svc, auth
}

// mustMailboxTeam — команда core (lead + worker); возвращает (workerID, leadID, teamID).
func mustMailboxTeam(t *testing.T, svc *service.TeamService, name string) (workerID, leadID, teamID int64) {
	t.Helper()
	team, err := svc.CreateTeam(context.Background(), service.CreateTeamRequest{
		Name: name,
		Spec: &service.TeamSpec{
			Segments: []service.SegmentSpec{{Name: "core"}},
			Roles: []service.RoleSpec{
				{Segment: "core", Name: "lead", AgentSpec: "a.yaml"},
				{Segment: "core", Name: "worker", AgentSpec: "b.yaml"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	detail, err := svc.GetTeam(context.Background(), team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	for _, r := range detail.Roles {
		switch r.Name {
		case "lead":
			leadID = r.ID
		case "worker":
			workerID = r.ID
		}
	}
	return workerID, leadID, team.ID
}

func TestMailboxInboxHTTP(t *testing.T) {
	srv, svc, _ := newMailboxTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	workerID, _, teamID := mustMailboxTeam(t, svc, "mb-http-inbox")

	// direct → worker (в inbox)
	do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "to_role_id": workerID, "type": "direct", "body": "hi worker",
	}, admin)
	// broadcast (НЕ в inbox)
	do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "type": "broadcast", "body": "standup",
	}, admin)

	st, out, _ := do(t, "GET", srv.URL+"/api/v1/roles/"+itoaID(workerID)+"/inbox", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("inbox: want 200, got %d %v", st, out)
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("inbox total = %v, want 1 (broadcast не в inbox)", out["total"])
	}
	if out["undelivered_count"].(float64) != 1 {
		t.Fatalf("undelivered_count = %v, want 1", out["undelivered_count"])
	}

	// 404 (нет роли)
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/roles/999999/inbox", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("inbox 404: want 404, got %d", st)
	}
}

func TestMailboxAckHTTP(t *testing.T) {
	srv, svc, _ := newMailboxTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	workerID, _, teamID := mustMailboxTeam(t, svc, "mb-http-ack")

	// создать direct-сообщение
	_, out, _ := do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "to_role_id": workerID, "type": "direct", "body": "hi",
	}, admin)
	msgID := itoaID(int64(out["id"].(float64)))

	// absorb → 200 + ack_state=absorbed + acked_by
	st, aout, _ := do(t, "POST", srv.URL+"/api/v1/messages/"+msgID+"/ack",
		map[string]any{"action": "absorb"}, admin)
	if st != http.StatusOK {
		t.Fatalf("ack absorb: want 200, got %d %v", st, aout)
	}
	if aout["ack_state"] != "absorbed" {
		t.Errorf("ack_state = %v, want absorbed", aout["ack_state"])
	}
	if aout["acked_by"] == nil || aout["acked_by"] == "" {
		t.Errorf("acked_by not set (attribution)")
	}
	if aout["acked_at"] == nil {
		t.Errorf("acked_at not set")
	}

	// повторный ack → 409
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages/"+msgID+"/ack",
		map[string]any{"action": "deny"}, admin)
	if st != http.StatusConflict {
		t.Fatalf("repeat ack: want 409, got %d", st)
	}
	// invalid action → 400
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages/"+msgID+"/ack",
		map[string]any{"action": "bogus"}, admin)
	if st != http.StatusBadRequest {
		t.Fatalf("invalid action: want 400, got %d", st)
	}
	// 404 (нет сообщения)
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages/999999/ack",
		map[string]any{"action": "absorb"}, admin)
	if st != http.StatusNotFound {
		t.Fatalf("ack 404: want 404, got %d", st)
	}
}

func TestMailboxOutboxHTTP(t *testing.T) {
	srv, svc, _ := newMailboxTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}
	workerID, leadID, teamID := mustMailboxTeam(t, svc, "mb-http-outbox")

	// worker → lead (FromRoleID=worker) — в outbox worker
	do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "from_role_id": workerID, "to_role_id": leadID, "type": "direct", "body": "from-worker",
	}, admin)

	st, out, _ := do(t, "GET", srv.URL+"/api/v1/roles/"+itoaID(workerID)+"/outbox", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("outbox: want 200, got %d %v", st, out)
	}
	if out["total"].(float64) != 1 {
		t.Fatalf("outbox total = %v, want 1", out["total"])
	}

	// 404 (нет роли)
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/roles/999999/outbox", nil, admin)
	if st != http.StatusNotFound {
		t.Fatalf("outbox 404: want 404, got %d", st)
	}
}

func TestMailboxRBAC(t *testing.T) {
	srv, svc, auth := newMailboxTestServer(t)
	_, viewerKey := mustCreateKey(t, auth, "mb-viewer", "viewer", "v")
	workerID, _, teamID := mustMailboxTeam(t, svc, "mb-http-rbac")

	// viewer: inbox (dashboard.read, viewer-ok) → 200
	st, _, _ := do(t, "GET", srv.URL+"/api/v1/roles/"+itoaID(workerID)+"/inbox", nil,
		map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer inbox: want 200, got %d", st)
	}
	// viewer: outbox (dashboard.read) → 200
	st, _, _ = do(t, "GET", srv.URL+"/api/v1/roles/"+itoaID(workerID)+"/outbox", nil,
		map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusOK {
		t.Fatalf("viewer outbox: want 200, got %d", st)
	}

	// viewer: ack (messages.update, operator+) → 403
	do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "to_role_id": workerID, "type": "direct", "body": "hi",
	}, map[string]string{"X-API-Key": "env-admin"})
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages/1/ack",
		map[string]any{"action": "absorb"}, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer ack: want 403, got %d", st)
	}
}
