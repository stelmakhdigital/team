package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// ---------- P2.7: Поиск по транскриптам (наивный, bounded, read-only) ----------
//
// Оператор ищет фразу по истории вывода всех сессий (транскрипты).
// Транскрипты — файлы <LogsDir>/session-<id>.log. Поиск наивный (substring,
// case-insensitive), bounded (tail ≤ transcriptMaxLines на файл, ≤ transcriptMaxSessions
// сессий), read-only, честный (файла нет/нечитаем → сессия пропускается, не ошибка).

const (
	// transcriptMaxLines — максимум строк транскрипта, сканируемых на файл (tail).
	// Решение по производительности: читаем только последние N строк (не весь файл в
	// память), как usage-scan. Транскрипты растут вниз → свежие совпадения в хвосте.
	transcriptMaxLines = 20_000
	// transcriptContext — контекст-окно вокруг совпадения (±N строк).
	transcriptContext = 3
	// transcriptMergeGap — слияние соседних окон, если они ближе этого (в строках).
	transcriptMergeGap = 3
	// transcriptMaxSessions — bounded: максимум сессий, сканируемых за один поиск.
	transcriptMaxSessions = 200
	// transcriptMaxSnippet — длина snippet (для быстрых предпросмотров/поисковиков).
	transcriptMaxSnippet = 500
	// transcriptMaxScan — время-бюджет скана (защита от долгих блокировок API).
	transcriptMaxScan = 3 * time.Second
)

type TranscriptSearchParams struct {
	Q      string
	RoleID *int64
	TeamID *int64
	State  *models.SessionState
	Limit  int
	Offset int
}

// TranscriptMatch — одно совпадение (контекст-окно) в транскрипте сессии.
type TranscriptMatch struct {
	SessionID  int64    `json:"session_id"`
	RoleName   string   `json:"role_name"`
	TeamName   string   `json:"team_name"`
	State      string   `json:"state"`
	LineNumber int      `json:"line_number"` // 1-based: первичное совпадение в окне
	MatchLine  string   `json:"match_line"`
	Context    []string `json:"context"` // строки окна (start..end)
	Snippet    string   `json:"snippet"` // короткая сводка окна
}

type TranscriptSearchResult struct {
	Results []TranscriptMatch `json:"results"`
	Total   int               `json:"total"`
	HasMore bool              `json:"has_more"`
}

// transcriptWindow — слиянное контекст-окно (0-based индексы строк, inclusive).
type transcriptWindow struct {
	start, end int
	primary    int // 0-based index первого совпадения в окне
}

// transcriptWindows — case-insensitive substring-матчи + контекст-окно ±ctx + слияние
// соседних окон (mergeGap). Чистая функция (детерминированная, тестируемая).
func transcriptWindows(lines []string, q string, ctx, mergeGap int) []transcriptWindow {
	needle := strings.ToLower(q)
	n := len(lines)
	var out []transcriptWindow
	for i, ln := range lines {
		if !strings.Contains(strings.ToLower(ln), needle) {
			continue
		}
		start := i - ctx
		if start < 0 {
			start = 0
		}
		end := i + ctx
		if end >= n {
			end = n - 1
		}
		if len(out) > 0 && start <= out[len(out)-1].end+mergeGap {
			if end > out[len(out)-1].end {
				out[len(out)-1].end = end
			}
			// primary остаётся первым совпадением окна (не обновляем при слиянии)
		} else {
			out = append(out, transcriptWindow{start: start, end: end, primary: i})
		}
	}
	return out
}

// readTailLines — последние maxLines строк файла (bounded памятью, один проход).
// Файл читается построчно; в памяти держим только кольцевой буфер из maxLines строк.
func readTailLines(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ring := make([]string, maxLines)
	idx := 0
	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024) // строка до 1MB
	for sc.Scan() {
		ring[idx] = sc.Text()
		idx = (idx + 1) % maxLines
		count++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	n := count
	if n > maxLines {
		n = maxLines
	}
	startIdx := (count - n) % maxLines
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ring[(startIdx+i)%maxLines])
	}
	return out, nil
}

// transcriptSnippet — короткая сводка окна (строки склеены, обрезаны).
func transcriptSnippet(lines []string) string {
	s := strings.Join(lines, "⏎ ")
	if len(s) <= transcriptMaxSnippet {
		return s
	}
	return s[:transcriptMaxSnippet] + "…"
}

// nameMaps — team_id→name + role_id→name для набора сессий (резолюция имён).
func (s *SessionService) nameMaps(ctx context.Context, sessions []*models.Session) (map[int64]string, map[int64]string) {
	teamName := map[int64]string{}
	roleName := map[int64]string{}
	for _, x := range sessions {
		if _, ok := teamName[x.TeamID]; !ok {
			team, err := s.teams.GetByID(ctx, s.db, x.TeamID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					teamName[x.TeamID] = ""
					continue
				}
				teamName[x.TeamID] = ""
			} else {
				teamName[x.TeamID] = team.Name
			}
		}
		if _, ok := roleName[x.RoleID]; !ok {
			role, err := s.roles.GetByID(ctx, s.db, x.RoleID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					roleName[x.RoleID] = ""
					continue
				}
				roleName[x.RoleID] = ""
			} else {
				roleName[x.RoleID] = role.Name
			}
		}
	}
	return teamName, roleName
}

// SearchTranscripts — поиск по транскриптам (bounded, read-only, честный).
func (s *SessionService) SearchTranscripts(ctx context.Context, p TranscriptSearchParams) (*TranscriptSearchResult, error) {
	q := strings.TrimSpace(p.Q)
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	deadline := time.Now().Add(transcriptMaxScan)

	// кандидаты: сессии с фильтром, newest-first (bounded ≤ transcriptMaxSessions).
	f := repository.SessionFilter{RoleID: p.RoleID, TeamID: p.TeamID, State: p.State, Limit: transcriptMaxSessions}
	sessions, _, err := s.sessions.List(ctx, s.db, f)
	if err != nil {
		return nil, fmt.Errorf("transcript search: list sessions: %w", err)
	}
	teamName, roleName := s.nameMaps(ctx, sessions)

	var items []TranscriptMatch
	for _, sess := range sessions {
		if time.Now().After(deadline) {
			break // время-бюджет: не блокируем API надолго
		}
		path := filepath.Join(s.LogsDir, fmt.Sprintf("session-%d.log", sess.ID))
		lines, err := readTailLines(path, transcriptMaxLines)
		if err != nil {
			continue // файла нет/нечитаем → пропускаем (честность, не ошибка)
		}
		for _, w := range transcriptWindows(lines, q, transcriptContext, transcriptMergeGap) {
			items = append(items, TranscriptMatch{
				SessionID:  sess.ID,
				RoleName:   roleName[sess.RoleID],
				TeamName:   teamName[sess.TeamID],
				State:      string(sess.State),
				LineNumber: w.primary + 1, // 1-based
				MatchLine:  lines[w.primary],
				Context:    lines[w.start : w.end+1],
				Snippet:    transcriptSnippet(lines[w.start : w.end+1]),
			})
		}
	}

	// Сортировка: session_id desc (новые сессии выше) → line_number asc (в сессии — по строкам).
	sort.Slice(items, func(i, j int) bool {
		if items[i].SessionID != items[j].SessionID {
			return items[i].SessionID > items[j].SessionID
		}
		return items[i].LineNumber < items[j].LineNumber
	})

	total := len(items)
	lo := p.Offset
	if lo > total {
		lo = total
	}
	hi := lo + limit
	if hi > total {
		hi = total
	}
	results := items[lo:hi]
	return &TranscriptSearchResult{Results: results, Total: total, HasMore: (p.Offset + limit) < total}, nil
}
