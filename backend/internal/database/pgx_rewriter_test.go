package database

import (
	"testing"
)

func TestRewritePlaceholders(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`SELECT 1`, `SELECT 1`},
		{`SELECT * FROM teams WHERE id = ?`, `SELECT * FROM teams WHERE id = $1`},
		{`SELECT * FROM a WHERE x = ? AND y = ?`, `SELECT * FROM a WHERE x = $1 AND y = $2`},
		{`SELECT '?' FROM t WHERE id = ?`, `SELECT '?' FROM t WHERE id = $1`},
		{`SELECT 'it''s ?' WHERE id = ?`, `SELECT 'it''s ?' WHERE id = $1`},
		{`SELECT ? FROM "weird?table" WHERE id = ?`, `SELECT $1 FROM "weird?table" WHERE id = $2`},
		{`SELECT '' WHERE id = ?`, `SELECT '' WHERE id = $1`},
		{`INSERT INTO t (a, b) VALUES (?, ?)`, `INSERT INTO t (a, b) VALUES ($1, $2)`},
	}
	for _, c := range cases {
		if got := rewritePlaceholders(c.in); got != c.want {
			t.Errorf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
