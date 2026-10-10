package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Run — точка входа teamctl: парсинг + dispatch, возвращает exit code.
//
//	0 — успех, 1 — HTTP/транспортная ошибка, 2 — usage (bad flags / unknown cmd).
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	case "whoami":
		return cmdWhoami(args[1:], stdout, stderr)
	case "task":
		return cmdTask(args[1:], stdout, stderr)
	case "session":
		return cmdSession(args[1:], stdout, stderr)
	case "attention":
		return cmdAttention(args[1:], stdout, stderr)
	case "config":
		return cmdConfig(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "teamctl: unknown command %q\n\n", args[0])
		printHelp(stderr)
		return 2
	}
}

const helpText = `teamctl — агентский CLI поверх REST-эндпоинтов демона (P2.12).

Команды:
  whoami [--session-id N | --role-id M] [--team-id T] [--json]
  task list [--state=] [--team-id=] [--json]
  task claim <id> [--json]                       (→ in_progress)
  task update <id> --state=... [--closure-reason=] [--note=] [--json]
  task handoff <id> --to-role-id M [--note=] [--json]
  task block <id> [--reason=] [--json]           (→ blocked)
  session list [--team-id=] [--state=] [--json]
  session transcript <id> [--lines=] [--json]
  session capture <id> [--lines=] [--json]
  session stop <id> [--json]
  attention list [--json]
  config get <key> [--json]
  config set <key> <value> [--json]
  config reset <key> [--json]

Конфигурация:
  --server / $TEAMCTL_SERVER   (default http://localhost:8080)
  --api-key / $TEAMCTL_API_KEY → header X-API-Key
  --json                       machine-parable JSON (иначе — человекочитаемо)

Выход: 0 = успех, 1 = ошибка HTTP/сети, 2 = usage. Ошибки — в stderr
(при --json — error envelope в stdout).

Примеры:
  teamctl whoami --session-id 7 --json
  teamctl task list --state=pending --json
  teamctl task claim 42
  teamctl session transcript 7 --lines 50
  teamctl config get tasks.closure_required_secs
`

func printHelp(w io.Writer) { fmt.Fprint(w, helpText) }

// ---------- common flags ----------

type common struct {
	server *string
	apiKey *string
	json   *bool
}

func addCommon(fs *flag.FlagSet) common {
	return common{
		server: fs.String("server", "", "base URL (default $TEAMCTL_SERVER or http://localhost:8080)"),
		apiKey: fs.String("api-key", "", "API key (default $TEAMCTL_API_KEY)"),
		json:   fs.Bool("json", false, "machine-parable JSON output"),
	}
}

func newFS(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func (c common) build(stdout, stderr io.Writer) (*Client, *octx) {
	return newClient(*c.server, *c.apiKey), &octx{json: *c.json, stdout: stdout, stderr: stderr}
}

// flagsWithValue — флаги, которые берут значение (для reorderFlagsFirst).
var flagsWithValue = map[string]bool{
	"--server": true, "--api-key": true, "--state": true, "--team-id": true,
	"--to-role-id": true, "--note": true, "--reason": true, "--lines": true,
	"--session-id": true, "--role-id": true, "--closure-reason": true,
}

// reorderFlagsFirst — двигает флаги (известные + их значения) вперёд, чтобы
// std `flag` (останавливается на первом non-flag аргументе) успел их распарсить,
// даже если в командной строке флаг стоит ПОСЛЕ positional (<id>).
// --flag=value остаётся как есть; positional (без префикса '-') — в хвост.
func reorderFlagsFirst(args []string) []string {
	var front, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		if eq := strings.Index(a, "="); eq >= 0 {
			front = append(front, a) // самодостаточный (--flag=value)
			continue
		}
		if flagsWithValue[a] && i+1 < len(args) {
			front = append(front, a, args[i+1])
			i++
			continue
		}
		front = append(front, a) // без значения (--json)
	}
	return append(front, rest...)
}

// ---------- whoami ----------

func cmdWhoami(args []string, stdout, stderr io.Writer) int {
	fs := newFS("whoami", stderr)
	c := addCommon(fs)
	sessionID := fs.Int64("session-id", 0, "session id (предпочтительно — агент знает свой)")
	roleID := fs.Int64("role-id", 0, "role id")
	teamID := fs.Int64("team-id", 0, "team id (опц., проверка совпадения)")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)

	q := url.Values{}
	if *sessionID != 0 {
		q.Set("session_id", strconv.FormatInt(*sessionID, 10))
	}
	if *roleID != 0 {
		q.Set("role_id", strconv.FormatInt(*roleID, 10))
	}
	if *teamID != 0 {
		q.Set("team_id", strconv.FormatInt(*teamID, 10))
	}
	path := "/api/v1/whoami"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	st, raw, err := client.Do(context.Background(), httpGet, path, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "whoami failed")
	}
	var v whoamiView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, whoamiHuman(v))
	return 0
}

// ---------- task ----------

func cmdTask(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "teamctl task: укажите подкоманду (list/claim/update/handoff/block)")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdTaskList(args[1:], stdout, stderr)
	case "claim":
		return cmdTaskClaim(args[1:], stdout, stderr)
	case "update":
		return cmdTaskUpdate(args[1:], stdout, stderr)
	case "handoff":
		return cmdTaskHandoff(args[1:], stdout, stderr)
	case "block":
		return cmdTaskBlock(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "teamctl task: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdTaskList(args []string, stdout, stderr io.Writer) int {
	fs := newFS("task list", stderr)
	c := addCommon(fs)
	state := fs.String("state", "", "фильтр по state")
	teamID := fs.Int64("team-id", 0, "фильтр по команде")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)

	q := url.Values{}
	if *state != "" {
		q.Set("state", *state)
	}
	if *teamID != 0 {
		q.Set("team_id", strconv.FormatInt(*teamID, 10))
	}
	path := "/api/v1/tasks"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	st, raw, err := client.Do(context.Background(), httpGet, path, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "task list failed")
	}
	var v tasksView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, tasksHuman(v))
	return 0
}

func cmdTaskClaim(args []string, stdout, stderr io.Writer) int {
	fs := newFS("task claim", stderr)
	c := addCommon(fs)
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	st, raw, err := client.Do(context.Background(), "PATCH", "/api/v1/tasks/"+strconv.FormatInt(id, 10)+"/state",
		map[string]any{"state": "in_progress"}, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "claim failed")
	}
	o.emit(raw, fmt.Sprintf("task %d → in_progress", id))
	return 0
}

func cmdTaskUpdate(args []string, stdout, stderr io.Writer) int {
	fs := newFS("task update", stderr)
	c := addCommon(fs)
	state := fs.String("state", "", "новое state (обязательно)")
	closure := fs.String("closure-reason", "", "closure reason (при done/blocked/handed_off)")
	note := fs.String("note", "", "комментарий")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	if *state == "" {
		fmt.Fprintln(o.stderr, "teamctl task update: --state обязателен")
		return 2
	}
	body := map[string]any{"state": *state}
	if *closure != "" {
		body["closure_reason"] = *closure
	}
	if *note != "" {
		body["comment"] = *note
	}
	st, raw, err := client.Do(context.Background(), "PATCH", "/api/v1/tasks/"+strconv.FormatInt(id, 10)+"/state", body, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "task update failed")
	}
	o.emit(raw, fmt.Sprintf("task %d → %s", id, *state))
	return 0
}

func cmdTaskHandoff(args []string, stdout, stderr io.Writer) int {
	fs := newFS("task handoff", stderr)
	c := addCommon(fs)
	toRole := fs.Int64("to-role-id", 0, "id роли-получателя (обязательно)")
	note := fs.String("note", "", "комментарий")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	if *toRole == 0 {
		fmt.Fprintln(o.stderr, "teamctl task handoff: --to-role-id обязателен")
		return 2
	}
	body := map[string]any{"to_role_id": *toRole}
	if *note != "" {
		body["comment"] = *note
	}
	st, raw, err := client.Do(context.Background(), "POST", "/api/v1/tasks/"+strconv.FormatInt(id, 10)+"/handoff", body, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "handoff failed")
	}
	o.emit(raw, fmt.Sprintf("task %d handed off → role %d", id, *toRole))
	return 0
}

func cmdTaskBlock(args []string, stdout, stderr io.Writer) int {
	fs := newFS("task block", stderr)
	c := addCommon(fs)
	reason := fs.String("reason", "", "причина блокировки (комментарий)")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	body := map[string]any{"state": "blocked"}
	if *reason != "" {
		body["comment"] = *reason
	}
	st, raw, err := client.Do(context.Background(), "PATCH", "/api/v1/tasks/"+strconv.FormatInt(id, 10)+"/state", body, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "block failed")
	}
	o.emit(raw, fmt.Sprintf("task %d → blocked", id))
	return 0
}

// taskID — первый positional-аргумент как int64.
func taskID(fs *flag.FlagSet, stderr io.Writer) (int64, bool) {
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "teamctl: укажите <id> задачи")
		return 0, false
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil || id <= 0 {
		fmt.Fprintf(stderr, "teamctl: некорректный <id>: %q\n", rest[0])
		return 0, false
	}
	return id, true
}

// ---------- session ----------

func cmdSession(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "teamctl session: укажите подкоманду (list/transcript/capture/stop)")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdSessionList(args[1:], stdout, stderr)
	case "transcript":
		return cmdSessionTranscript(args[1:], stdout, stderr)
	case "capture":
		return cmdSessionCapture(args[1:], stdout, stderr)
	case "stop":
		return cmdSessionStop(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "teamctl session: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdSessionList(args []string, stdout, stderr io.Writer) int {
	fs := newFS("session list", stderr)
	c := addCommon(fs)
	teamID := fs.Int64("team-id", 0, "фильтр по команде")
	state := fs.String("state", "", "фильтр по state")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)

	q := url.Values{}
	if *teamID != 0 {
		q.Set("team_id", strconv.FormatInt(*teamID, 10))
	}
	if *state != "" {
		q.Set("state", *state)
	}
	path := "/api/v1/sessions"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	st, raw, err := client.Do(context.Background(), httpGet, path, nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "session list failed")
	}
	var v sessionsView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, sessionsHuman(v))
	return 0
}

func cmdSessionTranscript(args []string, stdout, stderr io.Writer) int {
	fs := newFS("session transcript", stderr)
	c := addCommon(fs)
	lines := fs.Int("lines", 100, "сколько последних строк (limit)")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	st, raw, err := client.Do(context.Background(), httpGet,
		"/api/v1/sessions/"+strconv.FormatInt(id, 10)+"/transcript?limit="+strconv.Itoa(*lines), nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "transcript failed")
	}
	var v transcriptView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, transcriptHuman(id, v))
	return 0
}

func cmdSessionCapture(args []string, stdout, stderr io.Writer) int {
	fs := newFS("session capture", stderr)
	c := addCommon(fs)
	lines := fs.Int("lines", 20, "сколько последних строк экрана")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	st, raw, err := client.Do(context.Background(), httpGet,
		"/api/v1/sessions/"+strconv.FormatInt(id, 10)+"/capture?lines="+strconv.Itoa(*lines), nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "capture failed")
	}
	var v captureView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, captureHuman(id, v))
	return 0
}

func cmdSessionStop(args []string, stdout, stderr io.Writer) int {
	fs := newFS("session stop", stderr)
	c := addCommon(fs)
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	id, ok := taskID(fs, o.stderr)
	if !ok {
		return 2
	}
	st, raw, err := client.Do(context.Background(), "DELETE", "/api/v1/sessions/"+strconv.FormatInt(id, 10), nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "stop failed")
	}
	o.emit(raw, fmt.Sprintf("session %d: stop requested", id))
	return 0
}

// ---------- attention ----------

func cmdAttention(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "list" && args[0] != "help") {
		fmt.Fprintln(stderr, "teamctl attention: укажите подкоманду (list)")
		return 2
	}
	fs := newFS("attention list", stderr)
	c := addCommon(fs)
	if err := fs.Parse(reorderFlagsFirst(args[1:])); err != nil {
		return 2
	}
	client, o := c.build(stdout, stderr)
	st, raw, err := client.Do(context.Background(), httpGet, "/api/v1/attention", nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "attention list failed")
	}
	var v attentionView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, attentionHuman(v))
	return 0
}

// ---------- config ----------

func cmdConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "teamctl config: укажите подкоманду (get/set/reset)")
		return 2
	}
	switch args[0] {
	case "get":
		return cmdConfigGet(args[1:], stdout, stderr)
	case "set":
		return cmdConfigSet(args[1:], stdout, stderr)
	case "reset":
		return cmdConfigReset(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "teamctl config: unknown subcommand %q\n", args[0])
		return 2
	}
}

func cmdConfigGet(args []string, stdout, stderr io.Writer) int {
	fs := newFS("config get", stderr)
	c := addCommon(fs)
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "teamctl config get: укажите <key>")
		return 2
	}
	key := rest[0]
	client, o := c.build(stdout, stderr)
	st, raw, err := client.Do(context.Background(), httpGet, "/api/v1/config", nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "config get failed")
	}
	var v configListView
	_ = json.Unmarshal(raw, &v)
	k, found := v.Find(key)
	if !found {
		if o.json {
			fmt.Fprintln(o.stdout, marshalErr(st, "unknown_config_key", "key not found: "+key))
		} else {
			fmt.Fprintf(o.stderr, "teamctl: key не найден: %s\n", key)
		}
		return 1
	}
	// --json: выдать компактный {key,value,source}; human: "key = value"
	if o.json {
		out := map[string]any{"key": k.Key, "value": k.CurrentValue, "source": k.Source}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(o.stdout, string(b))
	} else {
		fmt.Fprintf(o.stdout, "%s = %v (source: %s)\n", k.Key, k.CurrentValue, k.Source)
	}
	return 0
}

func cmdConfigSet(args []string, stdout, stderr io.Writer) int {
	fs := newFS("config set", stderr)
	c := addCommon(fs)
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprintln(stderr, "teamctl config set: <key> <value>")
		return 2
	}
	key, val := rest[0], rest[1]
	client, o := c.build(stdout, stderr)
	// value: пробуем как JSON (number/bool/obj/array), иначе строка
	value := parseConfigValue(val)
	st, raw, err := client.Do(context.Background(), "PUT",
		"/api/v1/config/"+url.PathEscape(key), map[string]any{"value": value}, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "config set failed")
	}
	var v configSetView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, fmt.Sprintf("%s = %v (source: %s)", v.Key, v.Value, v.Source))
	return 0
}

func cmdConfigReset(args []string, stdout, stderr io.Writer) int {
	fs := newFS("config reset", stderr)
	c := addCommon(fs)
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fmt.Fprintln(stderr, "teamctl config reset: <key>")
		return 2
	}
	key := rest[0]
	client, o := c.build(stdout, stderr)
	st, raw, err := client.Do(context.Background(), "DELETE", "/api/v1/config/"+url.PathEscape(key), nil, nil)
	if err != nil {
		fmt.Fprintf(stderr, "teamctl: %v\n", err)
		return 1
	}
	if st < 200 || st >= 300 {
		return o.fail(st, raw, "config reset failed")
	}
	var v configResetView
	_ = json.Unmarshal(raw, &v)
	o.emit(raw, fmt.Sprintf("%s reset (source: %s)", v.Key, v.Source))
	return 0
}

// parseConfigValue — значение как JSON (number/bool/obj/array) либо строка.
func parseConfigValue(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// ---------- human formatters + views ----------

const httpGet = "GET"

func marshalErr(status int, code, msg string) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"status": status, "code": code, "message": msg}})
	return string(b)
}

type whoamiView struct {
	Agent struct {
		RoleName string `json:"role_name"`
		TeamName string `json:"team_name"`
		Address  string `json:"address"`
	} `json:"agent"`
	Session *struct {
		ID             int64    `json:"id"`
		State          string   `json:"state"`
		Model          string   `json:"model"`
		ContextUsedPct *float64 `json:"context_used_pct"`
	} `json:"session"`
	MyTasks struct {
		Pending []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Priority int    `json:"priority"`
		} `json:"pending"`
		InProgress []struct {
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			IsOverdue bool   `json:"is_overdue"`
		} `json:"in_progress"`
		Blocked []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Wake  *struct {
				Kind string `json:"kind"`
			} `json:"wake"`
		} `json:"blocked"`
	} `json:"my_tasks"`
	WaitingForMe []struct {
		FromRoleName *string `json:"from_role_name"`
		Type         string  `json:"type"`
		Summary      string  `json:"summary"`
	} `json:"waiting_for_me"`
	Upstream []struct {
		RoleName string `json:"role_name"`
		Via      string `json:"via"`
	} `json:"upstream"`
	Downstream []struct {
		RoleName string `json:"role_name"`
		Via      string `json:"via"`
	} `json:"downstream"`
}

func whoamiHuman(v whoamiView) string {
	var b strings.Builder
	who := v.Agent.Address
	if who == "" {
		who = v.Agent.RoleName
	}
	fmt.Fprintf(&b, "Agent: %s (role %s / team %s)\n", who, v.Agent.RoleName, v.Agent.TeamName)
	if v.Session != nil {
		line := fmt.Sprintf("Session #%d: %s", v.Session.ID, v.Session.State)
		if v.Session.ContextUsedPct != nil {
			line += fmt.Sprintf(" (context %.0f%%)", *v.Session.ContextUsedPct)
		}
		if v.Session.Model != "" {
			line += fmt.Sprintf(" [%s]", v.Session.Model)
		}
		b.WriteString(line + "\n")
	}
	mt := v.MyTasks
	fmt.Fprintf(&b, "My tasks: %d pending, %d in_progress, %d blocked\n",
		len(mt.Pending), len(mt.InProgress), len(mt.Blocked))
	for _, t := range mt.Pending {
		fmt.Fprintf(&b, "  - [pending] #%d %s\n", t.ID, t.Title)
	}
	for _, t := range mt.InProgress {
		mark := ""
		if t.IsOverdue {
			mark = " (OVERDUE)"
		}
		fmt.Fprintf(&b, "  - [in_progress] #%d %s%s\n", t.ID, t.Title, mark)
	}
	for _, t := range mt.Blocked {
		w := ""
		if t.Wake != nil {
			w = fmt.Sprintf(" (wake: %s)", t.Wake.Kind)
		}
		fmt.Fprintf(&b, "  - [blocked] #%d %s%s\n", t.ID, t.Title, w)
	}
	fmt.Fprintf(&b, "Waiting for me: %d\n", len(v.WaitingForMe))
	for _, m := range v.WaitingForMe {
		from := "?"
		if m.FromRoleName != nil {
			from = *m.FromRoleName
		}
		fmt.Fprintf(&b, "  - from %s (%s): %s\n", from, m.Type, truncate(m.Summary, 80))
	}
	b.WriteString("Upstream: " + roleNames(v.Upstream) + "\n")
	b.WriteString("Downstream: " + roleNames(v.Downstream) + "\n")
	return strings.TrimRight(b.String(), "\n")
}

func roleNames(rels []struct {
	RoleName string `json:"role_name"`
	Via      string `json:"via"`
}) string {
	if len(rels) == 0 {
		return "(none)"
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, fmt.Sprintf("%s (%s)", r.RoleName, r.Via))
	}
	return strings.Join(out, ", ")
}

type tasksView struct {
	Total int `json:"total"`
	Tasks []struct {
		ID                int64   `json:"id"`
		State             string  `json:"state"`
		DestinationRole   string  `json:"destination_role_name"`
		Title             string  `json:"title"`
		IsOverdue         bool    `json:"is_overdue"`
		ClosureRequiredAt *string `json:"closure_required_at"`
	} `json:"tasks"`
}

func tasksHuman(v tasksView) string {
	if len(v.Tasks) == 0 {
		return "no tasks"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d task(s):\n", v.Total)
	for _, t := range v.Tasks {
		mark := ""
		if t.IsOverdue {
			mark = " [OVERDUE]"
		}
		fmt.Fprintf(&b, "  #%d [%s] → %s: %s%s\n", t.ID, t.State, t.DestinationRole, t.Title, mark)
	}
	return strings.TrimRight(b.String(), "\n")
}

type sessionsView struct {
	Total    int `json:"total"`
	Sessions []struct {
		ID          int64  `json:"id"`
		State       string `json:"state"`
		RoleName    string `json:"role_name"`
		RuntimeType string `json:"runtime_type"`
	} `json:"sessions"`
}

func sessionsHuman(v sessionsView) string {
	if len(v.Sessions) == 0 {
		return "no sessions"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d session(s):\n", v.Total)
	for _, s := range v.Sessions {
		fmt.Fprintf(&b, "  #%d [%s] %s (%s)\n", s.ID, s.State, s.RoleName, s.RuntimeType)
	}
	return strings.TrimRight(b.String(), "\n")
}

type transcriptView struct {
	Transcript []struct {
		MessageType string  `json:"message_type"`
		Role        *string `json:"role"`
		Content     string  `json:"content"`
	} `json:"transcript"`
}

func transcriptHuman(id int64, v transcriptView) string {
	if len(v.Transcript) == 0 {
		return fmt.Sprintf("session %d: empty transcript", id)
	}
	var b strings.Builder
	for _, m := range v.Transcript {
		role := m.MessageType
		if m.Role != nil {
			role = *m.Role
		}
		fmt.Fprintf(&b, "%s: %s\n", role, strings.TrimSpace(m.Content))
	}
	return strings.TrimRight(b.String(), "\n")
}

type captureView struct {
	Lines  []string `json:"lines"`
	Source string   `json:"source"`
}

func captureHuman(id int64, v captureView) string {
	if len(v.Lines) == 0 {
		return fmt.Sprintf("session %d: empty capture (%s)", id, v.Source)
	}
	return strings.Join(v.Lines, "\n")
}

type attentionView struct {
	Total int `json:"total"`
	Items []struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Title    string `json:"title"`
	} `json:"items"`
}

func attentionHuman(v attentionView) string {
	if len(v.Items) == 0 {
		return "nothing needs attention"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d item(s) need attention:\n", v.Total)
	for _, it := range v.Items {
		fmt.Fprintf(&b, "  [%s] %s #%s: %s\n", it.Severity, it.Kind, it.ID, it.Title)
	}
	return strings.TrimRight(b.String(), "\n")
}

type configListView struct {
	Sections []struct {
		Name string `json:"name"`
		Keys []struct {
			Key          string `json:"key"`
			CurrentValue any    `json:"current_value"`
			Source       string `json:"source"`
		} `json:"keys"`
	} `json:"sections"`
}

type configKeyFound struct {
	Key          string
	CurrentValue any
	Source       string
}

// Find — ищет ключ в секциях (первое вхождение).
func (v *configListView) Find(key string) (configKeyFound, bool) {
	for _, s := range v.Sections {
		for _, k := range s.Keys {
			if k.Key == key {
				return configKeyFound{Key: k.Key, CurrentValue: k.CurrentValue, Source: k.Source}, true
			}
		}
	}
	return configKeyFound{}, false
}

type configSetView struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
}

type configResetView struct {
	Key    string `json:"key"`
	Source string `json:"source"`
}

// truncate — обрезка по rune (≤ n) с многоточием.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
