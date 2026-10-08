package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigratePG — опциональная миграция данных SQLite → PostgreSQL (B3/ADR-002).
// Источник — только sqlite (текущая БД daemon'а), цель — postgres (пустая или --force).
// Шаги: миграции схемы на цели → копирование всех таблиц (FK-порядок) одной транзакцией.
// Повторный запуск — только в пустую БД (иначе ошибка; --force не обходит проверку
// наличия данных, только предупреждает — повторное копирование затирает через
// TRUNCATE ... CASCADE).
func MigratePG(ctx context.Context, fromDSN, toDSN string, force bool) (*PGMigrateReport, error) {
	if !strings.HasPrefix(fromDSN, "sqlite:") {
		return nil, fmt.Errorf("source must be sqlite:<path>, got %q", fromDSN)
	}
	src, err := Open(fromDSN)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	defer src.Close()
	if err := src.Ping(); err != nil {
		return nil, fmt.Errorf("ping source: %w", err)
	}
	dst, err := Open(toDSN)
	if err != nil {
		return nil, fmt.Errorf("open target: %w", err)
	}
	defer dst.Close()
	if err := dst.Ping(); err != nil {
		return nil, fmt.Errorf("ping target: %w", err)
	}
	if err := Migrate(ctx, dst, "postgres"); err != nil {
		return nil, fmt.Errorf("apply postgres migrations: %w", err)
	}

	report := &PGMigrateReport{}

	// цель должна быть пустая (без force)
	var n int
	if err := dst.QueryRowContext(ctx, `SELECT COUNT(*) FROM teams`).Scan(&n); err != nil {
		return nil, fmt.Errorf("check target teams: %w", err)
	}
	if n > 0 {
		if !force {
			return nil, fmt.Errorf("target postgres already has %d teams; use --force to TRUNCATE and re-migrate", n)
		}
		if _, err := dst.ExecContext(ctx, `TRUNCATE teams CASCADE`); err != nil {
			return nil, fmt.Errorf("truncate target: %w", err)
		}
	}

	// таблицы источника (sqlite_master), пересечение с known order
	srcTables, err := sqliteTableNames(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("list source tables: %w", err)
	}
	for _, table := range pgCopyOrder {
		if !srcTables[table] {
			continue // старый sqlite-схема: таблицы может не быть
		}
		rows, err := copyTable(ctx, src, dst, table)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", table, err)
		}
		report.Tables = append(report.Tables, PGMigrateTable{Name: table, Rows: rows})
	}
	for name := range srcTables {
		if isKnownCopyTable(name) {
			continue
		}
		if name == "sqlite_sequence" || name == "schema_migrations" ||
			strings.HasPrefix(name, "sqlite_") || strings.HasPrefix(name, "idx_") {
			continue
		}
		report.Skipped = append(report.Skipped, name)
	}
	return report, nil
}

// PGMigrateReport — результат миграции.
type PGMigrateReport struct {
	Tables  []PGMigrateTable `json:"tables"`
	Skipped []string         `json:"skipped,omitempty"`
}

type PGMigrateTable struct {
	Name string `json:"name"`
	Rows int    `json:"rows"`
}

// pgCopyOrder — порядок копирования (FK-зависимости): сначала «родители».
var pgCopyOrder = []string{
	"teams", "segments", "roles", "relatives",
	"queue_tasks", "history_status",
	"sessions", "session_history", "watchdog_events",
	"chatrooms", "chatroom_messages", "messages",
	"workflows", "workflow_blocks", "workflow_connections",
	"library_items", "library_versions", "audit_log",
	"security_roles", "permissions", "role_permissions", "users",
	"api_keys", "secrets", "chatroom_reads",
	// schema_migrations НЕ копируем: целевая схема уже применила миграции
	// (Migrate выше), версии совпадают.
}

func isKnownCopyTable(name string) bool {
	for _, t := range pgCopyOrder {
		if t == name {
			return true
		}
	}
	return false
}

func sqliteTableNames(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type IN ('table','view')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// copyTable — копирует таблицу целиком (общий механизм: колонки из PRAGMA table_info,
// типы цели из information_schema, cast'ы для timestamptz/boolean).
func copyTable(ctx context.Context, src, dst *sql.DB, table string) (int, error) {
	cols, err := sqliteColumns(ctx, src, table)
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}
	pgTypes, err := pgColumnTypes(ctx, dst, table)
	if err != nil {
		return 0, fmt.Errorf("target types: %w", err)
	}
	// цель должна быть очищена (force-ветка TRUNCATE'ит целиком; без force — пустая)
	if _, err := dst.ExecContext(ctx, `DELETE FROM `+table); err != nil {
		return 0, fmt.Errorf("clear target: %w", err)
	}
	insertSQL := buildPGInsertSQL(table, cols, pgTypes)

	sel := `SELECT ` + quoteIDList(cols) + ` FROM ` + table
	srows, err := src.QueryContext(ctx, sel)
	if err != nil {
		return 0, fmt.Errorf("select source: %w", err)
	}
	defer srows.Close()

	colsN := len(cols)
	var (
		count int
		batch [][]any
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := dst.ExecContext(ctx, batchInsertSQL(insertSQL, len(batch)), batchArgs(batch)...); err != nil {
			return fmt.Errorf("insert batch: %w", err)
		}
		count += len(batch)
		batch = batch[:0]
		return nil
	}
	for srows.Next() {
		vals := make([]any, colsN)
		ptrs := make([]any, colsN)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := srows.Scan(ptrs...); err != nil {
			return 0, fmt.Errorf("scan row: %w", err)
		}
		// []byte → string (sqlite TEXT иногда как []byte)
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		batch = append(batch, vals)
		if len(batch) >= 100 {
			if err := flush(); err != nil {
				return 0, err
			}
		}
	}
	if err := srows.Err(); err != nil {
		return 0, err
	}
	if err := flush(); err != nil {
		return 0, err
	}
	return count, nil
}

func sqliteColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func pgColumnTypes(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var c, t string
		if err := rows.Scan(&c, &t); err != nil {
			return nil, err
		}
		out[c] = t
	}
	return out, rows.Err()
}

// buildPGInsertSQL — INSERT INTO t (cols) VALUES ($1...), с cast'ами по типам цели.
// (Функция принимает уже «готовые» $-плейсхолдеры; транслировать не нужно —
// вызывается по pg-соединению напрямую.)
func buildPGInsertSQL(table string, cols []string, pgTypes map[string]string) string {
	var vals []string
	for i, c := range cols {
		v := fmt.Sprintf("$%d", i+1)
		switch pgTypes[c] {
		case "timestamp with time zone":
			v += "::timestamptz"
		case "boolean":
			v += "::boolean"
		}
		vals = append(vals, v)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table, quoteIDList(cols), strings.Join(vals, ", "))
}

// batchInsertSQL — INSERT ... multi-VALUES на N строк из oneRow (одна строка $1..$k).
func batchInsertSQL(oneRow string, n int) string {
	valIdx := strings.Index(oneRow, "VALUES (")
	head := oneRow[:valIdx+len("VALUES ")]
	row := strings.TrimSuffix(oneRow[valIdx+len("VALUES ("):], ")")
	k := placeholdersTotal(row)
	patterns := make([]string, 0, n)
	for r := 0; r < n; r++ {
		patterns = append(patterns, "("+shiftPlaceholders(row, r, k)+")")
	}
	return head + strings.Join(patterns, ", ")
}

// shiftPlaceholders — $i → $i + rowOffset*k.
func shiftPlaceholders(s string, rowOffset, k int) string {
	if rowOffset == 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			j := i + 1
			n := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				n = n*10 + int(s[j]-'0')
				j++
			}
			fmt.Fprintf(&b, "$%d", n+rowOffset*k)
			i = j - 1
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// placeholdersTotal — число $N в строке (первая строка = k).
func placeholdersTotal(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			n++
		}
	}
	return n
}

func quoteIDList(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = `"` + c + `"`
	}
	return strings.Join(q, ", ")
}

func batchArgs(batch [][]any) []any {
	var out []any
	for _, r := range batch {
		out = append(out, r...)
	}
	return out
}
