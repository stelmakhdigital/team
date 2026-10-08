package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"daemon/internal/database"
	"daemon/internal/service"
)

// setupSecurityEnv — DB (migrated) + AuthService.
func setupSecurityEnv(t *testing.T, envKeys []string) (*service.AuthService, *service.SecretsManager) {
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
	auth := service.NewAuthService(db, envKeys)
	if err := auth.Init(context.Background()); err != nil {
		t.Fatalf("auth init: %v", err)
	}
	sm, _ := service.NewSecretsManager(db, "0000000000000000000000000000000000000000000000000000000000000000")
	return auth, sm
}

// createTestKey — ключ с ролью через AuthService.
func createTestKey(t *testing.T, auth *service.AuthService, name, role, user string, exp time.Duration) (id int64, key string) {
	t.Helper()
	k, plaintext, err := auth.CreateKey(context.Background(), name, role, user, exp)
	if err != nil {
		t.Fatalf("CreateKey(%s): %v", name, err)
	}
	return k.ID, plaintext
}

// ---------- Auth / RBAC ----------

func TestAuthEnvKeyIsAdmin(t *testing.T) {
	auth, _ := setupSecurityEnv(t, []string{"env-admin-key"})
	if !auth.Enabled() {
		t.Fatal("auth must be enabled with env keys")
	}
	ac, err := auth.ValidateAPIKey(context.Background(), "env-admin-key")
	if err != nil {
		t.Fatalf("ValidateAPIKey: %v", err)
	}
	if ac.RoleName != "admin" {
		t.Errorf("role = %q, want admin", ac.RoleName)
	}
	if !ac.HasPerm("config.update") {
		t.Error("admin must have config.update")
	}
	if len(ac.UserName) < 9 || ac.UserName[:9] != "operator:" {
		t.Errorf("UserName = %q, want \"operator:<4 hex>\"", ac.UserName)
	}
	if ac.UserID != nil || ac.APIKeyID != nil {
		t.Error("env key must not have user_id/api_key_id")
	}
	if _, err := auth.ValidateAPIKey(context.Background(), "wrong"); err == nil {
		t.Error("wrong key must fail")
	}
}

func TestAuthDisabledWithoutKeys(t *testing.T) {
	auth, _ := setupSecurityEnv(t, nil)
	if auth.Enabled() {
		t.Fatal("auth must be disabled without env keys and DB keys")
	}
}

func TestAuthDBKeysAndRoles(t *testing.T) {
	auth, _ := setupSecurityEnv(t, nil)
	if auth.Enabled() {
		t.Fatal("auth must be disabled before any DB keys exist")
	}
	_, opKey := createTestKey(t, auth, "frontend", "operator", "frontend-user", 0)
	_, viewKey := createTestKey(t, auth, "viewer-cli", "viewer", "viewer-user", 0)
	_, admKey := createTestKey(t, auth, "admin-cli", "admin", "", 0)

	if err := auth.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !auth.Enabled() {
		t.Fatal("auth must be enabled with DB keys")
	}

	// operator
	ac, err := auth.ValidateAPIKey(context.Background(), opKey)
	if err != nil {
		t.Fatalf("operator key: %v", err)
	}
	if ac.UserName != "frontend-user" || ac.RoleName != "operator" {
		t.Errorf("operator ctx: user=%q role=%q", ac.UserName, ac.RoleName)
	}
	if ac.UserID == nil || ac.APIKeyID == nil {
		t.Error("operator ctx: user_id/api_key_id must be set")
	}
	if ac.HasPerm("tasks.create") != true {
		t.Error("operator must have tasks.create")
	}
	if ac.HasPerm("config.update") {
		t.Error("operator must NOT have config.update")
	}

	// viewer
	vac, err := auth.ValidateAPIKey(context.Background(), viewKey)
	if err != nil {
		t.Fatalf("viewer key: %v", err)
	}
	if vac.RoleName != "viewer" {
		t.Errorf("viewer role = %q", vac.RoleName)
	}
	if vac.HasPerm("tasks.read") != true {
		t.Error("viewer must have tasks.read")
	}
	if vac.HasPerm("tasks.create") {
		t.Error("viewer must NOT have tasks.create")
	}
	if vac.HasPerm("audit.read") != true {
		t.Error("viewer must have audit.read")
	}

	// admin (DB)
	aac, err := auth.ValidateAPIKey(context.Background(), admKey)
	if err != nil {
		t.Fatalf("admin key: %v", err)
	}
	if aac.RoleName != "admin" || !aac.HasPerm("config.update") {
		t.Error("admin DB key must have all permissions")
	}
}

func TestAuthKeyExpiredAndRevoked(t *testing.T) {
	auth, _ := setupSecurityEnv(t, nil)
	// ключ с expires=1s → после сна expired
	_, quick := createTestKey(t, auth, "quick", "operator", "quick-user", 1*time.Second)
	time.Sleep(1100 * time.Millisecond)
	if _, err := auth.ValidateAPIKey(context.Background(), quick); err == nil {
		t.Error("expired key must fail")
	}

	id, key := createTestKey(t, auth, "to-revoke", "operator", "revoke-user", 0)
	if err := auth.RevokeKey(context.Background(), id); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if _, err := auth.ValidateAPIKey(context.Background(), key); err == nil {
		t.Error("revoked key must fail")
	}
	if err := auth.RevokeKey(context.Background(), 999); err == nil {
		t.Error("revoke unknown id must fail")
	}
}

func TestAuthListKeys(t *testing.T) {
	auth, _ := setupSecurityEnv(t, nil)
	createTestKey(t, auth, "k1", "operator", "u1", 0)
	createTestKey(t, auth, "k2", "viewer", "u2", 72*time.Hour)
	keys, err := auth.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %d, want 2", len(keys))
	}
	if keys[0].Name != "k1" || keys[1].Name != "k2" {
		t.Errorf("unexpected names: %v, %v", keys[0].Name, keys[1].Name)
	}
	if keys[1].RoleName != "viewer" || keys[1].ExpiresAt == nil {
		t.Errorf("k2: role=%q expires=%v", keys[1].RoleName, keys[1].ExpiresAt)
	}
}

// ---------- Secrets ----------

func TestSecretsRoundtrip(t *testing.T) {
	_, sm := setupSecurityEnv(t, nil)
	ctx := context.Background()
	if err := sm.Store(ctx, "github_token", "ghp_secret_123", "CI token"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// значение в БД зашифровано
	if err := sm.Store(ctx, "github_token", "ghp_updated", "CI token"); err != nil {
		t.Fatalf("Store upsert: %v", err)
	}
	got, err := sm.Get(ctx, "github_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "ghp_updated" {
		t.Errorf("Get = %q, want ghp_updated", got)
	}
	if _, err := sm.Get(ctx, "missing"); err == nil {
		t.Error("Get missing must fail")
	}
	items, err := sm.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Name != "github_token" || items[0].Description != "CI token" {
		t.Errorf("List = %+v", items)
	}
	if err := sm.Delete(ctx, "github_token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := sm.Delete(ctx, "github_token"); err == nil {
		t.Error("Delete again must fail")
	}
}

// ---------- unread_count (slice 6: RBAC user) ----------

func TestChatroomUnreadCount(t *testing.T) {
	svc, msvc := setupMessageEnv(t)
	teamID, _, _, workerID, _ := makeMessageTeam(t, svc, "unread-team")
	var userID int64 = 7

	// без user — unread_count = 0
	rooms, err := msvc.ListChatrooms(context.Background(), &teamID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rooms {
		if r.UnreadCount != 0 {
			t.Errorf("no user: unread = %d, want 0", r.UnreadCount)
		}
	}

	// user ещё не читал → после чтения unread = 0, после 2 новых сообщений → 2
	teams, _ := msvc.ListChatrooms(context.Background(), &teamID, &userID)
	var teamRoomID int64
	for _, r := range teams {
		if r.SegmentID == nil {
			teamRoomID = r.ID
		}
	}
	if teamRoomID == 0 {
		t.Fatal("no team-level chatroom")
	}
	if _, _, err := msvc.GetChatroomMessages(ctx(), teamRoomID, 50, 0, &userID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := msvc.SendChatroomMessage(ctx(), teamRoomID, struct {
			Body       string `json:"body"`
			FromRoleID *int64 `json:"from_role_id"`
		}{Body: "hello", FromRoleID: &workerID}); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	rooms, _ = msvc.ListChatrooms(ctx(), &teamID, &userID)
	for _, r := range rooms {
		if r.ID == teamRoomID && r.UnreadCount != 2 {
			t.Errorf("team room unread = %d, want 2", r.UnreadCount)
		}
	}
	// прочитали → unread = 0
	if _, _, err := msvc.GetChatroomMessages(ctx(), teamRoomID, 50, 0, &userID); err != nil {
		t.Fatal(err)
	}
	rooms, _ = msvc.ListChatrooms(ctx(), &teamID, &userID)
	for _, r := range rooms {
		if r.ID == teamRoomID && r.UnreadCount != 0 {
			t.Errorf("after read: unread = %d, want 0", r.UnreadCount)
		}
	}
}

// TestCreateKeyExistingUserRole — regression: второй ключ для существующего user'а
// с ДРУГОЙ ролью → conflict (раньше ON CONFLICT DO NOTHING молча оставлял
// роль первого ключа → operator-ключ получал viewer-права).
func TestCreateKeyExistingUserRole(t *testing.T) {
	auth, _ := setupSecurityEnv(t, nil)

	// user fe-it создаётся с ролью viewer
	_, k1 := createTestKey(t, auth, "key-viewer", "viewer", "fe-it", 0)
	if k1 == "" {
		t.Fatal("key1 empty")
	}
	// та же роль — ок (второй ключ тому же user'у)
	_, k2 := createTestKey(t, auth, "key-viewer-2", "viewer", "fe-it", 0)
	if k2 == "" {
		t.Fatal("key2 empty")
	}
	// другая роль — conflict с понятным сообщением
	_, _, err := auth.CreateKey(context.Background(), "key-op", "operator", "fe-it", 0)
	if err == nil {
		t.Fatal("expected conflict for existing user with different role")
	}
	if !strings.Contains(err.Error(), "already has role") {
		t.Fatalf("error = %q, want mention of existing role", err.Error())
	}
	// другой user — ок
	_, k3 := createTestKey(t, auth, "key-op-2", "operator", "fe-it-2", 0)
	if k3 == "" {
		t.Fatal("key3 empty")
	}
}
