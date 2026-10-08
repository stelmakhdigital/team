package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildPGInsertSQL(t *testing.T) {
	sql := buildPGInsertSQL("teams", []string{"id", "name", "created_at"},
		map[string]string{"id": "bigint", "name": "text", "created_at": "timestamp with time zone"})
	want := `INSERT INTO teams ("id", "name", "created_at") VALUES ($1, $2, $3::timestamptz)`
	if sql != want {
		t.Fatalf("got %q, want %q", sql, want)
	}
	// без cast'ов — обычные колонки
	sql = buildPGInsertSQL("audit_log", []string{"ip_address"},
		map[string]string{"ip_address": "text"})
	if !strings.HasSuffix(sql, "VALUES ($1)") {
		t.Fatalf("got %q", sql)
	}
}

func TestBatchInsertSQL(t *testing.T) {
	one := `INSERT INTO t ("a", "b") VALUES ($1, $2)`
	if got := batchInsertSQL(one, 1); got != one {
		t.Fatalf("1-row: %q", got)
	}
	got := batchInsertSQL(one, 2)
	want := `INSERT INTO t ("a", "b") VALUES ($1, $2), ($3, $4)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = batchInsertSQL(one, 3)
	want = `INSERT INTO t ("a", "b") VALUES ($1, $2), ($3, $4), ($5, $6)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShiftPlaceholders(t *testing.T) {
	if got := shiftPlaceholders("$1, $2", 1, 2); got != "$3, $4" {
		t.Fatalf("got %q", got)
	}
	if got := shiftPlaceholders("$1, $2", 0, 2); got != "$1, $2" {
		t.Fatalf("got %q", got)
	}
	if got := shiftPlaceholders("$12, $3", 1, 2); got != "$14, $5" {
		t.Fatalf("got %q", got)
	}
}

// TestOpenPGDriverPath — pgx-rewrite драйвер зарегистрирован, Open работает,
// Ping на несуществующий сервер даёт error (а не панику/регистрационный сбой).
func TestOpenPGDriverPath(t *testing.T) {
	db, err := Open("postgres://user:pass@127.0.0.1:1/nodb?connect_timeout=1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err == nil {
		t.Fatal("Ping to closed port: expected error, got nil")
	}
}
