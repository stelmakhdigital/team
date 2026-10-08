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
}

// NewServer собирает mux + middleware + routes.
func NewServer(svc *service.TeamService, tsvc *service.TaskService, opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &handlers{svc: svc, tsvc: tsvc, ssvc: opts.Sessions, msvc: opts.Messages,
		wsvc: opts.Workflows, lsvc: opts.Library, asvc: opts.Audit, msvcM: opts.Metrics,
		events: opts.Events, alerts: opts.Alerts}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if opts.DB != nil && opts.DB.Ping() != nil {
			writeError(w, r, &service.AppError{Code: "internal", Status: 503, Message: "database unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

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

	// Team Builder — segments / roles / relatives
	mux.HandleFunc("POST /api/v1/segments/{id}/roles", h.CreateRole)
	mux.HandleFunc("PATCH /api/v1/segments/{id}/layout", h.UpdateSegmentLayout)
	mux.HandleFunc("GET /api/v1/roles/{id}/config", h.GetRoleConfig)
	mux.HandleFunc("PATCH /api/v1/roles/{id}/config", h.UpdateRoleConfig)
	mux.HandleFunc("PATCH /api/v1/roles/{id}/layout", h.UpdateRoleLayout)
	mux.HandleFunc("PATCH /api/v1/relatives/{id}/layout", h.UpdateRelativeLayout)
	mux.HandleFunc("DELETE /api/v1/relatives/{id}", h.DeleteRelative)

	// Slice 2 — Tasks & History
	mux.HandleFunc("GET /api/v1/tasks", h.ListTasks)
	mux.HandleFunc("POST /api/v1/tasks", h.CreateTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}", h.GetTask)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}/state", h.UpdateTaskState)
	mux.HandleFunc("POST /api/v1/tasks/{id}/handoff", h.HandoffTask)
	mux.HandleFunc("GET /api/v1/tasks/{id}/history", h.GetTaskHistory)

	// Slice 2 — Dashboard
	mux.HandleFunc("GET /api/v1/dashboard/summary", h.DashboardSummary)
	mux.HandleFunc("GET /api/v1/dashboard/tasks", h.DashboardTasks)

	// Slice 3 — Sessions & Runtime
	mux.HandleFunc("GET /api/v1/sessions", h.ListSessions)
	mux.HandleFunc("POST /api/v1/sessions", h.CreateSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.GetSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", h.StopSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/history", h.GetSessionHistory)
	mux.HandleFunc("GET /api/v1/sessions/{id}/transcript", h.GetTranscript)
	mux.HandleFunc("GET /api/v1/dashboard/sessions", h.DashboardSessions)
	mux.HandleFunc("GET /api/v1/dashboard/alerts", h.DashboardAlerts)
	mux.HandleFunc("GET /api/v1/watchdog/events", h.WatchdogEvents)
	mux.HandleFunc("POST /api/v1/watchdog/events/{id}/read", h.MarkAlertRead)

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

	// Slice 5 — WebSocket (real-time события)
	mux.HandleFunc("GET /ws", h.HandleWS)

	// Slice 5b — Library
	mux.HandleFunc("GET /api/v1/library", h.ListLibrary)
	mux.HandleFunc("POST /api/v1/library", h.SaveToLibrary)
	mux.HandleFunc("GET /api/v1/library/{id}", h.GetLibraryItem)
	mux.HandleFunc("POST /api/v1/library/{id}/apply", h.ApplyLibraryItem)

	// Slice 5b — History Viewer: audit log + metrics
	mux.HandleFunc("GET /api/v1/audit", h.ListAudit)
	mux.HandleFunc("GET /api/v1/dashboard/metrics", h.DashboardMetrics)

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
