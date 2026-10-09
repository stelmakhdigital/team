package httpapi_test

import (
	"context"
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

// ---------- U4: Config (Settings) — HTTP tests ----------

// clearConfigHTTPEnv — hermetic: снять DAEMON_* переменные, влияющие на конфиг.
func clearConfigHTTPEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"DAEMON_LISTEN_ADDR", "DAEMON_DB_DSN", "DAEMON_MIGRATE",
		"DAEMON_SESSION_POLL_SECS", "DAEMON_SESSION_CONFIGS_DIR",
		"DAEMON_WATCHDOG_ENABLED", "DAEMON_WATCHDOG_SCAN_SECS",
		"DAEMON_WATCHDOG_STALE_SECS", "DAEMON_WATCHDOG_BLOCKED_SECS",
		"DAEMON_API_KEYS", "DAEMON_SECRET_KEY", "DAEMON_AGENT_SPECS_DIR",
		"DAEMON_LOG_LEVEL", "DAEMON_LOGS_DIR",
		"DAEMON_WORKSPACE_ROOT", "DAEMON_WORKSPACE_ARTIFACTS", "DAEMON_WORKSPACE_SCREENSHOTS",
		"DAEMON_CONFIG_FILE",
	}
	for _, k := range vars {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v)
			os.Unsetenv(k)
		}
	}
}

// newConfigTestServer — тест-сервер с Config и env-admin-ключом.
func newConfigTestServer(t *testing.T, adminKey string) *httptest.Server {
	t.Helper()
	clearConfigHTTPEnv(t)
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
	tsvc := service.NewTaskService(db, stores)
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	ssvc.LogsDir = t.TempDir() + "/logs"
	wsvc := service.NewWorkflowService(db, stores)
	auth := service.NewAuthService(db, []string{adminKey})
	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	cvc, err := service.NewConfigService(db)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		DB: db, Sessions: ssvc, Workflows: wsvc,
		Auth: auth, Config: cvc,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestConfigListHTTP(t *testing.T) {
	srv := newConfigTestServer(t, "admin-cfg")
	base := srv.URL + "/api/v1/config"
	st, out, _ := do(t, "GET", base, nil, map[string]string{"X-API-Key": "admin-cfg"})
	if st != http.StatusOK {
		t.Fatalf("GET /config: %d %v", st, out)
	}
	// meta
	meta, ok := out["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta missing: %v", out)
	}
	for _, f := range []string{"project", "version", "uptime_seconds", "config_hash", "started_at"} {
		if _, ok := meta[f]; !ok {
			t.Errorf("meta.%s missing: %v", f, meta)
		}
	}
	if meta["project"] != "team" {
		t.Errorf("meta.project = %v, want team", meta["project"])
	}
	// sections: все 8, у каждого ключа source
	sections, ok := out["sections"].([]any)
	if !ok || len(sections) != 8 {
		t.Fatalf("sections = %v, want 8", out["sections"])
	}
	wantSections := map[string]bool{"server": true, "storage": true, "tasks": true, "sessions": true,
		"watchdog": true, "security": true, "logging": true, "workspace": true}
	for _, s := range sections {
		sm := s.(map[string]any)
		name, _ := sm["name"].(string)
		if !wantSections[name] {
			t.Errorf("unexpected section %q", name)
		}
		delete(wantSections, name)
		keys, _ := sm["keys"].([]any)
		if len(keys) == 0 {
			t.Errorf("section %s: no keys", name)
		}
		for _, k := range keys {
			kk := k.(map[string]any)
			for _, f := range []string{"key", "section", "type", "current_value",
				"default_value", "source", "default_is_derived", "requires_restart", "editable"} {
				if _, ok := kk[f]; !ok {
					t.Errorf("key %v: field %s missing", kk["key"], f)
				}
			}
			src, _ := kk["source"].(string)
			if src != "default" && src != "file" && src != "env" && src != "override" {
				t.Errorf("key %v: source = %q", kk["key"], src)
			}
		}
	}
	if len(wantSections) != 0 {
		t.Errorf("missing sections: %v", wantSections)
	}
	// environment — массив (пустой при чистом env)
	if _, ok := out["environment"].([]any); !ok {
		t.Errorf("environment missing or not a list: %v", out["environment"])
	}
}

func TestConfigSetResetHTTP(t *testing.T) {
	srv := newConfigTestServer(t, "admin-cfg")
	base := srv.URL + "/api/v1/config"
	hdr := map[string]string{"X-API-Key": "admin-cfg"}

	// PUT — set override
	st, out, _ := do(t, "PUT", base+"/logging.level", map[string]any{"value": "debug"}, hdr)
	if st != http.StatusOK {
		t.Fatalf("PUT: %d %v", st, out)
	}
	if out["key"] != "logging.level" || out["value"] != "debug" || out["source"] != "override" {
		t.Fatalf("PUT response = %v", out)
	}
	if out["requires_restart"] != true || out["needs_restart"] != true {
		t.Errorf("PUT restart flags = %v", out)
	}
	// PUT того же значения — needs_restart false
	st, out, _ = do(t, "PUT", base+"/logging.level", map[string]any{"value": "debug"}, hdr)
	if st != http.StatusOK || out["needs_restart"] != false {
		t.Fatalf("PUT same value: %d %v", st, out)
	}
	// GET отражает override
	st, out, _ = do(t, "GET", base, nil, hdr)
	_ = st
	found := false
	for _, s := range out["sections"].([]any) {
		for _, k := range s.(map[string]any)["keys"].([]any) {
			if k.(map[string]any)["key"] == "logging.level" {
				found = true
				if k.(map[string]any)["source"] != "override" || k.(map[string]any)["current_value"] != "debug" {
					t.Errorf("GET after PUT: %v", k)
				}
			}
		}
	}
	if !found {
		t.Fatal("logging.level not in GET response")
	}

	// PUT неверный тип → 400 validation_failed
	st, out, _ = do(t, "PUT", base+"/logging.level", map[string]any{"value": 42}, hdr)
	if st != http.StatusBadRequest || out["error"].(map[string]any)["code"] != "validation_failed" {
		t.Fatalf("PUT bad type: %d %v", st, out)
	}
	// PUT secret-ключ → 400/403
	st, out, _ = do(t, "PUT", base+"/security.secret_key", map[string]any{"value": "0123"}, hdr)
	if st != http.StatusBadRequest && st != http.StatusForbidden {
		t.Fatalf("PUT secret: %d %v", st, out)
	}
	// PUT readonly → 403
	st, out, _ = do(t, "PUT", base+"/server.read_timeout", map[string]any{"value": "5s"}, hdr)
	if st != http.StatusForbidden {
		t.Fatalf("PUT readonly: %d %v", st, out)
	}
	// PUT unknown → 404 not_found
	st, out, _ = do(t, "PUT", base+"/nope.nope", map[string]any{"value": "x"}, hdr)
	if st != http.StatusNotFound || out["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("PUT unknown: %d %v", st, out)
	}

	// DELETE — reset → возврат к верхнему слою (default, env чистый)
	st, out, _ = do(t, "DELETE", base+"/logging.level", nil, hdr)
	if st != http.StatusOK {
		t.Fatalf("DELETE: %d %v", st, out)
	}
	if out["key"] != "logging.level" || out["source"] != "default" || out["value"] != "info" {
		t.Fatalf("DELETE response = %v, want source default value info", out)
	}
	// DELETE идемпотентен
	st, out, _ = do(t, "DELETE", base+"/logging.level", nil, hdr)
	if st != http.StatusOK || out["source"] != "default" {
		t.Fatalf("DELETE idempotent: %d %v", st, out)
	}
	// DELETE unknown → 404
	st, out, _ = do(t, "DELETE", base+"/nope.nope", nil, hdr)
	if st != http.StatusNotFound {
		t.Fatalf("DELETE unknown: %d %v", st, out)
	}
}

func TestConfigRBAC(t *testing.T) {
	srv, auth := newRBACTestServer(t, []string{"env-admin"})
	_, viewKey := mustCreateKey(t, auth, "cfg-viewer", "viewer", "cfg-viewer-user")
	_, opKey := mustCreateKey(t, auth, "cfg-operator", "operator", "cfg-operator-user")
	base := srv.URL + "/api/v1/config"

	// viewer: GET — 200 (config.read у admin+operator+viewer)
	st, out, _ := do(t, "GET", base, nil, map[string]string{"X-API-Key": viewKey})
	if st != http.StatusOK {
		t.Fatalf("viewer GET /config: %d %v", st, out)
	}
	// viewer: PUT — 403
	st, out, _ = do(t, "PUT", base+"/logging.level", map[string]any{"value": "debug"},
		map[string]string{"X-API-Key": viewKey})
	if st != http.StatusForbidden {
		t.Fatalf("viewer PUT /config: %d %v", st, out)
	}
	// operator: PUT — 403 (config.update admin-only; не выдаём operator)
	st, out, _ = do(t, "PUT", base+"/logging.level", map[string]any{"value": "debug"},
		map[string]string{"X-API-Key": opKey})
	if st != http.StatusForbidden {
		t.Fatalf("operator PUT /config: want 403, got %d %v", st, out)
	}
	// operator: DELETE — 403
	st, out, _ = do(t, "DELETE", base+"/logging.level", nil, map[string]string{"X-API-Key": opKey})
	if st != http.StatusForbidden {
		t.Fatalf("operator DELETE /config: want 403, got %d %v", st, out)
	}
	// admin (env-ключ): GET — 200
	st, out, _ = do(t, "GET", base, nil, map[string]string{"X-API-Key": "env-admin"})
	if st != http.StatusOK {
		t.Fatalf("admin GET /config: %d %v", st, out)
	}
}
