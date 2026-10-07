package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"daemon/internal/service"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func requestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError — единый error envelope (docs/contracts/error-model.md).
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := err.(*service.AppError)
	if !ok {
		appErr = &service.AppError{Code: "internal", Status: 500, Message: "internal server error"}
	}
	status := appErr.Status
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code:      appErr.Code,
		Message:   appErr.Message,
		RequestID: requestID(r.Context()),
		Details:   appErr.Details,
	}})
}

// decodeJSON — разбор body; невалидный JSON → 400.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return service.NewValidation("invalid JSON body: " + err.Error())
	}
	return nil
}

// parseIDParam — path param {id} → int64 (400 при мусоре).
func parseIDParam(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, service.NewValidation("invalid id parameter: " + s)
	}
	return id, nil
}

var (
	errUnauthorized  = &service.AppError{Code: "unauthorized", Status: 401, Message: "missing or invalid X-API-Key"}
	errInternalPanic = &service.AppError{Code: "internal", Status: 500, Message: "internal server error (panic recovered)"}
)
