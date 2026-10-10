package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/runtime"
)

// P1.8 — Health/Doctor-панель: «the surface that shows the truth».
// Read-only: показывает состояние daemon/БД/tmux/сессий/logs/git + ГДЕ РАСХОЖДАЮТСЯ
// БД-статус и реальная живость. Не чинит — только показывает расхождения.

type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

type CheckResult struct {
	Name     string         `json:"name"`
	Status   CheckStatus    `json:"status"`
	Detail   string         `json:"detail"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type DiagnosticsResult struct {
	GeneratedAt string        `json:"generated_at"`
	Overall     CheckStatus   `json:"overall"`
	Checks      []CheckResult `json:"checks"`
}

type DiagnosticsService struct {
	db          *sql.DB
	stores      *repository.Stores
	registry    *runtime.Registry
	dialect     string
	logsDir     string
	version     string
	startTime   time.Time
	execTimeout time.Duration
}

// NewDiagnosticsService — doctor-сервис. Registry опционален (nil → без divergence-чека).
func NewDiagnosticsService(db *sql.DB, stores *repository.Stores, registry *runtime.Registry, dialect, logsDir, version string) *DiagnosticsService {
	if version == "" {
		version = "0.1.0"
	}
	return &DiagnosticsService{
		db: db, stores: stores, registry: registry, dialect: dialect,
		logsDir: logsDir, version: version, startTime: time.Now(),
		execTimeout: 5 * time.Second,
	}
}

// Run — выполняет все чек'и (bounded, read-only, не падает на отдельной ошибке).
func (s *DiagnosticsService) Run(ctx context.Context) *DiagnosticsResult {
	checks := []CheckResult{
		s.checkDaemon(),
		s.checkDatabasePing(ctx),
		s.checkMigrations(ctx),
		s.checkTmux(ctx),
		s.checkSessionDivergence(ctx),
		s.checkLogsDir(),
		s.checkGit(ctx),
	}
	return &DiagnosticsResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Overall:     overallStatus(checks),
		Checks:      checks,
	}
}

func overallStatus(checks []CheckResult) CheckStatus {
	worst := CheckOK
	for _, c := range checks {
		switch c.Status {
		case CheckFail:
			return CheckFail
		case CheckWarn:
			worst = CheckWarn
		}
	}
	return worst
}

// checkDaemon — daemon up + version + uptime.
func (s *DiagnosticsService) checkDaemon() CheckResult {
	return CheckResult{
		Name:   "daemon",
		Status: CheckOK,
		Detail: "daemon running",
		Evidence: map[string]any{
			"version":  s.version,
			"uptime_s": int64(time.Since(s.startTime).Seconds()),
			"dialect":  s.dialect,
		},
	}
}

// checkDatabasePing — БД отвечает.
func (s *DiagnosticsService) checkDatabasePing(ctx context.Context) CheckResult {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.db.PingContext(c); err != nil {
		return CheckResult{Name: "database.ping", Status: CheckFail, Detail: "db ping failed: " + err.Error()}
	}
	return CheckResult{Name: "database.ping", Status: CheckOK, Detail: "database reachable"}
}

// checkMigrations — применённые vs доступные (расхождение → warn).
func (s *DiagnosticsService) checkMigrations(ctx context.Context) CheckResult {
	applied, available, err := database.MigrationStatus(ctx, s.db, s.dialect)
	if err != nil {
		return CheckResult{Name: "database.migrations", Status: CheckFail, Detail: "migrations check failed: " + err.Error()}
	}
	if available > applied {
		return CheckResult{
			Name:   "database.migrations",
			Status: CheckWarn,
			Detail: fmt.Sprintf("%d/%d migrations applied (%d pending)", applied, available, available-applied),
			Evidence: map[string]any{
				"applied": applied, "available": available, "pending": available - applied,
			},
		}
	}
	return CheckResult{
		Name:     "database.migrations",
		Status:   CheckOK,
		Detail:   fmt.Sprintf("all %d migrations applied", applied),
		Evidence: map[string]any{"applied": applied, "available": available},
	}
}

// checkTmux — tmux установлен/сервер жив (warn, если нет — process/pi не зависят).
func (s *DiagnosticsService) checkTmux(ctx context.Context) CheckResult {
	if _, err := exec.LookPath("tmux"); err != nil {
		return CheckResult{Name: "tmux.server", Status: CheckWarn, Detail: "tmux not installed (process/pi runtimes unaffected)"}
	}
	c, cancel := context.WithTimeout(ctx, s.execTimeout)
	defer cancel()
	out, err := exec.CommandContext(c, "tmux", "list-sessions").Output()
	if err != nil {
		return CheckResult{Name: "tmux.server", Status: CheckWarn, Detail: "tmux installed, but no server running"}
	}
	trimmed := strings.TrimSpace(string(out))
	n := 0
	if trimmed != "" {
		n = len(strings.Split(trimmed, "\n"))
	}
	return CheckResult{
		Name:     "tmux.server",
		Status:   CheckOK,
		Detail:   fmt.Sprintf("tmux server up (%d session(s))", n),
		Evidence: map[string]any{"sessions": n},
	}
}

// checkSessionDivergence — КЛЮЧЕВОЙ: БД говорит active, а адаптер — мёртв → fail.
func (s *DiagnosticsService) checkSessionDivergence(ctx context.Context) CheckResult {
	if s.stores == nil || s.stores.Sessions == nil {
		return CheckResult{Name: "sessions.divergence", Status: CheckWarn, Detail: "session store not wired"}
	}
	active, _, err := s.stores.Sessions.List(ctx, s.db, repository.SessionFilter{Active: true, Limit: 50})
	if err != nil {
		return CheckResult{Name: "sessions.divergence", Status: CheckFail, Detail: "list active sessions: " + err.Error()}
	}
	if len(active) == 0 {
		return CheckResult{Name: "sessions.divergence", Status: CheckOK, Detail: "no active sessions"}
	}
	if s.registry == nil {
		return CheckResult{Name: "sessions.divergence", Status: CheckWarn,
			Detail:   fmt.Sprintf("%d active session(s), adapter registry not wired (liveness not checked)", len(active)),
			Evidence: map[string]any{"active": len(active), "checked": 0}}
	}
	var stale []string
	checked := 0
	for _, sess := range active {
		if sess.RuntimeRef == "" {
			continue
		}
		adapter, err := s.registry.Get(sess.RuntimeType)
		if err != nil {
			continue
		}
		c, cancel := context.WithTimeout(ctx, s.execTimeout)
		st, err := adapter.Status(c, sess.RuntimeRef)
		cancel()
		if err != nil {
			continue // adapter error — не считаем stale (conservative)
		}
		checked++
		if !st.Alive {
			stale = append(stale, fmt.Sprintf("#%d(%s)", sess.ID, sess.RuntimeType))
		}
	}
	if len(stale) > 0 {
		return CheckResult{
			Name:   "sessions.divergence",
			Status: CheckFail,
			Detail: fmt.Sprintf("%d session(s) active in DB but not alive in adapter: %s", len(stale), strings.Join(stale, ", ")),
			Evidence: map[string]any{
				"active": len(active), "checked": checked, "stale": stale,
			},
		}
	}
	return CheckResult{
		Name:     "sessions.divergence",
		Status:   CheckOK,
		Detail:   fmt.Sprintf("%d active session(s), %d checked, all alive in adapter", len(active), checked),
		Evidence: map[string]any{"active": len(active), "checked": checked},
	}
}

// checkLogsDir — logs-dir существует + writable.
func (s *DiagnosticsService) checkLogsDir() CheckResult {
	if s.logsDir == "" {
		return CheckResult{Name: "logs.dir", Status: CheckWarn, Detail: "logs dir not configured"}
	}
	info, err := os.Stat(s.logsDir)
	if err != nil {
		return CheckResult{Name: "logs.dir", Status: CheckFail,
			Detail: "logs dir missing: " + s.logsDir, Evidence: map[string]any{"path": s.logsDir}}
	}
	tf, err := os.CreateTemp(s.logsDir, ".diagnostics-*")
	writable := err == nil
	if writable {
		name := tf.Name()
		_ = tf.Close()
		_ = os.Remove(name)
	}
	entries := 0
	if listing, err := os.ReadDir(s.logsDir); err == nil {
		entries = len(listing)
	}
	if !writable {
		return CheckResult{Name: "logs.dir", Status: CheckFail,
			Detail:   "logs dir not writable: " + s.logsDir,
			Evidence: map[string]any{"path": s.logsDir, "writable": false}}
	}
	return CheckResult{
		Name:   "logs.dir",
		Status: CheckOK,
		Detail: "logs dir writable",
		Evidence: map[string]any{
			"path": s.logsDir, "writable": true, "entries": entries, "is_dir": info.IsDir(),
		},
	}
}

// checkGit — git-статус по git-проектам (bounded 3, read-only + timeout, как P1.7).
func (s *DiagnosticsService) checkGit(ctx context.Context) CheckResult {
	var repos []string
	if s.stores != nil && s.stores.Projects != nil {
		projects, _, err := s.stores.Projects.List(ctx, s.db, repository.ProjectFilter{}, 10, 0)
		if err == nil {
			for _, p := range projects {
				if p.Git && p.Path != "" {
					repos = append(repos, p.Path)
					if len(repos) >= 3 {
						break
					}
				}
			}
		}
	}
	if len(repos) == 0 {
		return CheckResult{Name: "git", Status: CheckOK, Detail: "no git projects to check (skipped)"}
	}
	var okCount, errs int
	var detail []string
	for _, dir := range repos {
		c, cancel := context.WithTimeout(ctx, s.execTimeout)
		branch, err := exec.CommandContext(c, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
		head, herr := exec.CommandContext(c, "git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
		cancel()
		base := shortPath(dir)
		if err != nil {
			errs++
			detail = append(detail, base+": not a git repo")
			continue
		}
		okCount++
		b := strings.TrimSpace(string(branch))
		if herr == nil {
			b += " @ " + strings.TrimSpace(string(head))
		}
		detail = append(detail, base+": "+b)
	}
	status := CheckOK
	if errs > 0 {
		status = CheckWarn // часть проектов не git — warn (не fail: git опционален)
	}
	return CheckResult{
		Name:     "git",
		Status:   status,
		Detail:   fmt.Sprintf("checked %d git project(s): %d ok, %d not-a-repo", okCount+errs, okCount, errs),
		Evidence: map[string]any{"checked": len(repos), "detail": detail},
	}
}

func shortPath(p string) string {
	if len(p) <= 24 {
		return p
	}
	return "…" + p[len(p)-24:]
}
