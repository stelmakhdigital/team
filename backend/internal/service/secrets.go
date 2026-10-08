package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"daemon/internal/models"
)

// SecretsManager — encrypted at rest (AES-256-GCM), ключ из DAEMON_SECRET_KEY
// (64 hex-символа = 32 байта). ТЗ 06 §4. HTTP-эндпоинтов нет (их нет в контракте).
type SecretsManager struct {
	db    *sql.DB
	aead  cipher.AEAD
	nonce int
}

// NewSecretsManager — из hex-ключа (32 байта). keyHex = "" → error.
func NewSecretsManager(db *sql.DB, keyHex string) (*SecretsManager, error) {
	if keyHex == "" {
		return nil, fmt.Errorf("secrets manager: empty key")
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("secrets manager: DAEMON_SECRET_KEY must be 32 bytes hex (64 chars)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretsManager{db: db, aead: aead}, nil
}

func (m *SecretsManager) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := m.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ct), nil
}

func (m *SecretsManager) decrypt(encoded string) (string, error) {
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secrets: decode: %w", err)
	}
	if len(raw) < m.aead.NonceSize() {
		return "", fmt.Errorf("secrets: ciphertext too short")
	}
	pt, err := m.aead.Open(nil, raw[:m.aead.NonceSize()], raw[m.aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("secrets: decrypt: %w", err)
	}
	return string(pt), nil
}

// Store — upsert секрета (value шифруется).
func (m *SecretsManager) Store(ctx context.Context, name, value, description string) error {
	if m == nil || m.aead == nil {
		return fmt.Errorf("secrets manager not configured")
	}
	enc, err := m.encrypt(value)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx,
		`INSERT INTO secrets (name, value, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (name) DO UPDATE SET value = excluded.value,
		 description = excluded.description, updated_at = excluded.updated_at`,
		name, enc, description, nowRFC3339(), nowRFC3339())
	return err
}

// Get — расшифрованное значение.
func (m *SecretsManager) Get(ctx context.Context, name string) (string, error) {
	if m == nil || m.aead == nil {
		return "", fmt.Errorf("secrets manager not configured")
	}
	var enc, description string
	err := m.db.QueryRowContext(ctx,
		`SELECT value, COALESCE(description,'') FROM secrets WHERE name = ?`, name).
		Scan(&enc, &description)
	if err == sql.ErrNoRows {
		return "", NewNotFound("secret " + name)
	}
	if err != nil {
		return "", NewInternal(err)
	}
	return m.decrypt(enc)
}

// List — метаданные (без значений).
func (m *SecretsManager) List(ctx context.Context) ([]*models.Secret, error) {
	if m == nil || m.aead == nil {
		return nil, fmt.Errorf("secrets manager not configured")
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description,''), created_at, updated_at FROM secrets ORDER BY name`)
	if err != nil {
		return nil, NewInternal(err)
	}
	defer rows.Close()
	var out []*models.Secret
	for rows.Next() {
		var s models.Secret
		var c, u string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &c, &u); err != nil {
			return nil, NewInternal(err)
		}
		s.CreatedAt, _ = time.Parse(time.RFC3339, c)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, u)
		out = append(out, &s)
	}
	return out, rows.Err()
}

// Delete — удалить секрет.
func (m *SecretsManager) Delete(ctx context.Context, name string) error {
	if m == nil || m.aead == nil {
		return fmt.Errorf("secrets manager not configured")
	}
	res, err := m.db.ExecContext(ctx, `DELETE FROM secrets WHERE name = ?`, name)
	if err != nil {
		return NewInternal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NewNotFound("secret " + name)
	}
	return nil
}
