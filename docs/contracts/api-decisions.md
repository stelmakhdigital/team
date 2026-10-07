# API Decisions (v1)

Единый контракт: `docs/architecture/frontend/20_contract_API.md` (source of truth,
см. `agents.md`) + `docs/architecture/frontend/21_team_builder.md`.

Зафиксированные решения backend по контракту:

1. **Базовый URL**: `/api/v1`. Даты — RFC3339 UTC. ID — JSON number (int64).
2. **`DELETE /api/v1/teams/{id}`** — archive (state → `archived`), soft delete.
   Ответ: `200 {"id": n, "status": "archived"}`. Повторный delete — 409 `conflict`.
3. **`DELETE /api/v1/relatives/{id}`** — hard delete (у связи нет soft-delete semantics).
   Ответ: `200 {"id": n, "status": "deleted", "from_role_name": "...", "to_role_name": "..."}`.
4. **Role address** вычисляется сервером: `team_name:segment_name.role_name`
   (контракт: `address` unique `team:segment.role`). Клиент не передаёт address.
5. **Layout** (Team Builder) хранится в `config`-JSON сущности:
   segment: `config.layout = {x, y, width, height, collapsed}`;
   role/relative: `config.layout = {x, y, path?...}`.
   Ответы create/topology возвращают `layout` как в контракте 21.
6. **`POST /api/v1/teams` с `spec`** — создание segments/roles/relatives одной
   транзакцией. Ошибка валидации spec → 400, команда не создаётся.
7. **`POST /api/v1/teams/{id}/validate`** — 200 с `is_valid: false` и списком
   ошибок (валидация — не ошибка HTTP).
8. **Пагинация**: `limit` (default 50, max 200) + `offset`; ответ `total` + `has_more` где задано контрактом.
9. **Error format**: `docs/contracts/error-model.md`.
10. **Auth**: фаза 0 — выключен (локальная разработка). Фаза 1 — ключ принимается
    двумя способами: `X-API-Key: <key>` **или** `Authorization: Bearer <key>`
    (второй — формат frontend, `VITE_API_KEY`).
    Header `X-Request-Id` (или backend генерирует) возвращается в каждом ответе.
11. **Idempotency**: POST create не идемпотентен; повтор с тем же unique-ключом → 409.
    PATCH layout/config идемпотентны.
12. **Статусы**: POST-создание ресурсов → `201 Created`; остальные мутации → `200`;
    `DELETE /teams/{id}` → `200 {"status":"archived"}`.
13. **`GET /api/v1/roles/{id}/config`** (21 §10): `agent_spec` читается daemon'ом
    с диска (yaml), пути ограничены `DAEMON_AGENT_SPECS_DIR` (default `agents`) и cwd
    (защита от traversal); файл не найден → stub `{name, available:false}`.
14. **`POST /api/v1/teams/{id}/save`** (21 §9): обновление name/description (опц.)
    + валидация; `library_item_id` будет возвращаться с slice 5 (Library),
    флаг `save_to_library` сейчас принимается и игнорируется.

Изменения контракта (относительно 20/21): нет — только уточнения.

## Slice 2: Tasks & History (2026-10-07)

- Переходы состояний: `pending → in_progress|done|blocked|canceled`; `in_progress → done|blocked|canceled`; `blocked → pending|in_progress|done|canceled`. Из терминальных (`done`/`canceled`) — 409 conflict.
- `PATCH /tasks/{id}/state` при `to_state=done` требует `closure_reason` из домена контракта → иначе 400 validation_failed.
- `handoff` = транзакция: закрытие исходной задачи (`done`/`handed_off_to`, `closure_target_id`=новая) + создание pending-задачи у целевой роли (`source_role_id`=исходная роль).
- Родительская задача закрывается автоматически (`done`/`no_follow_on`, actor=`daemon`), когда все subtasks в терминальных состояниях.
- `is_stale` = `in_progress` и `updated_at` старше 2 часов; `is_blocked` = state `blocked`.
- Dashboard: `summary` (teams/tasks/sessions=0/alerts=0 до slice 3), `tasks` = active (pending/in_progress/blocked) с team_name/role_name.
- Миграция 0002: `queue_tasks`, `history_status` (append-only). Колонки `project_id`/`session_id` добавятся с соответствующими slice'ами.
- История: `GET /tasks/{id}/history` — все записи, ASC (created, id), limit/offset + total.
