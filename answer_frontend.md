# Ответ backend → лид/фронт (2026-10-08, ~01:00)

## ✅ Slice 6 + role/segment-apply — подтверждено (55/55)

Спасибо за прогон. Жду коммита slice 6 (и rewriter/migrate pg — тоже в рабочем
дереве), после него готов к финальному свипу.

## По открытым пунктам

1. **`chatroom.last_message` — работает** (проверено только что на живом
   daemon'е, свежий бинарь): чат-румы с сообщениями возвращают
   `last_message: {body, from_role_name, created_at}` (пример: chatroom 1,
   team 1 — `{"body":"last-message-check-2","from_role_name":"You",
   "created_at":"2026-10-08T09:57:51Z"}`). Наблюдение, похоже, снова против
   старого бинаря (как было с I3). Пустые `last_message` — там реально нет
   сообщений (optional, `omitempty`). Фикс не требуется.
2. **B3 (PG) — за это время реализовано** «SQLite по умолчанию + опциональная
   миграция» (решение (в) уважаем: PG-DSN пока никто не использует):
   - драйвер `pgx-rewrite` (авто-`?` → `$N`) — runtime-путь PG проходим,
     **миграция репозиториев на `$N` (вариант а) больше не нужна**;
   - `daemon migrate pg --to postgres://... [--from sqlite:<path>] [--force]`
     — схема + все таблицы (FK-порядок, batch, cast'ы);
   - unit-тесты зелёные; **e2e — ждёт PG-окружения** (docker/сервер): один
     прогон migrate + smoke daemon'а. ADR-002 и blockers.md обновлены.

## Что осталось (весь список хвостов)

| # | Хвост | Кто / статус |
|---|---|---|
| 1 | **Коммит WIP** (slices 3–6 + rewriter/migrate pg) | лид; дальше финальный свип фронта (55 тестов) |
| 2 | **PG e2e** (migrate + smoke с PG-DSN) | ждёт PG-окружения; код готов |
| 3 | **OpenAPI-спека** (весь API) | договор: в конце проекта, backend готов |
| 4 | Prometheus `/metrics` | по требованию (вне slices 1–6) |
| 5 | security events (ТЗ 06 §3.2: brute-force и т.п.), password-логин | опционально, не в контракте — по запросу |
| 6 | WS: ack subscribe (event до subscribe теряется) | **задокументированное поведение** (integration.md §4, api-decisions), не баг; по желанию — ack-расширение |

Известные ограничения (документированы, не баги): WS publish неблокирующий
(буфер 64 — медленный подписчик теряет события, REST = истина); gorilla:
read-таймаут «корruptит» соединение → клиент рекоネクтит (фронт умеет).

## Файлы

- Код rewriter/migrate: `internal/database/{pgx_rewriter,migrate_pg}*.go`,
  `cmd/daemon/migrate.go`; доки: ADR-002, `backend/README.md` (Postgres),
  `_workspace/blockers.md` (B3), снапшет `backend.md`.
