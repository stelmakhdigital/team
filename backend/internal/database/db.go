// Package database — подключение (sqlite/postgres) и миграции.
package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Open открывает БД по DSN:
//   - "sqlite:<path>" (path может быть ":memory:")
//   - "postgres://..." или "postgresql://..."
func Open(dsn string) (*sql.DB, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// "pgx-rewrite" = pgx stdlib + трансляция `?` → `$N` (см. pgx_rewriter.go):
		// репозитории написаны с sqlite-плейсхолдерами, pgx v5 их не понимает.
		db, err := sql.Open("pgx-rewrite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		return db, nil
	}

	path := strings.TrimPrefix(dsn, "sqlite:")
	if path == "" {
		path = "./daemon.db"
	}
	name := path
	if path != ":memory:" {
		name = fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	}
	db, err := sql.Open("sqlite", name)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	return db, nil
}
