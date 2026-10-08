package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- Slice 7: live-метрики сессий + session.output (HTTP e2e) ----------

// TestSessionLiveFieldsHTTP — GET /sessions/:id:
//   - JSONL usage в transcript → context-поля присутствуют (не 0, не omit);
//   - обычный вывод (TUI-подобный, без usage) → context-поля OMIT (не 0!);
//   - log_path — всегда; model — только pi (process → omit).
func TestSessionLiveFieldsHTTP(t *testing.T) {
	srv := newTestServer(t, nil)
	base := srv.URL + "/api/v1"

	st, out, _ := do(t, "POST", base+"/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	leadID := topologyRoleID(t, srv, teamID, "lead")
	workerID := topologyRoleID(t, srv, teamID, "worker")

	// 1) сессия с JSONL usage-записью → context-поля заполнены
	st, out, _ = do(t, "POST", base+"/sessions?team_id="+itoa(teamID), map[string]any{
		"role_id": leadID, "command": "sh",
		"args": []string{"-c",
			`printf '{"message":{"usage":{"input_tokens":20000,"cache_read_input_tokens":0,"output_tokens":10}}}\n'; sleep 30`},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create session: %d %v", st, out)
	}
	usageSess := int64(out["id"].(float64))
	time.Sleep(400 * time.Millisecond) // процесс успел записать строку

	st, out, _ = do(t, "GET", base+"/sessions/"+itoa(usageSess), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get session: %d %v", st, out)
	}
	if out["log_path"] == nil || out["log_path"] == "" {
		t.Error("log_path missing")
	}
	if v, ok := out["context_used_percentage"]; !ok {
		t.Error("context_used_percentage omitted, want ~10 (20000/200000)")
	} else if f, _ := v.(float64); f < 9.99 || f > 10.01 {
		t.Errorf("context_used_percentage = %v, want ~10", v)
	}
	if v, ok := out["context_total_input_tokens"]; !ok || int64(v.(float64)) != 20000 {
		t.Errorf("context_total_input_tokens = %v, want 20000", out["context_total_input_tokens"])
	}
	if v, ok := out["context_total_output_tokens"]; !ok || int64(v.(float64)) != 10 {
		t.Errorf("context_total_output_tokens = %v, want 10", out["context_total_output_tokens"])
	}
	if _, ok := out["model"]; ok {
		t.Errorf("model must be omitted for process runtime, got %v", out["model"])
	}
	st, _, _ = do(t, "DELETE", base+"/sessions/"+itoa(usageSess), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("stop session 1: %d", st)
	}

	// 2) обычный вывод без usage → context-поля OMIT (честность: не 0!)
	st, out, _ = do(t, "POST", base+"/sessions?team_id="+itoa(teamID), map[string]any{
		"role_id": workerID, "command": "sh", "args": []string{"-c", "echo plain-output; sleep 30"},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create session 2: %d %v", st, out)
	}
	plainSess := int64(out["id"].(float64))
	time.Sleep(400 * time.Millisecond)

	st, out, _ = do(t, "GET", base+"/sessions/"+itoa(plainSess), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("get session 2: %d %v", st, out)
	}
	if out["log_path"] == nil || out["log_path"] == "" {
		t.Error("log_path missing (plain session)")
	}
	for _, f := range []string{"context_used_percentage", "context_total_input_tokens", "context_total_output_tokens"} {
		if _, ok := out[f]; ok {
			t.Errorf("%s must be omitted without usage, got %v", f, out[f])
		}
	}
	if _, ok := out["model"]; ok {
		t.Errorf("model must be omitted for process runtime, got %v", out["model"])
	}
	st, _, _ = do(t, "DELETE", base+"/sessions/"+itoa(plainSess), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("stop session 2: %d", st)
	}
}

// TestSessionOutputWSHTTP — WS e2e: connect → subscribe session:{id} →
// сессия пишет строки → session.output с lines[] {ts,text,stream};
// молчание → тишина (нет событий без новых строк); stop → тейлер остановлен.
func TestSessionOutputWSHTTP(t *testing.T) {
	srv := newTestServer(t, nil)
	base := srv.URL + "/api/v1"

	st, out, _ := do(t, "POST", base+"/teams", specBody(), nil)
	if st != http.StatusCreated {
		t.Fatalf("create team: %d %v", st, out)
	}
	teamID := int64(out["id"].(float64))
	leadID := topologyRoleID(t, srv, teamID, "lead")

	// строки появятся ПОСЛЕ подключения WS (sleep 0.6)
	st, out, _ = do(t, "POST", base+"/sessions?team_id="+itoa(teamID), map[string]any{
		"role_id": leadID, "command": "sh",
		"args": []string{"-c", "sleep 0.6; echo ws-line-1; echo ws-line-2; sleep 30"},
	}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create session: %d %v", st, out)
	}
	sessID := int64(out["id"].(float64))

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Listener.Addr().String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()
	c := &wsCollector{}
	c.start(t, conn, 15*time.Second)

	if err := conn.WriteJSON(map[string]any{
		"type": "subscribe", "channels": []string{"session:" + itoa(sessID), "dashboard"},
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// ждём event с ws-line-1 (батч ≤500ms после появления строк)
	var got map[string]any
	deadline := time.Now().Add(8 * time.Second)
forScan:
	for time.Now().Before(deadline) {
		for _, ev := range c.snapshot() {
			if ev["type"] != "session.output" {
				continue
			}
			d, _ := ev["data"].(map[string]any)
			if d == nil {
				continue
			}
			if id, _ := d["session_id"].(float64); int64(id) != sessID {
				continue
			}
			lines, _ := d["lines"].([]any)
			for _, l := range lines {
				if s, _ := l.(map[string]any)["text"].(string); s == "ws-line-1" {
					got = ev
					break forScan
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got == nil {
		t.Fatalf("session.output with ws-line-1 not received; events: %+v", c.events)
	}
	data, _ := got["data"].(map[string]any)
	if rn, _ := data["role_name"].(string); rn != "lead" {
		t.Errorf("role_name = %v, want lead", data["role_name"])
	}
	lines := data["lines"].([]any)
	l0 := lines[0].(map[string]any)
	if l0["stream"] != "stdout" {
		t.Errorf("stream = %v, want stdout", l0["stream"])
	}
	if ts, _ := l0["ts"].(string); ts == "" {
		t.Error("line ts missing")
	}
	if _, ok := got["timestamp"]; !ok {
		t.Error("event timestamp missing")
	}

	// молчание: строки выведены, процесс спит → 1.3s тишины (нет событий без новых строк)
	silentStart := c.count()
	time.Sleep(1300 * time.Millisecond)
	for _, ev := range c.snapshot()[silentStart:] {
		if ev["type"] == "session.output" {
			t.Fatalf("session.output during silence (no new lines): %+v", ev["data"])
		}
	}

	// stop → тейлер остановлен, новых session.output нет
	st, _, _ = do(t, "DELETE", base+"/sessions/"+itoa(sessID), nil, nil)
	if st != http.StatusOK {
		t.Fatalf("stop session: %d", st)
	}
	afterStop := c.count()
	time.Sleep(1200 * time.Millisecond)
	for _, ev := range c.snapshot()[afterStop:] {
		if ev["type"] == "session.output" {
			t.Fatalf("session.output after stop: %+v", ev["data"])
		}
	}
}
