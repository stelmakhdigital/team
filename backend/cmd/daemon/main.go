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

	// P2.13 — Starter bundles: детерминированный + идемпотентный seed стартеров
	// (ON CONFLICT DO NOTHING; повторный вызов при каждом старте не дублирует).
	if err := service.SeedStarters(ctx, db); err != nil {
		return err
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
	// P0.3: timer-wakes срабатывают в watchdog-loop (task → pending).
	wd.FireTimerWakes = tsvc.FireArmedTimerWakes
	// P1.6 — Watchdog-джобы (context-usage-threshold): loop проверяет enabled джобы.
	wdjobs := service.NewWatchdogJobService(db, stores)
	wd.Jobs = stores.WatchdogJobs
	wd.JobEvents = stores.WatchdogJobEvents
	wd.LiveSession = ssvc.GetSessionView
	wd.FireWatchdogWakes = tsvc.FireArmedWatchdogWakes
	// P1.7 — Session briefing (startup context): сборщик + func для CreateSession.
	briefing := service.NewBriefingService(db, stores)
	ssvc.BriefingFunc = briefing.Build
	// P3.12 — agent_image: снапшот сессии (library + versions).
	ssvc.SetLibraryRepo(stores.Library)
	ssvc.SetLibraryVersionRepo(stores.LibraryVersions)
	// P1.8 — Health/Doctor-панель (diagnostics, read-only): daemon/БД/tmux/сессии/logs/git + divergence.
	diagnostics := service.NewDiagnosticsService(db, stores, rtRegistry, cfg.Dialect(), cfg.LogsDir, "0.1.0")
	alerts := &service.AlertStoreRef{Events: stores.Watchdog, Teams: stores.Teams, DB: db}

	// P2.9 — Pulse feed (events): sink → каждое событие шины пишется в events (bounded).
	eventsSvc := service.NewEventService(db, stores)
	eventsSvc.SetSink(bus)

	// P0.1 — Attention view ("Что требует меня")
	attention := service.NewAttentionService(db, stores)

	// P1.0 — Projects (мультипроектность)
	projSvc := service.NewProjectService(db, stores)

	// Slice 4 — Message Center
	msvc := service.NewMessageService(db, stores)
	msvc.Bus = bus

	// Slice 5 — Workflows + WebSocket
	wsvc := service.NewWorkflowService(db, stores)

	// P1.4 — Workflow engine (исполняемые воркфлоу) + projection hook в TaskService.
	engine := service.NewWorkflowEngine(db, stores)
	engine.Bus = bus
	tsvc.ProjectionHook = engine.ProjectStepClose

	// Slice 5b — Library + Audit + Metrics
	lservice := service.NewLibraryService(db, stores, svc)
	aservice := service.NewAuditService(db, stores)
	msvcMetrics := service.NewMetricsService(db)

	// U4 — Config (Settings): реестр ключей + layered resolution + overrides
	cvc, err := service.NewConfigService(db)
	if err != nil {
		return err
	}

	// P2.14 — Cost tracking: оценка стоимости по токенам + ценам (cost.model_prices).
	costSvc := service.NewCostService(cvc)
	ssvc.SetCostService(costSvc)
	// P2.14: dashboard-серия стоимости (llm_cost_usd) — по сессиям (bounded).
	msvcMetrics.CostSeries = ssvc.CostSeries
	// P2.14: budget-порог → watchdog-алерт (dedup: cooldown).
	wd.TotalCost = ssvc.CostForAll
	wd.CostSvc = costSvc

	// P0.2: дедлайн закрытия читает tasks.closure_required_secs на момент перехода
	// в in_progress (layered resolution, restart=false).
	tsvc.ClosureRequiredFunc = func(ctx context.Context) (time.Duration, error) {
		return cvc.ResolveDuration(ctx, "tasks.closure_required_secs")
	}

	// P1.5 — compact-plan + handover:
	//   context_wall_pct читается в рантайме (layered, restart=false);
	//   handover переиспользует TaskService.HandoffTask; attention получает compact-plan.
	ssvc.ContextWallPctFunc = func(ctx context.Context) (int, error) {
		return cvc.ResolveInt(ctx, "sessions.context_wall_pct")
	}
	ssvc.TaskHandoffFunc = func(ctx context.Context, req service.HandoffTaskRequest) (*service.HandoffTaskResult, error) {
		return tsvc.HandoffTask(ctx, req)
	}
	attention.SetContextWallFunc(func(ctx context.Context) ([]*service.CompactPlanEntry, error) {
		entries, _, err := ssvc.CompactPlan(ctx)
		return entries, err
	})

	// P2.15 — whoami ("кто я и что мне делать", компактный срез для агента)
	whoamiSvc := service.NewWhoamiService(db, stores, svc, tsvc, ssvc, msvc)

	handler := httpapi.NewServer(svc, tsvc, httpapi.Options{
		Logger: logger, DB: db,
		Sessions: ssvc, Alerts: alerts, Messages: msvc,
		Workflows: wsvc, Events: bus,
		Library: lservice, Audit: aservice, Metrics: msvcMetrics,
		Auth: authSvc, Config: cvc, Attention: attention, Projects: projSvc, Engine: engine,
		WatchdogJobs: wdjobs, Briefing: briefing,
		Diagnostics: diagnostics, EventsSvc: eventsSvc, Whoami: whoamiSvc,
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
