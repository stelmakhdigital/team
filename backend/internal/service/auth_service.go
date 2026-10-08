package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"daemon/internal/models"
)

// AuthService — RBAC: валидация API-ключей (env + БД), права по ролям (ТЗ 06 §2.1).
// Auth включается, если заданы env-ключи (DAEMON_API_KEYS) или в БД есть api_keys.
type AuthService struct {
	db      *sql.DB
	envKeys map[string]bool
	enabled bool
}

// AllPermissions — полный набор permissions (admin-роль).
var AllPermissions = []string{
	"teams.read", "teams.create", "teams.update", "teams.delete",
	"tasks.read", "tasks.create", "tasks.update",
	"sessions.read", "sessions.create", "sessions.stop",
	"messages.read", "messages.create",
	"chatrooms.read", "chatrooms.create",
	"workflows.read", "workflows.create", "workflows.update",
	"library.read", "library.create", "library.apply",
	"dashboard.read",
	"audit.read",
	"config.update",
}

func NewAuthService(db *sql.DB, envKeys []string) *AuthService {
	set := make(map[string]bool, len(envKeys))
	for _, k := range envKeys {
		if k != "" {
			set[k] = true
		}
	}
	return &AuthService{db: db, envKeys: set}
}

// Init — определяется включён ли auth (env-ключи или api_keys в БД).
// Вызывается после миграций.
func (s *AuthService) Init(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if len(s.envKeys) > 0 {
		s.enabled = true
		return nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&n); err != nil {
		return fmt.Errorf("auth init: count api_keys: %w", err)
	}
	s.enabled = n > 0
	return nil
}

// Enabled — auth принудителен (ключ обязателен + RBAC).
func (s *AuthService) Enabled() bool { return s != nil && s.enabled }

// ValidateAPIKey — env-ключ → admin (operator:<4 hex>); ключ из БД → user/role +
// permissions роли (+ доп. из ключа). Иначе — ошибка (middleware → 401).
func (s *AuthService) ValidateAPIKey(ctx context.Context, key string) (*models.AuthContext, error) {
	if key == "" {
		return nil, fmt.Errorf("missing api key")
	}
	if s.envKeys[key] {
		userName := "operator"
		if len(key) >= 4 {
			userName = "operator:" + key[len(key)-4:]
		}
		return &models.AuthContext{
			Authenticated: true,
			UserName:      userName,
			RoleName:      "admin",
			Permissions:   AllPermissions,
		}, nil
	}
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])

	var (
		id, userID              int64
		name, permJSON, created string
		expiresAt               string
		revoked                 int
		userName, roleName      sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT ak.id, COALESCE(ak.user_id, 0), ak.name, ak.permissions, ak.created_at,
		       COALESCE(ak.expires_at, ''), COALESCE(ak.revoked, 0),
		       u.username, sr.name
		FROM api_keys ak
		LEFT JOIN users u ON u.id = ak.user_id
		LEFT JOIN security_roles sr ON sr.id = u.role_id
		WHERE ak.key_hash = ?`, hash).
		Scan(&id, &userID, &name, &permJSON, &created, &expiresAt, &revoked, &userName, &roleName)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("api key not found")
	}
	if err != nil {
		return nil, NewInternal(err)
	}
	if revoked == 1 {
		return nil, fmt.Errorf("api key revoked")
	}
	if expiresAt != "" {
		if t, perr := time.Parse(time.RFC3339, expiresAt); perr == nil && time.Now().After(t) {
			return nil, fmt.Errorf("api key expired")
		}
	}
	role := roleName.String
	perms, _ := s.rolePermissions(ctx, role)
	var extra []string
	_ = json.Unmarshal([]byte(permJSON), &extra)
	for _, p := range extra {
		if !contains(perms, p) {
			perms = append(perms, p)
		}
	}
	keyID := id
	var userIDPtr *int64
	if userID != 0 {
		uid := userID
		userIDPtr = &uid
	}
	return &models.AuthContext{
		Authenticated: true,
		UserID:        userIDPtr,
		UserName:      userName.String,
		APIKeyID:      &keyID,
		RoleName:      role,
		Permissions:   perms,
	}, nil
}

// rolePermissions — permissions роли из БД; admin — AllPermissions.
func (s *AuthService) rolePermissions(ctx context.Context, role string) ([]string, error) {
	if role == "admin" {
		return AllPermissions, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN security_roles r ON r.id = rp.role_id
		WHERE r.name = ?`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

// CreateKey — создаёт user (если нет) + API-ключ. Возвращает открытый ключ (один раз).
func (s *AuthService) CreateKey(ctx context.Context, name, role, username string, expires time.Duration) (*models.APIKey, string, error) {
	if role == "" {
		role = "operator"
	}
	if username == "" {
		username = name
	}
	var roleID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM security_roles WHERE name = ?`, role).Scan(&roleID); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", NewNotFound("security role " + role)
		}
		return nil, "", NewInternal(err)
	}
	var existingRoleID *int64
	if err := s.db.QueryRowContext(ctx, `SELECT role_id FROM users WHERE username = ?`, username).Scan(&existingRoleID); err != nil && err != sql.ErrNoRows {
		return nil, "", NewInternal(err)
	}
	if existingRoleID != nil {
		if *existingRoleID == roleID {
			// пользователь уже с нужной ролью — просто создаём ключ
		} else {
			var curRole string
			_ = s.db.QueryRowContext(ctx, `SELECT name FROM security_roles WHERE id = ?`, *existingRoleID).Scan(&curRole)
			return nil, "", NewConflict(fmt.Sprintf(
				"user %q already has role %q (requested %q) — use another --user or change the user's role",
				username, curRole, role))
		}
	} else if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, role_id, created_at) VALUES (?, ?, ?)`, username, roleID, nowRFC3339()); err != nil {
		return nil, "", NewInternal(err)
	}
	var userID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&userID); err != nil {
		return nil, "", NewInternal(err)
	}

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", NewInternal(err)
	}
	key := "sk_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])

	var expiresAt any
	if expires > 0 {
		expiresAt = time.Now().UTC().Add(expires).Format(time.RFC3339)
	}
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO api_keys (key_hash, name, user_id, permissions, expires_at, created_at)
		 VALUES (?, ?, ?, '[]', ?, ?) RETURNING id`, hash, name, userID, expiresAt, nowRFC3339()).Scan(&id)
	if err != nil {
		// fallback без RETURNING
		res, ierr := s.db.ExecContext(ctx,
			`INSERT INTO api_keys (key_hash, name, user_id, permissions, expires_at, created_at)
			 VALUES (?, ?, ?, '[]', ?, ?)`, hash, name, userID, expiresAt, nowRFC3339())
		if ierr != nil {
			return nil, "", NewInternal(ierr)
		}
		id, _ = res.LastInsertId()
	}
	createdAt, _ := time.Parse(time.RFC3339, nowRFC3339())
	return &models.APIKey{ID: id, Name: name, UserID: &userID, UserName: username, RoleName: role, CreatedAt: createdAt}, key, nil
}

// ListKeys — список ключей (админ).
func (s *AuthService) ListKeys(ctx context.Context) ([]*models.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ak.id, ak.name, COALESCE(u.username,''), COALESCE(sr.name,''),
		       COALESCE(ak.expires_at,''), COALESCE(ak.revoked,0), ak.created_at
		FROM api_keys ak
		LEFT JOIN users u ON u.id = ak.user_id
		LEFT JOIN security_roles sr ON sr.id = u.role_id
		ORDER BY ak.id`)
	if err != nil {
		return nil, NewInternal(err)
	}
	defer rows.Close()
	var out []*models.APIKey
	for rows.Next() {
		var (
			k       models.APIKey
			created string
			expires string
			revoked int
		)
		if err := rows.Scan(&k.ID, &k.Name, &k.UserName, &k.RoleName, &expires, &revoked, &created); err != nil {
			return nil, NewInternal(err)
		}
		k.Revoked = revoked == 1
		if created != "" {
			k.CreatedAt, _ = time.Parse(time.RFC3339, created)
		}
		if expires != "" {
			if t, err := time.Parse(time.RFC3339, expires); err == nil {
				k.ExpiresAt = &t
			}
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

// RevokeKey — отозвать ключ (revoked → 1).
func (s *AuthService) RevokeKey(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET revoked = 1 WHERE id = ?`, id)
	if err != nil {
		return NewInternal(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return NewNotFound("api key")
	}
	return nil
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
