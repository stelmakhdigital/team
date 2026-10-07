# ADR-002: SQLite по умолчанию, PostgreSQL для production

Status: accepted (2026-10-07)

## Context
ТЗ 00_database.md: «по дефолту локальная SQLite (или что порекомендуешь), но с
возможностью конфигурации на PostgreSQL». В окружении нет gcc → cgo-драйверы
(mattn/go-sqlite3, pgx в cgo-режиме) нежелательны.

## Decision
- `database/sql` + два драйвера:
  - `modernc.org/sqlite` (чистый Go, без cgo) — по умолчанию;
  - `github.com/jackc/pgx/v5/stdlib` — если DSN `postgres://...`.
- Выбор БД только через `DAEMON_DB_DSN` (`sqlite:<path>` | `postgres://...`).
- Миграции: numbered SQL в `internal/database/migrations/{sqlite,postgres}/`
  (единая семантика, два диалекта DDL), применяются при старте через
  `schema_migrations`.
- SQLite: `PRAGMA foreign_keys=ON`, `journal_mode=WAL`.

## Consequences
- Локальный dev и тесты — без внешнего сервиса (in-memory sqlite).
- Production/конкурентные записи — только PostgreSQL.
- DDL-диалекты поддерживаются вручную (пока схема малая).
