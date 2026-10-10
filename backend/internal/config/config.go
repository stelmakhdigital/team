// Package config — конфигурация daemon (env-based).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr                                 string
	DBDSN                                      string
	APIKeys                                    []string
	LogLevel                                   string
	Migrate                                    bool
	SpecsDir                                   string
	ReadTimeout, WriteTimeout, ShutdownTimeout time.Duration

	// Slice 3 — sessions & watchdog
	SessionsPollInterval time.Duration // период reaper-проверки живости сессий
	WatchdogEnabled      bool
	WatchdogScanInterval time.Duration
	WatchdogStaleAfter   time.Duration // in_progress без обновлений
	WatchdogBlockedAfter time.Duration // blocked дольше
	LogsDir              string        // transcript-файлы
	ConfigsDir           string        // конфиги pi-сессий

	// P1.5 — контекст-стена (compact-plan): порог context_used_percentage (deфолт 80).
	// Читается в рантайме через ConfigService (requires_restart=false), дефолт из env.
	ContextWallPct int

	// Slice 6 — Security
	SecretKey string // AES-256-GCM ключ для secrets (64 hex-символа, пусто = выключен)

	// P0.2 — дедлайн закрытия задачи (дефолт 7200s = 2h; читается на момент перехода в in_progress)
	TasksClosureRequired time.Duration

	// U4 — workspace (рабочая область; дефолты host-derived)
	WorkspaceRoot           string // корень workspace (дефолт — cwd)
	WorkspaceArtifactsDir   string // артефакты (дефолт — root/artifacts)
	WorkspaceScreenshotsDir string // скриншоты (дефолт — root/screenshots)
	// LogsDir (DAEMON_LOGS_DIR) = workspace.logs_dir
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:      envStr("DAEMON_LISTEN_ADDR", ":8080"),
		DBDSN:           envStr("DAEMON_DB_DSN", "sqlite:./daemon.db"),
		APIKeys:         envList("DAEMON_API_KEYS"),
		LogLevel:        envStr("DAEMON_LOG_LEVEL", "info"),
		Migrate:         envStr("DAEMON_MIGRATE", "true") == "true",
		SpecsDir:        envStr("DAEMON_AGENT_SPECS_DIR", "agents"),
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    30 * time.Second,
		ShutdownTimeout: 10 * time.Second,

		SessionsPollInterval: envDuration("DAEMON_SESSION_POLL_SECS", 5*time.Second),
		WatchdogEnabled:      envStr("DAEMON_WATCHDOG_ENABLED", "true") == "true",
		WatchdogScanInterval: envDuration("DAEMON_WATCHDOG_SCAN_SECS", 30*time.Second),
		WatchdogStaleAfter:   envDuration("DAEMON_WATCHDOG_STALE_SECS", 2*time.Hour),
		WatchdogBlockedAfter: envDuration("DAEMON_WATCHDOG_BLOCKED_SECS", 1*time.Hour),
		LogsDir:              envStr("DAEMON_LOGS_DIR", "logs/sessions"),
		ConfigsDir:           envStr("DAEMON_SESSION_CONFIGS_DIR", "configs/sessions"),
		ContextWallPct:       envInt("DAEMON_SESSION_CONTEXT_WALL_PCT", 80),

		SecretKey: envStr("DAEMON_SECRET_KEY", ""),

		TasksClosureRequired: envDuration("DAEMON_TASKS_CLOSURE_REQUIRED_SECS", 2*time.Hour),
	}

	// U4 — workspace: дефолты host-derived (root = cwd; подпапки — от root)
	wsRoot := envStr("DAEMON_WORKSPACE_ROOT", ".")
	if wsRoot == "" {
		wsRoot = "."
	}
	cfg.WorkspaceRoot = wsRoot
	cfg.WorkspaceArtifactsDir = envStr("DAEMON_WORKSPACE_ARTIFACTS", filepath.Join(wsRoot, "artifacts"))
	cfg.WorkspaceScreenshotsDir = envStr("DAEMON_WORKSPACE_SCREENSHOTS", filepath.Join(wsRoot, "screenshots"))

	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("DAEMON_LISTEN_ADDR must not be empty")
	}
	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("DAEMON_DB_DSN must not be empty (sqlite:<path> or postgres://...)")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid DAEMON_LOG_LEVEL %q", cfg.LogLevel)
	}
	return cfg, nil
}

func (c *Config) Dialect() string {
	if strings.HasPrefix(c.DBDSN, "postgres://") || strings.HasPrefix(c.DBDSN, "postgresql://") {
		return "postgres"
	}
	return "sqlite"
}

func envStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	secs, err := time.ParseDuration(v + "s")
	if err != nil {
		return def
	}
	if secs <= 0 {
		return def
	}
	return secs
}

func envList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
