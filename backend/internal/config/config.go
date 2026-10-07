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
