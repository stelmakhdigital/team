// Package config — конфигурация daemon (env-based).
package config

import (
	"fmt"
	"os"
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

	// Slice 6 — Security
	SecretKey string // AES-256-GCM ключ для secrets (64 hex-символа, пусто = выключен)
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

		SecretKey: envStr("DAEMON_SECRET_KEY", ""),
	}

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
