package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

// pgx-rewrite — pgx stdlib-драйвер с трансляцией sqlite-плейсхолдеров `?` в
// `pgx` `$$1..$N`. Без него репозитории (написаны с `?` под sqlite) не работают
// с PostgreSQL (pgx v5 QueryRewriter удалён). Регистрация в init, имя отличное
// от "pgx" (его занимает init pgx stdlib).
func init() {
	sql.Register("pgx-rewrite", pgxRewriteDriver{base: stdlib.GetDefaultDriver()})
}

type pgxRewriteDriver struct{ base driver.Driver }

func (d pgxRewriteDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &pgxRewriteConn{Conn: c}, nil
}

type pgxRewriteConn struct{ driver.Conn }

// Prepare / PrepareContext — трансляция плейсхолдеров.
func (c *pgxRewriteConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(rewritePlaceholders(query))
}

func (c *pgxRewriteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if pc, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, rewritePlaceholders(query))
	}
	return nil, driver.ErrSkip
}

func (c *pgxRewriteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := c.Conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, rewritePlaceholders(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *pgxRewriteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := c.Conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, rewritePlaceholders(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *pgxRewriteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bt, ok := c.Conn.(driver.ConnBeginTx); ok {
		return bt.BeginTx(ctx, opts)
	}
	return nil, driver.ErrSkip
}

func (c *pgxRewriteConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

// rewritePlaceholders — `?` → `$1..$N` слева направо.
// Внутри строковых литералов (esc: ”) и идентификаторов в кавычках "?"
// не заменяет — там это часть строки/имени, а не плейсхолдер.
func rewritePlaceholders(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		switch {
		case inSingle:
			b.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					b.WriteByte('\'') // '' — экранированная кавычка, остаёмся внутри
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			b.WriteByte(ch)
			if ch == '"' {
				inDouble = false
			}
		case ch == '\'':
			inSingle = true
			b.WriteByte(ch)
		case ch == '"':
			inDouble = true
			b.WriteByte(ch)
		case ch == '?':
			n++
			fmt.Fprintf(&b, "$%d", n)
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}
