package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsCollector — читает WS-события в фоновом режиме (gorilla: после read-таймаута
// соединение «corrupt», поэтому один reader на всё тестирование).
type wsCollector struct {
	mu       sync.Mutex
	events   []map[string]any
	notified chan struct{}
}

func (c *wsCollector) start(t *testing.T, conn *websocket.Conn, total time.Duration) {
	t.Helper()
	c.notified = make(chan struct{})
	go func() {
		deadline := time.Now().Add(total)
		for {
			conn.SetReadDeadline(deadline)
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev map[string]any
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Errorf("decode ws event: %v (raw=%s)", err, raw)
				continue
			}
			c.mu.Lock()
			c.events = append(c.events, ev)
			n := len(c.events)
			c.mu.Unlock()
			select {
			case c.notified <- struct{}{}:
			default:
			}
			if n == 1 {
				// сигнал «хотя бы одно событие»
			}
		}
	}()
}

// waitEvent — ждёт n-е событие (1-based) или timeout.
func (c *wsCollector) waitEvent(t *testing.T, n int, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.events) >= n {
			ev := c.events[n-1]
			c.mu.Unlock()
			return ev
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("event #%d not received within %v; got %d: %+v", n, timeout, len(c.events), c.events)
	return nil
}

func (c *wsCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// snapshot — копия полученных событий (тестовый скан без блокировок/таймаутов).
func (c *wsCollector) snapshot() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]any, len(c.events))
	copy(out, c.events)
	return out
}

// TestWSSubscribeAndEvents — connect → событие ДО subscribe не приходит →
// subscribe → REST-действия → события приходят по каналам.
func TestWSSubscribeAndEvents(t *testing.T) {
	srv := newTestServer(t, nil)

	st, out, _ := do(t, "POST", srv.URL+"/api/v1/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	workerID := topologyRoleID(t, srv, teamID, "worker")

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()
	c := &wsCollector{}
	c.start(t, conn, 15*time.Second)

	// task.created ДО subscribe — на team-канале, но подписки нет → не должен прийти
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/tasks", map[string]any{
		"team_id": teamID, "destination_role_id": workerID, "title": "ws-task",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create task: %d", st)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "subscribe", "channels": []string{"team:" + itoa(teamID)},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// task.state_changed (team-канал) → первое полученное событие
	st, _, _ = do(t, "PATCH", srv.URL+"/api/v1/tasks/1/state",
		map[string]any{"state": "done", "closure_reason": "no_follow_on"}, nil)
	if st != http.StatusOK {
		t.Fatalf("task state: %d", st)
	}
	ev := c.waitEvent(t, 1, 3*time.Second)
	if ev["type"] != "task.state_changed" {
		t.Fatalf("event #1 = %v, want task.state_changed (task.created до subscribe пропущен)", ev["type"])
	}
	data, _ := ev["data"].(map[string]any)
	if data == nil || data["to_state"] != "done" {
		t.Fatalf("task.state_changed data = %v", ev["data"])
	}
	if ts, _ := ev["timestamp"].(string); ts == "" {
		t.Fatal("timestamp missing")
	}

	// chatroom-сообщение: события идут на chatroom:{id} И на team:{id}
	// (лента активности команды) → должно прийти на нашу подписку
	st, out, _ = do(t, "GET", srv.URL+"/api/v1/chatrooms?team_id="+itoa(teamID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("chatrooms: %d", st)
	}
	var chatroomID int64
	for _, raw := range out["chatrooms"].([]any) {
		room := raw.(map[string]any)
		if room["name"] == "backend-general" {
			chatroomID = int64(room["id"].(float64))
		}
	}
	if chatroomID == 0 {
		t.Fatal("backend-general chatroom not found")
	}
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/chatrooms/"+itoa(chatroomID)+"/messages",
		map[string]any{"body": "hi"}, nil)
	if st != http.StatusCreated {
		t.Fatalf("chatroom message: %d", st)
	}
	ev = c.waitEvent(t, 2, 3*time.Second)
	if ev["type"] != "message.sent" {
		t.Fatalf("event #2 = %v, want message.sent (chatroom)", ev["type"])
	}
	data, _ = ev["data"].(map[string]any)
	if data["type"] != "chatroom" || itoaInt64(data["chatroom_id"]) != chatroomID {
		t.Fatalf("chatroom message.sent data = %v, want chatroom_id=%d", data, chatroomID)
	}

	// broadcast (team-канал) → message.sent
	st, _, _ = do(t, "POST", srv.URL+"/api/v1/messages", map[string]any{
		"team_id": teamID, "type": "broadcast", "body": "ws-broadcast",
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("broadcast: %d", st)
	}
	ev = c.waitEvent(t, 3, 3*time.Second)
	if ev["type"] != "message.sent" {
		t.Fatalf("event #3 = %v, want message.sent", ev["type"])
	}
	data, _ = ev["data"].(map[string]any)
	if data["type"] != "broadcast" || data["body"] != "ws-broadcast" {
		t.Fatalf("message.sent data = %v", data)
	}
}

func itoaInt64(v any) int64 {
	f, ok := v.(float64)
	if !ok {
		return -1
	}
	return int64(f)
}

// TestWSAuth — auth: ?api_key= проходит, без/неверный — 401.
func TestWSAuth(t *testing.T) {
	srv := newTestServer(t, []string{"test-key"})

	if _, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Listener.Addr().String()+"/ws", nil); err == nil {
		t.Fatal("ws dial without key succeeded, want 401")
	}
	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+srv.Listener.Addr().String()+"/ws?api_key=test-key", nil)
	if err != nil {
		t.Fatalf("ws dial with key: %v (resp=%v)", err, resp)
	}
	conn.Close()
	if _, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Listener.Addr().String()+"/ws?api_key=wrong", nil); err == nil {
		t.Fatal("ws dial with wrong key succeeded, want 401")
	}
}

func topologyRoleID(t *testing.T, srv *httptest.Server, teamID int64, name string) int64 {
	t.Helper()
	_, out, _ := do(t, "GET", srv.URL+"/api/v1/teams/"+itoa(teamID)+"/topology", nil, nil)
	for _, raw := range out["roles"].([]any) {
		r := raw.(map[string]any)
		if r["name"] == name {
			return int64(r["id"].(float64))
		}
	}
	t.Fatalf("role %q not found in topology", name)
	return 0
}
