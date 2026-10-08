package httpapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/service"
)

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}

// requestIDMW — берёт X-Request-Id из запроса или генерирует; кладёт в ctx и ответ.
func requestIDMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = newRequestID()
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := contextWithRequestID(r.Context(), rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggingMW(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("http",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
			slog.String("request_id", requestID(r.Context())),
		)
	})
}

func recoveryMW(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic", slog.Any("recover", rec),
					slog.String("request_id", requestID(r.Context())))
				writeError(w, r, errInternalPanic)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// authContextKey — request context с AuthContext (slice 6, RBAC).
type authContextKey struct{}

func withAuthContext(ctx context.Context, c *models.AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, c)
}

func authContext(ctx context.Context) *models.AuthContext {
	c, _ := ctx.Value(authContextKey{}).(*models.AuthContext)
	return c
}

// authMW — проверка API-ключа + RBAC (slice 6; ADR-003, ТЗ 06 §2.1).
// Auth включён, если заданы ключи (env DAEMON_API_KEYS или api_keys в БД).
// Ключ: `X-API-Key` / `Authorization: Bearer` / `?api_key=` (WS).
// Без права на endpoint → 403 forbidden.
func authMW(auth *service.AuthService, logger *slog.Logger, next http.Handler) http.Handler {
	if !auth.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			if authH := r.Header.Get("Authorization"); strings.HasPrefix(authH, "Bearer ") {
				key = strings.TrimPrefix(authH, "Bearer ")
			}
		}
		if key == "" {
			// WS (браузер не шлёт заголовки) и утилиты: ?api_key=
			key = r.URL.Query().Get("api_key")
		}
		authCtx, err := auth.ValidateAPIKey(r.Context(), key)
		if err != nil || authCtx == nil {
			logger.Warn("auth: invalid api key",
				slog.String("path", r.URL.Path),
				slog.String("request_id", requestID(r.Context())))
			writeError(w, r, errUnauthorized)
			return
		}
		if perm := requiredPermission(r.Method, r.URL.Path); perm != "" && !authCtx.HasPerm(perm) {
			logger.Warn("auth: permission denied",
				slog.String("perm", perm),
				slog.String("user", authCtx.UserName),
				slog.String("path", r.URL.Path))
			writeError(w, r, errForbidden)
			return
		}
		ctx := withAuthContext(r.Context(), authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requiredPermission — (method, path) → permission (RBAC, slice 6).
// Пустая строка — без ограничения (healthz/readyz/не-api пути).
func requiredPermission(method, path string) string {
	if path == "/ws" {
		return "dashboard.read"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return "" // healthz/readyz/прочее
	}
	res := parts[2]
	id := ""
	if len(parts) > 3 {
		id = parts[3]
	}
	sub := ""
	if len(parts) > 4 {
		sub = parts[4]
	}
	switch method {
	case http.MethodGet:
		switch res {
		case "dashboard", "watchdog":
			return "dashboard.read"
		case "audit":
			return "audit.read"
		case "segments", "roles", "relatives":
			return "teams.read" // Team Builder: сегменты/роли/связи
		default:
			return res + ".read"
		}
	case http.MethodPost:
		switch {
		case res == "chatrooms" && sub == "messages":
			return "chatrooms.create"
		case res == "workflows" && id != "" && (sub == "blocks" || sub == "connections"):
			return "workflows.update"
		case res == "library" && id != "" && sub == "apply":
			return "library.apply"
		case res == "watchdog":
			return "dashboard.read" // mark alert read
		case res == "teams" && id != "" && (sub == "segments" || sub == "relatives" || sub == "save" || sub == "validate"):
			return "teams.update"
		case res == "segments" && id != "" && sub == "roles":
			return "teams.update"
		default:
			return res + ".create"
		}
	case http.MethodPatch:
		if res == "roles" && id != "" && sub == "config" {
			return "config.update"
		}
		switch res {
		case "segments", "roles", "relatives":
			return "teams.update" // layout-правки Team Builder
		default:
			return res + ".update"
		}
	case http.MethodDelete:
		switch res {
		case "teams":
			return "teams.delete"
		case "sessions":
			return "sessions.stop"
		case "relatives":
			return "teams.update"
		default:
			return res + ".update"
		}
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

// Hijack — прокидка внутрь (WebSocket upgrade, gorilla).
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
