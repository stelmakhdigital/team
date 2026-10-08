package service

import (
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/models"
)

// ---------- slice 7: live-метрики (unit, whitebox) ----------

func writeLog(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSessionModelFromPiConfig(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "session-7.yaml", "name: x\nmodel: anthropic/claude-sonnet\n")
	s := &SessionService{ConfigsDir: dir}

	if got := s.sessionModel(&models.Session{ID: 7, RuntimeType: "pi"}); got != "anthropic/claude-sonnet" {
		t.Fatalf("model = %q", got)
	}
	if got := s.sessionModel(&models.Session{ID: 7, RuntimeType: "process"}); got != "" {
		t.Fatalf("process runtime must have no model, got %q", got)
	}
	if got := s.sessionModel(&models.Session{ID: 99, RuntimeType: "pi"}); got != "" {
		t.Fatalf("no config file => no model, got %q", got)
	}
}

func TestEnrichLiveMetricsJSONL(t *testing.T) {
	dir := t.TempDir()
	log := writeLog(t, dir, "session-1.log",
		"noise line (не jsonl)\n"+
			`{"type":"message","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":50,"output_tokens":10}}}`+"\n"+
			`{"message":{"usage":{"input_tokens":300,"cache_read_input_tokens":0,"output_tokens":20}}}`+"\n")
	s := &SessionService{LogsDir: dir}
	sess := &models.Session{ID: 1, RuntimeType: "process", Config: map[string]any{}}
	v := &SessionView{}
	s.enrichLiveMetrics(v, sess)

	if v.LogPath != log {
		t.Fatalf("log_path = %q, want %q", v.LogPath, log)
	}
	if v.Model != "" {
		t.Fatalf("model = %q, want empty (process runtime)", v.Model)
	}
	if v.ContextTotalInputTokens == nil || *v.ContextTotalInputTokens != 450 {
		t.Fatalf("total_input = %v, want 450", v.ContextTotalInputTokens)
	}
	if v.ContextTotalOutputTokens == nil || *v.ContextTotalOutputTokens != 30 {
		t.Fatalf("total_output = %v, want 30", v.ContextTotalOutputTokens)
	}
	// последняя запись: input 300 / окно 200000 = 0.15%
	if v.ContextUsedPercentage == nil {
		t.Fatal("pct nil")
	}
	if d := *v.ContextUsedPercentage - 0.15; d > 1e-9 || d < -1e-9 {
		t.Fatalf("pct = %v, want 0.15", *v.ContextUsedPercentage)
	}
}

func TestEnrichLiveMetricsContextWindowOverride(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "session-2.log",
		`{"usage":{"input_tokens":1000,"output_tokens":5}}`+"\n")
	s := &SessionService{LogsDir: dir}
	sess := &models.Session{ID: 2, Config: map[string]any{"context_window": 2000}}
	v := &SessionView{}
	s.enrichLiveMetrics(v, sess)
	if v.ContextUsedPercentage == nil || *v.ContextUsedPercentage != 50 {
		t.Fatalf("pct = %v, want 50 (1000/2000)", v.ContextUsedPercentage)
	}
}

func TestEnrichLiveMetricsNoUsage(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "session-3.log", "plain\noutput\nno jsonl here\n")
	s := &SessionService{LogsDir: dir}
	v := &SessionView{}
	s.enrichLiveMetrics(v, &models.Session{ID: 3})
	if v.ContextUsedPercentage != nil || v.ContextTotalInputTokens != nil || v.ContextTotalOutputTokens != nil {
		t.Fatalf("no usage => all context fields must be nil: %+v", v)
	}
	if v.LogPath == "" {
		t.Fatal("log_path must be set even without usage")
	}
}

func TestEnrichLiveMetricsPctCap(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "session-4.log", `{"usage":{"input_tokens":300000,"output_tokens":1}}`+"\n")
	s := &SessionService{LogsDir: dir}
	v := &SessionView{}
	s.enrichLiveMetrics(v, &models.Session{ID: 4})
	if v.ContextUsedPercentage == nil || *v.ContextUsedPercentage != 100 {
		t.Fatalf("pct = %v, want capped at 100", v.ContextUsedPercentage)
	}
}

// ---------- slice 7: readNewLogLines (tailer-логика) ----------

func TestReadNewLogLinesBasics(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.log")

	// файла нет
	if _, _, _, ok := readNewLogLines(p, 0, nil); ok {
		t.Fatal("missing file: ok = true")
	}
	// первая порция с неполной строкой
	os.WriteFile(p, []byte("a\nb\npartial"), 0o644)
	lines, partial, off, ok := readNewLogLines(p, 0, nil)
	if !ok || len(lines) != 2 || lines[0] != "a" || lines[1] != "b" || string(partial) != "partial" || off != 11 {
		t.Fatalf("first read: lines=%q partial=%q off=%d ok=%v", lines, partial, off, ok)
	}
	// дописали остаток + новую строку
	f, _ := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	f.WriteString("done\nc\n")
	f.Close()
	lines, partial, off, ok = readNewLogLines(p, off, partial)
	if !ok || len(lines) != 2 || lines[0] != "partialdone" || lines[1] != "c" || len(partial) != 0 || off != 18 {
		t.Fatalf("second read: lines=%q partial=%q off=%d", lines, partial, off)
	}
	// без новых данных
	lines, _, off2, ok := readNewLogLines(p, off, nil)
	if !ok || len(lines) != 0 || off2 != off {
		t.Fatalf("idle read: lines=%q off=%d", lines, off2)
	}
}

func TestReadNewLogLinesTruncate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.log")
	os.WriteFile(p, []byte("12345\n"), 0o644)
	_, _, off, _ := readNewLogLines(p, 0, nil)
	os.WriteFile(p, []byte("x\n"), 0o644) // truncate
	lines, _, off, ok := readNewLogLines(p, off, nil)
	if !ok || len(lines) != 1 || lines[0] != "x" || off != 2 {
		t.Fatalf("after truncate: lines=%q off=%d", lines, off)
	}
}
