package service_test

import (
	"testing"

	"daemon/internal/models"
	"daemon/internal/service"
)

// ---------- P3.15: Mailbox (inbox/outbox) ----------

func TestInboxDirectOnlyAndUndelivered(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, workerID, _ := makeMessageTeam(t, svc, "mb-inbox")

	// direct → worker (будет в inbox)
	direct, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &workerID, Type: "direct", Body: "direct-to-worker",
	})
	if err != nil {
		t.Fatalf("send direct: %v", err)
	}
	// segment (ToRoleID=worker, type=segment) — НЕ в inbox (type-filter: только direct)
	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &workerID, Type: "segment", Body: "core-sync",
	}); err != nil {
		t.Fatalf("send segment: %v", err)
	}
	// broadcast (без ToRoleID) — НЕ в inbox
	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, Type: "broadcast", Body: "standup-broadcast",
	}); err != nil {
		t.Fatalf("send broadcast: %v", err)
	}

	res, err := msvc.Inbox(ctx(), workerID, 50, 0)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	// только direct → 1 сообщение (segment + broadcast исключены)
	if res.Total != 1 || len(res.Messages) != 1 {
		t.Fatalf("inbox total=%d len=%d, want 1/1 (segment+broadcast не в inbox)", res.Total, len(res.Messages))
	}
	if res.Messages[0].ID != direct.ID || res.Messages[0].Body != "direct-to-worker" {
		t.Fatalf("inbox msg = %d/%q, want %d/direct-to-worker", res.Messages[0].ID, res.Messages[0].Body, direct.ID)
	}
	if res.Messages[0].AckState != "pending" {
		t.Fatalf("ack_state = %q, want pending", res.Messages[0].AckState)
	}
	// undelivered_count = 1 (pending)
	if res.UndeliveredCount != 1 {
		t.Fatalf("undelivered_count = %d, want 1", res.UndeliveredCount)
	}
}

func TestInboxBoundedAnd404(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, workerID, _ := makeMessageTeam(t, svc, "mb-inbox-page")

	for i := 0; i < 3; i++ {
		if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
			TeamID: teamID, ToRoleID: &workerID, Type: "direct", Body: "m",
		}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	res, err := msvc.Inbox(ctx(), workerID, 2, 0)
	if err != nil {
		t.Fatalf("inbox page: %v", err)
	}
	if res.Total != 3 || len(res.Messages) != 2 || !res.HasMore {
		t.Fatalf("inbox page: total=%d len=%d has_more=%v, want 3/2/true", res.Total, len(res.Messages), res.HasMore)
	}
	// 404 (нет роли)
	if _, err := msvc.Inbox(ctx(), 999, 50, 0); err == nil {
		t.Fatal("unknown role: want not_found")
	}
	if _, err := msvc.Inbox(ctx(), 999, 50, 0); err != nil {
		isAppErr(t, err, "not_found")
	}
}

func TestAckAbsorbDenyDrop(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, workerID, _ := makeMessageTeam(t, svc, "mb-ack")

	sendToWorker := func() int64 {
		m, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
			TeamID: teamID, ToRoleID: &workerID, Type: "direct", Body: "x",
		})
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		return m.ID
	}

	// absorb
	view, err := msvc.AckMessage(ctx(), sendToWorker(), models.AckAbsorb, "lead")
	if err != nil {
		t.Fatalf("ack absorb: %v", err)
	}
	if view.AckState != "absorbed" || view.AckedBy == nil || *view.AckedBy != "lead" || view.AckedAt == nil {
		t.Fatalf("absorb: state=%q by=%v at=%v, want absorbed/lead/set", view.AckState, view.AckedBy, view.AckedAt)
	}

	// deny
	view, err = msvc.AckMessage(ctx(), sendToWorker(), models.AckDeny, "worker")
	if err != nil {
		t.Fatalf("ack deny: %v", err)
	}
	if view.AckState != "denied" || view.AckedBy == nil || *view.AckedBy != "worker" {
		t.Fatalf("deny: state=%q by=%v, want denied/worker", view.AckState, view.AckedBy)
	}

	// drop
	view, err = msvc.AckMessage(ctx(), sendToWorker(), models.AckDrop, "op")
	if err != nil {
		t.Fatalf("ack drop: %v", err)
	}
	if view.AckState != "dropped" {
		t.Fatalf("drop: state=%q, want dropped", view.AckState)
	}
}

func TestAckRepeat409Invalid400And404(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, workerID, _ := makeMessageTeam(t, svc, "mb-ack-err")
	m, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &workerID, Type: "direct", Body: "x",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// первый ack OK
	if _, err := msvc.AckMessage(ctx(), m.ID, models.AckAbsorb, "lead"); err != nil {
		t.Fatalf("first ack: %v", err)
	}
	// повторный ack → 409 (conflict)
	_, err = msvc.AckMessage(ctx(), m.ID, models.AckDeny, "lead")
	isAppErr(t, err, "conflict")

	// invalid action → 400 (validation_failed)
	_, err = msvc.AckMessage(ctx(), m.ID, models.AckAction("bogus"), "lead")
	isAppErr(t, err, "validation_failed")

	// 404 (нет сообщения)
	_, err = msvc.AckMessage(ctx(), 999, models.AckAbsorb, "lead")
	isAppErr(t, err, "not_found")
}

func TestOutboxAnd404(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, leadID, workerID, _ := makeMessageTeam(t, svc, "mb-outbox")

	// worker → lead (FromRoleID=worker) — в outbox worker
	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, FromRoleID: &workerID, ToRoleID: &leadID, Type: "direct", Body: "from-worker",
	}); err != nil {
		t.Fatalf("send from worker: %v", err)
	}
	// оператор (FromRoleID=nil) — НЕ в outbox worker
	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &leadID, Type: "direct", Body: "from-op",
	}); err != nil {
		t.Fatalf("send from op: %v", err)
	}

	msgs, total, err := msvc.Outbox(ctx(), workerID, 50, 0)
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].Body != "from-worker" {
		t.Fatalf("outbox: total=%d len=%d body=%q, want 1/1/from-worker", total, len(msgs), msgs[0].Body)
	}
	// 404 (нет роли)
	if _, _, err := msvc.Outbox(ctx(), 999, 50, 0); err == nil {
		t.Fatal("unknown role outbox: want not_found")
	}
	if _, _, err := msvc.Outbox(ctx(), 999, 50, 0); err != nil {
		isAppErr(t, err, "not_found")
	}
}
