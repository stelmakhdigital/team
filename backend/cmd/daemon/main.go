// Daemon — точка входа.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpapi "daemon/internal/api/http"
	"daemon/internal/config"
	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "admin" {
		if err := runAdmin(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		if err := runMigrate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	db, err := database.Open(cfg.DBDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Migrate {
		if err := database.Migrate(ctx, db, cfg.Dialect()); err != nil {
			return err
		}
	}

	// Slice 6 — Security: auth (RBAC) + secrets
	authSvc := service.NewAuthService(db, cfg.APIKeys)
	if err := authSvc.Init(ctx); err != nil {
		return err
	}
	var secretsMgr *service.SecretsManager
	if cfg.SecretKey != "" {
		secretsMgr, err = service.NewSecretsManager(db, cfg.SecretKey)
		if err != nil {
			return err
		}
	}

	logger.Info("starting daemon",
		"listen", cfg.ListenAddr, "db_dialect", cfg.Dialect(),
		"auth", authSvc.Enabled(), "secrets", secretsMgr != nil)

	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = cfg.SpecsDir
	tsvc := service.NewTaskService(db, stores)

	// Slice 4 — real-time события (EventBus; WS-хендлер /ws — slice 5)
	bus := service.NewEventBus()
	tsvc.Bus = bus

	// Slice 3 — Sessions & Runtime
	rtRegistry := runtime.NewRegistry()
	ssvc := service.NewSessionService(db, stores, rtRegistry)
	ssvc.LogsDir = cfg.LogsDir
	ssvc.ConfigsDir = cfg.ConfigsDir
	ssvc.Bus = bus
	wd := service.NewWatchdogService(db, stores, service.WatchdogConfig{
		StaleThreshold:   cfg.WatchdogStaleAfter,
		BlockedThreshold: cfg.WatchdogBlockedAfter,
		ScanInterval:     cfg.WatchdogScanInterval,
		Enabled:          cfg.WatchdogEnabled,
	})
	wd.Bus = bus
	alerts := &service.AlertStoreRef{Events: stores.Watchdog, Teams: stores.Teams, DB: db}

	// Slice 4 — Message Center
	msvc := service.NewMessageService(db, stores)
	msvc.Bus = bus

	// Slice 5 — Workflows + WebSocket
	wsvc := service.NewWorkflowService(db, stores)

	// Slice 5b — Library + Audit + Metrics
	lservice := service.NewLibraryService(db, stores, svc)
	aservice := service.NewAuditService(db, stores)
	msvcMetrics := service.NewMetricsService(db)

	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger: logger, DB: db,
		Sessions: ssvc, Alerts: alerts, Messages: msvc,
		Workflows: wsvc, Events: bus,
		Library: lservice, Audit: aservice, Metrics: msvcMetrics,
		Auth: authSvc,
	})

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Slice 3 — фоновые loop'ы: reaper сессий + watchdog
	reaperCtx, reaperStop := context.WithCancel(ctx)
	defer reaperStop()
	go runReaper(reaperCtx, ssvc, cfg.SessionsPollInterval, logger)
	go wd.Run(ctx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		reaperStop()
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func runReaper(ctx context.Context, ssvc *service.SessionService, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := ssvc.Reap(ctx)
			if err != nil {
				logger.Warn("session reaper: scan failed", "err", err)
			} else if n > 0 {
				logger.Info("session reaper: sessions reaped", "count", n)
			}
		}
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
