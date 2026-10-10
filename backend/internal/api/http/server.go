package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"

	"daemon/internal/service"
)

type Options struct {
	Logger *slog.Logger
	DB     *sql.DB
	// Slice 3 — Sessions & Runtime
	Sessions *service.SessionService
	Alerts   *service.AlertStoreRef
	// Slice 4 — Message Center
	Messages *service.MessageService
	// Slice 5 — Workflows + WebSocket
	Workflows *service.WorkflowService
	Events    *service.EventBus
	// Slice 5b — Library + Audit + Metrics
	Library *service.LibraryService
	Audit   *service.AuditService
	Metrics *service.MetricsService
	// Slice 6 — Security (RBAC, api_keys)
	Auth *service.AuthService
	// U4 — Config (Settings)
	Config *service.ConfigService
	// P0.1 — Attention view ("Что требует меня")
	Attention *service.AttentionService
	// P1.0 — Projects (мультипроектность)
	Projects *service.ProjectService
	// P1.4 — Workflow engine (исполняемые воркфлоу)
	Engine *service.WorkflowEngine
	// P1.6 — Watchdog-джобы (register, context-usage-threshold)
	WatchdogJobs *service.WatchdogJobService
	// P1.7 — Session briefing (startup context): preview-эндпоинт
	Briefing *service.BriefingService
	// P1.8 — Health/Doctor-панель (diagnostics, read-only)
	Diagnostics *service.DiagnosticsService
	// P2.9 — Pulse feed (events: GET /events)
	EventsSvc *service.EventService
	// P2.15 — whoami («кто я и что мне делать», компактный срез для агента)
	Whoami *service.WhoamiService
}

// NewServer собирает mux + middleware + routes.
func NewServer(svc *service.TeamService, tsvc *service.TaskService, opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &handlers{svc: svc, tsvc: tsvc, ssvc: opts.Sessions, msvc: opts.Messages,
		wsvc: opts.Workflows, lsvc: opts.Library, asvc: opts.Audit, msvcM: opts.Metrics,
		events: opts.Events, alerts: opts.Alerts, cvc: opts.Config, attention: opts.Attention,
		projects: opts.Projects, engine: opts.Engine, wdjobs: opts.WatchdogJobs,
		briefing: opts.Briefing, diagnostics: opts.Diagnostics, evs: opts.EventsSvc, whoami: opts.Whoami}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// P2.15 — whoami («кто я и что мне делать», компактный срез для агента; read, viewer-ok)
	mux.HandleFunc("GET /api/v1/whoami", h.GetWhoami)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if opts.DB != nil && opts.DB.Ping() != nil {
			writeError(w, r, &service.AppError{Code: "internal", Status: 503, Message: "database unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// P1.8 — Health/Doctor-панель (diagnostics, read-only; RBAC diagnostics.read)
	mux.HandleFunc("GET /api/v1/diagnostics", h.GetDiagnostics)
	mux.HandleFunc("POST /api/v1/diagnostics/run", h.RunDiagnostics)

	// Team Builder — teams
	mux.HandleFunc("GET /api/v1/teams", h.ListTeams)
	mux.HandleFunc("POST /api/v1/teams", h.CreateTeam)
	mux.HandleFunc("GET /api/v1/teams/{id}", h.GetTeam)
	mux.HandleFunc("DELETE /api/v1/teams/{id}", h.ArchiveTeam)
	mux.HandleFunc("GET /api/v1/teams/{id}/topology", h.GetTopology)
	mux.HandleFunc("POST /api/v1/teams/{id}/validate", h.ValidateTopology)
	mux.HandleFunc("POST /api/v1/teams/{id}/save", h.SaveTopology)
	mux.HandleFunc("POST /api/v1/teams/{id}/segments", h.CreateSegment)
	mux.HandleFunc("POST /api/v1/teams/{id}/relatives", h.CreateRelative)
	mux.HandleFunc("POST /api/v1/teams/{id}/briefing/preview", h.PreviewBriefing) // P1.7 (dry-run)

	// Team Builder — segments / roles / relatives
	mux.HandleFunc("POST /api/v1/segments/{id}/roles", h.CreateRole)
	mux.HandleFunc("PATCH /api/v1/segments/{id}/layout", h.UpdateSegmentLayout)
	mux.HandleFunc("PATCH /api/v1/segments/{id}", h.UpdateSegmentMeta) // U3-rename
	mux.HandleFunc("GET /api/v1/roles/{id}/config", h.GetRoleConfig)
	mux.HandleFunc("PATCH /api/v1/roles/{id}/config", h.UpdateRoleConfig)
	mux.HandleFunc("PATCH /api/v1/roles/{id}/layout", h.UpdateRoleLayout)
	mux.HandleFunc("PATCH /api/v1/relatives/{id}/layout", h.UpdateRelativeLayout)
	mux.HandleFunc("DELETE /api/v1/relatives/{id}", h.DeleteRelative)

	// Slice 2 — Tasks & History
	mux.HandleFunc("GET /api/v1/tasks", h.ListTasks)
	mux.HandleFunc("GET /api/v1/tasks/overdue", h.ListTasksOverdue) // P0.2 (literal — приоритетнее {id})
	mux.HandleFunc("POST /api/v1/tasks", h.CreateTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", h.GetTask)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}/state", h.UpdateTaskState)
	mux.HandleFunc("POST /api/v1/tasks/{id}/handoff", h.HandoffTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/resolve", h.ResolveTask) // P0.3
	mux.HandleFunc("GET /api/v1/tasks/{id}/history", h.GetTaskHistory)

	// Slice 2 — Dashboard
	mux.HandleFunc("GET /api/v1/dashboard/summary", h.DashboardSummary)
	mux.HandleFunc("GET /api/v1/dashboard/tasks", h.DashboardTasks)

	// Slice 3 — Sessions & Runtime
	mux.HandleFunc("GET /api/v1/sessions", h.ListSessions)
	mux.HandleFunc("POST /api/v1/sessions", h.CreateSession)
	mux.HandleFunc("GET /api/v1/sessions/search", h.SearchSessions) // P2.7 (literal — приоритетнее {id})
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.GetSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", h.StopSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/history", h.GetSessionHistory)
	mux.HandleFunc("GET /api/v1/sessions/{id}/transcript", h.GetTranscript)
	// P2.8: capture (стл-кадр экрана сессии) — GET, viewer-ok (sessions.read)
	mux.HandleFunc("GET /api/v1/sessions/{id}/capture", h.CaptureSession)
	// P2.11: send to session + verify — POST, operator+ (sessions.update)
	mux.HandleFunc("POST /api/v1/sessions/{id}/send", h.SendSession)
	// P2.9: Pulse feed — unified event feed (GET /events, dashboard.read, viewer-ok)
	mux.HandleFunc("GET /api/v1/events", h.ListEvents)
	mux.HandleFunc("GET /api/v1/compact-plan", h.CompactPlan)                // P1.5
	mux.HandleFunc("POST /api/v1/sessions/{id}/handover", h.HandoverSession) // P1.5
	mux.HandleFunc("GET /api/v1/dashboard/sessions", h.DashboardSessions)
	mux.HandleFunc("GET /api/v1/dashboard/alerts", h.DashboardAlerts)
	mux.HandleFunc("GET /api/v1/watchdog/events", h.WatchdogEvents)
	mux.HandleFunc("POST /api/v1/watchdog/events/{id}/read", h.MarkAlertRead)
	// P1.6 — Watchdog-джобы (register, context-usage-threshold)
	mux.HandleFunc("POST /api/v1/watchdog/jobs", h.RegisterWatchdogJob)
	mux.HandleFunc("GET /api/v1/watchdog/jobs", h.ListWatchdogJobs)
	mux.HandleFunc("GET /api/v1/watchdog/jobs/{id}", h.GetWatchdogJob)
	mux.HandleFunc("POST /api/v1/watchdog/jobs/{id}/stop", h.StopWatchdogJob)
	mux.HandleFunc("DELETE /api/v1/watchdog/jobs/{id}", h.DeleteWatchdogJob)

	// Slice 4 — Message Center
	mux.HandleFunc("GET /api/v1/messages", h.ListMessages)
	mux.HandleFunc("POST /api/v1/messages", h.SendMessage)
	mux.HandleFunc("GET /api/v1/chatrooms", h.ListChatrooms)
	mux.HandleFunc("GET /api/v1/chatrooms/{id}/messages", h.GetChatroomMessages)
	mux.HandleFunc("POST /api/v1/chatrooms/{id}/messages", h.SendChatroomMessage)

	// Slice 5 — Workflows
	mux.HandleFunc("GET /api/v1/workflows", h.ListWorkflows)
	mux.HandleFunc("POST /api/v1/workflows", h.CreateWorkflow)
	mux.HandleFunc("GET /api/v1/workflows/{id}", h.GetWorkflow)
	mux.HandleFunc("POST /api/v1/workflows/{id}/blocks", h.CreateWorkflowBlock)
	mux.HandleFunc("POST /api/v1/workflows/{id}/connections", h.CreateWorkflowConnection)
	mux.HandleFunc("PATCH /api/v1/workflows/{id}/blocks/{blockId}", h.UpdateWorkflowBlock)
	mux.HandleFunc("PATCH /api/v1/workflows/{id}", h.UpdateWorkflow) // U3-rename + P1.4 engine toggle
	mux.HandleFunc("DELETE /api/v1/workflows/{id}", h.DeleteWorkflow)
	mux.HandleFunc("DELETE /api/v1/workflows/{id}/blocks/{blockId}", h.DeleteWorkflowBlock)
	mux.HandleFunc("DELETE /api/v1/workflows/{id}/connections/{connectionId}", h.DeleteWorkflowConnection)

	// P1.4 — Workflow engine (исполняемые воркфлоу)
	mux.HandleFunc("POST /api/v1/workflows/{id}/run", h.RunWorkflow)
	mux.HandleFunc("GET /api/v1/workflows/{id}/instances", h.ListWorkflowInstances)
	mux.HandleFunc("GET /api/v1/workflow-instances/{iid}", h.GetWorkflowInstance)
	mux.HandleFunc("GET /api/v1/workflow-instances/{iid}/trace", h.GetWorkflowInstanceTrace)
	mux.HandleFunc("POST /api/v1/workflow-instances/{iid}/resume", h.ResumeWorkflowInstance)
	mux.HandleFunc("POST /api/v1/workflow-instances/{iid}/route", h.RouteWorkflowInstance)
	mux.HandleFunc("POST /api/v1/workflow-instances/{iid}/abort", h.AbortWorkflowInstance)

	// Slice 5 — WebSocket (real-time события)
	mux.HandleFunc("GET /ws", h.HandleWS)

	// Slice 5b — Library
	mux.HandleFunc("GET /api/v1/library", h.ListLibrary)
	mux.HandleFunc("POST /api/v1/library", h.SaveToLibrary)
	mux.HandleFunc("GET /api/v1/library/{id}", h.GetLibraryItem)
	mux.HandleFunc("PATCH /api/v1/library/{id}", h.UpdateLibraryItem) // U3-rename
	mux.HandleFunc("POST /api/v1/library/{id}/apply", h.ApplyLibraryItem)

	// Slice 5b — History Viewer: audit log + metrics
	mux.HandleFunc("GET /api/v1/audit", h.ListAudit)
	mux.HandleFunc("GET /api/v1/dashboard/metrics", h.DashboardMetrics)

	// P0.1 — Attention view ("Что требует меня"): единый срез требующего действия
	mux.HandleFunc("GET /api/v1/attention", h.ListAttention)

	// P1.0 — Projects (мультипроектность)
	mux.HandleFunc("GET /api/v1/projects", h.ListProjects)
	mux.HandleFunc("POST /api/v1/projects", h.CreateProject)
	mux.HandleFunc("GET /api/v1/projects/{id}", h.GetProject)
	mux.HandleFunc("PATCH /api/v1/projects/{id}", h.UpdateProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", h.ArchiveProject)

	// U4 — Config (Settings)
	mux.HandleFunc("GET /api/v1/config", h.ListConfig)
	mux.HandleFunc("PUT /api/v1/config/{key}", h.SetConfigKey)
	mux.HandleFunc("DELETE /api/v1/config/{key}", h.ResetConfigKey)

	var chain http.Handler = mux
	if opts.Audit != nil {
		chain = auditMW(opts.Audit, logger, chain)
	}
	// Slice 6: auth (аутентификация + RBAC) снаружи — audit видит AuthContext
	if opts.Auth != nil {
		chain = authMW(opts.Auth, logger, chain)
	}
	chain = loggingMW(logger, chain)
	chain = recoveryMW(logger, chain)
	chain = requestIDMW(chain)
	return chain
}
