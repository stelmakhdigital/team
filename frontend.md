# Frontend — роль и статус (точка восстановления сессии)

> Файл для восстановления работы. **Последнее обновление: 2026-10-08, ~12:30 (после F14).**
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

## 2. Что сделано (F1–F14)
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
- **F13** хвосты + auth-ready WS + LibraryPage apply (2026-10-08):
  1) **WS auth (критичный баг)**: `useWebSocket` не шёл `?api_key=` → при включённом auth (slice 6)
     браузерный WS был бы 401. Фикс в `getApiConfig()`: wsUrl += `?api_key=` (или `&api_key=`),
     если задан `VITE_API_KEY`. Интеграционные тесты тоже шлют `?api_key=` (INTEGRATION_API_KEY).
     Проверено: **52/52 против демона с DAEMON_API_KEYS** (REST X-API-Key + WS ?api_key=).
  2) **LibraryPage**: save теперь с выбором команды (было хардкод `source_id: 1`);
     detail-pane: Apply-секция (team: «Apply as new team» + «Merge into <select>»;
     workflow: «Apply to team <select>»; role/segment: hint «not supported (400)»).
  3) Unit: `tests/apiConfig.test.ts` (4: wsUrl+api_key, query-merge, mode), mock `applyLibrary` (6 сценариев),
     appSmoke: Library (save-row + apply-контролы, team + workflow items).
  4) Лид-решение **blockers B3** (PG `?` vs pgx v5 `$N`): **(в) — PG out-of-scope до окружения**;
     при появлении окружения — миграция на `$N` + e2e на PG-DSN. ADR-002 остаётся целью.
- **F14** slice 6 (RBAC) + role/segment-apply (2026-10-08, backend-агент довёл WIP slice 6):
  1) **3 RBAC-интеграционных теста** (`INTEGRATION_VIEWER_KEY`/`INTEGRATION_OPERATOR_KEY`,
     автоскип без ключей): viewer GET 200/POST 403 `forbidden`; operator POST 201 /
     PATCH role config 403; audit запись DB-ключа с user_id/api_key_id.
     Live-прогон против демона backend'а на :8080 (env-live-key + itest-* ключи, созданы через CLI).
  2) **role/segment-apply**: mock `applyLibrary` (merge/applied, идемпотентность, дубль 409),
     LibraryPage — Apply для всех типов (role/segment: target-team select);
     контракт 20 §5.4 обновлён (поведение по всем типам), §4.2 `from_role_name: "You"`.
  Итог: **55/55** (37 unit + 18 интеграционных) против slice-6 демона; typecheck/build OK. ADR-002 остаётся целью.

## 3. Текущий статус (2026-10-08, ~12:30, после F14)
- Git: HEAD = **F14-коммит** (см. `git log --oneline -3`); до него: F13, `83d8c92` (F12), `ba52616` (F11).
- Тесты: **55/55** (37 unit + 18 интеграционных, включая 3 RBAC); прогонялось против
  демона backend'а на :8080 (slice 6 WIP, auth: env-live-key + DB-ключи itest-viewer/itest-operator).
  typecheck OK; build OK (82.9 KB gzip).
- **Backend-агент активен**: WIP **slice 6 (RBAC + api_keys + secrets + role/segment-apply)**
  ещё не закоммичен; его демон на :8080 (DB `/tmp/daemon-slice6.db`, auth ВКЛ). Не убивать.
  Ключи для прогонов: `env-live-key` (admin, env) + DB-ключи (`./bin/daemon admin keys create`);
  мои: itest-viewer/itest-operator (значения — в истории сессии / можно создать новые).
- Контракт 20 актуален: §5.4 apply (все типы), §4.2 ("You"), §3.5 range, §3.6/§3.7 lifecycle.
- `docs/architecture/integration.md` написан; B3 закрыт решением (в).
- Open (non-blocking, backend): chatroom `last_message` (optional) не возвращается.

### Мои следующие шаги
1. После коммита slice 6 — финальный свип (55 тестов) + пометка «done» в статусах.
2. (опционально) OpenAPI: договорено с backend — в конце проекта.
3. (опционально) WS: ack subscribe (у backend) — сейчас задокументировано, что event до subscribe теряется.

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
2. `cd frontend && npm test` — ожидается **55/55** (интеграционные скипаются без daemon;
   RBAC-тесты — без `INTEGRATION_VIEWER_KEY`/`INTEGRATION_OPERATOR_KEY`).
   Запуск против демонов: `INTEGRATION_BASE_URL` (default :8080) + `INTEGRATION_API_KEY` (admin);
   свои прогоны — :8081 (свой демон), :8080 — демон backend'а (slice 6, `env-live-key`).
3. `git log --oneline -3` — HEAD = F14-коммит (или новее, если продолжил).
4. Дальше — «Следующие шаги» (п.3): финальный свип после коммита slice 6.
