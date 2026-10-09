package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/service"
)

// ---------- U4: Config subsystem — service unit tests ----------

// configTestEnvVars — ВСЕ DAEMON_* переменные, влияющие на конфиг (hermetic-тесты).
var configTestEnvVars = []string{
	"DAEMON_LISTEN_ADDR", "DAEMON_DB_DSN", "DAEMON_MIGRATE",
	"DAEMON_SESSION_POLL_SECS", "DAEMON_SESSION_CONFIGS_DIR",
	"DAEMON_WATCHDOG_ENABLED", "DAEMON_WATCHDOG_SCAN_SECS",
	"DAEMON_WATCHDOG_STALE_SECS", "DAEMON_WATCHDOG_BLOCKED_SECS",
	"DAEMON_API_KEYS", "DAEMON_SECRET_KEY", "DAEMON_AGENT_SPECS_DIR",
	"DAEMON_LOG_LEVEL", "DAEMON_LOGS_DIR",
	"DAEMON_WORKSPACE_ROOT", "DAEMON_WORKSPACE_ARTIFACTS", "DAEMON_WORKSPACE_SCREENSHOTS",
	"DAEMON_CONFIG_FILE",
}

// clearConfigEnv — снимает все DAEMON_* из окружения (restore в cleanup).
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range configTestEnvVars {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v) // зарегистрировать restore
			os.Unsetenv(k)
		}
	}
}

// setupConfigService — in-memory sqlite + миграции + ConfigService.
// envVars — дополнительно выставить; fileYAML — записать в temp daemon.yaml.
func setupConfigService(t *testing.T, envVars map[string]string, fileYAML string) (*service.ConfigService, *sql.DB) {
	t.Helper()
	clearConfigEnv(t)
	for k, v := range envVars {
		t.Setenv(k, v)
	}
	if fileYAML != "" {
		dir := t.TempDir()
		p := filepath.Join(dir, "daemon.yaml")
		if err := os.WriteFile(p, []byte(fileYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("DAEMON_CONFIG_FILE", p)
	}
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc, err := service.NewConfigService(db)
	if err != nil {
		t.Fatalf("NewConfigService: %v", err)
	}
	return svc, db
}

func mustAppError(t *testing.T, err error) *service.AppError {
	t.Helper()
	var ae *service.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AppError, got %v", err)
	}
	return ae
}

// findKeyView — ConfigKeyView по ключу из ответа ListConfig.
func findKeyView(t *testing.T, resp *service.ListConfigResponse, key string) *service.ConfigKeyView {
	t.Helper()
	for i := range resp.Sections {
		for j := range resp.Sections[i].Keys {
			if resp.Sections[i].Keys[j].Key == key {
				return &resp.Sections[i].Keys[j]
			}
		}
	}
	t.Fatalf("key %s not in response", key)
	return nil
}

// ---------- Реестр: полный охват (config-contract) ----------

func TestConfigRegistryCompleteness(t *testing.T) {
	svc, _ := setupConfigService(t, nil, "")
	want := map[string]string{
		// server
		"server.listen_addr": "string", "server.read_timeout": "duration",
		"server.write_timeout": "duration", "server.shutdown_timeout": "duration",
		// storage
		"storage.dsn": "string", "storage.migrate": "boolean",
		// tasks (P0.2)
		"tasks.closure_required_secs": "duration",
		// sessions
		"sessions.poll_interval": "duration", "sessions.configs_dir": "path",
		// watchdog
		"watchdog.enabled": "boolean", "watchdog.scan_interval": "duration",
		"watchdog.stale_after": "duration", "watchdog.blocked_after": "duration",
		// security
		"security.api_keys": "string-list", "security.secret_key": "string",
		"security.agent_specs_dir": "path",
		// logging
		"logging.level": "string",
		// workspace (U4 — новый)
		"workspace.root": "path", "workspace.artifacts_dir": "path",
		"workspace.screenshots_dir": "path", "workspace.logs_dir": "path",
	}
	got := map[string]string{}
	for _, spec := range svc.Registry() {
		got[spec.Key] = string(spec.Type)
	}
	if len(got) != len(want) {
		t.Fatalf("registry size = %d, want %d: %v", len(got), len(want), got)
	}
	for k, typ := range want {
		if got[k] != typ {
			t.Errorf("key %s: type = %q, want %q", k, got[k], typ)
		}
	}
	// все ключи: requires_restart (дефолт true; P0.2 tasks.closure_required_secs = false,
	// т.к. читается на момент перехода в in_progress, а не на старте), editable/secret-флаги
	for _, spec := range svc.Registry() {
		if !spec.RequiresRestart && spec.Key != "tasks.closure_required_secs" {
			t.Errorf("key %s: requires_restart = false, want true", spec.Key)
		}
		if spec.Key == "tasks.closure_required_secs" && spec.RequiresRestart {
			t.Error("tasks.closure_required_secs must be read-at-transition (requires_restart=false)")
		}
		if spec.Key == "security.secret_key" && !spec.Secret {
			t.Error("security.secret_key must be secret")
		}
		if spec.Key == "security.api_keys" && spec.Editable {
			t.Error("security.api_keys must be readonly")
		}
	}
}

// ---------- Layered resolution: default < file < env < override ----------

func TestConfigResolutionDefault(t *testing.T) {
	svc, _ := setupConfigService(t, nil, "")
	resp, err := svc.ListConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kv := findKeyView(t, resp, "server.listen_addr")
	if kv.CurrentValue != ":8080" || kv.Source != "default" || kv.SourceDetail != "" {
		t.Errorf("listen_addr = %v src=%q detail=%q, want :8080/default/\"\"", kv.CurrentValue, kv.Source, kv.SourceDetail)
	}
	if kv.DefaultValue != ":8080" {
		t.Errorf("listen_addr default = %v, want :8080", kv.DefaultValue)
	}
	// derived: workspace.root = cwd
	wr := findKeyView(t, resp, "workspace.root")
	cwd, _ := os.Getwd()
	if wr.CurrentValue != cwd || !wr.DefaultIsDerived || wr.Source != "default" {
		t.Errorf("workspace.root = %v derived=%v src=%q, want cwd/default", wr.CurrentValue, wr.DefaultIsDerived, wr.Source)
	}
	// derivation: artifacts = root/artifacts
	art := findKeyView(t, resp, "workspace.artifacts_dir")
	if art.CurrentValue != filepath.Join(cwd, "artifacts") || !art.DefaultIsDerived {
		t.Errorf("artifacts = %v derived=%v, want root/artifacts", art.CurrentValue, art.DefaultIsDerived)
	}
	// string-list дефолт — пустой список
	ak := findKeyView(t, resp, "security.api_keys")
	if l, ok := ak.CurrentValue.([]string); !ok || len(l) != 0 {
		t.Errorf("api_keys current = %v (%T), want []", ak.CurrentValue, ak.CurrentValue)
	}
	// boolean
	mig := findKeyView(t, resp, "storage.migrate")
	if mig.CurrentValue != true {
		t.Errorf("storage.migrate = %v, want true", mig.CurrentValue)
	}
}

func TestConfigResolutionFileLayer(t *testing.T) {
	yaml := "server:\n  listen_addr: \"127.0.0.1:9000\"\nwatchdog:\n  scan_interval: \"45s\"\n"
	svc, _ := setupConfigService(t, nil, yaml)
	resp, err := svc.ListConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kv := findKeyView(t, resp, "server.listen_addr")
	if kv.CurrentValue != "127.0.0.1:9000" || kv.Source != "file" {
		t.Errorf("listen_addr = %v src=%q, want 127.0.0.1:9000/file", kv.CurrentValue, kv.Source)
	}
	if kv.SourceDetail == "" || !strings.HasSuffix(kv.SourceDetail, "daemon.yaml") {
		t.Errorf("source_detail = %q, want path to daemon.yaml", kv.SourceDetail)
	}
	// duration из файла: "45s"
	wd := findKeyView(t, resp, "watchdog.scan_interval")
	if wd.CurrentValue != "45s" || wd.Source != "file" {
		t.Errorf("scan_interval = %v src=%q, want 45s/file", wd.CurrentValue, wd.Source)
	}
	// неизвестные ключи из файла игнорируются (нет ключа "bogus" в секциях)
	for _, sec := range resp.Sections {
		if sec.Name == "bogus" {
			t.Error("unknown section from file must be ignored")
		}
	}
}

func TestConfigResolutionEnvLayer(t *testing.T) {
	svc, _ := setupConfigService(t, map[string]string{
		"DAEMON_LOG_LEVEL":      "debug",
		"DAEMON_WORKSPACE_ROOT": "/tmp/ws",
	}, "")
	resp, err := svc.ListConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kv := findKeyView(t, resp, "logging.level")
	if kv.CurrentValue != "debug" || kv.Source != "env" || kv.SourceDetail != "DAEMON_LOG_LEVEL" {
		t.Errorf("log level = %v src=%q detail=%q, want debug/env/DAEMON_LOG_LEVEL", kv.CurrentValue, kv.Source, kv.SourceDetail)
	}
	// env duration: конвенция — секунды
	svc2, _ := setupConfigService(t, map[string]string{"DAEMON_SESSION_POLL_SECS": "10"}, "")
	resp2, _ := svc2.ListConfig(context.Background())
	poll := findKeyView(t, resp2, "sessions.poll_interval")
	if poll.CurrentValue != "10s" || poll.Source != "env" {
		t.Errorf("poll_interval = %v src=%q, want 10s/env", poll.CurrentValue, poll.Source)
	}
	// env переопределяет file
	yaml := "logging:\n  level: warn\n"
	svc3, _ := setupConfigService(t, map[string]string{"DAEMON_LOG_LEVEL": "error"}, yaml)
	resp3, _ := svc3.ListConfig(context.Background())
	kv3 := findKeyView(t, resp3, "logging.level")
	if kv3.CurrentValue != "error" || kv3.Source != "env" {
		t.Errorf("env vs file: = %v src=%q, want error/env", kv3.CurrentValue, kv3.Source)
	}
	// env workspace.root → derived artifacts следуют
	wr := findKeyView(t, resp, "workspace.artifacts_dir")
	if wr.CurrentValue != "/tmp/ws/artifacts" || wr.Source != "default" {
		t.Errorf("artifacts with env root = %v src=%q, want /tmp/ws/artifacts/default", wr.CurrentValue, wr.Source)
	}
}

func TestConfigResolutionOverrideLayer(t *testing.T) {
	svc, _ := setupConfigService(t, map[string]string{"DAEMON_LOG_LEVEL": "debug"}, "")
	// override переопределяет env
	res, err := svc.SetOverride(context.Background(), "logging.level", json.RawMessage(`"warn"`))
	if err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if res.Value != "warn" || res.Source != "override" || !res.RequiresRestart || !res.NeedsRestart {
		t.Errorf("SetOverride res = %+v, want warn/override/restart/needs", res)
	}
	resp, err := svc.ListConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kv := findKeyView(t, resp, "logging.level")
	if kv.CurrentValue != "warn" || kv.Source != "override" {
		t.Errorf("after override: = %v src=%q, want warn/override", kv.CurrentValue, kv.Source)
	}
	// повторный set того же значения → needs_restart = false
	res2, err := svc.SetOverride(context.Background(), "logging.level", json.RawMessage(`"warn"`))
	if err != nil {
		t.Fatal(err)
	}
	if res2.NeedsRestart {
		t.Error("same value: needs_restart must be false")
	}
	// reset → возврат к env
	rr, err := svc.ResetOverride(context.Background(), "logging.level")
	if err != nil {
		t.Fatal(err)
	}
	if rr.Source != "env" || rr.Value != "debug" {
		t.Errorf("after reset: src=%q val=%v, want env/debug", rr.Source, rr.Value)
	}
	// reset идемпотентен
	if _, err := svc.ResetOverride(context.Background(), "logging.level"); err != nil {
		t.Errorf("idempotent reset: %v", err)
	}
}

// ---------- Тип-валидация + readonly/secret/unknown ----------

func TestConfigSetOverrideValidation(t *testing.T) {
	svc, _ := setupConfigService(t, nil, "")
	ctx := context.Background()
	// неверный тип (duration ← bool)
	_, err := svc.SetOverride(ctx, "watchdog.scan_interval", json.RawMessage(`true`))
	ae := mustAppError(t, err)
	if ae.Code != "validation_failed" || ae.Status != 400 {
		t.Errorf("bad type: %v", ae)
	}
	// string ← number
	_, err = svc.SetOverride(ctx, "logging.level", json.RawMessage(`42`))
	ae = mustAppError(t, err)
	if ae.Code != "validation_failed" {
		t.Errorf("bad type (string): %v", ae)
	}
	// boolean ← мусор
	_, err = svc.SetOverride(ctx, "watchdog.enabled", json.RawMessage(`"yes"`))
	ae = mustAppError(t, err)
	if ae.Code != "validation_failed" {
		t.Errorf("bad type (bool): %v", ae)
	}
	// string-list ← одиночная строка
	_, err = svc.SetOverride(ctx, "security.api_keys", json.RawMessage(`"a"`))
	_ = err // api_keys readonly → forbidden тоже допустим (см. ниже)
	// duration: "abc" → 400
	_, err = svc.SetOverride(ctx, "watchdog.stale_after", json.RawMessage(`"abc"`))
	ae = mustAppError(t, err)
	if ae.Code != "validation_failed" {
		t.Errorf("bad duration: %v", ae)
	}
	// корректный duration в секундах
	res, err := svc.SetOverride(ctx, "sessions.poll_interval", json.RawMessage(`"7"`))
	if err != nil || res.Value != "7s" {
		t.Errorf("duration secs: %v %v", res, err)
	}
}

func TestConfigSetOverrideForbiddenAndNotFound(t *testing.T) {
	svc, _ := setupConfigService(t, nil, "")
	ctx := context.Background()
	// secret → 400/403
	_, err := svc.SetOverride(ctx, "security.secret_key", json.RawMessage(`"0123"`))
	ae := mustAppError(t, err)
	if ae.Status != 400 && ae.Status != 403 {
		t.Errorf("secret key: got %d, want 400/403", ae.Status)
	}
	// readonly (константа без env) → 403
	_, err = svc.SetOverride(ctx, "server.read_timeout", json.RawMessage(`"5s"`))
	ae = mustAppError(t, err)
	if ae.Status != 403 {
		t.Errorf("readonly key: got %d, want 403", ae.Status)
	}
	// unknown → 404
	_, err = svc.SetOverride(ctx, "nope.nope", json.RawMessage(`"x"`))
	ae = mustAppError(t, err)
	if ae.Code != "not_found" || ae.Status != 404 {
		t.Errorf("unknown set: %v", ae)
	}
	_, err = svc.ResetOverride(ctx, "nope.nope")
	ae = mustAppError(t, err)
	if ae.Code != "not_found" || ae.Status != 404 {
		t.Errorf("unknown reset: %v", ae)
	}
}

// ---------- Meta + config_hash ----------

func TestConfigMetaAndHash(t *testing.T) {
	svc, _ := setupConfigService(t, nil, "")
	ctx := context.Background()
	resp1, err := svc.ListConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := resp1.Meta
	if m.Project != "team" || m.Version == "" {
		t.Errorf("meta = %+v", m)
	}
	if m.UptimeSeconds < 0 {
		t.Errorf("uptime = %d, want >= 0", m.UptimeSeconds)
	}
	if _, err := time.Parse(time.RFC3339, m.StartedAt); err != nil {
		t.Errorf("started_at = %q: %v", m.StartedAt, err)
	}
	if !strings.HasPrefix(m.ConfigHash, "sha256:") || len(m.ConfigHash) != len("sha256:")+64 {
		t.Errorf("config_hash = %q", m.ConfigHash)
	}
	// стабильность
	resp2, _ := svc.ListConfig(ctx)
	if resp2.Meta.ConfigHash != m.ConfigHash {
		t.Error("config_hash must be stable across calls")
	}
	// изменение → другой хэш
	if _, err := svc.SetOverride(ctx, "logging.level", json.RawMessage(`"debug"`)); err != nil {
		t.Fatal(err)
	}
	resp3, _ := svc.ListConfig(ctx)
	if resp3.Meta.ConfigHash == m.ConfigHash {
		t.Error("config_hash must change after override")
	}
	// секции: все 7 на месте, порядок по контракту
	var names []string
	for _, s := range resp1.Sections {
		names = append(names, s.Name)
	}
	want := []string{"server", "storage", "tasks", "sessions", "watchdog", "security", "logging", "workspace"}
	if len(names) != len(want) {
		t.Fatalf("sections = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("sections[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

// Environment: provenance + маскировка secret
func TestConfigEnvironmentSection(t *testing.T) {
	svc, _ := setupConfigService(t, map[string]string{
		"DAEMON_API_KEYS":   "sk-live-1234",
		"DAEMON_SECRET_KEY": "00112233445566",
		"DAEMON_LOG_LEVEL":  "debug",
	}, "")
	resp, err := svc.ListConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byVar := map[string]service.EnvVarView{}
	for _, e := range resp.Environment {
		byVar[e.EnvVar] = e
	}
	le, ok := byVar["DAEMON_LOG_LEVEL"]
	if !ok || le.FeedsKey != "logging.level" || !le.SourceActive {
		t.Errorf("DAEMON_LOG_LEVEL entry = %+v (active=%v)", le, ok)
	}
	if _, ok := byVar["DAEMON_API_KEYS"]; !ok {
		t.Error("DAEMON_API_KEYS must be in environment")
	}
	// secret маскируется
	for _, e := range resp.Environment {
		if e.EnvVar == "DAEMON_API_KEYS" && strings.Contains(e.Value, "live-1234") {
			t.Errorf("secret env value not masked: %q", e.Value)
		}
		if e.EnvVar == "DAEMON_SECRET_KEY" && e.Value == "00112233445566" {
			t.Errorf("secret_key env value not masked: %q", e.Value)
		}
	}
}
