# ADR-002: SQLite по умолчанию, PostgreSQL для production

Status: accepted (2026-10-07)

## Context
ТЗ 00_database.md: «по дефолту локальная SQLite (или что порекомендуешь), но с
возможностью конфигурации на PostgreSQL». В окружении нет gcc → cgo-драйверы
(mattn/go-sqlite3, pgx в cgo-режиме) нежелательны.

## Decision
- `database/sql` + два драйвера:
  - `modernc.org/sqlite` (чистый Go, без cgo) — **по умолчанию**;
  - `github.com/jackc/pgx/v5/stdlib` — если DSN `postgres://...` (**опционально**).
- Выбор БД только через `DAEMON_DB_DSN` (`sqlite:<path>` | `postgres://...`).
- **Плейсхолдеры** (дополнение 2026-10-08): репозитории написаны с sqlite-`?`;
  для PG зарегистрирован драйвер `pgx-rewrite` (обёртка pgx stdlib с трансляцией
  `?` → `$1..$N`, `internal/database/pgx_rewriter.go`). pgx v5 встроенный
  QueryRewriter не имеет (убран в v5) — отсюда обёртка.
- **Опциональная миграция данных** (дополнение 2026-10-08):
  `daemon migrate pg --to postgres://... [--from sqlite:<path>] [--force]` —
  применяет postgres-миграции схемы и копирует все таблицы (FK-порядок, batch,
  cast'ы timestamptz). SQLite остаётся рабочей БД до переключения `DAEMON_DB_DSN`.
- Миграции: numbered SQL в `internal/database/migrations/{sqlite,postgres}/`
  (единая семантика, два диалекта DDL), применяются при старте через
  `schema_migrations`.
- SQLite: `PRAGMA foreign_keys=ON`, `journal_mode=WAL`.

## Consequences
- Локальный dev и тесты — без внешнего сервиса (in-memory sqlite).
- Production/конкурентные записи — PostgreSQL (опционально, через DSN + migrate).
- DDL-диалекты поддерживаются вручную (пока схема малая).
- Ограничение: e2e-проверка PG-пути (rewriter + migrate) требует живого
  postgres-сервера; локально (без PG/docker) проверены: unit-тесты трансляции
  плейсхолдеров/INSERT-билдера, путь driver registration/open/ping, CLI-обвязка.
