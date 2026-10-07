# Blockers / Open questions

## B1. Отсутствуют документы (зафиксировано 2026-10-07, backend)
- `docs/contracts/openapi.yaml` — отсутствовал. Создан `docs/contracts/error-model.md`
  и `docs/contracts/api-decisions.md`; OpenAPI — запланировано (backend, slice 2).
- `docs/architecture/integration.md` — отсутствует (область Lead). Backend работает
  по `agents.md` + `20_contract_API.md` как единый контракт.
- `AGENTS.md` (капсом) — отсутствует, используется `agents.md`.
- `_workspace/frontend-status.md` / `_workspace/integration-status.md` — отсутствовали,
  интеграционный статус создан backend.

## B2. Контракт не задавал error-схему
Решено: ADR-003. Нужно подтвердить Frontend Engineer'ом.

## OQ1. Auth для UI
Пока без auth (локально). Требуется решение Lead: когда включаем API-ключи/RBAC?

## OQ2. DELETE /api/v1/teams/{id}
Интерпретировано как archive (по 01_daemon.md). Требуется подтверждение.

## F1–F4. Blockers frontend-агента (статус от 2026-10-07, ответ backend)
- **#1 Auth**: решено — `Authorization: Bearer <VITE_API_KEY>` теперь принимается backend'
  (и `X-API-Key`); key задаётся через `DAEMON_API_KEYS` (ADR-003 обновлён). Сайд-эффекта
  для mock режима нет. Осталось решение Lead по включению auth в dev-окружении.
- **#2 Error model**: зафиксирована (`docs/contracts/error-model.md`); frontend flex-парсинг
  с ней совместим. Небольшой mismatch: backend-код `validation_failed` vs frontend-ветка
  `validation` — зафиксировано в integration-status как known mismatch.
- **#3 GET /api/v1/workflows (список)**: принято в план backend (slice 5, workflows).
  Контракт 20 дополнить методом/кодами ответа — с Lead.
- **#4 layout в request'ах**: backend принимает `layout` во всех create/patch endpoint'ах
  slice 1 (хранится в config, возвращается в topology/create/PATCH layout). Решено.

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

## 8. [LEAD] Layout-расхождения (non-blocking, исправить в slice 1)
- Topology layout: `segments[].position` без `width`/`height` (контракт: есть).
- `PATCH /segments/{id}/layout`: ответ `previous_layout`/`new_layout` — плоский
  `{x,y,...}` вместо `SegmentLayout {segment_id, position, collapsed}`;
  к тому же `previous_layout` == `new_layout` (old не сохраняется).
- Create-ответы: `layout: null` сразу после создания (в in-memory config layout
  хранится как struct, `LayoutFromConfig` ждёт map) — после DB roundtrip ок.
- Spec relatives: backend принимает `from`/`to` ("segment.role"), контракт 20
  описывает `from_role`/`to_role`. Лид-решение: принимаем `from`/`to` backend'а
  как уточнение контракта (адресный формат), **контракт 20 обновить** (open).
  Остальное в #8 (layout width/height) — CLOSED: добавлено в topologyView.

## 9. [LEAD-решение, CLOSED] Auth = `X-API-Key`
- Backend реализовал `X-API-Key` (`DAEMON_API_KEYS`), в ТЗ auth не задан.
- РЕШЕНИЕ: фиксируем `X-API-Key` (header). Frontend обновлён: отправляет
  `X-API-Key` (вместо Bearer) в real mode. Blockers #1 закрыт этим решением.
