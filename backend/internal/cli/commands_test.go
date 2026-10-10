package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daemon/internal/cli"
)

// ---------- P2.12: teamctl CLI (httptest: команда → request + parse) ----------

type captured struct {
	Method   string
	Path     string
	RawQuery string
	Body     []byte
	APIKey   string
}

// startDaemon — mock-демон: записывает request, respond задаёт ответ.
func startDaemon(t *testing.T, respond func(w http.ResponseWriter, rec *captured)) (*httptest.Server, *captured) {
	t.Helper()
	rec := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.Method, rec.Path, rec.RawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		rec.Body, rec.APIKey = b, r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		respond(w, rec)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func assertReq(t *testing.T, rec *captured, method, path, apiKey string) {
	t.Helper()
	if rec.Method != method {
		t.Errorf("method = %q, want %q", rec.Method, method)
	}
	if rec.Path != path {
		t.Errorf("path = %q, want %q", rec.Path, path)
	}
	if apiKey != "" && rec.APIKey != apiKey {
		t.Errorf("X-API-Key = %q, want %q", rec.APIKey, apiKey)
	}
}

func TestWhoamiCommand(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"agent":{"role_id":42,"role_name":"go-dev","team_name":"Dev","address":"dev:backend.go-dev"},
			"session":{"id":7,"state":"running","context_used_pct":61},
			"my_tasks":{"pending":[{"id":1,"title":"a"}],"in_progress":[],"blocked":[]},
			"waiting_for_me":[],"upstream":[],"downstream":[]}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "test-key")

	// --json: machine-parable
	code, out, _ := runCLI(t, "whoami", "--session-id", "7", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	assertReq(t, rec, "GET", "/api/v1/whoami", "test-key")
	if !strings.Contains(rec.RawQuery, "session_id=7") {
		t.Errorf("query = %q, want session_id=7", rec.RawQuery)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output not JSON: %v (%q)", err, out)
	}
	if ag, _ := got["agent"].(map[string]any); ag == nil || ag["role_name"] != "go-dev" {
		t.Errorf("agent = %v", got["agent"])
	}

	// human: компактно
	code, out, _ = runCLI(t, "whoami", "--role-id", "42")
	if code != 0 {
		t.Fatalf("human exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/whoami", "test-key")
	if !strings.Contains(rec.RawQuery, "role_id=42") {
		t.Errorf("query = %q, want role_id=42", rec.RawQuery)
	}
	if !strings.Contains(out, "Agent:") || !strings.Contains(out, "go-dev") {
		t.Errorf("human output = %q", out)
	}
}

func TestTaskList(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"tasks":[{"id":1,"state":"pending","destination_role_name":"go-dev","title":"do it"}],"total":1}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, out, _ := runCLI(t, "task", "list", "--state=pending")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/tasks", "k")
	if !strings.Contains(rec.RawQuery, "state=pending") {
		t.Errorf("query = %q", rec.RawQuery)
	}
	if !strings.Contains(out, "#1 [pending]") {
		t.Errorf("output = %q", out)
	}
}

func TestTaskClaim(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"id":5,"state":"in_progress"}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, out, _ := runCLI(t, "task", "claim", "5")
	if code != 0 {
		t.Fatalf("exit = %d %s", code, out)
	}
	assertReq(t, rec, "PATCH", "/api/v1/tasks/5/state", "k")
	var body map[string]any
	_ = json.Unmarshal(rec.Body, &body)
	if body["state"] != "in_progress" {
		t.Errorf("body = %v, want state=in_progress", body)
	}
	if !strings.Contains(out, "in_progress") {
		t.Errorf("output = %q", out)
	}
}

func TestTaskHandoffFlagsAfterPositional(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"id":5,"state":"pending"}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	// positional ПЕРЕД флагом (как в spec: task handoff <id> --to-role-id M)
	code, _, errOut := runCLI(t, "task", "handoff", "5", "--to-role-id", "3", "--note", "done")
	if code != 0 {
		t.Fatalf("exit = %d, err=%s", code, errOut)
	}
	assertReq(t, rec, "POST", "/api/v1/tasks/5/handoff", "k")
	var body map[string]any
	_ = json.Unmarshal(rec.Body, &body)
	if int64(body["to_role_id"].(float64)) != 3 {
		t.Errorf("to_role_id = %v, want 3", body["to_role_id"])
	}
	if body["comment"] != "done" {
		t.Errorf("comment = %v, want done", body["comment"])
	}
}

func TestTaskUpdateClosure(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"id":5,"state":"done"}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, _, _ := runCLI(t, "task", "update", "5", "--state=done", "--closure-reason=no_follow_on", "--note=finished")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "PATCH", "/api/v1/tasks/5/state", "k")
	var body map[string]any
	_ = json.Unmarshal(rec.Body, &body)
	if body["state"] != "done" || body["closure_reason"] != "no_follow_on" || body["comment"] != "finished" {
		t.Errorf("body = %v", body)
	}
}

func TestTaskBlock(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"id":5,"state":"blocked"}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, _, _ := runCLI(t, "task", "block", "5", "--reason", "waiting on db")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "PATCH", "/api/v1/tasks/5/state", "k")
	var body map[string]any
	_ = json.Unmarshal(rec.Body, &body)
	if body["state"] != "blocked" || body["comment"] != "waiting on db" {
		t.Errorf("body = %v", body)
	}
}

func TestSessionCommands(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, c *captured) {
		switch {
		case strings.Contains(c.Path, "/transcript"):
			w.Write([]byte(`{"transcript":[{"message_type":"user","content":"hi"}]}`))
		case strings.Contains(c.Path, "/capture"):
			w.Write([]byte(`{"lines":["l1","l2"],"source":"tmux-pane"}`))
		default:
			w.Write([]byte(`{"id":5,"state":"stopping"}`))
		}
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")

	// transcript
	code, out, _ := runCLI(t, "session", "transcript", "5", "--lines", "50")
	if code != 0 {
		t.Fatalf("transcript exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/sessions/5/transcript", "k")
	if !strings.Contains(rec.RawQuery, "limit=50") {
		t.Errorf("transcript query = %q", rec.RawQuery)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("transcript out = %q", out)
	}

	// capture (default lines=20)
	code, out, _ = runCLI(t, "session", "capture", "5")
	if code != 0 {
		t.Fatalf("capture exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/sessions/5/capture", "k")
	if !strings.Contains(rec.RawQuery, "lines=20") {
		t.Errorf("capture query = %q", rec.RawQuery)
	}
	if !strings.Contains(out, "l1\nl2") {
		t.Errorf("capture out = %q", out)
	}

	// stop
	code, _, _ = runCLI(t, "session", "stop", "5")
	if code != 0 {
		t.Fatalf("stop exit = %d", code)
	}
	assertReq(t, rec, "DELETE", "/api/v1/sessions/5", "k")
}

func TestSessionList(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"sessions":[{"id":1,"state":"running","role_name":"lead","runtime_type":"pi"}],"total":1}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, out, _ := runCLI(t, "session", "list", "--team-id", "1", "--state", "running")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/sessions", "k")
	if !strings.Contains(rec.RawQuery, "team_id=1") || !strings.Contains(rec.RawQuery, "state=running") {
		t.Errorf("query = %q", rec.RawQuery)
	}
	if !strings.Contains(out, "#1 [running]") {
		t.Errorf("output = %q", out)
	}
}

func TestAttentionList(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"total":1,"items":[{"kind":"task_blocked","id":"3","severity":"high","title":"stuck"}]}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, out, _ := runCLI(t, "attention", "list")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/attention", "k")
	if !strings.Contains(out, "task_blocked") || !strings.Contains(out, "stuck") {
		t.Errorf("output = %q", out)
	}
}

func TestConfigGet(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"sections":[{"name":"tasks","keys":[
			{"key":"tasks.closure_required_secs","current_value":7200,"source":"default"}]}]}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	// human
	code, out, _ := runCLI(t, "config", "get", "tasks.closure_required_secs")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "GET", "/api/v1/config", "k")
	if !strings.Contains(out, "tasks.closure_required_secs = 7200") {
		t.Errorf("output = %q", out)
	}
	// --json: {key,value,source}
	code, out, _ = runCLI(t, "config", "get", "tasks.closure_required_secs", "--json")
	if code != 0 {
		t.Fatalf("json exit = %d", code)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if int64(got["value"].(float64)) != 7200 || got["key"] != "tasks.closure_required_secs" {
		t.Errorf("json = %v", got)
	}
}

func TestConfigSet(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"key":"tasks.closure_required_secs","value":3600,"source":"override"}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	// value = число (parse как JSON → int, не string)
	code, _, _ := runCLI(t, "config", "set", "tasks.closure_required_secs", "3600")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "PUT", "/api/v1/config/tasks.closure_required_secs", "k")
	var body map[string]any
	_ = json.Unmarshal(rec.Body, &body)
	if v, ok := body["value"].(float64); !ok || int64(v) != 3600 {
		t.Errorf("value = %v (%T), want number 3600", body["value"], body["value"])
	}

	// value = строка (parse как JSON не удался → string)
	code, _, _ = runCLI(t, "config", "set", "some.string", "hello world")
	if code != 0 {
		t.Fatalf("string exit = %d", code)
	}
	assertReq(t, rec, "PUT", "/api/v1/config/some.string", "k")
	_ = json.Unmarshal(rec.Body, &body)
	if body["value"] != "hello world" {
		t.Errorf("value = %v, want string", body["value"])
	}
}

func TestConfigReset(t *testing.T) {
	srv, rec := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.Write([]byte(`{"key":"some.key","source":"default","value":42}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")
	code, _, _ := runCLI(t, "config", "reset", "some.key")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	assertReq(t, rec, "DELETE", "/api/v1/config/some.key", "k")
}

func TestHTTPErrorExitCodes(t *testing.T) {
	// 404 → exit 1, human: stderr; --json: error envelope в stdout
	srv, _ := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"session not found"}}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "k")

	code, out, errOut := runCLI(t, "session", "capture", "999")
	if code != 1 {
		t.Fatalf("404 human exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "not_found") || !strings.Contains(errOut, "404") {
		t.Errorf("stderr = %q, want not_found/404", errOut)
	}

	code, out, _ = runCLI(t, "session", "capture", "999", "--json")
	if code != 1 {
		t.Fatalf("404 --json exit = %d, want 1", code)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("--json error not JSON: %v (%q)", err, out)
	}
	er, _ := env["error"].(map[string]any)
	if er == nil || er["code"] != "not_found" {
		t.Errorf("error envelope = %v", env)
	}
}

func Test403Forbidden(t *testing.T) {
	srv, _ := startDaemon(t, func(w http.ResponseWriter, _ *captured) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"forbidden","message":"permission denied"}}`))
	})
	t.Setenv("TEAMCTL_SERVER", srv.URL)
	t.Setenv("TEAMCTL_API_KEY", "viewer-key")
	code, _, _ := runCLI(t, "task", "claim", "5")
	if code != 1 {
		t.Fatalf("403 exit = %d, want 1", code)
	}
}

func TestHelpAndUsage(t *testing.T) {
	// help → 0
	code, out, _ := runCLI(t, "help")
	if code != 0 || !strings.Contains(out, "teamctl") {
		t.Errorf("help: code=%d out=%q", code, out)
	}
	// без аргументов → 0 (help)
	code, out, _ = runCLI(t)
	if code != 0 || !strings.Contains(out, "teamctl") {
		t.Errorf("no-args: code=%d", code)
	}
	// неизвестная команда → 2
	code, _, errOut := runCLI(t, "bogus")
	if code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Errorf("unknown: code=%d err=%q", code, errOut)
	}
	// task без подкоманды → 2
	code, _, _ = runCLI(t, "task")
	if code != 2 {
		t.Errorf("task no-sub: code=%d, want 2", code)
	}
}
