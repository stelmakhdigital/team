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

// RunDiagnostics — POST /api/v1/diagnostics/run (явный полный прогон).
// Fail-severity findings → алерт в Attention (P0.1): новый unread diagnostics_fail (critical, link /health),
// с дедупом (не дублирует, если алерт уже в Attention). Read-only для чеков.
func (h *handlers) RunDiagnostics(w http.ResponseWriter, r *http.Request) {
	if h.diagnostics == nil {
		writeError(w, r, service.NewNotFound("diagnostics service not configured"))
		return
	}
	res := h.diagnostics.Run(r.Context())
	if _, err := h.diagnostics.ReportFail(r.Context(), res); err != nil {
		// алерт — best-effort: не ломаем ответ прогона (чек'и уже выполнены)
		_ = err
	}
	writeJSON(w, http.StatusOK, res)
}
