package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// octx — вывод команды: --json (machine-parable) vs human + stdout/stderr.
type octx struct {
	json   bool
	stdout io.Writer
	stderr io.Writer
}

// emit — печатает результат (2xx).
// --json: raw JSON (pretty) в stdout; human: compact-строка в stdout.
func (o *octx) emit(raw []byte, human string) {
	if o.json {
		o.printJSON(raw)
	} else {
		fmt.Fprintln(o.stdout, human)
	}
}

// fail — HTTP-ошибка (non-2xx). --json: error envelope в stdout; human: stderr.
// Возвращает exit code (1).
func (o *octx) fail(status int, raw []byte, fallback string) int {
	code, msg := parseErrorEnvelope(raw)
	if msg == "" {
		msg = fallback
	}
	if code == "" {
		code = "error"
	}
	if o.json {
		env := map[string]any{"error": map[string]any{"status": status, "code": code, "message": msg}}
		b, _ := json.Marshal(env)
		fmt.Fprintln(o.stdout, string(b))
	} else {
		fmt.Fprintf(o.stderr, "teamctl: %s (HTTP %d): %s\n", code, status, msg)
	}
	return 1
}

// printJSON — pretty-JSON в stdout (raw уже валидный JSON; иначе raw as-is).
func (o *octx) printJSON(raw []byte) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Fprintln(o.stdout, string(raw))
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(o.stdout, string(raw))
		return
	}
	fmt.Fprintln(o.stdout, string(b))
}

// parseErrorEnvelope — {error:{code,message}} из ответа демона.
func parseErrorEnvelope(raw []byte) (code, message string) {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", ""
	}
	return env.Error.Code, env.Error.Message
}
