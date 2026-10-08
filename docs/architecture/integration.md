# Frontend ↔ Backend Integration

> Владелец: **лид (frontend-инженер)**. Обновляется вместе с `_workspace/integration-status.md`.
> Единицы истины: `docs/architecture/frontend/20_contract_API.md` (REST + WS) и
> `docs/architecture/frontend/21_team_builder.md` (Team Builder).
> Уточнения без смены форматов — `docs/contracts/api-decisions.md`, error-модель —
> `docs/contracts/error-model.md`.

## 1. Модель интеграции

```
Browser (React 18 + TS, Vite 5)
   │  UI знает только интерфейс `Api` (src/api/types.ts)
   ├─ MockAdapter  (in-memory, latency, error-simulator)   ← VITE_API_MODE=mock (default)
   └─ RealAdapter  (fetch, X-API-Key)                      ← VITE_API_MODE=real
          │ same-origin: /api/*, /ws, /healthz
          ▼
      Vite dev-прокси (vite.config.ts)
          │  BACKEND_URL (default http://localhost:8080)
          ▼
      Go daemon (cmd/daemon): REST /api/v1/* + WS /ws + /healthz /readyz
```

- **Контракт 20/21 — единственный источник истины.** Frontend-типы — зеркало
  (`src/types/api.ts`). Расхождение «live vs контракт» = баг той стороны, которая
  отклоняется от контракта (исключение — задокументированные уточнения в
  `api-decisions.md`).
- **Mock-режим** — default, для offline-разработки UI и unit-тестов; покрывается
  тестами (mockAdapter.test.ts). В mock ничего не отстает от real-контракта.
- **Real-режим** — same-origin (Vite-прокси в dev; в production SPA отдаёт daemon).
  CORS в dev не нужен.

## 2. Auth

- Заголовок **`X-API-Key`** (lead-решение, blockers #9). Real-адаптер шлёт его,
  если задан `VITE_API_KEY`. WS: `?api_key=` (браузерный WebSocket не шлёт
  заголовки).
- Slice 6 (backend): auth включается при `DAEMON_API_KEYS` или DB-ключах
  (`daemon admin keys`); роли admin/operator/viewer; 403 `forbidden`.
  Frontend: flex-парсер ошибок коды понимает; `forbidden` — generic-ветка.

## 3. Ошибки

Единый envelope: `{"error": {code, message, request_id, details?}}`.
Frontend — flex-parse (`src/api/errors.ts`): понимает и envelope, и legacy;
known-коды мапятся в friendly-сообщения (`not_found`, `conflict`, `validation_failed`,
`forbidden`, ...). 404 на list-эндпоинтах, которые ещё не реализованы backend'ом,
UI показывает как нейтральный `Unavailable` («not available yet»), а не «Not found + Retry»
(F11).

## 4. Real-time (WebSocket)

- `GET /ws` (upgrade). Клиент шлёт `{"type":"subscribe","channels":[...]}` —
  повторный subscribe заменяет набор. Сервер шлёт `{"type","data","timestamp"}`
  только по подписанным каналам: `team:{id}`, `task:{id}`, `session:{id}`,
  `chatroom:{id}`, `watchdog:{id}` + глобальный `dashboard` (дубль каждого события).
- Frontend: `useWebSocket` (real + mock-синтетика). `connected` — только после
  `onopen` (F11: бейдж не врет при 404/неподключённом WS).
- Известная тонкость: событие, опубликованное **до** того, как сервер прочитал
  `subscribe`, теряется (нет ack). UI-код, критичный к событиям, должен
  идемпотентно рефетчить состояние при (re)connect.

## 5. Интеграционные тесты

- `frontend/tests/realIntegration.test.ts` — **15 тестов** против живого daemon
  (автоскип, если `healthz` недоступен/401). Покрывают:
  Team Builder vertical (topology snake_case + layout, validate, save), role config,
  error envelope (404/409), dashboard summary/tasks, sessions lifecycle + crash→failed
  + watchdog alert, tasks lifecycle (create/state/handoff/closure), messages
  (direct/broadcast/filters), chatrooms (авто-создание, send/list), workflows
  (CRUD, blocks, connections: индексы в POST /workflows, drag-patch с old/new),
  library (save/list/get/apply new+merge, `save_to_library` → `library_item_id`),
  audit (запись после действия + фильтр), metrics (12 точек × 5 серий, ranges),
  WS (subscribe dashboard → task.created).
- Настройки окружения:
  - `INTEGRATION_BASE_URL` — base daemon (default `http://localhost:8080`);
  - `INTEGRATION_API_KEY` — если daemon запущен с `DAEMON_API_KEYS`.
- Запуск: поднять daemon (см. §6), затем `INTEGRATION_BASE_URL=... npm test`.
- Тесты идемпотентны: все сущности создаются с уникальными stamp-именами `IT-*`;
  повторные прогоны на одной БД не падают (учитывается unique (type,name) в library).

## 6. Как поднять живой backend (для интеграции)

Backend WIP может не быть закоммичен — собирать из его working dir:

```bash
export PATH="$PATH:$HOME/sdk/go/bin"
cd backend && go build -o /tmp/daemon ./cmd/daemon
DAEMON_DB_DSN="sqlite:/tmp/daemon-<имя>.db" \
DAEMON_AGENT_SPECS_DIR=$(git rev-parse --show-toplevel)/agents \
DAEMON_LISTEN_ADDR=127.0.0.1:8080 /tmp/daemon
```

**Порты:** 8080 — «общий» демон (кто поднял первым — его, обычно backend-агент);
конфликтующие прогоны — на 8081+ и `INTEGRATION_BASE_URL`. Чужой демон не убивать;
если :8080 занят демоном с auth — передать `INTEGRATION_API_KEY` или взять свой порт.
Каждый агент — своя БД (`/tmp/daemon-<имя>.db`).

## 7. Протокол синхронизации

- `_workspace/integration-status.md` — общая таблица + API change log (пишут оба).
  Любое изменение форматов/эндпоинтов backend'ом фиксируется change-log-записью
  с «Frontend impact».
- `answer_backend.md` / `answer_frontend.md` — почтовые ящики (пишет тот, чей
  ответ; читает другой).
- `_workspace/blockers.md` — блокеры; закрывает лид.
- Формат-расхождение, найденное live: (1) сверить с контрактом 20/21, (2) задокументировать
  в change-log + api-decisions, (3) исправить отклонившуюся сторону, (4) зафиксировать
  интеграционным тестом.

## 8. Стейты экранов

- loading / empty / error (retry) / **unavailable** (404 на ещё не реализованном
  list-эндпоинте) — `src/components/ui/States.tsx`.
- Error-simulator для dev: `?mockError=unauthorized|forbidden|not_found|conflict|server|network`.

## 9. Команды

```bash
cd frontend
npm run dev                          # :5173, mock по умолчанию
VITE_API_MODE=real npm run dev       # real: нужен daemon (прокси → BACKEND_URL)
npm run typecheck && npm test && npm run build
INTEGRATION_BASE_URL=http://localhost:8080 npm test   # интеграционные против живого daemon
```
