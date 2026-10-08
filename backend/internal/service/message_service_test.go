package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/repository"
	"daemon/internal/service"
)

// setupMessageEnv — DB + team service + message service.
func setupMessageEnv(t *testing.T) (*service.TeamService, *service.MessageService) {
	t.Helper()
	db, err := database.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stores := repository.NewStores(db)
	svc := service.NewTeamService(db, stores)
	svc.SpecsDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(svc.SpecsDir, "a.yaml"), []byte("name: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc.SpecsDir, "b.yaml"), []byte("name: b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msvc := service.NewMessageService(db, stores)
	return svc, msvc
}

// makeMessageTeam — команда: сегмент core (lead, worker) + review (reviewer).
func makeMessageTeam(t *testing.T, svc *service.TeamService, name string) (teamID, coreSeg, leadID, workerID, reviewerID int64) {
	t.Helper()
	team, err := svc.CreateTeam(ctx(), service.CreateTeamRequest{
		Name: name,
		Spec: &service.TeamSpec{
			Segments: []service.SegmentSpec{{Name: "core"}, {Name: "review"}},
			Roles: []service.RoleSpec{
				{Segment: "core", Name: "lead", AgentSpec: "a.yaml"},
				{Segment: "core", Name: "worker", AgentSpec: "b.yaml"},
				{Segment: "review", Name: "reviewer", AgentSpec: "a.yaml"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	detail, err := svc.GetTeam(ctx(), team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	for _, seg := range detail.Segments {
		if seg.Name == "core" {
			coreSeg = seg.ID
		}
	}
	for _, r := range detail.Roles {
		switch r.Name {
		case "lead":
			leadID = r.ID
		case "worker":
			workerID = r.ID
		case "reviewer":
			reviewerID = r.ID
		}
	}
	return team.ID, coreSeg, leadID, workerID, reviewerID
}

// ---------- Chatrooms (авто-создание) ----------

func TestChatroomsAutoCreated(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, _, _ := makeMessageTeam(t, svc, "auto-rooms")

	rooms, err := msvc.ListChatrooms(ctx(), &teamID, nil)
	if err != nil {
		t.Fatalf("ListChatrooms: %v", err)
	}
	// 1 team-level + 2 segment-level
	if len(rooms) != 3 {
		t.Fatalf("chatrooms = %d, want 3: %+v", len(rooms), rooms)
	}
	byName := map[string]*service.ChatroomView{}
	for _, r := range rooms {
		byName[r.Name] = r
	}
	teamRoom, ok := byName["auto-rooms"]
	if !ok {
		t.Fatalf("team-level chatroom %q not found: %+v", "auto-rooms", rooms)
	}
	if teamRoom.SegmentID != nil {
		t.Fatalf("team room SegmentID = %v, want nil", *teamRoom.SegmentID)
	}
	if teamRoom.MembersCount != 3 {
		t.Fatalf("team room members_count = %d, want 3", teamRoom.MembersCount)
	}
	coreRoom, ok := byName["core-general"]
	if !ok {
		t.Fatalf("segment chatroom %q not found: %+v", "core-general", rooms)
	}
	if coreRoom.MembersCount != 2 {
		t.Fatalf("core room members_count = %d, want 2", coreRoom.MembersCount)
	}

	// last_message обновляется после отправки
	view, err := msvc.SendChatroomMessage(ctx(), coreRoom.ID, struct {
		Body       string `json:"body"`
		FromRoleID *int64 `json:"from_role_id"`
	}{Body: "hello team"})
	if err != nil {
		t.Fatalf("SendChatroomMessage: %v", err)
	}
	if view.IsMine != true || view.FromRoleName != "You" {
		t.Fatalf("operator message: is_mine=%v from_role_name=%q, want true/You", view.IsMine, view.FromRoleName)
	}
	rooms, _ = msvc.ListChatrooms(ctx(), &teamID, nil)
	for _, r := range rooms {
		if r.ID == coreRoom.ID {
			if r.LastMessage == nil || r.LastMessage.Body != "hello team" || r.LastMessage.FromRoleName != "You" {
				t.Fatalf("last_message = %+v, want hello team / You", r.LastMessage)
			}
		}
	}
}

func TestChatroomMessagesPaginationAnd404(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, _, _ := makeMessageTeam(t, svc, "room-page")
	rooms, _ := msvc.ListChatrooms(ctx(), &teamID, nil)
	var roomID int64
	for _, r := range rooms {
		if r.Name == "core-general" {
			roomID = r.ID
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := msvc.SendChatroomMessage(ctx(), roomID, struct {
			Body       string `json:"body"`
			FromRoleID *int64 `json:"from_role_id"`
		}{Body: "msg"}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	msgs, total, err := msvc.GetChatroomMessages(ctx(), roomID, 2, 0, nil)
	if err != nil {
		t.Fatalf("GetChatroomMessages: %v", err)
	}
	if total != 3 || len(msgs) != 2 {
		t.Fatalf("total=%d len=%d, want 3/2", total, len(msgs))
	}
	rest, _, err := msvc.GetChatroomMessages(ctx(), roomID, 2, 2, nil)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(rest) != 1 {
		t.Fatalf("page 2 len = %d, want 1", len(rest))
	}

	if _, _, err := msvc.GetChatroomMessages(ctx(), 999, 10, 0, nil); err == nil {
		t.Fatal("unknown chatroom: want not_found error")
	}
}

// ---------- Messages ----------

func TestSendMessageDirectBroadcastSegment(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, leadID, workerID, reviewerID := makeMessageTeam(t, svc, "msg-types")

	// direct
	direct, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &workerID, Type: "direct", Body: "ping",
	})
	if err != nil {
		t.Fatalf("direct: %v", err)
	}
	if len(direct.DeliveredTo) != 1 || direct.DeliveredTo[0] != workerID {
		t.Fatalf("direct delivered_to = %v, want [%d]", direct.DeliveredTo, workerID)
	}

	// broadcast → все 3 роли
	bcast, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, Type: "broadcast", Body: "standup",
	})
	if err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if len(bcast.DeliveredTo) != 3 {
		t.Fatalf("broadcast delivered_to = %v, want 3 roles", bcast.DeliveredTo)
	}

	// segment → все роли сегмента to_role_id (core: lead+worker)
	seg, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &leadID, Type: "segment", Body: "core-sync",
	})
	if err != nil {
		t.Fatalf("segment: %v", err)
	}
	if len(seg.DeliveredTo) != 2 {
		t.Fatalf("segment delivered_to = %v, want 2 roles", seg.DeliveredTo)
	}
	// reviewer не должен попасть в delivered_to
	for _, id := range seg.DeliveredTo {
		if id == reviewerID {
			t.Fatalf("segment delivered to reviewer %d, want only core roles", reviewerID)
		}
	}
}

func TestSendMessageValidation(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, leadID, _, _ := makeMessageTeam(t, svc, "msg-val")
	otherTeam, _, _, _, _ := makeMessageTeam(t, svc, "msg-val-other")

	// неизвестная команда
	var err error
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: 999, ToRoleID: &leadID, Type: "direct", Body: "x",
	})
	isAppErr(t, err, "not_found")

	// archived команда
	_ = svc.ArchiveTeam(ctx(), otherTeam)
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: otherTeam, ToRoleID: &leadID, Type: "direct", Body: "x",
	})
	isAppErr(t, err, "conflict")

	// server-internal тип
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &leadID, Type: "system", Body: "x",
	})
	isAppErr(t, err, "validation_failed")

	// пустое тело
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &leadID, Type: "direct",
	})
	isAppErr(t, err, "validation_failed")

	// direct без to_role_id
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, Type: "direct", Body: "x",
	})
	isAppErr(t, err, "validation_failed")

	// segment без to_role_id
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, Type: "segment", Body: "x",
	})
	isAppErr(t, err, "validation_failed")

	// роль из другой команды
	_, _, _, _, otherReviewer := makeMessageTeam(t, svc, "msg-val-third")
	_, err = msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &otherReviewer, Type: "direct", Body: "x",
	})
	isAppErr(t, err, "validation_failed")
}

func TestListMessagesFilters(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, leadID, workerID, _ := makeMessageTeam(t, svc, "msg-filters")
	otherTeam, _, otherLead, _, _ := makeMessageTeam(t, svc, "msg-filters-other")

	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &workerID, Type: "direct", Body: "a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: teamID, ToRoleID: &leadID, Type: "direct", Body: "b",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := msvc.SendMessage(ctx(), service.SendMessageRequest{
		TeamID: otherTeam, ToRoleID: &otherLead, Type: "direct", Body: "c",
	}); err != nil {
		t.Fatal(err)
	}

	all, total, err := msvc.ListMessages(ctx(), repository.MessageFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("all: total=%d len=%d, want 3", total, len(all))
	}
	// от оператора: is_mine = true, from_role_name = "You" (консистентно с chatrooms)
	if !all[0].IsMine {
		t.Fatalf("operator message is_mine = false, want true")
	}
	if all[0].FromRoleName == nil || *all[0].FromRoleName != "You" {
		t.Fatalf("operator message from_role_name = %v, want You", all[0].FromRoleName)
	}
	// to_role_name подставлен
	if all[0].ToRoleName == nil {
		t.Fatal("to_role_name = nil, want role name")
	}

	byTeam, total, err := msvc.ListMessages(ctx(), repository.MessageFilter{TeamID: &teamID, Limit: 50})
	if err != nil {
		t.Fatalf("list by team: %v", err)
	}
	if total != 2 || len(byTeam) != 2 {
		t.Fatalf("by team: total=%d, want 2", total)
	}

	toRole := workerID
	byTo, total, err := msvc.ListMessages(ctx(), repository.MessageFilter{TeamID: &teamID, ToRoleID: &toRole, Limit: 50})
	if err != nil {
		t.Fatalf("list by to_role: %v", err)
	}
	if total != 1 || byTo[0].Body != "a" {
		t.Fatalf("by to_role: total=%d body=%q, want 1/a", total, byTo[0].Body)
	}

	// сортировка: новые раньше
	if byTeam[0].ID < byTeam[1].ID {
		t.Fatalf("order: want newest first, got %d then %d", byTeam[0].ID, byTeam[1].ID)
	}
}
