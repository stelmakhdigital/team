package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// Slice 4 — Message Center: полная вертикаль по HTTP.
func TestMessageCenterVertical(t *testing.T) {
	srv := newTestServer(t, nil)

	// 1. создание команды (spec: 2 сегмента, 3 роли)
	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: status=%d body=%v", st, out)
	}
	teamID := int64(out["id"].(float64))

	// 2. chatrooms: 1 team-level + 2 segment-level (авто-создание)
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/chatrooms?team_id="+itoa(teamID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("chatrooms: status=%d body=%v", st, out)
	}
	rooms := out["chatrooms"].([]any)
	if len(rooms) != 3 {
		t.Fatalf("chatrooms = %d, want 3: %v", len(rooms), rooms)
	}
	var coreRoomID int64
	for _, raw := range rooms {
		room := raw.(map[string]any)
		if room["name"] == "backend-general" {
			coreRoomID = int64(room["id"].(float64))
			if n, _ := room["members_count"].(float64); n != 2 {
				t.Fatalf("backend-general members_count = %v, want 2", room["members_count"])
			}
		}
	}
	if coreRoomID == 0 {
		t.Fatalf("core room not found: %v", rooms)
	}

	// 3. отправка в чат → 201, last_message = «You»
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/chatrooms/"+itoa(coreRoomID)+"/messages",
		map[string]any{"body": "hello team"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("send chatroom message: status=%d body=%v", st, out)
	}
	if out["status"] != "sent" {
		t.Fatalf("send status = %v, want sent", out["status"])
	}

	// 4. лента чата: 1 сообщение, is_mine=true, has_more=false
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/chatrooms/"+itoa(coreRoomID)+"/messages?limit=10", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("chatroom messages: status=%d body=%v", st, out)
	}
	cmsgs := out["messages"].([]any)
	if len(cmsgs) != 1 || out["has_more"] != false {
		t.Fatalf("chatroom messages: len=%d has_more=%v, want 1/false", len(cmsgs), out["has_more"])
	}
	cm := cmsgs[0].(map[string]any)
	if cm["body"] != "hello team" || cm["is_mine"] != true || cm["from_role_name"] != "You" {
		t.Fatalf("chatroom message view = %v", cm)
	}

	// 5. пустое тело → 400
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/chatrooms/"+itoa(coreRoomID)+"/messages",
		map[string]any{"body": ""}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("empty chatroom body: status=%d, want 400", st)
	}

	// 6. неизвестный чат → 404
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/chatrooms/999/messages",
		map[string]any{"body": "x"}, nil)
	if st != http.StatusNotFound {
		t.Fatalf("unknown chatroom: status=%d, want 404", st)
	}

	// 7. last_message в списке чатов
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/chatrooms?team_id="+itoa(teamID), nil, nil)
	rooms = out["chatrooms"].([]any)
	for _, raw := range rooms {
		room := raw.(map[string]any)
		if int64(room["id"].(float64)) == coreRoomID {
			lm, _ := room["last_message"].(map[string]any)
			if lm == nil || lm["body"] != "hello team" || lm["from_role_name"] != "You" {
				t.Fatalf("last_message = %v", room["last_message"])
			}
		}
	}

	// 8. direct-сообщение: 201 + delivered_to
	// id ролей берём из topology
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/teams/"+itoa(teamID)+"/topology", nil, nil)
	if st != http.StatusOK {
		t.Fatalf("topology: status=%d", st)
	}
	top := out
	roles := top["roles"].([]any)
	var workerID int64
	for _, raw := range roles {
		r := raw.(map[string]any)
		if r["name"] == "worker" {
			workerID = int64(r["id"].(float64))
		}
	}
	if workerID == 0 {
		t.Fatalf("worker role not found in topology")
	}
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "to_role_id": workerID, "type": "direct", "body": "ping worker",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("send message: status=%d body=%v", st, out)
	}
	if out["status"] != "sent" {
		t.Fatalf("send message status = %v, want sent", out["status"])
	}
	delivered := out["delivered_to"].([]any)
	if len(delivered) != 1 || int64(delivered[0].(float64)) != workerID {
		t.Fatalf("delivered_to = %v, want [%d]", delivered, workerID)
	}

	// 9. broadcast → 3 роли
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "type": "broadcast", "body": "standup",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("broadcast: status=%d body=%v", st, out)
	}
	delivered = out["delivered_to"].([]any)
	if len(delivered) != 3 {
		t.Fatalf("broadcast delivered_to = %v, want 3", delivered)
	}

	// 10. segment (to worker → сегмент backend: lead+worker)
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "to_role_id": workerID, "type": "segment", "body": "core-sync",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("segment: status=%d body=%v", st, out)
	}
	delivered = out["delivered_to"].([]any)
	if len(delivered) != 2 {
		t.Fatalf("segment delivered_to = %v, want 2", delivered)
	}

	// 11. валидация: unknown team 404, system type 400, direct без to_role 400
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": 999, "type": "direct", "body": "x",
	}, nil)
	if st != http.StatusNotFound {
		t.Fatalf("unknown team: status=%d, want 404", st)
	}
	st, out, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "type": "system", "body": "x",
	}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("system type: status=%d body=%v, want 400", st, out)
	}
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "type": "direct", "body": "x",
	}, nil)
	if st != http.StatusBadRequest {
		t.Fatalf("direct without to_role: status=%d, want 400", st)
	}

	// 12. список сообщений: 3 шт., фильтры, has_more
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/messages?team_id="+itoa(teamID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("list messages: status=%d body=%v", st, out)
	}
	if out["total"].(float64) != 3 {
		t.Fatalf("total = %v, want 3", out["total"])
	}
	msgs := out["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["is_mine"] != true {
		t.Fatalf("operator message is_mine = %v, want true", first["is_mine"])
	}
	if first["to_role_name"] == nil {
		t.Fatal("to_role_name missing")
	}

	st, out, _ = do(t, "GET", srv.URL+"/api/v1/messages?team_id="+itoa(teamID)+"&to_role_id="+itoa(workerID), nil, nil)
	if out["total"].(float64) != 2 {
		t.Fatalf("by to_role total = %v, want 2", out["total"])
	}

	st, out, _ = do(t, "GET", srv.URL+"/api/v1/messages?team_id="+itoa(teamID)+"&limit=1", nil, nil)
	if out["has_more"] != true {
		t.Fatalf("has_more = %v, want true (limit=1, total=3)", out["has_more"])
	}
	msgs = out["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("limit=1: len = %d, want 1", len(msgs))
	}
}

func itoa(v int64) string {
	return fmt.Sprintf("%d", v)
}
