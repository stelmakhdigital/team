# Frontend — роль и статус (точка восстановления сессии)

> Файл для восстановления работы. **Последнее обновление: 2026-10-08, ~11:10 (после F12, коммит см. ниже; до него `57aed07`/`ba52616`).**
> Рабочая зона: `frontend/**` (плюс статус-файлы `_workspace/frontend-status.md`, `_workspace/integration-status.md`, `_workspace/blockers.md`, доки `docs/architecture/frontend.md`, `docs/architecture/integration.md` (владею как лид), контракт `docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`).
> Роль: **frontend-инженер + lead-интегратор** (могу менять любые файлы для интеграции,
> но рабочую зону backend не трогаю — бекенду отдаю списки в `answer_backend.md`, читаю его отчёты в `answer_frontend.md`).

## 1. Стек и конвенции
- Vite 5 + React 18 + TypeScript (strict) + react-router-dom v6 + vitest/jsdom. **Без внешних runtime-зависимостей** (канвас — нативный SVG + pointer events + HTML5 DnD, без react-flow).
- Контракт — единственный источник истины: `docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`. Mirror типов: `src/types/api.ts`.
- API-фасад: `src/api/index.ts` (`Api`-интерфейс); два адаптера: `src/api/mock/adapter.ts` (in-memory, latency, error-simulator) и `src/api/real.ts` (fetch). UI знает только `Api`.
- Режимы: `VITE_API_MODE=mock|real` (`.env`, gitignored; default mock).
  - real mode: **same-origin** — Vite-прокси `/api`, `/ws`, `/healthz` → `BACKEND_URL` (default `http://localhost:8080`), CORS в dev не нужен.
  - Auth: заголовок **`X-API-Key`** (`VITE_API_KEY`), лид-решение (blockers #9).
- Error envelope `{"error":{code,message,request_id,details?}}` — flex-parse в `src/api/errors.ts`; friendly-map включает `validation_failed`.
- Коммит-айдент: `stelmakhdigital <budaev.digital@gmail.com>`; remote `git@github.com:stelmakhdigital/team.git`, ветка `master`.
- **НЕ коммитить WIP backend**: untracked `backend/**`, `agents/`, `answer_*.md`, `backend.md`, `backend/logs/` и чужие изменения `_workspace/backend-status.md`, `docs/architecture/backend.md`, `docs/contracts/api-decisions.md` — это зона backend-агента, он сам закоммитит.

## 2. Что сделано (F1–F12)
- **F1** каркас: layout (AppShell sidebar), 8 маршрутов, env-конфиг.
- **F2** типы 1-в-1 с контрактом, API client, mock adapter + seed.
- **F3** Dashboard: SummaryCards, TaskList, SessionGrid, AlertsPanel, MetricsChart (SVG).
- **F4** Team Builder (главная вертикаль): TeamsPage (list/create), TeamBuilderPage — SVG-canvas: drop сегментов/ролей, drag (debounced PATCH layout), RelativeConnector (создание/удаление связей), ConfigPanel, BottomPanel (server-validate + local hints, save, zoom/grid/snap).
- **F5** loading/empty/error/retry, toasts, dev-error-simulator (`?mockError=...`).
- **F6** Message Center, Library, History (audit + task history).
- **F7** Workflow Editor (blocks, connections, drag, patch).
- **F8** `useWebSocket` (real + mock-синтетика), unit-тесты.
- **F9** интеграция с реальным backend: Team Builder — real; Dashboard summary+tasks, History task history — real (slice 2); Dashboard sessions+alerts, History session history/transcript — real (slice 3, live e2e: start→stop, crash→failed+watchdog alert; `Api.sessions`); infra: `.env`, vite-прокси, same-origin default.
- **F10** Tasks lifecycle UI: страница `/tasks` (список+фильтры, create, inline-переходы state по карте `lib/task.ts`, handoff, done→closure_reason, expandable → история+subtasks); группа `tasks` в Api (real+mock). Контракт 20 §3.7.
- **F11** UI-аудит + фиксы (2026-10-08, headless Playwright, скриншоты real+mock, все 7 страниц):
  1) `useWebSocket` — `connected` только после onopen (бейдж больше не врёт при 404/неподключённом WS);
  2) 404 на list-эндпоинтах → `Unavailable` («not available yet», без Retry) — Dashboard metrics, History audit, Library, Messages;
  3) Canvas fit-to-view: авто-fit топологии при загрузке + кнопка ⤢ Fit (`contentBounds()` в palette.ts, `scrollRef` в TeamCanvas, `fitToView` в TeamBuilderPage).
  Audit подтвердил: drag ролей/сегментов (PATCH), HTML5 DnD из палитры, connect, config, save/validate — работают; 0 console/page errors.
- **F12** интеграционные тесты slice 4–5 (2026-10-08, `tests/realIntegration.test.ts` 7 → **15**):
  messages (direct/broadcast/filters, system→400), chatrooms (авто-создание, send/list),
  workflows (CRUD, индексы connections в POST, drag-patch old/new),
  library (save/409/list/get spec/apply new+merge, `save_to_library` → `library_item_id`, workflow-apply 400/409),
  audit (запись + фильтр action), metrics (12 точек × 5 серий × ranges),
  WS (subscribe `dashboard` → `task.created`; обработка гонки subscribe/event).
  Фасад: `Api.library.applyLibrary` (POST /library/{id}/apply, real+mock), `Api.dashboard.getMetrics({range})`.
  Контракт 20 §3.5: `?range=1h|24h|7d`. Тесты идемпотентны (stamp-имена `IT-*`), автоскип без daemon,
  настраиваются `INTEGRATION_BASE_URL` (default :8080) / `INTEGRATION_API_KEY`.

## 3. Текущий статус (2026-10-08, ~11:10)
- Git: HEAD — коммит F12 (sm. `git log --oneline -3`); до него `57aed07` (docs), `ba52616` (F11).
- Тесты: **45/45** (30 unit + 15 интеграционных против живого daemon; автоскип без daemon); typecheck OK; production build OK (81.9 KB gzip). Два последовательных прогона зелёные.
- **Backend-агент активен (2026-10-08, ~10:56+)**: в WIP — **slice 6 (RBAC + api_keys + secrets)**;
  его демон на :8080 (`./bin/daemon`, DB `/tmp/daemon-slice6.db`, `DAEMON_API_KEYS` — auth ВКЛ).
  Чужой демон не убивать: свои прогоны — `INTEGRATION_BASE_URL=http://localhost:8081` (собственный демон)
  или `INTEGRATION_API_KEY` против его демона. Slice 6 ещё не закоммичен.
- `docs/architecture/integration.md` — **написан** (лид-обязанность, blockers B1 закрыта).
- Контракт 20 актуален: §3.5 range; §5.4 apply; §3.6/§3.7 lifecycle.
- Наблюдения для backend (non-blocking, в `answer_backend.md`): from_role_name omitempty в GET /messages;
  имя item `"team-<name>"` в save_to_library; chatroom last_message (optional) не возвращается.

### Мои следующие шаги
1. Slice 6 (когда закоммитится): прогнать 15 интеграционных тестов с DB-ключом (admin);
   добавить ветку `forbidden` (403) в `src/api/errors.ts` friendly-map + интеграционные RBAC-тесты
   (viewer → 403 на POST; operator → 403 на PATCH /roles/{id}/config, 200 на остальное).
2. (не-blocking) real-интеграция WorkflowEditor drag → `PATCH /workflows/:id/blocks/:blockId`
   (endpoint уже покрыт интеграционным тестом; UI ещё на моках для этого действия) и
   apply-library из LibraryPage (`applyLibrary` уже в фасадe).
3. При необходимости — ветка `forbidden` в UI error-обработчике (friendly-текст).

## 4. Ключевые файлы
| Файл | Назначение |
|---|---|
| `frontend/src/types/api.ts` | mirror контракта (типы) |
| `frontend/src/api/{index,real,config,errors,http}.ts` | фасад, real-адаптер, env, ошибки, fetch-обёртка |
| `frontend/src/api/mock/{adapter,data}.ts` | mock-режим (in-memory seed) |
| `frontend/src/pages/TeamBuilderPage.tsx` | главный канвас (+fitToView, scrollRef) |
| `frontend/src/pages/TasksPage.tsx` | /tasks: create/state/handoff (F10) |
| `frontend/src/components/TeamBuilder/*` | canvas (scrollRef), toolbar, config/bottom panels (Fit), palette (`contentBounds`) |
| `frontend/src/components/Dashboard/*` | панели дашборда |
| `frontend/src/components/ui/States.tsx` | loading/empty/error/**unavailable** (`isNotFoundError`) |
| `frontend/src/hooks/{useQuery,useMutation,useWebSocket}.ts` | данные/мутации/WS (честный статус: connected после onopen) |
| `frontend/src/lib/task.ts` | task transitions + closure reasons |
| `frontend/vite.config.ts` | dev-сервер + прокси `/api`,`/ws`,`/healthz` → BACKEND_URL |
| `frontend/tests/realIntegration.test.ts` | интеграционные тесты (15; автоскип, если нет daemon; INTEGRATION_BASE_URL/INTEGRATION_API_KEY) |
| `frontend/tests/topology.test.ts` | unit: validate + contentBounds |
| `frontend/.env` / `.env.example` | режимы (`.env` gitignored) |
| `_workspace/{frontend,integration,blockers}-status.md` | статусы (integration-status — общий с backend) |
| `docs/architecture/frontend/20_contract_API.md` | API-контракт (владею как лид) |

## 5. Команды
```bash
cd frontend
npm run dev                 # :5173, mock по умолчанию
VITE_API_MODE=real npm run dev   # real: нужен daemon (прокси → :8080)
npm run typecheck && npm test && npm run build
# живой backend для интеграционных тестов (WIP backend НЕ собран в git — брать из его working dir):
export PATH="$PATH:$HOME/sdk/go/bin"   # tilde не разворачивается в этом shell
cd ../backend && go build -o /tmp/daemon ./cmd/daemon
# СВОЙ демон (не конфликтовать с backend-агентом на :8080):
(DAEMON_LISTEN_ADDR=127.0.0.1:8081 DAEMON_DB_DSN="sqlite:/tmp/daemon-lead.db" \
 DAEMON_AGENT_SPECS_DIR=<root>/agents /tmp/daemon > /tmp/daemon-lead.log 2>&1 &)
INTEGRATION_BASE_URL=http://localhost:8081 npm test   # 15 интеграционных против живого daemon
# headless-аудит (playwright; системные либы через LD_LIBRARY_PATH, sudo недоступен):
LD_LIBRARY_PATH=/tmp/pwlibs/extracted/usr/lib/x86_64-linux-gnu node /tmp/shots/<скрипт>.mjs
```

## 6. Открытые вопросы / лид-обязанности
- Kонтракт 20: после коммита backend slice 6 сверить RBAC-формы (403 `forbidden`, audit user_id/api_key_id, unread_count) — при расхождениях править контракт (я, лид).
- К бекенду (non-blocking, в answer_backend.md): WS read-ping (gorilla «корruptит» соединение по read-таймауту — у них в change-log), `from_role_name` в GET /messages ("You"), `last_message` в chatrooms, задокументировать имя item `"team-<name>"` в save_to_library.

## 7. Как восстановить сессию
1. Прочитать этот файл + `_workspace/integration-status.md` (таблица + последние 2–3 записи change-log) + `_workspace/blockers.md` + `answer_backend.md` (мой последний ответ backend'у).
2. `cd frontend && npm test` — ожидается **45/45** (интеграционные скипаются без daemon; поднять по п.5, `INTEGRATION_BASE_URL=http://localhost:8081`).
3. `git log --oneline -3` — HEAD = коммит F12 (или новее, если продолжил).
4. Дальше — «Следующие шаги» (п.3): RBAC-ветки после коммита slice 6, WorkflowEditor drag, apply-library из UI.
