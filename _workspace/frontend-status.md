# Frontend status

## Current phase
implementation (F1–F14 done; весь UI интегрирован с реальным backend; 55/55 тестов включая
RBAC (slice 6, live на :8080) и auth (X-API-Key + WS ?api_key=); LibraryPage: save + apply
для всех типов; контракт 20: §5.4 все типы apply, §4.2 "You", §3.5 range; B3 закрыт (в);
next: финальный свип после коммита slice 6)

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
- F11 UI-audit + fixes (2026-10-08): headless-аудит всех страниц (real+mock, скриншоты):
  1) **WS-бейдж честный** (баг-фикс): `useWebSocket` раньше ставил `connected` до onopen —
     при 404 на /ws бейдж врал «real-time: connected». Теперь connected только после onopen.
  2) **404-панели → Unavailable** (не «Not found + Retry»): Dashboard metrics, History audit,
     Library, Messages — 404 на list-эндпоинте = «feature not available yet» (нейтрально, без Retry).
  3) **Canvas fit-to-view**: авто-fit топологии при загрузке + кнопка ⤢ Fit в BottomPanel
     (`contentBounds()` в palette.ts; zoom 0.4–1.25 + скролл к контенту). Раньше 2000×1200 канвас
     показывал только левый верхний угол, контент обрезался — «графическое редактирование некорректно».
  Verified: role/segment-drag (PATCH), HTML5 DnD из палитры, connect, config, save/validate — работают.
  Тесты: 37/37 (+3 unit для contentBounds).
- F12 интеграционные тесты slice 4–5 (2026-10-08): `tests/realIntegration.test.ts` 7 → **15**
  (messages direct/broadcast/filters + system→400; chatrooms авто-создание + send/list;
  workflows CRUD + индексы connections в POST + drag-patch old/new; library save/409/list/get
  spec/apply new+merge + save_to_library→library_item_id + workflow-apply 400/409; audit
  запись+фильтр; metrics 12×5 × ranges; WS subscribe dashboard → task.created).
  Фасад: `Api.library.applyLibrary` (POST /library/{id}/apply, real+mock),
  `Api.dashboard.getMetrics({range})`. Тесты идемпотентны (stamp-имена IT-*),
  автоскип без daemon; настраиваются `INTEGRATION_BASE_URL` / `INTEGRATION_API_KEY`
  (порт :8080 можно делить с backend-агентом — я гонял на :8081). Итог: 45/45.
- F13 хвосты + auth-ready (2026-10-08):
  1) **WS auth баг-фикс**: `getApiConfig()` добавляет `?api_key=` (или `&api_key=`) в wsUrl
     при `VITE_API_KEY` — без этого `useWebSocket` при auth-демане (slice 6) получил бы 401.
     Интеграционные тесты тоже шлют `?api_key=` (INTEGRATION_API_KEY).
  2) **LibraryPage**: save — выбор команды (было хардкод source_id:1); Apply в detail-pane:
     team → «Apply as new team» / «Merge into <select>»; workflow → «Apply to team <select>»;
     role/segment → hint «not supported (400)».
  3) Unit: apiConfig.test.ts (4), mock applyLibrary (6 сценариев), appSmoke Library (team+workflow).
  4) Лид-решение blockers B3 (PG `?` vs pgx v5): **(в) PG out-of-scope до окружения**,
     потом миграция на `$N` + e2e на PG-DSN.
  Итог: **52/52** (37 unit + 15 integration), включая прогон против демона с DAEMON_API_KEYS.
  WorkflowEditor drag → PATCH block подтверждён уже real (Api.workflows.updateBlock) — хвост F12 закрыт.
- F14 slice 6 (RBAC) + role/segment-apply (2026-10-08, backend довёл WIP slice 6):
  1) **3 RBAC-интеграционных теста** (автоскип без `INTEGRATION_VIEWER_KEY`/
     `INTEGRATION_OPERATOR_KEY`): viewer GET 200/POST 403 `forbidden`; operator POST 201 /
     PATCH role config 403; audit запись DB-ключа с user_id/api_key_id.
     Live-прогон против демона backend'а на :8080 (env-live-key + itest-viewer/itest-operator).
  2) **role/segment-apply**: mock `applyLibrary` (merge/applied, идемпотентность, дубль 409),
     LibraryPage — Apply для всех типов (role/segment: target-team select);
     контракт 20 §5.4 обновлён (все типы), §4.2 `from_role_name: "You"` (наблюдение закрыто).
  Итог: **55/55** (37 unit + 18 интеграционных) против slice-6 демона; typecheck/build OK.
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
- `GET/POST /api/v1/library`, `GET /api/v1/library/:id`, `POST /api/v1/library/:id/apply`
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
- unit/component tests: OK (vitest, 55 тестов: topology + contentBounds, errors, api config (wsUrl+api_key), mock-контракт (вкл. tasks lifecycle + library apply все типы), UI states, app smoke (Dashboard/Teams/Tasks/Library), real-integration×18 (вкл. 3 RBAC))
- production build: OK (82.9 KB gzip)
- интеграция с живым backend: OK — прогон против slice-6 демона :8080 (auth:
  env-live-key + DB-ключи itest-viewer/itest-operator): Team Builder, slice 2/3, tasks,
  messages/chatrooms, workflows, library (все типы apply), audit (user_id/api_key_id),
  metrics, WS (?api_key=), RBAC 403

## Docs (лид)
- `docs/architecture/integration.md` — написан 2026-10-08 (модель, auth, ошибки, WS,
  интеграционные тесты, как поднять backend, протокол синхронизации).

## Backend impact
- Slice 4–5: форматы сходятся; наблюдения: from_role_name — **закрыто** (slice 6, "You");
  имя item `"team-<name>"` в save_to_library (уточнение, не блокирует);
  chatroom last_message (optional) — open, non-blocking.
- Slice 6 (RBAC): **live-проверено** (3 RBAC-теста, 55/55); ждём коммит WIP slice 6
  для финального свипа.

## Blockers
- см. _workspace/blockers.md

## Next step
- После коммита slice 6 — финальный свип (55 тестов) + пометки «done» в статусах.
