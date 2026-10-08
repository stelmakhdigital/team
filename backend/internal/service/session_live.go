package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"daemon/internal/models"
)

// Slice 7 — live-метрики сессий (контракт 20 §3.6).
//
// Принципы (согласовано с лид-инженером):
//   - всё additive и опциональное: поле omit = «рантайм не знает» → UI «--»;
//   - model: из конфига pi-сессии (configs/sessions/session-<id>.yaml) —
//     единственное устойчивое место (req.Config может не дойти до storage);
//   - context-метрики: парсинг JSONL-записей с `usage` из transcript-лога
//     (формат pi JSONL: {"message":{"usage":{"input_tokens":N,...}}} или
//     {"usage":{...}}). ТUI-вывод pi (ANSI) usage НЕ содержит → поля omit
//     (честно); если рантайм пишет JSONL с usage — поля заполняются.
//
// Семантика полей:
//   - context_total_input_tokens — СУММА (input+cache_read) по всем usage-записям;
//   - context_total_output_tokens — сумма output по всем usage-записям;
//   - context_used_percentage — ПОСЛЕДНЯЯ запись: (input+cache_read)/окно*100,
//     окно = config.context_window, иначе 200000 (документированное допущение).

const (
	defaultContextWindow = 200000
	usageScanTailBytes   = 256 << 10 // сканируем хвост transcript-файла (256KB)
)

// sessionLogPath — путь transcript-файла сессии (лог = transcript).
func (s *SessionService) sessionLogPath(sessID int64) string {
	if s.LogsDir == "" {
		return ""
	}
	return filepath.Join(s.LogsDir, fmt.Sprintf("session-%d.log", sessID))
}

// enrichLiveMetrics — заполняет опциональные live-поля SessionView (slice 7).
// Ошибки (файлов нет/не читается) → поля остаются omit (UI «--»).
func (s *SessionService) enrichLiveMetrics(v *SessionView, sess *models.Session) {
	v.LogPath = s.sessionLogPath(sess.ID)
	v.Model = s.sessionModel(sess)

	path := v.LogPath
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return
	}
	off := int64(0)
	if fi.Size() > usageScanTailBytes {
		off = fi.Size() - usageScanTailBytes
	}
	buf := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return
	}

	latestInput, totalInput, totalOutput, found := 0, 0, 0, false
	for _, line := range strings.Split(string(buf), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		usage := extractUsage(rec)
		if usage == nil {
			continue
		}
		in := intNum(usage["input_tokens"]) + intNum(usage["cache_read_input_tokens"])
		out := intNum(usage["output_tokens"])
		latestInput = in
		totalInput += in
		totalOutput += out
		found = true
	}
	if !found {
		return // рантайм не отдаёт usage — всё остаётся «--»
	}
	lo := int64(totalOutput)
	ti := int64(totalInput)
	v.ContextTotalInputTokens = &ti
	v.ContextTotalOutputTokens = &lo

	window := defaultContextWindow
	if w, ok := sess.Config["context_window"]; ok {
		if i, err := asInt(w); err == nil && i > 0 {
			window = i
		}
	}
	pct := float64(latestInput) / float64(window) * 100
	if pct > 100 {
		pct = 100
	}
	v.ContextUsedPercentage = &pct
}

// sessionModel — модель рантайма: pi — из конфига сессии; иначе «--» (omit).
func (s *SessionService) sessionModel(sess *models.Session) string {
	if sess.RuntimeType != "pi" || s.ConfigsDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(s.ConfigsDir, fmt.Sprintf("session-%d.yaml", sess.ID)))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if m, ok := strings.CutPrefix(line, "model: "); ok {
			m = strings.TrimSpace(m)
			if m != "" {
				return m
			}
		}
	}
	return ""
}

// extractUsage — ищет usage-объект: верхний уровень или rec.message.usage
// (формат pi JSONL). Возвращает nil, если usage нет/не объект.
func extractUsage(rec map[string]any) map[string]any {
	if u, ok := rec["usage"].(map[string]any); ok {
		return u
	}
	if m, ok := rec["message"].(map[string]any); ok {
		if u, ok := m["usage"].(map[string]any); ok {
			return u
		}
	}
	return nil
}

func intNum(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func asInt(v any) (int, error) {
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case int:
		return x, nil
	case string:
		return strconv.Atoi(x)
	default:
		return 0, fmt.Errorf("not a number")
	}
}
