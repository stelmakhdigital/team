package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"daemon/internal/runtime"
)

// ---------- P2.8: Capture (стл-кадр экрана сессии) ----------
//
// Оператор получает «стл-кадр» экрана/вывода сессии: последние N строк как
// артефакт (сохраняется в файл, можно прикрепить). Read-only для сессии.
//
// Механика (асимметрия, задокументирована в api-decisions §P2.8):
//   - tmux / pi (pi = tmux внутри) → `tmux capture-pane` (реальный экран);
//     tmux нет/сессия мертва → 409 (не 500).
//   - process → tail transcript-файла (у process нет «экрана» — лог);
//     файла нет → 200 с пустыми lines (честность, не ошибка).

const (
	captureDefaultLines = 20
	captureMaxLines     = 200
	// captureMaxPerSession — bounded-cleanup: сколько capture-файлов держать на сессию.
	captureMaxPerSession = 20
)

// CaptureResult — ответ GET /sessions/{id}/capture.
type CaptureResult struct {
	SessionID   int64    `json:"session_id"`
	RuntimeType string   `json:"runtime_type"`
	Lines       []string `json:"lines"`
	CapturedAt  string   `json:"captured_at"`
	Source      string   `json:"source"` // "tmux-pane" | "log-tail"
	SavedPath   string   `json:"saved_path"`
}

// Capture — стл-кадр сессии (последние lines строк экрана/лога) + сохранение снапшота.
// 404 — сессия не найдена; 409 — tmux не установлен/сессия мертва (для tmux/pi).
func (s *SessionService) Capture(ctx context.Context, id int64, lines int) (*CaptureResult, error) {
	if lines <= 0 {
		lines = captureDefaultLines
	}
	if lines > captureMaxLines {
		lines = captureMaxLines // clamp (документировано: max 200)
	}
	sess, err := s.getSession(ctx, id)
	if err != nil {
		return nil, err // 404
	}
	var captured []string
	var source string
	switch sess.RuntimeType {
	case "process":
		// process: лог-тейл (нет «экрана»); файла нет → пустые lines (честность).
		captured, _ = s.tailTranscript(sess.ID, lines)
		source = "log-tail"
	default: // tmux / pi
		adapter, err := s.runt.Get(sess.RuntimeType)
		if err != nil {
			return nil, err
		}
		captured, err = adapter.Capture(ctx, sess.RuntimeRef, lines)
		if err != nil {
			if errors.Is(err, runtime.ErrTmuxUnavailable) || errors.Is(err, runtime.ErrTmuxSessionDead) {
				return nil, NewConflict("screen not available: " + err.Error()) // 409 (clear reason)
			}
			return nil, NewConflict("screen not available: " + err.Error()) // 409 (не 500)
		}
		source = "tmux-pane"
	}
	if captured == nil {
		captured = []string{}
	}
	// сохранение снапшота (bounded-cleanup)
	savedPath, err := s.saveCapture(sess.ID, captured)
	if err != nil {
		return nil, err
	}
	return &CaptureResult{
		SessionID:   sess.ID,
		RuntimeType: sess.RuntimeType,
		Lines:       captured,
		CapturedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Source:      source,
		SavedPath:   savedPath,
	}, nil
}

// tailTranscript — последние lines строк transcript-файла (файла нет → пустые, не ошибка).
func (s *SessionService) tailTranscript(id int64, lines int) ([]string, error) {
	path := filepath.Join(s.LogsDir, fmt.Sprintf("session-%d.log", id))
	l, err := readTailLines(path, lines)
	if err != nil {
		return []string{}, nil // честность: нет лога → пустой снимок
	}
	return l, nil
}

// saveCapture — пишет снапшот в <LogsDir>/captures/session-<id>-<ts>.txt + bounded-cleanup.
func (s *SessionService) saveCapture(id int64, lines []string) (string, error) {
	dir := filepath.Join(s.LogsDir, "captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("capture: mkdir: %w", err)
	}
	name := fmt.Sprintf("session-%d-%d.txt", id, time.Now().UnixNano())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return "", fmt.Errorf("capture: write: %w", err)
	}
	s.cleanupCaptures(dir)
	return path, nil
}

// cleanupCaptures — bounded: держать ≤ captureMaxPerSession снапшотов на сессию.
func (s *SessionService) cleanupCaptures(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type cap struct {
		id   int64
		path string
		ts   int64
	}
	var caps []cap
	for _, e := range entries {
		name := e.Name()
		id, ts, ok := parseCaptureName(name)
		if !ok {
			continue
		}
		caps = append(caps, cap{id: id, path: filepath.Join(dir, name), ts: ts})
	}
	byID := map[int64][]cap{}
	for _, c := range caps {
		byID[c.id] = append(byID[c.id], c)
	}
	for _, list := range byID {
		if len(list) <= captureMaxPerSession {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].ts < list[j].ts })
		for _, old := range list[:len(list)-captureMaxPerSession] {
			_ = os.Remove(old.path)
		}
	}
}

// parseCaptureName — session-<id>-<ts>.txt → (id, ts, ok).
func parseCaptureName(name string) (int64, int64, bool) {
	if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".txt") {
		return 0, 0, false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(name, "session-"), ".txt")
	parts := strings.Split(mid, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	id, err1 := strconv.ParseInt(parts[0], 10, 64)
	ts, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return id, ts, true
}
