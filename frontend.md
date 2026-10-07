# Frontend — роль и статус (точка восстановления сессии)

> Файл для восстановления работы. **Последнее обновление: 2026-10-08, ~02:10 (после F11, коммит `ba52616` pushed).**
> Рабочая зона: `frontend/**` (плюс статус-файлы `_workspace/frontend-status.md`, `_workspace/integration-status.md`, `_workspace/blockers.md`, доки `docs/architecture/frontend.md`, контракт `docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`).
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

## 2. Что сделано (F1–F11)
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

## 3. Текущий статус (2026-10-08, конец сессии)
- Git: `master` = `origin/master` = **`ba52616`** (F11 audit-фиксы). До него: `04f8749` (F10), `43b6a3a` (slice 3), `b5e818b` (run-ready).
- Тесты: **37/37** (30 unit + 7 интеграционных против живого daemon на :8080; автоскип без daemon); typecheck OK; production build OK (81.6 KB gzip).
- **Backend дошил slice 4–5 во время F11-аудита** (WIP у него в рабочем дереве, не закоммичен): `/ws` (101 Switching Protocols), `/dashboard/metrics`, `/audit`, `/library`, `/messages`, `/chatrooms`, `/workflows` — все live. `transcript.total` — закрыт (slice 5b).
- **Весь UI проверен live в real mode** (скриншоты): Dashboard (+metrics с реальными графиками), Teams, Tasks, Workflows, Messages (chatrooms по командам), Library, History (audit-лог наполнен), Builder (авто-fit). WS-бейдж «real-time: connected» теперь истинный.
- Что на mocks — **ничего**. Mock-режим остаётся для offline-разработки (default) и покрывается тестами.
- Slice 1 layout (blockers #8) — закрыто. Контракт 20 (я, лид) актуален: §2.0 `GET /workflows`, §3.6 Session lifecycle, §3.7 Task lifecycle, `RelativeSpec` from/to.

### Мои следующие шаги
1. **Интеграционные тесты real-эндпоинтов slice 4–5** в `frontend/tests/realIntegration.test.ts`:
   messages (create direct/broadcast + list), chatrooms (list, messages), workflows (CRUD + blocks/connections),
   library (save→list→get→apply new+merge; `save_to_library` в POST /teams/:id/save → `library_item_id`),
   audit (list + фильтры), metrics (12 точек, ranges), WS dashboard-канал (создать task → получить event).
   Форматы live-проверены вручную, форматы в change-log `integration-status.md`.
2. После коммита backend slice 4–5 (он ещё в WIP) — прогнать полный e2e-свип и зафиксировать в интеграционных тестах.
3. `docs/architecture/integration.md` — написать (лид, бекенд ждёт; п.6).
4. Потенциально: real-интеграция WorkflowEditor (drag blocks → `PATCH /workflows/:id/blocks/:blockId`),
   apply-library из LibraryPage (сейчас list-only + Unavailable-fallback).

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
| `frontend/tests/realIntegration.test.ts` | интеграционные тесты (7; автоскип, если нет daemon на :8080) |
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
(DAEMON_DB_DSN="sqlite:/tmp/daemon-lead.db" DAEMON_AGENT_SPECS_DIR=<root>/agents /tmp/daemon &)
curl -s localhost:8080/healthz
# headless-аудит (playwright; системные либы через LD_LIBRARY_PATH, sudo недоступен):
LD_LIBRARY_PATH=/tmp/pwlibs/extracted/usr/lib/x86_64-linux-gnu node /tmp/shots/<скрипт>.mjs
```

## 6. Открытые вопросы / лид-обязанности
- Написать `docs/architecture/integration.md` (бекенд ждёт).
- Kонтракт 20: после коммита backend slice 4–5 сверить форматы library/workflow/audit с контрактом (по change-log в `integration-status.md`), при расхождениях — править контракт (я, лид).
- К бекенду (non-blocking): WS read-ping (gorilla «корruptит» соединение по read-таймауту — у них в change-log), `unread_count` (RBAC slice 6), apply library для role/segment.

## 7. Как восстановить сессию
1. Прочитать этот файл + `_workspace/integration-status.md` (таблица + последние 2 записи change-log) + `_workspace/blockers.md`.
2. `cd frontend && npm test` — ожидается **37/37** (интеграционные скипаются без daemon; поднять по п.5).
3. `git log --oneline -3` — HEAD должен быть `ba52616` (или новее, если продолжил).
4. Дальше — «Следующие шаги» (п.3): интеграционные тесты slice 4–5, integration.md, сверка контракта.
