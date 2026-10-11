package service_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// evidenceEnv — self-contained окружение (db + stores + services), чтобы иметь
// доступ к db/stores для EvidenceService + SetEvidenceRepo.
type evidenceEnv struct {
	db     *sql.DB
	stores *repository.Stores
	svc    *service.TeamService
	tsvc   *service.TaskService
	ctx    context.Context
}

func setupEvidenceEnv(t *testing.T) *evidenceEnv {
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
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsvc := service.NewTaskService(db, stores)
	tsvc.SetEvidenceRepo(stores.Evidence)
	return &evidenceEnv{db: db, stores: stores, svc: svc, tsvc: tsvc, ctx: context.Background()}
}

func (e *evidenceEnv) ev() *service.EvidenceService {
	return service.NewEvidenceService(e.db, e.stores)
}

// makeEvidenceTask — команда + 2 роли + одна задача (pending).
func (e *evidenceEnv) makeTask(t *testing.T) (teamID, roleA, roleB, taskID int64) {
	t.Helper()
	teamID, roleA, roleB = makeTeamWithRoles(t, e.svc, "evidence-team")
	task, err := e.tsvc.CreateTask(e.ctx, service.CreateTaskRequest{
		TeamID: teamID, DestinationRoleID: roleA, Title: "evidence task",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return teamID, roleA, roleB, task.ID
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func strp(s string) *string { return &s }

// E2: submit (inline sha, ref sha, валидация, атрибуция).
func TestEvidenceSubmit(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)

	// inline → серверный sha256
	content := "screenshot-bytes-123"
	e1, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{
		TaskID: taskID, Type: models.EvidenceScreenshot, Name: "ui.png", Content: &content, SubmittedBy: "lead",
	})
	if err != nil {
		t.Fatalf("Submit inline: %v", err)
	}
	if e1.Status != models.EvidenceSubmitted {
		t.Fatalf("status = %v, want submitted", e1.Status)
	}
	if e1.ContentSHA == nil || *e1.ContentSHA != sha256hex(content) {
		t.Fatalf("inline content_sha = %v, want sha256(%q)", e1.ContentSHA, content)
	}
	if e1.SubmittedBy != "lead" {
		t.Fatalf("submitted_by = %q, want lead", e1.SubmittedBy)
	}

	// ref → sha из body
	ref := "https://artifacts/x/log.txt"
	e2, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{
		TaskID: taskID, Type: models.EvidenceLog, Name: "build.log", ContentRef: &ref,
		ContentSHA: strp("abc123"), SubmittedBy: "worker",
	})
	if err != nil {
		t.Fatalf("Submit ref: %v", err)
	}
	if e2.ContentSHA == nil || *e2.ContentSHA != "abc123" {
		t.Fatalf("ref content_sha = %v, want abc123", e2.ContentSHA)
	}

	// атрибуция по умолчанию = human (auth выключен)
	e3, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{
		TaskID: taskID, Type: models.EvidenceOther, Name: "note", Content: strp("hi"),
	})
	if err != nil {
		t.Fatalf("Submit default actor: %v", err)
	}
	if e3.SubmittedBy != "human" {
		t.Fatalf("default submitted_by = %q, want human", e3.SubmittedBy)
	}
}

func TestEvidenceSubmitValidation(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)
	content := "x"

	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: "bogus", Name: "n", Content: &content}); err == nil {
		t.Fatal("want error for invalid etype")
	}
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "", Content: &content}); err == nil {
		t.Fatal("want error for empty name")
	}
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "n"}); err == nil {
		t.Fatal("want error when no content/ref")
	}
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: 999999, Type: models.EvidenceTest, Name: "n", Content: &content}); err == nil {
		t.Fatal("want 404 for unknown task")
	}
}

func TestEvidenceList(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t1", Content: strp("a")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceLog, Name: "t2", Content: strp("b")}); err != nil {
		t.Fatal(err)
	}
	items, total, err := e.ev().List(e.ctx, taskID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(items))
	}
	if _, _, err := e.ev().List(e.ctx, 999999, 50, 0); err == nil {
		t.Fatal("want 404 for unknown task")
	}
}

func TestEvidenceVerdict(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)
	content := "artifact"
	ev, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t", Content: &content, SubmittedBy: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	reason := "looks good"

	// withdraw из submitted → 400
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: ev.ID, Action: models.EvidenceWithdraw, JudgedBy: "judge"}); err == nil {
		t.Fatal("want 400 for withdraw from submitted")
	}
	// accept → status=accepted + judged_by/at
	acc, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: ev.ID, Action: models.EvidenceAccept, Reason: &reason, JudgedBy: "judge"})
	if err != nil {
		t.Fatal(err)
	}
	if acc.Status != models.EvidenceAccepted || acc.JudgedBy == nil || *acc.JudgedBy != "judge" || acc.JudgedAt == nil {
		t.Fatalf("accept: status=%v judged_by=%v judged_at=%v", acc.Status, acc.JudgedBy, acc.JudgedAt)
	}
	// reject (последний вердикт побеждает)
	rej, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: ev.ID, Action: models.EvidenceReject, JudgedBy: "judge2"})
	if err != nil {
		t.Fatal(err)
	}
	if rej.Status != models.EvidenceRejected {
		t.Fatalf("reject: status=%v", rej.Status)
	}
	// withdraw из rejected → withdrawn
	w, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: ev.ID, Action: models.EvidenceWithdraw, JudgedBy: "judge3"})
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != models.EvidenceWithdrawn {
		t.Fatalf("withdraw: status=%v", w.Status)
	}
}

func TestEvidenceVerdictErrors(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)
	content := "artifact"
	ev, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t", Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: ev.ID, Action: "bogus"}); err == nil {
		t.Fatal("want 400 for invalid action")
	}
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: 12345, EvidenceID: ev.ID, Action: models.EvidenceAccept}); err == nil {
		t.Fatal("want 404 for cross-task verdict")
	}
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: 999999, Action: models.EvidenceAccept}); err == nil {
		t.Fatal("want 404 for unknown evidence")
	}
}

// E3: verdict на terminal-задаче допустим (acceptance ≠ closure).
func TestEvidenceVerdictOnTerminalTask(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)
	if _, err := e.tsvc.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: taskID, State: models.TaskDone, ClosureReason: ptrReason(models.ClosureNoFollowOn),
	}); err != nil {
		t.Fatalf("close task: %v", err)
	}
	content := "late artifact"
	ev, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t", Content: &content})
	if err != nil {
		t.Fatalf("submit on terminal task: %v", err)
	}
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: ev.ID, Action: models.EvidenceAccept, JudgedBy: "judge"}); err != nil {
		t.Fatalf("verdict on terminal task must be allowed: %v", err)
	}
}

// E3: readiness/acceptance агрегаты в TaskView.
func TestEvidenceReadinessAcceptance(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)

	// none (нет evidence)
	detail, err := e.tsvc.GetTask(e.ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.Acceptance != "none" || detail.Task.Readiness != "none" || detail.Task.EvidenceCount != 0 {
		t.Fatalf("empty: acceptance=%q readiness=%q count=%d, want none/none/0",
			detail.Task.Acceptance, detail.Task.Readiness, detail.Task.EvidenceCount)
	}

	// submitted → pending/pending
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t1", Content: strp("a")}); err != nil {
		t.Fatal(err)
	}
	detail, _ = e.tsvc.GetTask(e.ctx, taskID)
	if detail.Task.Acceptance != "pending" || detail.Task.Readiness != "pending" || detail.Task.EvidenceCount != 1 {
		t.Fatalf("submitted: acceptance=%q readiness=%q count=%d, want pending/pending/1",
			detail.Task.Acceptance, detail.Task.Readiness, detail.Task.EvidenceCount)
	}

	// reject → rejected (acceptance) / pending (readiness)
	items, _, _ := e.ev().List(e.ctx, taskID, 50, 0)
	rejReason := "nope"
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: items[0].ID, Action: models.EvidenceReject, Reason: &rejReason}); err != nil {
		t.Fatal(err)
	}
	detail, _ = e.tsvc.GetTask(e.ctx, taskID)
	if detail.Task.Acceptance != "rejected" || detail.Task.Readiness != "pending" {
		t.Fatalf("rejected: acceptance=%q readiness=%q, want rejected/pending",
			detail.Task.Acceptance, detail.Task.Readiness)
	}

	// accept (новый артефакт) → accepted/ready
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t2", Content: strp("b")}); err != nil {
		t.Fatal(err)
	}
	items, _, _ = e.ev().List(e.ctx, taskID, 50, 0)
	if _, err := e.ev().Verdict(e.ctx, service.VerdictEvidenceRequest{TaskID: taskID, EvidenceID: items[0].ID, Action: models.EvidenceAccept}); err != nil {
		t.Fatal(err)
	}
	detail, _ = e.tsvc.GetTask(e.ctx, taskID)
	if detail.Task.Acceptance != "accepted" || detail.Task.Readiness != "ready" || detail.Task.EvidenceCount != 2 {
		t.Fatalf("accepted: acceptance=%q readiness=%q count=%d, want accepted/ready/2",
			detail.Task.Acceptance, detail.Task.Readiness, detail.Task.EvidenceCount)
	}
}

// E3: closure ≠ acceptance — задача done, но acceptance=pending.
func TestClosureNotAcceptance(t *testing.T) {
	e := setupEvidenceEnv(t)
	_, _, _, taskID := e.makeTask(t)

	if _, err := e.tsvc.UpdateTaskState(e.ctx, service.UpdateTaskStateRequest{
		ID: taskID, State: models.TaskDone, ClosureReason: ptrReason(models.ClosureNoFollowOn),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ev().Submit(e.ctx, service.SubmitEvidenceRequest{TaskID: taskID, Type: models.EvidenceTest, Name: "t", Content: strp("x")}); err != nil {
		t.Fatal(err)
	}
	detail, err := e.tsvc.GetTask(e.ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.State != "done" || detail.Task.Acceptance != "pending" {
		t.Fatalf("state=%q acceptance=%q, want done/pending (closure ≠ acceptance)",
			detail.Task.State, detail.Task.Acceptance)
	}
}
