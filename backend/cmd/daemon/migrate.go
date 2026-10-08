package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"daemon/internal/config"
	"daemon/internal/database"
)

// runMigrate — опциональная миграция данных SQLite → PostgreSQL (B3/ADR-002):
//
//	daemon migrate pg --to postgres://user:pass@host:5432/db [--from sqlite:./daemon.db] [--force]
//
// SQLite остаётся дефолтом; PG — опциональная цель (схема + данные).
func runMigrate(args []string) error {
	if len(args) < 1 || args[0] != "pg" {
		return fmt.Errorf("usage: daemon migrate pg --to postgres://... [--from sqlite:<path>] [--force]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("migrate pg", flag.ContinueOnError)
	from := fs.String("from", cfg.DBDSN, "источник: sqlite:<path> (default: DAEMON_DB_DSN)")
	to := fs.String("to", "", "цель: postgres://user:pass@host:5432/db (required)")
	force := fs.Bool("force", false, "очистить целевые таблицы, если данные уже есть")
	timeout := fs.Duration("timeout", 10*time.Minute, "общий таймаут миграции")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *to == "" {
		return fmt.Errorf("--to postgres://... is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	report, err := database.MigratePG(ctx, *from, *to, *force)
	if err != nil {
		return err
	}
	total := 0
	for _, t := range report.Tables {
		fmt.Printf("  %-24s %d rows\n", t.Name, t.Rows)
		total += t.Rows
	}
	fmt.Printf("migrated %d tables, %d rows -> postgres\n", len(report.Tables), total)
	if len(report.Skipped) > 0 {
		fmt.Printf("skipped (not a known table): %v\n", report.Skipped)
	}
	fmt.Println("note: run the daemon with DAEMON_DB_DSN=postgres://... to switch over")
	return nil
}
