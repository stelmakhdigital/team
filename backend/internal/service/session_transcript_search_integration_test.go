package service_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P2.7: SearchTranscripts (интеграция: БД + файлы + фильтры) ----------

// mkTranscript — пишет transcript-файл сессии (logDir/session-<id>.log).
func mkTranscript(t *testing.T, logDir string, sessionID int64, lines []string) {
	t.Helper()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i, ln := range lines {
		b.WriteString(ln)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	if err := os.WriteFile(filepath.Join(logDir, "session-"+strconv.FormatInt(sessionID, 10)+".log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkSession(t *testing.T, e *sessionEnv, teamID, roleID int64, state models.SessionState) int64 {
	t.Helper()
	sess := &models.Session{TeamID: teamID, RoleID: roleID, RuntimeType: "process", State: state}
	if err := e.stores.Sessions.Create(e.ctx, e.db, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

func TestSearchTranscriptsMatch(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "search-team")
	// role_name из spec-файлов: role_a.yaml → "role_a.yaml"
	s1 := mkSession(t, e, teamID, roleA, models.SessionRunning)
	s2 := mkSession(t, e, teamID, roleB, models.SessionStopped)
	mkTranscript(t, e.ssvc.LogsDir, s1, []string{
		"start", "work", "found the target alpha here", "more", "done", "end",
	})
	mkTranscript(t, e.ssvc.LogsDir, s2, []string{
		"begin", "the target beta is here", "stop",
	})

	res, err := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "target"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || len(res.Results) != 2 {
		t.Fatalf("total = %d (results=%d), want 2", res.Total, len(res.Results))
	}
	// session_id desc → s2 (большой id) первым
	if res.Results[0].SessionID != s2 {
		t.Fatalf("first result session = %d, want %d (desc)", res.Results[0].SessionID, s2)
	}
	// context-окно ±3 вокруг совпадения
	r0 := res.Results[0]
	if len(r0.Context) < 2 {
		t.Fatalf("context too short: %v", r0.Context)
	}
	if !strings.Contains(strings.ToLower(r0.MatchLine), "target beta") {
		t.Fatalf("match_line = %q, want contains 'target beta'", r0.MatchLine)
	}
	// role_name + team_name + state решены
	if r0.State != "stopped" || r0.TeamName == "" || r0.RoleName == "" {
		t.Fatalf("result meta wrong: state=%q team=%q role=%q", r0.State, r0.TeamName, r0.RoleName)
	}
	// case-insensitive: "TARGET" тоже находит
	res2, _ := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "TARGET"})
	if res2.Total != 2 {
		t.Fatalf("case-insensitive total = %d, want 2", res2.Total)
	}
}

func TestSearchTranscriptsFilters(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, roleB := makeTeamWithRoles(t, e.svc, "search-filters")
	s1 := mkSession(t, e, teamID, roleA, models.SessionRunning)
	s2 := mkSession(t, e, teamID, roleB, models.SessionStopped)
	mkTranscript(t, e.ssvc.LogsDir, s1, []string{"needle here"})
	mkTranscript(t, e.ssvc.LogsDir, s2, []string{"needle here"})

	// фильтр role_id=roleA → только s1
	resRole, _ := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "needle", RoleID: &roleA})
	if resRole.Total != 1 || resRole.Results[0].SessionID != s1 {
		t.Fatalf("role filter: total=%d first=%d, want 1/%d", resRole.Total, resRole.Results[0].SessionID, s1)
	}
	// фильтр state=stopped → только s2
	st := models.SessionStopped
	resState, _ := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "needle", State: &st})
	if resState.Total != 1 || resState.Results[0].SessionID != s2 {
		t.Fatalf("state filter: total=%d first=%d, want 1/%d", resState.Total, resState.Results[0].SessionID, s2)
	}
	// фильтр team_id
	resTeam, _ := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "needle", TeamID: &teamID})
	if resTeam.Total != 2 {
		t.Fatalf("team filter: total=%d, want 2", resTeam.Total)
	}
}

func TestSearchTranscriptsEmpty(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "search-empty")
	s1 := mkSession(t, e, teamID, roleA, models.SessionRunning)
	mkTranscript(t, e.ssvc.LogsDir, s1, []string{"no match here", "still nothing"})
	res, err := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "zzzznotfound"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 || len(res.Results) != 0 || res.HasMore {
		t.Fatalf("empty: total=%d results=%d hasMore=%v, want 0/0/false", res.Total, len(res.Results), res.HasMore)
	}
}

func TestSearchTranscriptsMissingFile(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "search-missing")
	// сессия БЕЗ transcript-файла → пропускается (не ошибка)
	_ = mkSession(t, e, teamID, roleA, models.SessionRunning)
	// + сессия с файлом
	s2 := mkSession(t, e, teamID, roleA, models.SessionRunning)
	mkTranscript(t, e.ssvc.LogsDir, s2, []string{"found needle"})
	res, err := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "needle"})
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("missing-file: total=%d, want 1 (skipped missing)", res.Total)
	}
}

func TestSearchTranscriptsBoundedTail(t *testing.T) {
	e := setupSessionEnv(t)
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, "search-bounded")
	s1 := mkSession(t, e, teamID, roleA, models.SessionRunning)
	// 20100 строк: match "headonly" на строке 0 (в голове), "tailneedle" на последней.
	// Tail-ограничение (20k) → "headonly" НЕ найден, "tailneedle" найден.
	lines := make([]string, 20100)
	lines[0] = "headonly marker"
	for i := 1; i < 20099; i++ {
		lines[i] = "filler"
	}
	lines[20099] = "tailneedle marker"
	mkTranscript(t, e.ssvc.LogsDir, s1, lines)

	resHead, _ := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "headonly"})
	if resHead.Total != 0 {
		t.Fatalf("bounded: head match found (total=%d), want 0 (tail only)", resHead.Total)
	}
	resTail, _ := e.ssvc.SearchTranscripts(e.ctx, service.TranscriptSearchParams{Q: "tailneedle"})
	if resTail.Total != 1 {
		t.Fatalf("bounded: tail match total=%d, want 1", resTail.Total)
	}
}
