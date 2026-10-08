package service

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"
)

// session.output — live-терминал (контракт 20 §4.3, slice 7): батчи строк
// transcript-лога сессии. Интервал батчей — 500ms, событие публикуется ТОЛЬКО
// при новых строках (молчание → тишина). Каналы: 'session:<id>', 'team:<id>'
// (+ 'dashboard' добавляет newEvent автоматически).
const (
	outputBatchInterval  = 500 * time.Millisecond
	outputMaxBatchLines  = 500     // защита медленных подписчиков (контракт: batch ≤ 500ms)
	outputMaxReadPerTick = 1 << 20 // 1MB за тик — всё остальное догоняется след. тиками
	outputLineMaxLen     = 4000    // длиннее — обрезаем (UI-терминал)
)

type tailState struct {
	offset  int64
	partial []byte
	stop    chan struct{}
}

// startOutputTailer — запуск goroutine-тейлера лога сессии (no-op без Bus).
func (s *SessionService) startOutputTailer(sessID, teamID int64, roleName, logPath string) {
	if s.Bus == nil {
		return
	}
	s.tailMu.Lock()
	if _, ok := s.tails[sessID]; ok {
		s.tailMu.Unlock()
		return
	}
	st := &tailState{stop: make(chan struct{})}
	s.tails[sessID] = st
	s.tailMu.Unlock()
	go s.tailLoop(sessID, teamID, roleName, logPath, st)
}

// stopOutputTailer — остановка тейлера (вызов из markSessionState при
// stopped/failed; повтор — no-op).
func (s *SessionService) stopOutputTailer(sessID int64) {
	s.tailMu.Lock()
	st := s.tails[sessID]
	delete(s.tails, sessID)
	s.tailMu.Unlock()
	if st != nil {
		close(st.stop)
	}
}

func (s *SessionService) tailLoop(sessID, teamID int64, roleName, path string, st *tailState) {
	ticker := time.NewTicker(outputBatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-st.stop:
			return
		case <-ticker.C:
		}

		lines, newPartial, newOffset, ok := readNewLogLines(path, st.offset, st.partial)
		if !ok {
			continue // файла ещё нет / ошибки чтения — ждём след. тик
		}
		st.offset, st.partial = newOffset, newPartial
		if len(lines) == 0 {
			continue // молчание — события НЕ публикуем
		}
		if len(lines) > outputMaxBatchLines {
			lines = lines[len(lines)-outputMaxBatchLines:]
		}
		now := time.Now().UTC().Format(time.RFC3339)
		out := make([]any, 0, len(lines))
		for _, l := range lines {
			if len(l) > outputLineMaxLen {
				l = l[:outputLineMaxLen]
			}
			out = append(out, map[string]any{"ts": now, "text": l, "stream": "stdout"})
		}
		s.Bus.Publish(newEvent("session.output", map[string]any{
			"session_id": sessID,
			"role_name":  roleName,
			"lines":      out,
		}, fmt.Sprintf("team:%d", teamID), fmt.Sprintf("session:%d", sessID)))
	}
}

// readNewLogLines — новые полные строки файла с offset (частичная строка
// догоняется в partial). Файл короче offset (truncate/rotate) → переоткрытие
// с нуля. Возвращает (строки, новый partial, новый offset, ok).
func readNewLogLines(path string, offset int64, partial []byte) ([]string, []byte, int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, partial, offset, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, partial, offset, false
	}
	size := fi.Size()
	if size < offset {
		offset, partial = 0, nil // truncate/rotate — читаем заново
	}
	if size == offset && len(partial) == 0 {
		return nil, partial, offset, true // новых данных нет
	}
	n := size - offset
	if n > outputMaxReadPerTick {
		n = outputMaxReadPerTick
	}
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, offset)
	if err != nil && err.Error() != "unexpected EOF" {
		return nil, partial, offset, false
	}
	buf = buf[:got]
	full := append(append([]byte{}, partial...), buf...)
	// последняя неполная строка (без завершающего \n) — в partial
	cut := len(full)
	if cut > 0 && full[cut-1] != '\n' {
		if i := bytes.LastIndexByte(full, '\n'); i >= 0 {
			partial = append([]byte{}, full[i+1:]...)
			full = full[:i+1]
		} else {
			partial = append([]byte{}, full...)
			full = nil
		}
	} else {
		partial = nil
	}
	newOffset := offset + int64(got)
	if len(full) == 0 {
		return nil, partial, newOffset, true
	}
	lines := strings.Split(strings.TrimSuffix(string(full), "\n"), "\n")
	return lines, partial, newOffset, true
}
