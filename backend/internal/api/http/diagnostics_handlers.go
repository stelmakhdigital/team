package httpapi

import (
	"net/http"

	"daemon/internal/service"
)

// P1.8 — Health/Doctor-панель: «the surface that shows the truth».
// Read-only: GET /api/v1/diagnostics (быстрый срез) + POST /api/v1/diagnostics/run
// (явный полный прогон). Оба — RBAC diagnostics.read (operator+).

// GetDiagnostics — GET /api/v1/diagnostics.
func (h *handlers) GetDiagnostics(w http.ResponseWriter, r *http.Request) {
	if h.diagnostics == nil {
		writeError(w, r, service.NewNotFound("diagnostics service not configured"))
		return
	}
	res := h.diagnostics.Run(r.Context())
	writeJSON(w, http.StatusOK, res)
}

// RunDiagnostics — POST /api/v1/diagnostics/run (явный полный прогон, read-only).
func (h *handlers) RunDiagnostics(w http.ResponseWriter, r *http.Request) {
	if h.diagnostics == nil {
		writeError(w, r, service.NewNotFound("diagnostics service not configured"))
		return
	}
	res := h.diagnostics.Run(r.Context())
	writeJSON(w, http.StatusOK, res)
}
