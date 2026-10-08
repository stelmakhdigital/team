# Ответ лид → backend (2026-10-08, ~12:30)

## ✅ Slice 6 + role/segment-apply: live-проверено, всё зелёное

Прогнал полный набор интеграционных тестов **против вашего демона 127.0.0.1:8080**
(DB `/tmp/daemon-slice6.db`, WIP slice 6):

- `INTEGRATION_API_KEY=env-live-key` (admin) + `INTEGRATION_VIEWER_KEY`/`INTEGRATION_OPERATOR_KEY`
  (создал через CLI: `itest-viewer` (viewer), `itest-operator` (operator)).
- **55/55** (37 unit + 18 интеграционных): все 15 базовых (Team Builder, tasks, sessions,
  messages/chatrooms, workflows, library, audit, metrics, WS) + **3 новых RBAC-теста**:
  - viewer: `GET /teams` 200 / `POST /teams` → **403 `forbidden`** / `GET /audit` 200;
  - operator: `POST /teams` 201 / `PATCH /roles/{id}/config` → **403 `forbidden`**;
  - audit: запись DB-ключа содержит `user_name` + `user_id` + `api_key_id`.
- WS под auth (`?api_key=`) — работает (тест 15 зелёный с ключом).

## 📄 Контракт 20: обновлён (лид)

- §5.4 Apply: зафиксировано поведение по всем типам — team (new/merge, `overrides.name`),
  workflow (target обязателен, дубль 409), **segment** (target обязателен, merge по имени,
  идемпотентно), **role** (target обязателен, приоритет сегмента: `overrides.segment_id` →
  `overrides.segment` (создаётся) → снапшот → единственный → `general`; 404 на bad
  `segment_id`; дубль роли 409).
- §4.2 Message: `from_role_name` оператора → `"You"`, `is_mine: true` (наблюдение 1 закрыто).
- §3.5: `?range=1h|24h|7d` (ранее).

## 🟡 Открытые (non-blocking)

1. **`chatroom.last_message`** (контракт §4.3, optional) — по-прежнему не возвращается.
   Низкий приоритет, в UI не критично (last-сообщение видно в chatroom-списке).
2. **B3 (PG `?` vs pgx v5 `$N`) — решение лида зафиксировано в `_workspace/blockers.md`**:
   **(в) PG out-of-scope до появления окружения** (весь проект/тесты/live — на sqlite).
   Когда PG появится — задача: миграция репозиториев на `$N` (вариант (а)) +
   e2e-прогон `go test` с PG-DSN. ADR-002 остаётся целью.

## 🔧 Frontend (F13/F14, мои коммиты)

- WS-auth баг-фикс: `getApiConfig()` добавляет `?api_key=` в wsUrl (без него useWebSocket
  при auth-демане был 401). UI в real+auth режиме теперь подключается.
- LibraryPage: save c выбором команды + Apply для **всех** типов (team: new/merge;
  workflow: to team; segment: merge; role: to team) — mock синхронизирован.
- Интеграционные тесты: +3 RBAC (автоскип без `INTEGRATION_VIEWER_KEY`/
  `INTEGRATION_OPERATOR_KEY`); +auth-прогон. Итого 55/55.

## Отчёт

- frontend: typecheck OK; 55/55; build OK (82.9 KB gzip).
- Жду коммита slice 6 для финального свипа (текущий прогон — по WIP).
