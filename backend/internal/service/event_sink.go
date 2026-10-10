package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"daemon/internal/repository"
)

// ---------- P2.9: event sink — WSEvent → structured EventIn ----------
//
// События пишутся в таблицу `events` в тот же момент, когда публикуются в
// EventBus (sink вызывается синхронно из Publish — additive, без изменения
// WS-семантики). session.output НЕ пишем (live-транскрипт — поток, не событие).

// eventSkipTypes — типы, которые НЕ записываем в events (live-потоки).
var eventSkipTypes = map[string]bool{
	"session.output": true, // live-транскрипт — поток, не событие
}

func eventSkip(e WSEvent) bool { return eventSkipTypes[e.Type] }

func anyFrom(m map[string]any, k string) any {
	if m == nil {
		return nil
	}
	return m[k]
}

func int64FromAny(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	}
	return 0, false
}

// teamIDFromChannels — из канала "team:<id>" (есть у всех team-событий).
func teamIDFromChannels(channels []string) int64 {
	for _, c := range channels {
		if strings.HasPrefix(c, "team:") {
			if id, err := strconv.ParseInt(strings.TrimPrefix(c, "team:"), 10, 64); err == nil {
				return id
			}
		}
	}
	return 0
}

// parseEvent — WSEvent → (EventIn, ok). ok=false — пропустить (не событие).
func parseEvent(e WSEvent) (*repository.EventIn, bool) {
	if eventSkip(e) {
		return nil, false
	}
	data, _ := e.Data.(map[string]any)
	ev := &repository.EventIn{
		Type:         e.Type,
		EntityKind:   "other",
		EntityID:     0,
		Summary:      e.Type,
		CreatedAtStr: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// team_id: из Data, иначе из каналов
	if tid, ok := int64FromAny(anyFrom(data, "team_id")); ok && tid != 0 {
		ev.TeamID = tid
	} else if tid := teamIDFromChannels(e.Channels); tid != 0 {
		ev.TeamID = tid
	}
	if data != nil {
		switch e.Type {
		case "task.created", "task.state_changed":
			ev.EntityKind = "task"
			ev.EntityID, _ = int64FromAny(anyFrom(data, "task_id"))
			ev.Summary = taskSummary(e.Type, data)
		case "session.started", "session.stopped":
			ev.EntityKind = "session"
			ev.EntityID, _ = int64FromAny(anyFrom(data, "session_id"))
			if rid, ok := int64FromAny(anyFrom(data, "role_id")); ok {
				ev.RoleID = rid
			}
			ev.Summary = sessionSummary(e.Type, data)
		case "message.sent":
			ev.EntityKind = "message"
			ev.EntityID, _ = int64FromAny(anyFrom(data, "message_id"))
			ev.Summary = messageSummary(data)
		case "alert.created":
			ev.EntityKind = "alert"
			ev.EntityID, _ = int64FromAny(anyFrom(data, "alert_id"))
			ev.Summary = alertSummary(data)
		case "workflow.instance_updated":
			ev.EntityKind = "workflow"
			ev.EntityID, _ = int64FromAny(anyFrom(data, "instance_id"))
			ev.Summary = workflowSummary(data)
		}
		if b, err := json.Marshal(data); err == nil {
			ev.Payload = string(b)
		}
	}
	return ev, true
}

// --- summary-генераторы (краткая человекочитаемая строка) ---

func taskSummary(typ string, m map[string]any) string {
	id, _ := int64FromAny(anyFrom(m, "task_id"))
	title := strFromAny(anyFrom(m, "title"))
	to := strFromAny(anyFrom(m, "to_state"))
	from := strFromAny(anyFrom(m, "from_state"))
	switch typ {
	case "task.created":
		if title != "" {
			return fmt.Sprintf("Task #%d created: %s", id, title)
		}
		return fmt.Sprintf("Task #%d created", id)
	default: // state_changed
		if from != "" && to != "" {
			return fmt.Sprintf("Task #%d: %s → %s", id, from, to)
		}
		if to != "" {
			return fmt.Sprintf("Task #%d → %s", id, to)
		}
		return fmt.Sprintf("Task #%d state changed", id)
	}
}

func sessionSummary(typ string, m map[string]any) string {
	id, _ := int64FromAny(anyFrom(m, "session_id"))
	role := strFromAny(anyFrom(m, "role_name"))
	rt := strFromAny(anyFrom(m, "runtime_type"))
	switch typ {
	case "session.started":
		parts := []string{}
		if role != "" {
			parts = append(parts, role)
		}
		if rt != "" {
			parts = append(parts, rt)
		}
		if len(parts) > 0 {
			return fmt.Sprintf("Session #%d started (%s)", id, strings.Join(parts, ", "))
		}
		return fmt.Sprintf("Session #%d started", id)
	default: // stopped
		return fmt.Sprintf("Session #%d stopped", id)
	}
}

func messageSummary(m map[string]any) string {
	from := strFromAny(anyFrom(m, "from_role_name"))
	to := strFromAny(anyFrom(m, "to_role_name"))
	kind := strFromAny(anyFrom(m, "type"))
	switch {
	case from != "" && to != "":
		return fmt.Sprintf("Message: %s → %s (%s)", from, to, kind)
	case from != "":
		return fmt.Sprintf("Message from %s (%s)", from, kind)
	default:
		return fmt.Sprintf("Message sent (%s)", kind)
	}
}

func alertSummary(m map[string]any) string {
	et := strFromAny(anyFrom(m, "event_type"))
	sev := strFromAny(anyFrom(m, "severity"))
	desc := strFromAny(anyFrom(m, "description"))
	if desc != "" {
		return fmt.Sprintf("Alert [%s] %s: %s", sev, et, desc)
	}
	return fmt.Sprintf("Alert [%s] %s", sev, et)
}

func workflowSummary(m map[string]any) string {
	id, _ := int64FromAny(anyFrom(m, "instance_id"))
	st := strFromAny(anyFrom(m, "state"))
	if st != "" {
		return fmt.Sprintf("Workflow instance #%d → %s", id, st)
	}
	return fmt.Sprintf("Workflow instance #%d updated", id)
}
