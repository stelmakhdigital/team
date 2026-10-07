# Frontend — роль и статус (точка восстановления сессии)

> Файл для восстановления работы. Последнее обновление: 2026-10-07 (после теста slice 3).
> Рабочая зона: `frontend/**` (плюс статус-файлы `_workspace/*`, доки `docs/architecture/frontend.md`).
> Роль: **frontend-инженер + lead-интегратор** (могу менять любые файлы для интеграции,
> но рабочую зону backend не трогаю — бекенду отдаю списки, см. `answer_backend.md`).

## 1. Стек и конвенции
- Vite 5 + React 18 + TypeScript (strict) + react-router-dom v6 + vitest/jsdom. **Без внешних runtime-зависимостей** (канвас — нативный SVG + pointer events + HTML5 DnD, без react-flow).
- Контракт — единственный источник истины: `docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`. Мirror типов: `src/types/api.ts`.
- API-фасад: `src/api/index.ts` (`Api`-интерфейс); два адаптера: `src/api/mock/adapter.ts` (in-memory, latency, error-simulator) и `src/api/real.ts` (fetch). UI знает только `Api`.
- Режимы: `VITE_API_MODE=mock|real` (`.env`, gitignored; default mock).
  - real mode: **same-origin** — Vite-прокси `/api`, `/ws`, `/healthz` → `BACKEND_URL` (default `http://localhost:8080`), CORS в dev не нужен.
  - Auth: заголовок **`X-API-Key`** (`VITE_API_KEY`), лид-решение (blockers #9).
- Error envelope `{"error":{code,message,request_id,details?}}` — flex-parse в `src/api/errors.ts`; friendly-map включает `validation_failed`.
- Коммит-айдент: `stelmakhdigital <budaev.digital@gmail.com>`; remote `git@github.com:stelmakhdigital/team.git`, ветка `master`. **Не коммитить WIP backend** (untracked session/runtime файлы).

## 2. Что сделано (F1–F9, всё в `frontend/`)
- **F1** каркас: layout (AppShell sidebar), 8 маршрутов, env-конфиг.
- **F2** типы 1-в-1 с контрактом, API client, mock adapter + seed.
- **F3** Dashboard: SummaryCards, TaskList, SessionGrid, AlertsPanel, MetricsChart (SVG).
- **F4** Team Builder (главная вертикаль): TeamsPage (list/create), TeamBuilderPage — SVG-canvas: drop сегментов/ролей, drag (debounced PATCH layout), RelativeConnector (создание/удаление связей), ConfigPanel, BottomPanel (server-validate + local hints, save, zoom/grid/snap).
- **F5** loading/empty/error/retry, toasts, dev-error-simulator (`?mockError=...`).
- **F6** Message Center, Library, History (audit + task history).
- **F7** Workflow Editor (blocks, connections, drag, patch).
- **F8** `useWebSocket` (real + mock-синтетика), unit-тесты.
- **F9** интеграция с реальным backend:
  - Team Builder — **real** (интеграционные тесты зелёные);
  - Dashboard summary+tasks, History task history — **real** (slice 2);
  - Dashboard sessions+alerts, History session history/transcript — **real** (slice 3, live e2e:
    lifecycle start→stop, crash→failed+watchdog alert; `Api.sessions` list/create/get/stop);
  - infra: `.env` + `.env.example`, vite-прокси, same-origin default (commit `b5e818b`).
- **F10** Tasks lifecycle UI: страница `/tasks` (список+фильтры, create, inline-переходы state,
  handoff, done→closure_reason, expandable → история+subtasks); группа `tasks` в Api (real+mock);
  работает в mock и real (backend slice 2). Контракт 20 §3.7.

## 3. Текущий статус (2026-10-07, после F10 Tasks UI)
- Тесты: **34/34** (unit + 7 интеграционных против живого daemon на :8080); typecheck OK; production build OK (81 KB gzip).
- Backend `go test ./...` — **все зелёные** (failing `TestSessionFailedProcessAndWatchdog` исправлен бекендом). Каталог `agents/` с fixture-spec'ами (pi-lead/pi-worker/pi-reviewer) существует — live-сессии гоняются.
- Live e2e slice 3 (real API client): create (role+`sleep`) → running → history → transcript → dashboard/sessions → stop (идемпотентен 200); crash `exit 3` → **failed** + exit_code + history → failed + watchdog alert.
- Slice 1 layout (blockers #8) — закрыто live: topology `layout.relatives` заполнен; PATCH layout —
  нормальные SegmentLayout, previous≠new; create-ответы возвращают layout, если передан в request.
- Контракт 20 (я, лид) обновлён: §3.6 Session lifecycle, §2.0 `GET /workflows`, `RelativeSpec` from/to.
- WS `/ws` → 404 (slice 5, ожидаемо). `GET /dashboard/metrics`, `GET /audit` → 404 (slice 5).
- Known mismatch (backend): `GET /sessions/:id/transcript` без `total`.

### Что на mocks и когда переключаем
| Экран/панель | Сейчас | Переключаем, когда |
|---|---|---|
| Messages | mock | slice 4 |
| Workflows list/editor, Library apply | mock | slice 5 (+ реализация `GET /workflows`, задокументирован в 2.0) |
| WS real | mock-синтетика | slice 5 |
| Dashboard metrics | mock/404 | slice 5 |
| History audit | mock/404 | slice 5 |

### Мои следующие шаги
1. Slice 4 → Messages real (после отчёта бекенда по slice 4).
2. Slice 5 → Workflows/Library/WS/audit/metrics + реализация `GET /workflows` (контракт готов, 2.0).
3. Бекенду (non-blocking): `transcript.total`.
4. ~~UI управления задачами~~ — **сделано (F10, 2026-10-07)**: страница `/tasks` (create/state/handoff),
   real API (slice 2), интеграционные тесты. Контракт 20 §3.7.

### F10: UI управления задачами (сделано 2026-10-07)
- `src/pages/TasksPage.tsx` — /tasks: список (фильтры team/state), create-модалка,
  inline-переходы state (карта `lib/task.ts`), handoff-модалка, done → closure_reason,
  expandable-строка → история + subtasks.
- Api: группа `tasks` (list/create/get/updateState/handoff) real + mock.
- Тесты: mock lifecycle + интеграционный tasks lifecycle (live) + app smoke /tasks.
- Тесты: **34/34** (27 unit + 7 интеграционных); typecheck/build OK.

## 4. Ключевые файлы
| Файл | Назначение |
|---|---|
| `frontend/src/types/api.ts` | mirror контракта (типы) |
| `frontend/src/api/{index,real,config,errors,http}.ts` | фасад, real-адаптер, env, ошибки, fetch-обёртка |
| `frontend/src/api/mock/{adapter,data}.ts` | mock-режим |
| `frontend/src/pages/TeamBuilderPage.tsx` | главный канвас |
| `frontend/src/pages/TasksPage.tsx` | /tasks: create/state/handoff (F10) |
| `frontend/src/components/TeamBuilder/*` | canvas, toolbar, config/bottom panels, palette |
| `frontend/src/components/Dashboard/*` | панели дашборда |
| `frontend/src/hooks/{useQuery,useMutation,useWebSocket}.ts` | данные/мутации/WS |
| `frontend/src/lib/task.ts` | task transitions + closure reasons |
| `frontend/vite.config.ts` | dev-сервер + **прокси** `/api`,`/ws`,`/healthz` → BACKEND_URL |
| `frontend/tests/realIntegration.test.ts` | интеграционные тесты (6; автоскип, если нет daemon на :8080) |
| `frontend/.env` / `.env.example` | режимы (`.env` gitignored) |

## 5. Команды
```bash
cd frontend
npm run dev        # :5173, mock по умолчанию
VITE_API_MODE=real npm run dev   # real: нужен daemon (прокси → :8080)
npm run typecheck && npm test && npm run build
# живой backend для интеграционных тестов:
export PATH="$PATH:$HOME/sdk/go/bin"   # tilde не разворачивается в этом shell
cd ../backend && go build -o /tmp/daemon ./cmd/daemon
(DAEMON_DB_DSN="sqlite:/tmp/daemon-lead.db" DAEMON_AGENT_SPECS_DIR=<каталог с spec'ами> /tmp/daemon &)
curl -s localhost:8080/healthz
```

## 6. Открытые вопросы к контракту (на мне, lead)
- Контракт 20: `RelativeSpec.from_role/to_role` → `from`/`to` (формат `"segment.role"`, backend так отдаёт; blockers #8).
- Контракт 20: добавить `GET /api/v1/workflows` (blockers #3).
- `docs/architecture/integration.md` — должен написать лид (бекенд ждёт).
- Остатки slice 1 у бекенда (layout.relatives, формы previous/new layout, null layout в create-ответах) — в `answer_backend.md` п.3.

## 7. Как восстановить сессию
1. Прочитать этот файл + `_workspace/integration-status.md` (последние 2 записи) + `_workspace/blockers.md`.
2. `cd frontend && npm test` — должен быть 34/34 (интеграционные скипаются без daemon; поднять по п.5).
3. `git log --oneline -3` — последний frontend-коммит `b5e818b`.
4. Дальше — «Следующие шаги» (п.3), после отчёта бекенда (формат отчёта — в `answer_backend.md`).
