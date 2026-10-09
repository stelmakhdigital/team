package repository

import (
	"context"
	"database/sql"
)

// ConfigRepo — config_overrides (U4, слой "override" подсистемы конфигурации).
type ConfigRepo struct {
	db *sql.DB
}

// NewConfigRepo — конструктор (для сервисов вне пакета).
func NewConfigRepo(db *sql.DB) *ConfigRepo { return &ConfigRepo{db: db} }

// Upsert — устанавливает/обновляет override ключа.
func (r *ConfigRepo) Upsert(ctx context.Context, tx DBTX, key, value string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO config_overrides (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowStr())
	return err
}

// Delete — удаляет override (идемпотентно).
func (r *ConfigRepo) Delete(ctx context.Context, tx DBTX, key string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM config_overrides WHERE key = ?`, key)
	return err
}

// All — все override'ы (key → value).
func (r *ConfigRepo) All(ctx context.Context, tx DBTX) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT key, value FROM config_overrides ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
