# Frontend status

## Current phase
implementation (F1–F8 done; F9 — Team Builder интегрирован с реальным backend, остальные слайсы ждут backend slice 2–5)

## Implemented
- docs/architecture/frontend.md — архитектура и frontend-план (F1–F9)
- F1: каркас Vite + React 18 + TypeScript, layout (sidebar), маршруты, env-конфиг
- F2: типы `src/types/api.ts` 1-в-1 с контрактом (20+21), API client (fetch, ApiClientError), mock adapter (in-memory store, latency, error-simulator), seed-данные по контракту
- F3: Dashboard: SummaryCards, TaskList, SessionGrid, AlertsPanel, MetricsChart (SVG) на mocks
- F4: Team Builder вертикальный срез: TeamsPage (list/create), TeamBuilderPage — SVG canvas: drop сегментов/ролей из тулбара, drag (debounced PATCH layout), связи RelativeConnector (create/delete), ConfigPanel (role config), BottomPanel (validate: server + local hints, save, zoom/grid/snap)
- F5: loading/empty/error/retry states, toasts, dev-переключатель mock-ошибок (unauthorized/forbidden/not_found/conflict/server/network)
- F6: Message Center (messages + chatrooms), Library (list/save), History (audit + task history)
- F7: Workflow Editor (blocks, connections, drag, patch)
- F8: useWebSocket (real + mock-синтетика), unit-тесты (topology, client, mock adapter)
- F9: Team Builder вертикаль интегрирована с реальным backend (real API client,
  `tests/realIntegration.test.ts` — 3 интеграционных теста зелёные на живом daemon).
  Auth заголовок переведён на `X-API-Key` (лид-решение, blockers #9).
  Остальные экраны — на mocks до backend slice 2–5.

## Pages
- `/` Dashboard
- `/teams` Teams (list, create)
- `/teams/:id` Team Builder (canvas)
- `/workflows` Workflows list
- `/workflows/:id` Workflow Editor
- `/messages` Message Center
- `/library` Library
- `/history` History Viewer

## API usage
- `GET /api/v1/teams`, `POST /api/v1/teams`, `GET /api/v1/teams/:id/topology`
- `POST /api/v1/teams/:id/segments`, `POST /api/v1/segments/:segmentId/roles`
- `PATCH /api/v1/segments/:id/layout`, `PATCH /api/v1/roles/:id/layout`
- `POST /api/v1/teams/:id/relatives`, `DELETE /api/v1/relatives/:id`
- `GET/PATCH /api/v1/roles/:id/config`
- `POST /api/v1/teams/:id/validate`, `POST /api/v1/teams/:id/save`
- `GET /api/v1/dashboard/{summary,tasks,sessions,alerts,metrics}`
- `GET/POST /api/v1/messages`, `GET /api/v1/chatrooms`, `GET/POST /api/v1/chatrooms/:id/messages`
- `GET/POST /api/v1/library`, `GET /api/v1/library/:id`
- `GET /api/v1/audit`, `GET /api/v1/tasks/:id/history`
- `GET/POST /api/v1/workflows`, `GET /api/v1/workflows/:id`, `POST …/blocks`, `POST …/connections`, `PATCH …/blocks/:blockId`
- WS: `ws://…/ws` subscribe channels

## Mock status
- MockAdapter полностью покрывает используемые endpoint'ы, данные строго по типам контракта
- `VITE_API_MODE=mock` (default) | `real` (fetch, `VITE_API_BASE_URL`, Bearer `VITE_API_KEY`)
- Error-simulator: `?mockError=unauthorized|forbidden|not_found|conflict|server|network`
- WS mock: синтетические `task.state_changed`, `alert.created`, `message.sent`

## Commands
```bash
cd frontend
npm install
npm run dev        # http://localhost:5173 (mock mode by default)
npm run typecheck  # tsc --noEmit
npm run lint       # tsc --noEmit
npm test           # vitest run
npm run build      # production build
```

## Validation
- typecheck: OK
- unit/component tests: OK (vitest, 28 тестов: topology, errors, mock-контракт, UI states, app smoke, real-integration)
- production build: OK
- интеграция с живым backend: OK (Team Builder vertical: create→topology→validate→save, role config, error envelope)

## Backend impact
- НУЖЕН: `GET /api/v1/workflows` (список), в контракте только `GET /workflows/:id` (blockers #3)
- Контракт 21: request'ы содержат поле `layout` — backend принимает (slice 1), ok
- Контракт 20: `RelativeSpec.from_role/to_role` vs backend `from/to` — нужно обновить контракт (blockers #8)
- Error-модель: envelope `error.code/message/request_id` — frontend flex-parse + `validation_failed` обработаны (blockers #2 — закрыть)
- Auth: `X-API-Key` реализован с обеих сторон (blockers #1 — закрыть)

## Blockers
- см. _workspace/blockers.md

## Next step
- Ждать backend slice 2 (dashboard, tasks, history) → переключить Dashboard/History на real API
- Slice 3–5: sessions, messages, workflows/library/WS → по мере готовности
- Обновить контракт 20: spec relatives `from/to` (blockers #8)
