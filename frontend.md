# Frontend — роль и статус (точка восстановления сессии)

> Файл для восстановления работы. Последнее обновление: 2026-10-08 (после UI-аудита F11 + backend slice 5 live).
> Рабочая зона: `frontend/**` (плюс статус-файлы `_workspace/*`, доки `docs/architecture/frontend.md`).
> Роль: **frontend-инженер + lead-интегратор** (могу менять любые файлы для интеграции,
> но рабочую зону backend не трогаю — бекенду отдаю списки, см. `answer_backend.md`).

## 1. Стек и конвенции
- Vite 5 + React 18 + TypeScript (strict) + react-router-dom v6 + vitest/jsdom. **Без внешних runtime-зависимостей** (канвас — нативный SVG + pointer events + HTML5 DnD, без react-flow).
- Контракт — единственный источник истины: `docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`. Mirror типов: `src/types/api.ts`.
- API-фасад: `src/api/index.ts` (`Api`-интерфейс); два адаптера: `src/api/mock/adapter.ts` (in-memory, latency, error-simulator) и `src/api/real.ts` (fetch). UI знает только `Api`.
- Режимы: `VITE_API_MODE=mock|real` (`.env`, gitignored; default mock).
  - real mode: **same-origin** — Vite-прокси `/api`, `/ws`, `/healthz` → `BACKEND_URL` (default `http://localhost:8080`), CORS в dev не нужен.
  - Auth: заголовок **`X-API-Key`** (`VITE_API_KEY`), лид-решение (blockers #9).
- Error envelope `{"error":{code,message,request_id,details?}}` — flex-parse в `src/api/errors.ts`; friendly-map включает `validation_failed`.
- Коммит-айдент: `stelmakhdigital <budaev.digital@gmail.com>`; remote `git@github.com:stelmakhdigital/team.git`, ветка `master`. **Не коммитить WIP backend** (untracked session/runtime файлы).

## 2. Что сделано (F1–F11, всё в `frontend/`)
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
- **F11** UI-аудит + фиксы (2026-10-08, headless Playwright, скриншоты real+mock, все 7 страниц):
  1) `useWebSocket` — `connected` только после onopen (бейдж больше не врёт при 404/неподключённом WS);
  2) 404 на list-эндпоинтах → `Unavailable` («not available yet», без Retry) — Dashboard metrics,
     History audit, Library, Messages;
  3) Canvas fit-to-view: авто-fit топологии при загрузке + кнопка ⤢ Fit (`contentBounds()` в palette.ts,
     scrollRef в TeamCanvas, fitToView в TeamBuilderPage). Drag/DnD/connect/config/save/validate verified.

## 3. Текущий статус (2026-10-08, после F11)
- Тесты: **37/37** (30 unit + 7 интеграционных против живого daemon на :8080); typecheck OK; build OK (81.6 KB gzip).
- **Backend дошил slice 4–5** (во время аудита): `/ws` (101), `/dashboard/metrics`, `/audit`, `/library`,
  `/messages`, `/chatrooms`, `/workflows` — все live.
- **Весь UI проверен live в real mode** (скриншоты): Dashboard (+metrics с реальными графиками),
  History (audit-лог наполнен), Messages (chatrooms), Library, Workflows, Builder (авто-fit).
- WS-бейдж «real-time: connected» теперь истинный (реальное WS-соединение).
- `transcript.total` — закрыт backend'ом (slice 5b). Slice 1 layout (blockers #8) — закрыто live.
- Контракт 20 (я, лид): §3.6 Session lifecycle, §2.0 `GET /workflows`, `RelativeSpec` from/to.

### Что на mocks — ничего
Весь UI на real API (verified 2026-10-08). Mock-режим остаётся для offline-разработки
(`VITE_API_MODE=mock`, default) и покрывается отдельными тестами.

### Мои следующие шаги
1. Интеграционные тесты real-эндпоинтов slice 4–5 (messages/chatrooms, workflows, library save/apply,
   audit, metrics, WS dashboard-канал) — форматы live-проверены, добавить автотесты.
2. ~~Slice 4 → Messages real~~ — **сделано** (verified live). ~~Slice 5 → Workflows/Library/WS/audit/metrics~~ — **сделано** (verified live).
3. ~~UI управления задачами~~ — **сделано (F10)**. ~~`transcript.total`~~ — **закрыт backend'ом**.

## 4. Ключевые файлы
| Файл | Назначение |
|---|---|
| `frontend/src/types/api.ts` | mirror контракта (типы) |
| `frontend/src/api/{index,real,config,errors,http}.ts` | фасад, real-адаптер, env, ошибки, fetch-обёртка |
| `frontend/src/api/mock/{adapter,data}.ts` | mock-режим |
| `frontend/src/pages/TeamBuilderPage.tsx` | главный канвас |
| `frontend/src/pages/TasksPage.tsx` | /tasks: create/state/handoff (F10) |
| `frontend/src/components/TeamBuilder/*` | canvas, toolbar, config/bottom panels, palette (contentBounds) |
| `frontend/src/components/Dashboard/*` | панели дашборда |
| `frontend/src/components/ui/States.tsx` | loading/empty/error/**unavailable** states |
| `frontend/src/hooks/{useQuery,useMutation,useWebSocket}.ts` | данные/мутации/WS (честный статус) |
| `frontend/src/lib/task.ts` | task transitions + closure reasons |
| `frontend/vite.config.ts` | dev-сервер + **прокси** `/api`,`/ws`,`/healthz` → BACKEND_URL |
| `frontend/tests/realIntegration.test.ts` | интеграционные тесты (7; автоскип, если нет daemon на :8080) |
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
- ~~Контракт 20: `RelativeSpec.from_role/to_role` → `from`/`to`~~ — **закрыто** (контракт обновлён).
- ~~Контракт 20: добавить `GET /api/v1/workflows`~~ — **закрыто** (backend реализовал, §2.0).
- `docs/architecture/integration.md` — должен написать лид (бекенд ждёт).

## 7. Как восстановить сессию
1. Прочитать этот файл + `_workspace/integration-status.md` (последние 2 записи) + `_workspace/blockers.md`.
2. `cd frontend && npm test` — должен быть 37/37 (интеграционные скипаются без daemon; поднять по п.5).
3. `git log --oneline -3` — последний frontend-коммит (F11 audit-фиксы, 2026-10-08).
4. Дальше — «Следующие шаги» (п.3): интеграционные тесты slice 4–5.
