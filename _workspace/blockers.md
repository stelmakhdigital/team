# Blockers / Open questions

## B1. Отсутствуют документы (зафиксировано 2026-10-07, backend)
- `docs/contracts/openapi.yaml` — отсутствовал. Создан `docs/contracts/error-model.md`
  и `docs/contracts/api-decisions.md`; OpenAPI — договорено с Lead: сгенерировать в конце
  проекта (весь API), backend готов.
- `docs/architecture/integration.md` — CLOSED (2026-10-08, лид): написан. Backend работает
  по `agents.md` + `20_contract_API.md` как единый контракт.
- `AGENTS.md` (капсом) — отсутствует, используется `agents.md`.
- `_workspace/frontend-status.md` / `_workspace/integration-status.md` — отсутствовали,
  интеграционный статус создан backend.

## B2. Контракт не задавал error-схему
Решено: ADR-003. Нужно подтвердить Frontend Engineer'ом.

## B3. [BACKEND, открыто, 2026-10-08] PostgreSQL-путь: плейсхолдеры `?` не работают с pgx v5
- Все запросы репозиториев (slices 1–6) написаны с sqlite-плейсхолдерами `?`;
  pgx v5 stdlib требует `$1…$N` (в v5 QueryRewriter удалён). DSN `postgres://...`
  (ADR-002) формально поддерживается (`database.Open`, миграции postgres-диалекта),
  но первый же запрос упадёт с ошибкой плейсхолдера.
- Локально postgres/docker нет — проверить e2e невозможно без окружения.
- Варианты решения (нужно решение lead'а): (а) переписать репозитории на `$N`;
  (б) wrapper-драйвер database/sql с трансляцией `?`→`$N`; (в) объявить PG out-of-scope
  до появления окружения (сейчас весь проект, тесты и live — на sqlite).
- До решения: PG-DSN — НЕ использовать; документация ADR-002 остаётся как цель.
- **РЕШЕНИЕ (лид, 2026-10-08): (в)** — PG out-of-scope до появления окружения.
  Обоснование: весь проект (тесты/live/демо) сейчас на sqlite, PG-окружения нет,
  а (б) wrapper маскирует диалект в репозиториях и усложняет отладку.
  Когда окружение появится — отдельная задача: миграция на `$N` (вариант (а),
  pgx v5 stdlib) + e2e-прогон `go test` с PG-DSN; ADR-002 остаётся целью.

## OQ1. Auth для UI
FAZE 0 (без ключей) — локальная разработка, по-прежнему. Slice 6 (2026-10-08): auth включается
при `DAEMON_API_KEYS` **или** api_keys в БД (CLI `daemon admin keys`); RBAC-роли
admin/operator/viewer, 403 `forbidden`; audit user_id/api_key_id. Решение lead'а нужно
только для выбора дефолтного режима продакшена (env-ключ vs DB-ключи).

## OQ2. DELETE /api/v1/teams/{id}
Интерпретировано как archive (по 01_daemon.md). Требуется подтверждение.

## F1–F4. Blockers frontend-агента (статус от 2026-10-07, ответ backend)
- **#1 Auth**: решено — `X-API-Key` (header) — **CLOSED** (блокеры #9).
- **#2 Error model**: зафиксирована, frontend flex-парсинг совместим — **CLOSED**.
- **#3 GET /api/v1/workflows (список)**: контракт 20 дополнен §2.0 (лид, 2026-10-07);
  реализация — slice 5. **contract closed, impl pending**.
- **#4 layout в request'ах**: backend принимает — **CLOSED**.

## 6. [LEAD integration check 2026-10-07] Topology endpoint сериализует raw-модели (CLOSED)
- `GET /api/v1/teams/{id}/topology` возвращает `team/segments/roles/relatives`
  напрямую из `internal/models` БЕЗ json-тегов → ключи в PascalCase (`ID`, `TeamID`,
  `AgentSpec`, …), а контракт и frontend ждут snake_case.
- Не хватает полей контракта: team: `segments_count`, `roles_count`;
  segment: `roles_count`; role: `segment_name`, `session`;
  relative: `from_role_name`, `to_role_name`. Лишнее: `SpecPath`, `Config`.
- Для сравнения: `GET /teams/{id}` (GetTeam) строит нормальные snake_case views —
  тот же подход нужен и в GetTopology.
- Impact: TeamBuilderPage не может рендерить canvas с реального backend.
- Статус: **CLOSED (2026-10-07)** — лид добавил contract-вью `topologyView` в handlers.go + regression-тест.

## 7. [LEAD] Нет `GET /api/v1/roles/{id}/config` (CLOSED — маршрут уже был, билд устарел)
- Контракт 21 §10: GET role config (agent_spec, profiles, plugins, skills, startup).
- Frontend ConfigPanel зависит от него.
- Статус: **CLOSED (2026-10-07)** — handler и маршрут существовали; первый smoke использовал устаревший бинарник. Интеграционный тест frontend зелёный.

## 8. [LEAD] Layout-расхождения — **CLOSED (2026-10-07, live-проверено)**
- Topology layout: width/height + `layout.relatives` — есть.
- PATCH layout: `previous_layout`/`new_layout` — нормальные `SegmentLayout{segment_id,
  position{x,y,width,height}, collapsed}`; `previous != new` (old сохраняется).
- Spec relatives: зафиксирован адресный формат `from`/`to` — **контракт 20 обновлён** (лид).
- Осталось non-blocking: create-ответы `layout: null` — **live-проверено: закрыто**
  (layout возвращается в create-ответе, если передан в request; без layout — null, ок).
- Новый known mismatch (backend): `GET /sessions/:id/transcript` без поля `total`
  (контракт 20 §6.4 требует `{transcript, total, has_more}`).

## 9. [LEAD-решение, CLOSED] Auth = `X-API-Key`
- Backend реализовал `X-API-Key` (`DAEMON_API_KEYS`), в ТЗ auth не задан.
- РЕШЕНИЕ: фиксируем `X-API-Key` (header). Frontend обновлён: отправляет
  `X-API-Key` (вместо Bearer) в real mode. Blockers #1 закрыт этим решением.
