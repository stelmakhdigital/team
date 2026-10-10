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
	"daemon/internal/models"
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

// checkSessionDivergence — КЛЮЧЕВОЙ: где БД-статус расходится с реальной живостью.
//   - stale: БД active, адаптер — мёртв → fail (показываем «working», но сессии нет).
//   - zombie: БД stopped/failed, адаптер — жив → warn (утёкший процесс/панель).
//
// Бounded: active ≤50, stopped/failed ≤10. Read-only: adapter error → не считаем (conservative).
func (s *DiagnosticsService) checkSessionDivergence(ctx context.Context) CheckResult {
	if s.stores == nil || s.stores.Sessions == nil {
		return CheckResult{Name: "sessions.divergence", Status: CheckWarn, Detail: "session store not wired"}
	}
	if s.registry == nil {
		active, _, err := s.stores.Sessions.List(ctx, s.db, repository.SessionFilter{Active: true, Limit: 50})
		if err != nil {
			return CheckResult{Name: "sessions.divergence", Status: CheckFail, Detail: "list active sessions: " + err.Error()}
		}
		return CheckResult{Name: "sessions.divergence", Status: CheckWarn,
			Detail:   fmt.Sprintf("%d active session(s), adapter registry not wired (liveness not checked)", len(active)),
			Evidence: map[string]any{"active": len(active), "checked": 0}}
	}

	// 1) stale: active в БД, мёртв в адаптере.
	active, _, err := s.stores.Sessions.List(ctx, s.db, repository.SessionFilter{Active: true, Limit: 50})
	if err != nil {
		return CheckResult{Name: "sessions.divergence", Status: CheckFail, Detail: "list active sessions: " + err.Error()}
	}
	var staleRefs []string
	var staleSessions []map[string]any
	checked := 0
	for _, sess := range active {
		alive, ok := s.refAlive(ctx, sess)
		if !ok {
			continue
		}
		checked++
		if !alive {
			staleRefs = append(staleRefs, fmt.Sprintf("#%d(%s)", sess.ID, sess.RuntimeType))
			staleSessions = append(staleSessions, sessEvidence(sess))
		}
	}

	// 2) zombie: stopped/failed в БД, жив в адаптере (утёкший процесс/панель).
	var zombieRefs []string
	var zombieSessions []map[string]any
	for _, state := range []models.SessionState{models.SessionStopped, models.SessionFailed} {
		stopped, err := s.stores.Sessions.ListByState(ctx, s.db, state, 10)
		if err != nil {
			continue
		}
		for _, sess := range stopped {
			alive, ok := s.refAlive(ctx, sess)
			if !ok || !alive {
				continue
			}
			zombieRefs = append(zombieRefs, fmt.Sprintf("#%d(%s)", sess.ID, sess.RuntimeType))
			zombieSessions = append(zombieSessions, sessEvidence(sess))
			if len(zombieRefs) >= 20 {
				break
			}
		}
	}

	evidence := map[string]any{"active": len(active), "checked": checked}
	if len(staleSessions) > 0 {
		evidence["stale"] = staleRefs
		evidence["stale_sessions"] = staleSessions
	}
	if len(zombieSessions) > 0 {
		evidence["zombie"] = zombieRefs
		evidence["zombie_sessions"] = zombieSessions
	}
	if len(staleSessions) > 0 {
		return CheckResult{
			Name: "sessions.divergence", Status: CheckFail,
			Detail:   fmt.Sprintf("%d session(s) active in DB but not alive in adapter: %s", len(staleSessions), strings.Join(staleRefs, ", ")),
			Evidence: evidence,
		}
	}
	if len(zombieSessions) > 0 {
		return CheckResult{
			Name: "sessions.divergence", Status: CheckWarn,
			Detail:   fmt.Sprintf("%d session(s) stopped/failed in DB but still alive in adapter (zombie): %s", len(zombieSessions), strings.Join(zombieRefs, ", ")),
			Evidence: evidence,
		}
	}
	return CheckResult{
		Name: "sessions.divergence", Status: CheckOK,
		Detail:   fmt.Sprintf("%d active session(s), %d checked, no divergence", len(active), checked),
		Evidence: evidence,
	}
}

// refAlive — живость runtime_ref через адаптер (false/ok=false если adapter не знает ref).
func (s *DiagnosticsService) refAlive(ctx context.Context, sess *models.Session) (alive, ok bool) {
	if sess.RuntimeRef == "" {
		return false, false
	}
	adapter, err := s.registry.Get(sess.RuntimeType)
	if err != nil {
		return false, false
	}
	c, cancel := context.WithTimeout(ctx, s.execTimeout)
	defer cancel()
	st, err := adapter.Status(c, sess.RuntimeRef)
	if err != nil {
		return false, false // conservative: adapter error — не считаем расхождением
	}
	return st.Alive, true
}

// sessEvidence — структурированные данные сессии для UI drill-down (EntityLink).
func sessEvidence(sess *models.Session) map[string]any {
	return map[string]any{
		"id":      sess.ID,
		"runtime": sess.RuntimeType,
		"label":   fmt.Sprintf("Сессия #%d", sess.ID),
	}
}

// FailChecks — список чек'ов с fail-статусом (для алерта в Attention).
func (r *DiagnosticsResult) FailChecks() []CheckResult {
	var out []CheckResult
	for _, c := range r.Checks {
		if c.Status == CheckFail {
			out = append(out, c)
		}
	}
	return out
}

// ReportFail — fail-severity findings → алерт в Attention (P0.1), read-only-безопасно:
// при наличии fail-чек'ов создаёт ОДИН unread watchdog-event diagnostics_fail
// (critical, link на /health). Дедуп: если уже есть unread diagnostics_fail — не дублирует.
// Возвращает id созданного события (0 — не создавал: нет fail или уже есть).
func (s *DiagnosticsService) ReportFail(ctx context.Context, res *DiagnosticsResult) (int64, error) {
	fails := res.FailChecks()
	if len(fails) == 0 || s.stores == nil || s.stores.Watchdog == nil {
		return 0, nil
	}
	exists, err := s.stores.Watchdog.ExistsUnreadByType(ctx, s.db, "diagnostics_fail")
	if err != nil {
		return 0, fmt.Errorf("diagnostics report: exists-unread: %w", err)
	}
	if exists {
		return 0, nil // дедуп: алерт уже в Attention
	}
	var names []string
	for _, c := range fails {
		names = append(names, c.Name)
	}
	evt := &models.WatchdogEvent{
		EventType:      "diagnostics_fail",
		Severity:       "critical",
		Description:    fmt.Sprintf("Diagnostics: %d check(s) failed: %s", len(fails), strings.Join(names, ", ")),
		IsRead:         false,
		RequiresAction: true,
	}
	if err := s.stores.Watchdog.Create(ctx, s.db, evt); err != nil {
		return 0, fmt.Errorf("diagnostics report: create event: %w", err)
	}
	return evt.ID, nil
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
