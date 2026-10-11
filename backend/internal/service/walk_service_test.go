package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- fake clock (injectable, AdvanceDue) ----------

type fakeWalkClock struct{ now time.Time }

func (c *fakeWalkClock) Now() time.Time      { return c.now }
func (c *fakeWalkClock) Add(d time.Duration) { c.now = c.now.Add(d) }

type walkSendOutcome struct {
	res *service.SendToSessionResult
	err error
}

type walkMockSender struct {
	queue []walkSendOutcome
	calls []string
}

func (m *walkMockSender) enqueue(res *service.SendToSessionResult, err error) {
	m.queue = append(m.queue, walkSendOutcome{res: res, err: err})
}
func (m *walkMockSender) Send(ctx context.Context, sessionID int64, text string, submit bool) (*service.SendToSessionResult, error) {
	m.calls = append(m.calls, text)
	if len(m.queue) == 0 {
		return nil, errors.New("no queued send result")
	}
	out := m.queue[0]
	m.queue = m.queue[1:]
	return out.res, out.err
}

func walkSendStatus(s service.SendStatus) *service.SendToSessionResult {
	return &service.SendToSessionResult{Status: s, Sent: s != service.SendStatusRetained, SentAt: "t"}
}

// ---------- env ----------

// walkEnvT — self-contained: db + stores + team svc + walk svc (mock sender + fake clock).
type walkEnvT struct {
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	wsvc   *service.WalkService
	sender *walkMockSender
	clock  *fakeWalkClock
	ctx    context.Context
}

func setupWalkEnv(t *testing.T) *walkEnvT {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	_ = os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755)
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sender := &walkMockSender{}
	clock := &fakeWalkClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	wsvc := service.NewWalkService(db, stores, sender)
	wsvc.SetClock(clock)
	return &walkEnvT{db: db, stores: stores, svc: svc, wsvc: wsvc, sender: sender, clock: clock, ctx: context.Background()}
}

// makeSession — команда + роль + строка сессии (tmux) в Б.
func (e *walkEnvT) makeSession(t *testing.T) int64 {
	t.Helper()
	teamID, roleID, _ := makeTeamWithRoles(t, e.svc, "walk-team")
	sess := &models.Session{
		TeamID: teamID, RoleID: roleID,
		RuntimeType: "tmux", RuntimeRef: "walk-sess", State: models.SessionRunning,
	}
	if err := e.stores.Sessions.Create(e.ctx, e.db, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

// setWalkCreatedAt — align walk.created_at with fake clock (determinism for AdvanceDue).
func (e *walkEnvT) setWalkCreatedAt(t *testing.T, walkID int64, at time.Time) {
	t.Helper()
	if _, err := e.db.ExecContext(e.ctx, `UPDATE walks SET created_at = ? WHERE id = ?`, at.Format(time.RFC3339Nano), walkID); err != nil {
		t.Fatalf("set created_at: %v", err)
	}
}

// ---------- Create ----------

func TestWalkCreateValidation(t *testing.T) {
	e := setupWalkEnv(t)
	ctx := e.ctx

	// 404 session.
	if _, err := e.wsvc.CreateWalk(ctx, 99999, service.CreateWalkRequest{Name: "w", Mode: "manual", Steps: []string{"a"}}); err == nil {
		t.Fatal("want 404 for unknown session")
	}
	sid := e.makeSession(t)

	// empty name.
	if _, err := e.wsvc.CreateWalk(ctx, sid, service.CreateWalkRequest{Mode: "manual", Steps: []string{"a"}}); err == nil {
		t.Fatal("want 400 for empty name")
	}
	// invalid mode.
	if _, err := e.wsvc.CreateWalk(ctx, sid, service.CreateWalkRequest{Name: "w", Mode: "nope", Steps: []string{"a"}}); err == nil {
		t.Fatal("want 400 for invalid mode")
	}
	// no steps.
	if _, err := e.wsvc.CreateWalk(ctx, sid, service.CreateWalkRequest{Name: "w", Mode: "manual"}); err == nil {
		t.Fatal("want 400 for no steps")
	}
	// interval without interval_secs.
	if _, err := e.wsvc.CreateWalk(ctx, sid, service.CreateWalkRequest{Name: "w", Mode: "interval", Steps: []string{"a"}}); err == nil {
		t.Fatal("want 400 for interval without interval_secs")
	}
	// empty step content.
	if _, err := e.wsvc.CreateWalk(ctx, sid, service.CreateWalkRequest{Name: "w", Mode: "manual", Steps: []string{"a", "  "}}); err == nil {
		t.Fatal("want 400 for empty step content")
	}

	// success.
	v, err := e.wsvc.CreateWalk(ctx, sid, service.CreateWalkRequest{
		Name: "onboarding", Mode: "manual", Steps: []string{"hi", "step2", "step3"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v.Status != "active" || v.CurrentSeq != 1 || v.Total != 3 || v.SentCount != 0 || v.Progress != "0/3" {
		t.Fatalf("created walk = %+v, want active/seq1/3steps/0-3", v)
	}
	if len(v.Steps) != 3 || v.Steps[0].State != "pending" || v.Steps[2].Seq != 3 {
		t.Fatalf("steps = %+v, want 3 pending, seq 1..3", v.Steps)
	}
}

// ---------- Advance ----------

func TestWalkAdvanceCompleted(t *testing.T) {
	e := setupWalkEnv(t)
	sid := e.makeSession(t)
	v, err := e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{
		Name: "w", Mode: "manual", Steps: []string{"s1", "s2", "s3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)

	// advance 1: delivered, seq 2.
	a1, err := e.wsvc.Advance(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a1.CurrentSeq != 2 || a1.Status != "active" || a1.Progress != "1/3" || a1.Steps[0].State != "delivered" {
		t.Fatalf("after advance1 = %+v", a1)
	}
	// advance 2.
	e.wsvc.Advance(e.ctx, v.ID)
	// advance 3: last → completed.
	a3, err := e.wsvc.Advance(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a3.Status != "completed" || a3.CurrentSeq != 4 || a3.Progress != "3/3" {
		t.Fatalf("after advance3 = %+v, want completed/seq4/3-3", a3)
	}
	// advance on completed → 409.
	if _, err := e.wsvc.Advance(e.ctx, v.ID); err == nil {
		t.Fatal("want 409 advance on completed")
	}
	// mock sender called 3 times with the step contents.
	if len(e.sender.calls) != 3 || e.sender.calls[0] != "s1" || e.sender.calls[2] != "s3" {
		t.Fatalf("sender calls = %v, want [s1 s2 s3]", e.sender.calls)
	}
}

func TestWalkAdvanceRetainedPauses(t *testing.T) {
	e := setupWalkEnv(t)
	sid := e.makeSession(t)
	v, _ := e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{
		Name: "w", Mode: "manual", Steps: []string{"prompt-step", "s2"},
	})
	// first Send → retained (guard).
	ret := walkSendStatus(service.SendStatusRetained)
	ret.Guard = "interactive-prompt"
	e.sender.enqueue(ret, nil)

	a1, err := e.wsvc.Advance(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a1.Status != "paused" || a1.CurrentSeq != 1 || a1.Steps[0].State != "retained" || a1.Steps[0].Note == nil {
		t.Fatalf("after retained = %+v, want paused/seq1/step0 retained+note", a1)
	}
	// advance on paused → 409.
	if _, err := e.wsvc.Advance(e.ctx, v.ID); err == nil {
		t.Fatal("want 409 advance on paused")
	}
	// resume → active; retry the SAME step (seq still 1).
	r, err := e.wsvc.Resume(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "active" || r.CurrentSeq != 1 {
		t.Fatalf("after resume = %+v, want active/seq1", r)
	}
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	a2, _ := e.wsvc.Advance(e.ctx, v.ID) // retry step1 → delivered, seq 2
	if a2.CurrentSeq != 2 || a2.Steps[0].State != "delivered" || a2.Status != "active" {
		t.Fatalf("after retry = %+v, want seq2/delivered/active", a2)
	}
	// step2 → completed.
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	a3, _ := e.wsvc.Advance(e.ctx, v.ID)
	if a3.Status != "completed" {
		t.Fatalf("after final = %+v, want completed", a3)
	}
}

func TestWalkAdvanceFailed(t *testing.T) {
	e := setupWalkEnv(t)
	sid := e.makeSession(t)
	v, _ := e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{
		Name: "w", Mode: "manual", Steps: []string{"s1", "s2"},
	})
	e.sender.enqueue(nil, errors.New("tmux session dead"))
	a1, err := e.wsvc.Advance(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a1.Status != "failed" || a1.Steps[0].State != "failed" || a1.Steps[0].Note == nil {
		t.Fatalf("after failed = %+v, want failed/step0 failed+note", a1)
	}
	// advance on failed → 409.
	if _, err := e.wsvc.Advance(e.ctx, v.ID); err == nil {
		t.Fatal("want 409 advance on failed")
	}
}

// ---------- Pause / Resume / Skip ----------

func TestWalkPauseResumeSkip(t *testing.T) {
	e := setupWalkEnv(t)
	sid := e.makeSession(t)
	v, _ := e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{
		Name: "w", Mode: "manual", Steps: []string{"s1", "s2"},
	})
	// pause.
	p, err := e.wsvc.Pause(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "paused" {
		t.Fatalf("after pause = %+v", p)
	}
	// pause again → 409.
	if _, err := e.wsvc.Pause(e.ctx, v.ID); err == nil {
		t.Fatal("want 409 pause on paused")
	}
	// resume.
	r, err := e.wsvc.Resume(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "active" {
		t.Fatalf("after resume = %+v", r)
	}

	// skip: current step (seq1) → skipped, seq2.
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	sk, err := e.wsvc.Skip(e.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sk.CurrentSeq != 2 || sk.Steps[0].State != "skipped" || sk.Status != "active" {
		t.Fatalf("after skip = %+v, want seq2/step0 skipped", sk)
	}
	if len(e.sender.calls) != 0 {
		t.Fatalf("skip must not call sender, calls=%v", e.sender.calls)
	}
	// skip last → completed.
	sk2, _ := e.wsvc.Skip(e.ctx, v.ID)
	if sk2.Status != "completed" {
		t.Fatalf("after skip-last = %+v, want completed", sk2)
	}
}

// ---------- AdvanceDue (interval pacing + clock) ----------

func TestWalkAdvanceDueInterval(t *testing.T) {
	e := setupWalkEnv(t)
	sid := e.makeSession(t)
	interval := 3600
	v, err := e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{
		Name: "w", Mode: "interval", IntervalSecs: &interval, Steps: []string{"s1", "s2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.setWalkCreatedAt(t, v.ID, e.clock.now) // align created_at with fake clock T0.

	// at T0: not due (0 < 3600s).
	if err := e.wsvc.AdvanceDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.sender.calls) != 0 {
		t.Fatalf("not due at T0, but sender called %d times", len(e.sender.calls))
	}
	// advance 3600s → due → step1.
	e.clock.Add(time.Duration(interval) * time.Second)
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	if err := e.wsvc.AdvanceDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.sender.calls) != 1 {
		t.Fatalf("after due tick, sender calls = %d, want 1", len(e.sender.calls))
	}
	// immediately again (same time) → not due for step2 (now - last_sent_at = 0).
	if err := e.wsvc.AdvanceDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.sender.calls) != 1 {
		t.Fatalf("second tick same time, sender calls = %d, want 1 (pacing)", len(e.sender.calls))
	}
	// advance another interval → due → step2 (completed).
	e.clock.Add(time.Duration(interval) * time.Second)
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	_ = e.wsvc.AdvanceDue(e.ctx)
	w, _ := e.wsvc.GetWalk(e.ctx, v.ID)
	if w.Status != "completed" {
		t.Fatalf("after all due ticks = %+v, want completed", w)
	}
}

func TestWalkAdvanceDueManualNotAuto(t *testing.T) {
	e := setupWalkEnv(t)
	sid := e.makeSession(t)
	v, _ := e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{
		Name: "w", Mode: "manual", Steps: []string{"s1"},
	})
	e.setWalkCreatedAt(t, v.ID, e.clock.now)
	e.clock.Add(time.Hour)
	e.sender.enqueue(walkSendStatus(service.SendStatusDelivered), nil)
	if err := e.wsvc.AdvanceDue(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.sender.calls) != 0 {
		t.Fatalf("manual walk must not auto-advance, calls=%d", len(e.sender.calls))
	}
	w, _ := e.wsvc.GetWalk(e.ctx, v.ID)
	if w.Status != "active" || w.CurrentSeq != 1 {
		t.Fatalf("manual walk should be untouched = %+v", w)
	}
}

// ---------- Get / List / 404 ----------

func TestWalkGetListNotFound(t *testing.T) {
	e := setupWalkEnv(t)
	// get unknown → 404.
	if _, err := e.wsvc.GetWalk(e.ctx, 99999); err == nil {
		t.Fatal("want 404 get unknown walk")
	}
	sid := e.makeSession(t)
	// list unknown session → 404.
	if _, err := e.wsvc.ListWalks(e.ctx, 99999); err == nil {
		t.Fatal("want 404 list unknown session")
	}
	// create + list.
	_, _ = e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{Name: "a", Mode: "manual", Steps: []string{"x"}})
	_, _ = e.wsvc.CreateWalk(e.ctx, sid, service.CreateWalkRequest{Name: "b", Mode: "manual", Steps: []string{"y", "z"}})
	list, err := e.wsvc.ListWalks(e.ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %d, want 2", len(list))
	}
}
