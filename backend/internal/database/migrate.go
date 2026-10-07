package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations
var migrationsFS embed.FS

type migration struct {
	Version int
	Name    string
	UpSQL   string
}

// Migrate применяет numbered-миграции диалекта (sqlite|postgres) по порядку,
// идемпотентно (schema_migrations).
func Migrate(ctx context.Context, db *sql.DB, dialect string) error {
	dir := filepath.Join("migrations", dialect)
	entries, err := migrationsFS.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list migrations %s: %w", dir, err)
	}

	var migs []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		parts := strings.SplitN(base, "_", 2)
		if len(parts) != 2 {
			return fmt.Errorf("bad migration filename %q (want NNN_name.sql)", e.Name())
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("bad migration version in %q: %w", e.Name(), err)
		}
		data, err := migrationsFS.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		migs = append(migs, migration{Version: v, Name: e.Name(), UpSQL: string(data)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].Version < migs[j].Version })

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, m := range migs {
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if exists > 0 {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.UpSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.Version, m.Name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
