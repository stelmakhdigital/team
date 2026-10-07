package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"

	"daemon/internal/service"
)

type Options struct {
	Logger  *slog.Logger
	APIKeys []string
	DB      *sql.DB
}

// NewServer собирает mux + middleware + routes.
func NewServer(svc *service.TeamService, tsvc *service.TaskService, opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &handlers{svc: svc, tsvc: tsvc}

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

	// Slice 2 — Dashboard (sessions/alerts — slice 3)
	mux.HandleFunc("GET /api/v1/dashboard/summary", h.DashboardSummary)
	mux.HandleFunc("GET /api/v1/dashboard/tasks", h.DashboardTasks)

	var chain http.Handler = mux
	chain = authMW(validKeySet(opts.APIKeys), chain)
	chain = loggingMW(logger, chain)
	chain = recoveryMW(logger, chain)
	chain = requestIDMW(chain)
	return chain
}

func validKeySet(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return set
}
