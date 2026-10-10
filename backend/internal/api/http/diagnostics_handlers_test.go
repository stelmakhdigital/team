package httpapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	httpapi "daemon/internal/api/http"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P1.8: Health/Doctor-панель (HTTP: /diagnostics, RBAC) ----------

type diagServer struct {
	srv  *httptest.Server
	auth *service.AuthService
}

func newDiagnosticsTestServer(t *testing.T) *diagServer {
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
	rtRegistry := runtime.NewRegistry()
	ssvc := service.NewSessionService(db, stores, rtRegistry)
	ssvc.LogsDir = t.TempDir() + "/logs"
	auth := service.NewAuthService(db, []string{"env-admin"})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	diagnostics := service.NewDiagnosticsService(db, stores, rtRegistry, "sqlite", ssvc.LogsDir, "test")
	handler := httpapi.NewServer(svc, service.NewTaskService(db, stores), httpapi.Options{
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
		DB:          db,
		Sessions:    ssvc,
		Auth:        auth,
		Diagnostics: diagnostics,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &diagServer{srv: srv, auth: auth}
}

func TestDiagnosticsHTTP(t *testing.T) {
	s := newDiagnosticsTestServer(t)
	admin := map[string]string{"X-API-Key": "env-admin"}

	// GET /diagnostics → 200 + checks
	st, out, _ := do(t, "GET", s.srv.URL+"/api/v1/diagnostics", nil, admin)
	if st != http.StatusOK {
		t.Fatalf("GET: want 200, got %d %v", st, out)
	}
	if out["overall"] == "" {
		t.Fatal("overall missing")
	}
	checks, _ := out["checks"].([]any)
	if len(checks) == 0 {
		t.Fatal("checks empty")
	}
	// есть sessions.divergence + database.ping
	names := map[string]bool{}
	for _, c := range checks {
		if cm, ok := c.(map[string]any); ok {
			names[cm["name"].(string)] = true
		}
	}
	if !names["sessions.divergence"] || !names["database.ping"] || !names["daemon"] {
		t.Fatalf("missing expected checks: %v", names)
	}

	// POST /diagnostics/run → 200
	st2, out2, _ := do(t, "POST", s.srv.URL+"/api/v1/diagnostics/run", nil, admin)
	if st2 != http.StatusOK {
		t.Fatalf("POST run: want 200, got %d %v", st2, out2)
	}
}

func TestDiagnosticsHTTPRBAC(t *testing.T) {
	s := newDiagnosticsTestServer(t)

	// 401: no key
	st, _, _ := do(t, "GET", s.srv.URL+"/api/v1/diagnostics", nil, nil)
	if st != http.StatusUnauthorized {
		t.Fatalf("no key: want 401, got %d", st)
	}
	// viewer: 403 (diagnostics.read — operator+)
	_, viewerKey := mustCreateKey(t, s.auth, "diag-viewer", "viewer", "v")
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/diagnostics", nil, map[string]string{"X-API-Key": viewerKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer GET: want 403, got %d", st)
	}
	// operator: 200
	_, opKey := mustCreateKey(t, s.auth, "diag-operator", "operator", "o")
	st, _, _ = do(t, "GET", s.srv.URL+"/api/v1/diagnostics", nil, map[string]string{"X-API-Key": opKey})
	if st != http.StatusOK {
		t.Fatalf("operator GET: want 200, got %d", st)
	}
	// operator: POST run 200
	st, _, _ = do(t, "POST", s.srv.URL+"/api/v1/diagnostics/run", nil, map[string]string{"X-API-Key": opKey})
	if st != http.StatusOK {
		t.Fatalf("operator POST run: want 200, got %d", st)
	}
}
