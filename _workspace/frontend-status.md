# Frontend status

## Current phase
implementation (F1–F8 done; F9 — Team Builder, Dashboard (summary/tasks/sessions/alerts),
History (task/session history, transcript) интегрированы с реальным backend;
Tasks lifecycle UI (create/state/handoff) — real; Messages/Workflows/Library/WS/metrics ждут backend slice 4–5)

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
  `tests/realIntegration.test.ts`). Auth заголовок переведён на `X-API-Key` (лид-решение, blockers #9).
- F9 (slice 2): Dashboard (summary+tasks) и History (task history) интегрированы
  с реальным backend; интеграционные тесты расширены (итого 4, все зелёные).
- F9 (slice 3, 2026-10-07): Dashboard sessions/alerts и History (session history,
  transcript) на real API. Добавлена группа `sessions` в Api-фасад (list/create/get/stop —
  real + mock), HistoryPage берёт реальные id task/session из dashboard (убран хардкод #1).
  Live e2e: create → running → history → transcript → dashboard → stop (идемпотентен) и
  crash-сценарий: exit 3 → state=failed + exit_code + history → failed + watchdog alert.
  Интеграционных тестов стало 6 (slice 3: lifecycle + failed/watchdog).
- F10 Tasks lifecycle UI (2026-10-07): страница `/tasks` (список с фильтрами team/state,
  create-модалка, inline-переходы state по ALLOWED-карте, handoff-модалка, done требует
  closure_reason, expandable-строка → история + subtasks). Группа `tasks` в Api-фасад
  (list/create/get/updateState/handoff — real + mock). `lib/task.ts` — transitions + closure reasons.
  Работает в mock и real (backend slice 2 API готов). Контракт 20 дополнен §3.7 Task lifecycle.
- F9 (infra): UI готов к запуску в обоих режимах — `npm run dev` (mock, default, .env создан);
  real mode: same-origin + Vite-прокси `/api`,`/ws`,`/healthz` → backend (BACKEND_URL,
  default :8080) — CORS в dev не нужен; default base URL/ws URL = same-origin.
  Остальные экраны/панели — на mocks до backend slice 3–5.

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
- `GET/POST /api/v1/sessions` (lifecycle: create+start), `GET /api/v1/sessions/:id`, `DELETE /api/v1/sessions/:id` (stop, идемпотентен)
- `GET /api/v1/sessions/:id/history`, `GET /api/v1/sessions/:id/transcript`
- `GET /api/v1/tasks` (list + фильтры), `POST /api/v1/tasks`, `GET /api/v1/tasks/:id`
- `PATCH /api/v1/tasks/:id/state`, `POST /api/v1/tasks/:id/handoff`, `GET /api/v1/tasks/:id/history`
- `GET/POST /api/v1/messages`, `GET /api/v1/chatrooms`, `GET/POST /api/v1/chatrooms/:id/messages`
- `GET/POST /api/v1/library`, `GET /api/v1/library/:id`
- `GET /api/v1/audit`, `GET /api/v1/tasks/:id/history`
- `GET/POST /api/v1/workflows`, `GET /api/v1/workflows/:id`, `POST …/blocks`, `POST …/connections`, `PATCH …/blocks/:blockId`
- WS: `ws://…/ws` subscribe channels

## Mock status
- MockAdapter полностью покрывает используемые endpoint'ы, данные строго по типам контракта
- `VITE_API_MODE=mock` (default) | `real` (fetch, `VITE_API_BASE_URL`, `X-API-Key` `VITE_API_KEY`)
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
- unit/component tests: OK (vitest, 34 теста: topology, errors, mock-контракт (вкл. tasks lifecycle), UI states, app smoke (Dashboard/Teams/Tasks), real-integration×7)
- production build: OK (78 KB gzip)
- интеграция с живым backend: OK (Team Builder vertical, slice 2: dashboard summary/tasks + task history, slice 3: sessions lifecycle + history/transcript + alerts, tasks lifecycle: create/state/handoff)

## Backend impact
- СДЕЛАНО (контракт 20 обновлён, лид 2026-10-07): `GET /api/v1/workflows` задокументирован (2.0) — ждём реализацию в slice 5 (blockers #3)
- СДЕЛАНО (контракт 20, лид): RelativeSpec = адресный формат `from`/`to` (blockers #8 — закрыть)
- СДЕЛАНО (контракт 20, лид): раздел 3.6 Session lifecycle (POST/GET/DELETE /sessions) — формы зафиксированы по live-проверке
- Осталось у backend: `GET /sessions/:id/transcript` — нет поля `total` (контракт 20 §6.4 требует `{transcript, total, has_more}`)
- Контракт 21: request'ы содержат поле `layout` — backend принимает (slice 1), ok
- Error-модель: envelope `error.code/message/request_id` — frontend flex-parse + `validation_failed` обработаны (blockers #2 — закрыть)
- Auth: `X-API-Key` реализован с обеих сторон (blockers #1 — закрыть)

## Blockers
- см. _workspace/blockers.md

## Next step
- Slice 4 (Messages) → real; Slice 5: Workflows/Library/WS/audit/metrics + `GET /workflows`
- ~~UI управления задачами~~ — сделано (F10, страница /tasks, real API)
- Backend: доделать `transcript.total` (non-blocking)
