package httpapi

import (
	"encoding/json"
	"net/http"

	"daemon/internal/service"
)

// ---------- Config / Settings (U4, контракт config-contract) ----------

var errConfigNotWired = &service.AppError{Code: "internal", Status: 503, Message: "config service not wired"}

// ListConfig — GET /api/v1/config: все ключи по секциям + meta + environment.
func (h *handlers) ListConfig(w http.ResponseWriter, r *http.Request) {
	if h.cvc == nil {
		writeError(w, r, errConfigNotWired)
		return
	}
	resp, err := h.cvc.ListConfig(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetConfigKey — PUT /api/v1/config/{key}: установить override (body: {"value": any}).
func (h *handlers) SetConfigKey(w http.ResponseWriter, r *http.Request) {
	if h.cvc == nil {
		writeError(w, r, errConfigNotWired)
		return
	}
	key := r.PathValue("key")
	var body struct {
		Value json.RawMessage `json:"value"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if len(body.Value) == 0 || string(body.Value) == "null" {
		writeError(w, r, service.NewValidation(`request body must contain "value"`))
		return
	}
	res, err := h.cvc.SetOverride(r.Context(), key, body.Value)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ResetConfigKey — DELETE /api/v1/config/{key}: сбросить override (идемпотентно).
func (h *handlers) ResetConfigKey(w http.ResponseWriter, r *http.Request) {
	if h.cvc == nil {
		writeError(w, r, errConfigNotWired)
		return
	}
	key := r.PathValue("key")
	res, err := h.cvc.ResetOverride(r.Context(), key)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
