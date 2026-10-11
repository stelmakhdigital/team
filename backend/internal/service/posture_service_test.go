package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"daemon/internal/database"
	"daemon/internal/models"
	"daemon/internal/repository"
	"daemon/internal/runtime"
	"daemon/internal/service"
)

// ---------- P3.16: PostureService (scope resolution + Check + Decide) ----------

type postureEnv struct {
	db     *sql.DB
	stores *repository.Stores
	config *service.ConfigService
	svc    *service.TeamService
	ssvc   *service.SessionService
	psvc   *service.PostureService
	ctx    context.Context
}

func setupPostureEnv(t *testing.T) *postureEnv {
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
	if err := os.MkdirAll(filepath.Join(svc.SpecsDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"role_a.yaml", "role_b.yaml"} {
		if err := os.WriteFile(filepath.Join(svc.SpecsDir, "agents", n), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := service.NewConfigService(db)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ssvc := service.NewSessionService(db, stores, runtime.NewRegistry())
	ssvc.LogsDir = filepath.Join(t.TempDir(), "logs")
	ssvc.ConfigsDir = filepath.Join(t.TempDir(), "configs")
	psvc := service.NewPostureService(db, stores, cfg, nil)
	return &postureEnv{db: db, stores: stores, config: cfg, svc: svc, ssvc: ssvc,
		psvc: psvc, ctx: context.Background()}
}

// makePostureTeamSession — команда + 2 роли + одна сессия (для scope tests).
func (e *postureEnv) makeTeamSession(t *testing.T, teamName string) (teamID, sessionID int64) {
	t.Helper()
	teamID, roleA, _ := makeTeamWithRoles(t, e.svc, teamName)
	sess, err := e.ssvc.CreateSession(e.ctx, teamID, service.CreateSessionRequest{
		RoleID: roleA, RuntimeType: "process", Command: "echo",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return teamID, sess.ID
}

// M8: sensitive registry — какие операции чувствительные.
func TestMatchSensitive(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
		wantOp       string
	}{
		{"DELETE", "/api/v1/sessions/1", true, "session.stop"},
		{"DELETE", "/api/v1/tasks/7", true, "delete"},
		{"DELETE", "/api/v1/teams/3", true, "delete"},
		{"POST", "/api/v1/workflow-instances/5/abort", true, "workflow.abort"},
		{"POST", "/api/v1/sessions/1/handover", true, "session.handover"},
		{"DELETE", "/api/v1/library/9", true, "library.prune"},
		{"PUT", "/api/v1/config/posture.default", true, "config.set"},
		{"DELETE", "/api/v1/config/posture.default", true, "delete"},
		// non-sensitive
		{"GET", "/api/v1/sessions/1", false, ""},
		{"POST", "/api/v1/sessions/1", false, ""},
		{"PATCH", "/api/v1/sessions/1", false, ""},
		{"GET", "/api/v1/attention", false, ""},
		{"POST", "/api/v1/human-requests/1/decide", false, ""}, // decide — НЕ sensitive (иначе рекурсия)
	}
	for _, c := range cases {
		op, _, got := service.MatchSensitive(c.method, c.path)
		if got != c.want {
			t.Errorf("MatchSensitive(%s %s) ok = %v, want %v", c.method, c.path, got, c.want)
		}
		if c.want && c.wantOp != "" && op != c.wantOp {
			t.Errorf("MatchSensitive(%s %s) op = %q, want %q", c.method, c.path, op, c.wantOp)
		}
	}
}

// M8: scope resolution (session→team, task→team, global default).
func TestPostureScopeResolution(t *testing.T) {
	e := setupPostureEnv(t)
	teamID, sessionID := e.makeTeamSession(t, "posture-scope")

	// по умолчанию — delegated (team default)
	if p := e.psvc.EffectivePosture(e.ctx, "/api/v1/sessions/"+itoa(sessionID)+"/stop"); p != models.PostureDelegated {
		t.Fatalf("default effective = %v, want delegated", p)
	}
	// team → human_led
	if err := e.psvc.SetTeamPosture(e.ctx, teamID, models.PostureHumanLed); err != nil {
		t.Fatalf("SetTeamPosture: %v", err)
	}
	// session scope наследует от team
	if p := e.psvc.EffectivePosture(e.ctx, "/api/v1/sessions/"+itoa(sessionID)+"/stop"); p != models.PostureHumanLed {
		t.Fatalf("session inherits team = %v, want human_led", p)
	}
	// session override → delegated (перекрывает team)
	if err := e.psvc.SetSessionPosture(e.ctx, sessionID, ptr(models.PostureDelegated)); err != nil {
		t.Fatalf("SetSessionPosture: %v", err)
	}
	if p := e.psvc.EffectivePosture(e.ctx, "/api/v1/sessions/"+itoa(sessionID)+"/stop"); p != models.PostureDelegated {
		t.Fatalf("session override = %v, want delegated", p)
	}
	// global scope → config default (delegated)
	if p := e.psvc.EffectivePosture(e.ctx, "/api/v1/config/posture.default"); p != models.PostureDelegated {
		t.Fatalf("global default = %v, want delegated", p)
	}
	// team scope → human_led
	if p := e.psvc.EffectivePosture(e.ctx, "/api/v1/teams/"+itoa(teamID)); p != models.PostureHumanLed {
		t.Fatalf("team scope = %v, want human_led", p)
	}
}

func ptr[T any](v T) *T { return &v }
func itoa(v int64) string {
	b := []byte{}
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if len(b) == 0 {
		return "0"
	}
	return string(b)
}

// M8: Check — delegated → не паркуем; human_led + sensitive → park; non-sensitive → не паркуем; dedup.
func TestPostureCheck(t *testing.T) {
	e := setupPostureEnv(t)
	_, sessionID := e.makeTeamSession(t, "posture-check")

	// delegated → не паркуем (DELETE session)
	park, reqID, op, err := e.psvc.Check(e.ctx, "DELETE", "/api/v1/sessions/"+itoa(sessionID), "", "key")
	if err != nil {
		t.Fatalf("Check delegated: %v", err)
	}
	if park || reqID != 0 || op != "" {
		t.Fatalf("delegated: park=%v reqID=%d op=%q, want no park", park, reqID, op)
	}

	// human_led + sensitive → park
	team, _, err := e.stores.Teams.List(e.ctx, e.db, 100, 0)
	if err != nil {
		t.Fatalf("list teams: %v", err)
	}
	if err := e.psvc.SetTeamPosture(e.ctx, team[0].ID, models.PostureHumanLed); err != nil {
		t.Fatalf("set human_led: %v", err)
	}
	park, reqID, op, err = e.psvc.Check(e.ctx, "DELETE", "/api/v1/sessions/"+itoa(sessionID), "", "key")
	if err != nil {
		t.Fatalf("Check human_led: %v", err)
	}
	if !park || reqID == 0 || op != "session.stop" {
		t.Fatalf("human_led+stop: park=%v reqID=%d op=%q, want park session.stop", park, reqID, op)
	}

	// non-sensitive (POST session) → не паркуем даже в human_led
	if park, _, _, err = e.psvc.Check(e.ctx, "POST", "/api/v1/sessions", "", "key"); err != nil {
		t.Fatalf("Check non-sensitive: %v", err)
	} else if park {
		t.Fatal("non-sensitive: park=true, want false")
	}

	// dedup: повторный CHECK на ту же операцию → тот же reqID (не создаёт дубль)
	park2, reqID2, _, err := e.psvc.Check(e.ctx, "DELETE", "/api/v1/sessions/"+itoa(sessionID), "", "key")
	if err != nil {
		t.Fatalf("Check dedup: %v", err)
	}
	if !park2 || reqID2 != reqID {
		t.Fatalf("dedup: park=%v reqID=%d, want same %d", park2, reqID2, reqID)
	}
}

// M8: Decide — approve/deny, 409 на повтор, 404.
func TestPostureDecide(t *testing.T) {
	e := setupPostureEnv(t)
	_, sessionID := e.makeTeamSession(t, "posture-decide")
	team, _, err := e.stores.Teams.List(e.ctx, e.db, 100, 0)
	if err != nil {
		t.Fatalf("list teams: %v", err)
	}
	_ = e.psvc.SetTeamPosture(e.ctx, team[0].ID, models.PostureHumanLed)
	_, reqID, _, err := e.psvc.Check(e.ctx, "DELETE", "/api/v1/sessions/"+itoa(sessionID), "", "key")
	if err != nil || reqID == 0 {
		t.Fatalf("park: %v %d", err, reqID)
	}

	// approve
	reason := "looks good"
	hr, err := e.psvc.Decide(e.ctx, reqID, "approve", reason, "human-lead")
	if err != nil {
		t.Fatalf("Decide approve: %v", err)
	}
	if hr.Status != models.HRApproved {
		t.Fatalf("status = %v, want approved", hr.Status)
	}
	if hr.DecidedBy == nil || *hr.DecidedBy != "human-lead" {
		t.Fatal("decided_by not set")
	}

	// повторный decide → 409 (conflict)
	if _, err := e.psvc.Decide(e.ctx, reqID, "deny", "", "other"); err == nil {
		t.Fatal("repeat decide: want conflict, got nil")
	}

	// deny на другой запрос
	_, reqID2, _, _ := e.psvc.Check(e.ctx, "DELETE", "/api/v1/sessions/"+itoa(sessionID+1), "", "key")
	if reqID2 != 0 {
		hr2, err := e.psvc.Decide(e.ctx, reqID2, "deny", "no", "human-lead")
		if err != nil {
			t.Fatalf("Decide deny: %v", err)
		}
		if hr2.Status != models.HRDenied {
			t.Fatalf("status = %v, want denied", hr2.Status)
		}
	}

	// 404
	if _, err := e.psvc.Decide(e.ctx, 999999, "approve", "", "x"); err == nil {
		t.Fatal("decide unknown: want 404, got nil")
	}

	// invalid decision → validation
	_, reqID3, _, _ := e.psvc.Check(e.ctx, "DELETE", "/api/v1/sessions/"+itoa(sessionID+2), "", "key")
	if reqID3 != 0 {
		if _, err := e.psvc.Decide(e.ctx, reqID3, "maybe", "", "x"); err == nil {
			t.Fatal("invalid decision: want validation, got nil")
		}
	}
}
