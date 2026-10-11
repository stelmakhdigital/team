package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P3.16: Mode/permission posture (delegated vs human-led) ----------

// postureMW — интeрцептор: перед чувствительной операцией проверяет effective posture.
// human_led + sensitive → park (202 + human_request); delegated → пропуск.
// Bypass: X-Posture-Approved: <id> (replay approved-операции, один раз).
func postureMW(ps *service.PostureService, logger *slog.Logger, next http.Handler) http.Handler {
	if ps == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		// bypass: approved replay (X-Posture-Approved: <id>) — one-time.
		if idStr := r.Header.Get("X-Posture-Approved"); idStr != "" {
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				if ok, _ := ps.MarkReplayed(r.Context(), id); ok {
					next.ServeHTTP(w, r)
					return
				}
				// не approved / уже replayed → 403 (bypass отклонён)
				writeError(w, r, &service.AppError{Code: "forbidden", Status: http.StatusForbidden,
					Message: "posture replay not allowed (not approved or already replayed)"})
				return
			}
		}
		// только sensitive операции
		_, _, sensitive := service.MatchSensitive(r.Method, r.URL.Path)
		if !sensitive {
			next.ServeHTTP(w, r)
			return
		}
		// прочитать body (bounded 64KB) + вернуть для downstream.
		var bodyStr string
		if r.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			bodyStr = string(b)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		park, reqID, operation, err := ps.Check(r.Context(), r.Method, r.URL.Path, bodyStr, extractAPIKey(r))
		if err != nil {
			// fail-open: при ошибке проверки не блокируем (лог + пропуск).
			logger.Warn("posture: check failed (fail-open)", "err", err, "path", r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}
		if park {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"parked":           true,
				"human_request_id": reqID,
				"operation":        operation,
				"message":          "операция требует подтверждения (human-led scope)",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractAPIKey — ключ из запроса (X-API-Key / Bearer / ?api_key=) — для replay.
func extractAPIKey(r *http.Request) string {
	key := r.Header.Get("X-API-Key")
	if key == "" {
		if authH := r.Header.Get("Authorization"); strings.HasPrefix(authH, "Bearer ") {
			key = strings.TrimPrefix(authH, "Bearer ")
		}
	}
	if key == "" {
		key = r.URL.Query().Get("api_key")
	}
	return key
}

// ---------- Posture get/set ----------

type setPostureBody struct {
	Posture *string `json:"posture"` // null = сессия наследует от команды
}

// SetTeamPosture — PATCH /api/v1/teams/{id}/posture (RBAC config.update, admin).
func (h *handlers) SetTeamPosture(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body setPostureBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.Posture == nil || !models.Posture(*body.Posture).Valid() {
		writeError(w, r, service.NewValidation("posture must be 'delegated' or 'human_led'"))
		return
	}
	if err := h.posture.SetTeamPosture(r.Context(), id, models.Posture(*body.Posture)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "posture": *body.Posture, "status": "updated"})
}

// SetSessionPosture — PATCH /api/v1/sessions/{id}/posture (RBAC sessions.update, operator+).
func (h *handlers) SetSessionPosture(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body setPostureBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if body.Posture != nil && !models.Posture(*body.Posture).Valid() {
		writeError(w, r, service.NewValidation("posture must be 'delegated' or 'human_led'"))
		return
	}
	var p *models.Posture
	if body.Posture != nil {
		v := models.Posture(*body.Posture)
		p = &v
	}
	if err := h.posture.SetSessionPosture(r.Context(), id, p); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "posture": body.Posture, "status": "updated"})
}

// ---------- Human-request get / decide (approve/deny + replay) ----------

// HumanRequestView — паркованная операция (auth_key НЕ раскрывается).
type HumanRequestView struct {
	ID             int64   `json:"id"`
	ScopeType      string  `json:"scope_type"`
	ScopeID        *int64  `json:"scope_id,omitempty"`
	TeamID         *int64  `json:"team_id,omitempty"`
	Method         string  `json:"method"`
	Path           string  `json:"path"`
	Body           *string `json:"body,omitempty"`
	Operation      string  `json:"operation"`
	Reversible     bool    `json:"reversible"`
	Status         string  `json:"status"`
	DecisionReason *string `json:"decision_reason,omitempty"`
	DecidedBy      *string `json:"decided_by,omitempty"`
	DecidedAt      *string `json:"decided_at,omitempty"`
	ReplayStatus   *int    `json:"replay_status,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func humanRequestView(hr *models.HumanRequest) HumanRequestView {
	var decidedAt *string
	if hr.DecidedAt != nil {
		s := hr.DecidedAt.UTC().Format(time.RFC3339)
		decidedAt = &s
	}
	return HumanRequestView{
		ID: hr.ID, ScopeType: hr.ScopeType, ScopeID: hr.ScopeID, TeamID: hr.TeamID,
		Method: hr.Method, Path: hr.Path, Body: hr.Body, Operation: hr.Operation,
		Reversible: hr.Reversible, Status: string(hr.Status),
		DecisionReason: hr.DecisionReason, DecidedBy: hr.DecidedBy, DecidedAt: decidedAt,
		ReplayStatus: hr.ReplayStatus,
		CreatedAt:    hr.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    hr.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// GetHumanRequest — GET /api/v1/human-requests/{id} (RBAC dashboard.read, viewer-ok).
func (h *handlers) GetHumanRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	hr, err := h.posture.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, humanRequestView(hr))
}

// ListHumanRequests — GET /api/v1/human-requests (RBAC dashboard.read, viewer-ok).
// Только pending (требуют решения).
func (h *handlers) ListHumanRequests(w http.ResponseWriter, r *http.Request) {
	items, err := h.posture.ListPending(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	views := make([]HumanRequestView, 0, len(items))
	for _, hr := range items {
		views = append(views, humanRequestView(hr))
	}
	writeJSON(w, http.StatusOK, map[string]any{"human_requests": views, "total": len(views)})
}

type decideBody struct {
	Decision string  `json:"decision"` // approve | deny
	Reason   *string `json:"reason"`
}

// DecideHumanRequest — POST /api/v1/human-requests/{id}/decide (RBAC tasks.update, operator+).
// approve → replay оригинального запроса (через полный chain: auth+RBAC, bypass только park).
func (h *handlers) DecideHumanRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body decideBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	var reason string
	if body.Reason != nil {
		reason = *body.Reason
	}
	hr, err := h.posture.Decide(r.Context(), id, body.Decision, reason, actorName(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	// approve → replay (однократно, через auth+RBAC).
	if body.Decision == "approve" {
		status := h.replayRequest(r.Context(), hr)
		_ = h.posture.SetReplayStatus(r.Context(), id, status)
		hr, _ = h.posture.Get(r.Context(), id) // обновить статус (replayed) + replay_status
	}
	writeJSON(w, http.StatusOK, humanRequestView(hr))
}

// replayRequest — реконструирует оригинальный запрос + прогоняет через полный chain.
// Bypass только park (X-Posture-Approved); auth+RBAC выполняются как обычно.
// Возвращает HTTP-статус replay.
func (h *handlers) replayRequest(ctx context.Context, hr *models.HumanRequest) int {
	if h.chain == nil {
		return http.StatusInternalServerError
	}
	var body *bytes.Reader
	if hr.Body != nil {
		body = bytes.NewReader([]byte(*hr.Body))
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, hr.Method, hr.Path, body)
	if err != nil {
		return http.StatusInternalServerError
	}
	if hr.AuthKey != nil {
		req.Header.Set("X-API-Key", *hr.AuthKey)
	}
	req.Header.Set("X-Posture-Approved", strconv.FormatInt(hr.ID, 10))
	rec := httptest.NewRecorder()
	h.chain.ServeHTTP(rec, req)
	return rec.Code
}
