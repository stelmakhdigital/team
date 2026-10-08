package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"daemon/internal/models"
	"daemon/internal/service"
)

// auditMW — запись в audit_log для мутаций (контракт 20 §6.3).
// Записывает только успешные (2xx) POST/PATCH/DELETE.
// Slice 6: user_id/user_name/api_key_id — из AuthContext (authMW снаружи).
func auditMW(audit *service.AuditService, logger *slog.Logger, next http.Handler) http.Handler {
	if audit == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 200 && rec.status < 300 {
			action, resource := auditAction(r.Method, r.URL.Path)
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			if ip == "" {
				ip = r.RemoteAddr
			}
			entry := models.AuditEntry{
				Action:    action,
				Resource:  strPtrOrNil(resource),
				IPAddress: &ip,
				UserAgent: strPtrOrNil(r.UserAgent()),
			}
			if ac := authContext(r.Context()); ac != nil && ac.Authenticated {
				entry.UserID = ac.UserID
				entry.UserName = strPtrOrNil(ac.UserName)
				entry.APIKeyID = ac.APIKeyID
			} else {
				userName := "operator"
				entry.UserName = &userName
			}
			_ = audit.Record(r.Context(), entry)
		}
	})
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// auditAction — (method, path) → (action, resource).
func auditAction(method, path string) (string, string) {
	// path: /api/v1/<resource>[/<id>][/<sub>...]
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return strings.ToLower(method), path
	}
	res := parts[2]
	id := ""
	if len(parts) > 3 {
		id = parts[3]
	}
	resID := ""
	if id != "" {
		resID = res + ":" + id
	}
	switch {
	case res == "sessions" && id == "" && method == http.MethodPost:
		return "session.start", "session"
	case res == "sessions" && id != "" && method == http.MethodDelete:
		return "session.stop", resID
	case res == "sessions" && id != "":
		return "session.update", resID
	case res == "sessions" && method == http.MethodPost:
		return "session.start", "session"
	case res == "tasks" && id != "" && parts[4] == "state":
		return "task.state_update", resID
	case res == "tasks" && id != "" && parts[4] == "handoff":
		return "task.handoff", resID
	case res == "tasks" && method == http.MethodPost:
		return "task.create", "task"
	case res == "tasks":
		return "task.update", resID
	case res == "teams" && method == http.MethodPost:
		return "team.create", "team"
	case res == "teams" && method == http.MethodDelete:
		return "team.archive", resID
	case res == "teams" && id != "" && len(parts) > 4:
		sub := parts[4]
		switch sub {
		case "segments":
			return "segment.create", "team:" + id
		case "relatives":
			return "relative.create", "team:" + id
		case "save":
			return "team.save", resID
		case "validate":
			return "team.validate", resID
		}
		return "team.update", resID
	case res == "teams":
		return "team.update", resID
	case res == "segments" && id != "" && parts[4] == "roles":
		return "role.create", "segment:" + id
	case res == "segments" && id != "" && parts[4] == "layout":
		return "segment.layout_update", "segment:" + id
	case res == "segments":
		return "segment.update", resID
	case res == "roles":
		return "role.update", resID
	case res == "relatives" && method == http.MethodDelete:
		return "relative.delete", resID
	case res == "relatives":
		return "relative.update", resID
	case res == "messages" && method == http.MethodPost:
		return "message.send", "message"
	case res == "chatrooms" && id != "" && parts[4] == "messages" && method == http.MethodPost:
		return "chatroom.message_send", "chatroom:" + id
	case res == "workflows" && method == http.MethodPost:
		return "workflow.create", "workflow"
	case res == "workflows" && id != "":
		if len(parts) > 4 {
			switch parts[4] {
			case "blocks":
				return "workflow.block_create", "workflow:" + id
			case "connections":
				return "workflow.connection_create", "workflow:" + id
			}
		}
		return "workflow.update", resID
	case res == "library" && method == http.MethodPost:
		return "library.save", "library"
	case res == "library" && id != "" && parts[4] == "apply":
		return "library.apply", "library:" + id
	case res == "watchdog" && id != "" && len(parts) > 4:
		return "watchdog.alert_read", "watchdog:" + id
	default:
		action := strings.ToLower(method) + "_" + res
		return action, resID
	}
}
