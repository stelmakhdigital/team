package models

import "time"

// User — человек/сервис-учётка (RBAC, slice 6; ТЗ 06 §2.1).
type User struct {
	ID        int64
	Username  string
	Email     *string
	Password  *string
	RoleID    *int64
	RoleName  string
	CreatedAt time.Time
}

// APIKey — API-ключ, привязанный к user (ключ в БД не храним — sha256 hex).
type APIKey struct {
	ID          int64
	KeyHash     string
	Name        string
	UserID      *int64
	UserName    string
	RoleName    string
	Permissions []string // доп. permissions поверх роли (JSON-массив в БД)
	ExpiresAt   *time.Time
	Revoked     bool
	CreatedAt   time.Time
}

// AuthContext — результат валидации API-ключа (в request context).
type AuthContext struct {
	Authenticated bool
	UserID        *int64
	UserName      string // username или "operator:<4 hex>" (env-ключ)
	APIKeyID      *int64
	RoleName      string   // admin | operator | viewer
	Permissions   []string // permissions роли (+ доп. из ключа)
}

// HasPerm — проверка permission. Админ (role admin) — все permissions.
func (c *AuthContext) HasPerm(perm string) bool {
	if c == nil {
		return false
	}
	if c.RoleName == "admin" {
		return true
	}
	for _, p := range c.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// Secret — элемент secrets (value — зашифрованный).
type Secret struct {
	ID          int64
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
