# Integration status

Таблица синхронизации backend/frontend (обновляют оба агента; источник контракта —
`docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`).

| Функция | Backend | Frontend | Контракт | Интеграция | Проблемы |
|---|---|---|---|---|---|
| Team Builder: teams CRUD | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: segments/roles/relatives CRUD | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: layout (PATCH) | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: topology + validate | done (исправлено лидом) | done (mock+real) | ready | **done** (real) | — |
| Team Builder: role config (GET/PATCH) | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: save (POST /teams/{id}/save) | done | done (mock+real) | ready | **done** (real) | save_to_library — slice 5 |
| Error model (все endpoint) | done | done (flex-parse + validation_failed) | changed | **done** (real) | — |
| Tasks & History | done (slice 2) | done (mock+real, UI /tasks: create/state/handoff) | ready (§3.7) | **done** (real, incl. lifecycle UI) | — |
| Dashboard | done (slice 3: summary+tasks+sessions+alerts) | done (mock+real) | ready | **done** (summary, tasks, sessions, alerts) | metrics — slice 5 |
| Sessions (runtime) | done (slice 3: lifecycle, reaper, watchdog) | done (mock+real: Api.sessions list/create/get/stop, HistoryPage на реальных id) | ready (3.6 добавлен) | **done** (real, e2e: start→stop, crash→failed+alert) | transcript без `total` (контракт требует) |
| Message Center | done (slice 4: messages, chatrooms, event bus; slice 6: unread_count per user) | **done (real, verified live 2026-10-08)** | ready (уточнения в api-decisions) | **done (real)** | mark-read: GET /chatrooms/{id}/messages (DB-ключ с user) |
| WS (real-time) | done (slice 5a: /ws, subscribe channels, EventBus) | **done (real, verified live: 101 Switching Protocols, badge connected)** | ready | **done (real)** | gorilla: read-таймаут «корruptит» соединение (документировано) |
| Workflows (список + редактор) | done (slice 5a: CRUD workflows/blocks/connections) | **done (real, verified live 2026-10-08)** | ready (уточнения в api-decisions) | **done (real)** | connections в POST /workflows — индексы blocks |
| Library | done (slice 5b: save/get/apply team+workflow; 6+: apply role+segment) | **done (real, verified live 2026-10-08)** | ready (role/segment — уточнения в api-decisions) | **done (real)** | — |
| History Viewer (audit + metrics + transcripts) | done (slice 5b: audit log, dashboard/metrics, transcript total; slice 6: user_id/api_key_id) | **done (real, verified live 2026-10-08)** | ready | **done (real)** | llm_tokens = 0 |
| Frontend UI-audit (2026-10-08) | — | **done** | — | **done** | WS-бейдж честный; 404→Unavailable; canvas авто-fit + Fit-кнопка (F11) |

## Integration verification (lead, 2026-10-07)

Team Builder вертикаль проверена **на живом backend** (frontend real API client,
`tests/realIntegration.test.ts`, 4 теста): create team+spec → topology
(snake_case, layout с width/height) → validate → save; role config;
error envelope (404 not_found, 409 conflict); **slice 2: dashboard/summary
(контрактные формы teams/tasks/sessions/alerts) + dashboard/tasks + tasks/{id}/history**.
Все зелёные.

Режим работы frontend: `VITE_API_MODE=real` + `VITE_API_BASE_URL=http://localhost:8080`
→ Team Builder, teams-list, Dashboard (summary+tasks), History (task history)
работают на реальном API; остальные панели/экраны — на mocks или в error-state
(sessions/alerts/metrics — 404 до slice 3).

Backend endpoint slice 2 (дополнение, все под `/api/v1`):
`GET/POST /tasks` · `GET /tasks/{id}` · `PATCH /tasks/{id}/state` ·
`POST /tasks/{id}/handoff` · `GET /tasks/{id}/history` ·
`GET /dashboard/summary` · `GET /dashboard/tasks`

Backend endpoint slice 1 (все под `/api/v1`):
`GET/POST /teams` · `GET/DELETE /teams/{id}` · `GET /teams/{id}/topology` ·
`POST /teams/{id}/validate` · `POST /teams/{id}/save` · `POST /teams/{id}/segments` ·
`POST /teams/{id}/relatives` · `POST /segments/{id}/roles` · `PATCH /segments/{id}/layout` ·
`GET/PATCH /roles/{id}/config` · `PATCH /roles/{id}/layout` · `PATCH /relatives/{id}/layout` ·
`DELETE /relatives/{id}` · `GET /healthz` · `GET /readyz`

## API change log

### 2026-10-08 — lead: F14 — RBAC-интеграционные тесты + role/segment apply (frontend) + контракт
- **Live-прогон slice 6 против демона 127.0.0.1:8080** (WIP backend, `env-live-key` +
  DB-ключи itest-viewer/itest-operator): **55/55** (37 unit + 18 интеграционных).
- Новые интеграционные RBAC-тесты (автоскип без `INTEGRATION_VIEWER_KEY`/
  `INTEGRATION_OPERATOR_KEY`): viewer (GET 200 / POST 403 `forbidden`), operator
  (POST 201 / PATCH role config 403), audit (user_id/api_key_id у записи DB-ключа).
- Frontend: mock `applyLibrary` для role/segment (merge/applied, идемпотентность,
  дубль роли 409 — зеркало backend slice 6); LibraryPage: Apply для **всех** типов
  (role/segment — target-team select); unit: mock applyLibrary расширен, apiConfig, appSmoke.
- **Контракт 20 (лид)**: §5.4 — поведение apply по всем типам (segment/role: target
  обязателен, приоритет сегмента для role, идемпотентность/409); §4.2 — `from_role_name`
  оператора = `"You"` (наблюдение 1 из answer_backend закрыто backend'ом).
- **B3 — решение лида** (в blockers.md): (в) PG out-of-scope до окружения; потом
  миграция `?`→`$N` + e2e на PG-DSN.
- Open (non-blocking): chatroom `last_message` (optional) по-прежнему не возвращается.
- Validation: typecheck OK; 55/55 (два прогона); build OK (82.9 KB gzip).

### 2026-10-08 — lead: F13 хвосты + WS-auth + LibraryPage apply (форматы не меняются)
- **Frontend баг-фикс (критичный для slice 6)**: `getApiConfig()` добавляет `?api_key=`
  (или `&api_key=`) в wsUrl при `VITE_API_KEY` — браузерный WS не шлёт заголовки,
  без этого useWebSocket при auth-демане получал 401. Интеграционные тесты аналогично
  (INTEGRATION_API_KEY → ?api_key=).
- LibraryPage: save c выбором команды (было хардкод source_id:1); Apply в detail-pane
  (team: new/merge; workflow: to team; роль/сегмент — в F14).
- Unit: apiConfig (wsUrl+api_key), appSmoke Library; проверка auth-пути: 52/52 против
  демона с DAEMON_API_KEYS (:8081, REST X-API-Key + WS ?api_key=).

### 2026-10-08 — lead: интеграционные тесты frontend slice 4–5 (автотесты, форматы не меняются)
- `frontend/tests/realIntegration.test.ts`: **7 → 15 тестов** (45/45 всего): +messages
  (direct/broadcast/filters, system→400), +chatrooms (авто-создание, send/list),
  +workflows (CRUD, индексы connections в POST, drag-patch old/new), +library
  (save/409/list/get spec/apply new+merge, save_to_library→library_item_id,
  workflow-apply 400/409), +audit (запись+фильтр), +metrics (12×5, ranges),
  +WS dashboard-канал (subscribe → task.created).
- Фасад (контракт уже предусматривал): `Api.library.applyLibrary` (POST /library/{id}/apply,
  real+mock), `Api.dashboard.getMetrics({range})`. Контракт 20 §3.5: зафиксирован
  `?range=1h|24h|7d`.
- Инфраструктура: тесты принимают `INTEGRATION_BASE_URL` (default :8080) и
  `INTEGRATION_API_KEY` — демон можно делить (lead гонял на :8081, не трогая демон
  backend'а на :8080 с auth).
- Наблюдения для backend (non-blocking, см. answer_backend.md): from_role_name omitempty
  в GET /messages; имя item `"team-<name>"` в save_to_library (уточнение для контракта);
  chatroom last_message не возвращается (optional).
- Написан `docs/architecture/integration.md` (лид-обязанность, blockers B1).
- Validation: typecheck OK; 45/45 (два прогона); production build OK (81.9 KB gzip).

### 2026-10-08 — backend slice 6: Security (RBAC + api_keys + secrets) (ничего не ломается)
- **Auth/RBAC** (ТЗ 06 §2.1): auth включается при `DAEMON_API_KEYS` **или** при api_keys в БД.
  Env-ключи = admin; DB-ключи `sk_...` (CLI `daemon admin keys create --name --role [--user] [--expires]`,
  list/revoke) — привязаны к user + роли (admin/operator/viewer). В БД — sha256-хэш.
  Нет права → **403 `forbidden`** (новый код; error-model.md дополнен). Роли:
  admin (все), operator (без config.update), viewer (read-only).
- **Audit** (20 §6.3): `GET /audit` записей += `user_id`, `api_key_id` (для DB-ключей);
  env-ключ — `user_name: "operator:<4 hex>"` (без user_id), как раньше.
- **unread_count** (20 §4.2): теперь реальный для аутентифицированного user (DB-ключ):
  `chatroom_reads`; `GET /chatrooms/{id}/messages` помечает чат прочитанным (side-effect).
  Без user (env-ключ / auth выключен) — 0.
- **Secrets** (ТЗ 06 §4): `secrets` (AES-256-GCM, `DAEMON_SECRET_KEY`); service-уровень,
  HTTP-эндпоинтов нет (нет в контракте).
- **Frontend impact**: ноль для mock-режима. Real: с включённым auth — ключ обязателен
  (формат как раньше: X-API-Key / Bearer / ?api_key=); новый код `forbidden` (403)
  — flex-парсер обработает как generic. unread_count заполнится, только если
  frontend ходит с DB-ключом (user) — через CLI key.
- Validation: gofmt/vet/test PASS (+TestRBACViewerForbidden, +security_service_test).
  Live 127.0.0.1:8080: viewer GET 200/POST 403, operator 201, PATCH config 403/200 (admin),
  audit user_id/api_key_id, unread 0→1→0.

### 2026-10-08 — frontend: UI-audit (headless, скриншоты) + фиксы F11
- Аудит: real+mock, все 7 страниц. «Не всё грузится» = 404 на slice-5 endpoints (backend
  дошел slice 4–5 в ходе аудита — теперь всё грузится, live-verified: dashboard+metrics,
  history+audit, messages+chatrooms, library, workflows, /ws 101).
- Фиксы: (1) `useWebSocket` — connected только после onopen (бейдж не врал при 404);
  (2) 404 на list-эндпоинтах → нейтральный `Unavailable` («not available yet») вместо
  «Not found + Retry» (dashboard metrics, history audit, library, messages);
  (3) canvas fit-to-view: авто-fit топологии при загрузке + кнопка Fit (`contentBounds()`);
  drag-и/DnD/connect/config/save/validate — verified working (скриншоты).
- Tests: 37/37 (+3 unit contentBounds); typecheck/build OK (81.6 KB gzip).

### 2026-10-08 — backend slice 5b: Library + History Viewer (audit/metrics) (новые endpoints, ничего не ломается)
- **Library** (контракт 20 §5): `GET /library` (type/group/search, `groups`),
  `POST /library` (снапшот spec команды/workflow; `source_id`, unique (type,name) → 409),
  `GET /library/{id}` (item + `spec` + `versions`), `POST /library/{id}/apply`:
  - team: без `target_team_id` → новая команда (`applied` + created_resources.teams);
    с `target_team_id` → merge отсутствующих сегментов/ролей/relatives (`merged` +
    created_resources.segments/roles); `overrides.name` — имя новой команды;
  - workflow: в `target_team_id` (обязателен) → `applied`;
  - role/segment: 400 (следующий шаг);
  - `downloads_count` инкрементится при apply.
- **`POST /teams/{id}/save`**: при `save_to_library: true` — снапшот в библиотеку,
  ответ += `library_item_id` (ранее флаг принимался и игнорировался).
- **Audit log** (контракт 20 §6.3): `GET /audit` (user_id/action/resource/start_time/end_time,
  limit/offset, total). Записывает middleware: успешные POST/PATCH/DELETE →
  `action` (team.create, task.create, task.state_update, session.start/stop, message.send,
  workflow.*, library.*, ...), `resource` (team:1, task:2, ...), user_name ("operator" или
  "operator:<4 hex>" при API-ключе), ip_address, user_agent. user_id/api_key_id — slice 6.
- **Metrics** (контракт 20 §3.5): `GET /dashboard/metrics?range=1h|24h|7d` → 12 точек:
  tasks_created/completed (из queue_tasks/history_status), sessions_active + queue_size
  (snapshots), llm_tokens = 0 (LLM-агентов нет).
- **Transcript** (контракт 20 §6.4): `GET /sessions/{id}/transcript` теперь возвращает
  `total` (известный mismatch с контрактом закрыт).
- Frontend impact: ноль для mock-режима; real client (api.library.*, audit, metrics) —
  форматы совпадают с моками.
- Validation: gofmt/vet/test PASS (+TestLibraryVertical, TestLibraryWorkflowApply,
  TestAuditAndMetrics). Live-проверка 127.0.0.1:8080 — пройдена (save/list/get/apply
  new+merge, save_to_library, audit, metrics created/completed=1).

### 2026-10-08 — backend slice 5a: WS /ws + Workflows (новые endpoints, ничего не ломается)
- `GET /ws` — WebSocket (kонтракт 20 §WebSocket). Subscribe: `{"type":"subscribe","channels":[...]}`
  (повторный заменяет). События: `{type, data, timestamp}` только по подписанным каналам:
  `team:{id}`, `task:{id}`, `session:{id}`, `chatroom:{id}`, `watchdog:{id}` + глобальный
  `dashboard` (каждое событие дублируется туда — DashboardPage фронтенда подписан на него).
  Auth: при включённых API-ключих — `?api_key=` (браузерный WS не шлёт заголовки).
  Типы событий: task.created, task.state_changed, session.started, session.stopped,
  message.sent, alert.created (+data как в контракте).
- Workflows (контракт 20 §2): `GET /workflows?team_id=&state=`, `GET /workflows/{id}`
  (workflow+blocks+connections), `POST /workflows` (± blocks/connections),
  `POST /workflows/{id}/blocks`, `POST /workflows/{id}/connections`,
  `PATCH /workflows/{id}/blocks/{blockId}` (drag&drop, changes.position/config old/new).
  Уточнение: в `POST /workflows` connections `from_block_id`/`to_block_id` — **индексы**
  массива `blocks` (0-based); отдельно создаваемые connections — реальные id блоков.
- Frontend impact: ноль для mock-режима. Real: `useWebSocket` — работает сразу
  (канал dashboard); WorkflowsPage/WorkflowEditorPage — real client уже обращается
  к этим путям (форматы совпадают с моками).
- Validation: gofmt/vet/test PASS (WS-тесты: subscribe/фильтры/auth; workflows:
  service + HTTP-вертикаль). Live-проверка на 127.0.0.1:8080: task.created +
  message.sent (broadcast) доставлены по WS; workflows CRUD — все пути.
- Slice 5b (впереди): Library (save/apply, `POST /teams/{id}/save` library_item_id),
  History Viewer (audit + transcripts), metrics.

### 2026-10-08 — backend slice 4: Message Center (новые endpoints, ничего не ломается)
- `GET/POST /api/v1/messages` — фильтры team_id/queue_task_id/from_role_id/to_role_id/type,
  `total`/`has_more`; delivery: direct (to_role_id обязателен) / broadcast (все роли team)
  / segment (все роли сегмента to_role_id); ответ `{id, status:'sent', delivered_to}`.
- `GET /api/v1/chatrooms` (опц. `?team_id=`), `GET/POST /api/v1/chatrooms/{id}/messages`
  (limit/offset, `has_more`).
- Chatrooms создаются автоматически: при создании команды — team-level (имя команды,
  topic "Whole team"), при создании сегмента — `<segment>-general` (topic "<Segment> channel").
- Sender: в request'ах нет from (контракт) → `from_role_id` NULL = оператор: `from_role_name="You"`,
  `is_mine=true`. Опц. расширение: `from_role_id` в POST-х (валидация по команде).
- `type system|watchdog` — только серверные (API → 400). `unread_count=0` до user-модели (RBAC, slice 6).
- EventBus (in-memory pub/sub): task.created, task.state_changed, session.started,
  session.stopped, message.sent, alert.created (+timestamp). WS-хендлер `/ws` — slice 5.
- Frontend impact: ноль для mock-режима; real client `api.messages.*` уже обращается к этим путям.
- Validation: gofmt/vet/test PASS (service + HTTP-вертикаль); live-проверка на 127.0.0.1:8080.

### 2026-10-08 — backend: ответ на answer_backend.md (slice 3 failing-тест + agents/ + slice-1 контракт)
- **Punkt 1**: fix гонки process-адаптера — crash (exit != 0) теперь всегда →
  `state=failed` + exit_code (+ history metadata); user-stop (SIGTERM) → `stopped`;
  user-stop после crash не затирает `failed`. Live: `sh -c "exit 3"` → `failed`.
- **Punkt 2**: fixture-spec'и `agents/pi-lead.yaml`, `agents/pi-worker.yaml`,
  `agents/pi-reviewer.yaml` (корень проекта). `agent_spec` резолвится с fallback
  `.yaml`/`.yml` (`pi-lead` → `agents/pi-lead.yaml`); `GET /roles/{id}/config`
  для них → `available: true`. I4 (404 на несуществующий spec) не пострадал.
  Live-сессии (process/tmux/pi) можно поднимать.
- **Punkt 3**: всё закрыто — (1) `topology.layout.relatives` = все relatives
  `[{relative_id, from_role_id, to_role_id, path?}]`; (2) PATCH layout-ответы в
  формате контракта 21 §5 (SegmentLayout/position) + фикс previous==new;
  (3) create-ответы: layout не null.
- **Punkt 4**: зафиксировано к slice 5 (WS `/ws`, `GET /workflows`).
- Frontend: можно гнать live slice 3 end-to-end (daemon на 127.0.0.1:8080,
  DAEMON_AGENT_SPECS_DIR=/home/arkalaust/CODE/PROJECTS/team/agents, свежий бинарь).

### 2026-10-08 — backend: интеграционные фиксы I1/I2/I4 (отчёт frontend по скриншоту)
- **I1** `POST /teams/{id}/save`: payload теперь принимает опциональные `segments`/`roles`
  (массивы объектов `{segment?, name}`); неизвестные имена → **400 validation_failed** с
  `details.errors: [{field: "segments[0].name", reason: "unknown segment ... in team ..."}]`.
  Контрактное поведение сохранено: без этих полей — как раньше (200 + validation).
- **I2** `POST /teams/{id}/validate` (и `validation` в ответе save): добавлено поле
  `valid` (алиас `is_valid`); пустая команда (нет сегментов и ролей) →
  `is_valid: false` + error `NOT_EMPTY` (severity error, location type=team).
- **I4** `POST /segments/{id}/roles`: несуществующий `agent_spec` → **404 not_found**
  (`agent_spec <path> not found`). Файл ищется как `<path>` и `SpecsDir/<path>`.
  Создание ролей inline в `POST /teams` (spec) проверку НЕ проходит (логику не меняли).
- **I3** `GET /teams/{id}`: `relatives` в текущем билде уже есть — подтверждено live,
  frontend тестировал старый бинарь. Без изменений.
- Validation: `go test ./...` OK (добавлены регресс-тесты I1/I2/I4: service + HTTP);
  live-проверка всех 4 случаев против `bin/daemon` (rebuild 2026-10-08) — все зелёные.
- Frontend: пере-proгон интеграционных тестов I1–I4 (daemon на 127.0.0.1:8080, свежий бинарь).

### 2026-10-07 — backend slice 3: Sessions & Runtime (новые endpoints, ничего не ломается)
- `POST /api/v1/sessions?team_id={id}` (role_id + command/runtime_type/queue_task_id),
  `GET /sessions` (+`?team_id&role_id&state&active`), `GET /sessions/{id}`,
  `DELETE /sessions/{id}` (stop, идемпотентно), `GET /sessions/{id}/history`,
  `GET /sessions/{id}/transcript?limit=`
- `GET /dashboard/sessions` (активные, team_name/role_name/uptime),
  `GET /dashboard/alerts` (WatchdogAlert по контракту §3.4),
  `GET /watchdog/events`, `POST /watchdog/events/{id}/read`
- Runtime: process (реальный процесс), tmux, pi (pi CLI в tmux + конфиг); container/k8s → 400
- Reaper: exited → stopped/failed + exit_code; watchdog: stale/blocked/drift, дедупликация
- `dashboard/summary`: sessions/alerts теперь реальные (sessions.running = starting+running+idle;
  alerts.warning = severity high)
- Frontend impact: ноль для mock-режима; real client — можно подключать sessions/dashboard
- Validation: `go test ./...` OK; live smoke (create→running→transcript→stop) OK

### 2026-10-07 — lead: slice 2 интеграция (Tasks & Dashboard)
- Найдено и исправлено: `dashboard/summary.alerts` отдавал `total/running/failed`,
  контракт требует `total/critical/warning` → добавлен struct `DashboardAlerts` (backend).
- Фиксация форм task lifecycle (реализовано backend'ом): `POST /tasks` → 201
  `{id, state, status:created}`; `PATCH /tasks/{id}/state` → TaskView (done требует
  closure_reason, иначе 400 validation_failed; из терминального → 409);
  `POST /tasks/{id}/handoff` → `{closed_task_id, new_task_id, status, task}` (повтор → 409);
  `GET /tasks/{id}/history` → `{history, total}` (отсутствие `from_state` в первом entry
  допустимо — omitempty, frontend-тип опциональный).
- Frontend: интеграционный тест slice 2 (summary forms, dashboard/tasks, history).
- Validation: backend `go test ./...` OK; frontend `vitest` 29/29 (4 интеграционных); build OK.

### 2026-10-07 — backend slice 2: Tasks & History (новые endpoints, ничего не ломается)
- `GET/POST /api/v1/tasks`, `GET /tasks/{id}` (task + subtasks),
  `PATCH /tasks/{id}/state`, `POST /tasks/{id}/handoff`, `GET /tasks/{id}/history`
- `GET /api/v1/dashboard/summary` (teams/tasks/sessions=0/alerts=0),
  `GET /api/v1/dashboard/tasks` (active, is_stale>2ч, is_blocked)
- Переходы: pending→in_progress|done|blocked|canceled; in_progress→done|blocked|canceled;
  blocked→pending|in_progress|done|canceled; из терминальных — 409; done требует closure_reason (400 без него)
- handoff: транзакция (закрыть исходную handed_off_to + новая задача у целевой роли)
- Родитель авто-закрывается (no_follow_on), когда все subtasks завершены
- Frontend impact: ноль для mock-режима; real client для задач/dashboard можно переключать
- Validation: `go test ./...` OK (lifecycle, transitions, handoff, dashboard, HTTP)

### 2026-10-07 — lead: интеграционные фиксы backend (Team Builder unblock)
- `GET /teams/{id}/topology`: была сериализация raw-моделей (PascalCase, без
  `roles_count`/`segment_name`/`from_role_name`/`to_role_name`/счётчиков команды).
  Добавлен contract-вью `topologyView` в `internal/api/http/handlers.go`
  (snake_case + поля контракта + segment layout width/height). Тест расширен.
- `LoadAgentSpec` stub: nil-слайсы → пустые срезы (JSON `[]` вместо `null`
  для `plugins`/`skills`/`hooks`/`mcp.servers`/`startup`).
- Frontend: auth заголовок `X-API-Key` (вместо Bearer, решение blockers #9);
  code `validation_failed` в friendly-error map; интеграционные тесты real client.
- Validation: backend `go test ./...` OK; frontend `vitest` 28/28 (вкл. 3 интеграционных); build OK.

### 2026-10-07 — error model (backend)
- Endpoint: все
- Change: единый error envelope `{"error": {code, message, request_id, details?}}`
- Frontend impact: парсинг по `error.code` — реализован (flex-parse)
- Validation: интеграционные тесты frontend против живого backend

### 2026-10-07 — уточнения Team Builder (backend, без смены форматов)
- DELETE team = archive (200, повтор → 409); POST create → 201; layout в config,
  возвращается в create-ответах/topology/PATCH layout
- Spec relatives: `from`/`to` в формате `"segment.role"` (контракт 20 описывает
  `from_role`/`to_role`) — принято как уточнение, контракт 20 нужно обновить (blockers #8)

### 2026-10-07 — lead: тест-раунд slice 3 (Sessions)
- Backend: `gofmt`/`go vet`/`go build` чисто.
- **Failing:** `service.TestSessionFailedProcessAndWatchdog` — после падения
  процесса state = `stopped`, ожидается `failed` (watchdog-сценарий). Бекенду поправить.
- Live (daemon на :8080, сборка бекенда): I1–I4 — все 4 интеграционных бага
  закрыты: save→400+details; validate→`valid:false`+NOT_EMPTY; GetTeam relatives ✓;
  role с несуществующим agent_spec→404 not_found.
- Slice 3 endpoint'ы live: sessions CRUD + history + transcript, dashboard/sessions,
  dashboard/alerts, watchdog/events[+read]. Формы Session/SessionHistory/Alert
  сходятся с контрактом 20 (cpu_percent/memory_bytes — optional, ок).
- **WS `/ws` → 404** (slice 5, как запланировано).
- **Блок live-тестов сессий:** каталог `agents/` (DAEMON_AGENT_SPECS_DIR) не существует —
  создать роль с валидным agent_spec нельзя, сессию не поднять. Нужен каталог с хотя бы
  одним spec для интеграционных проверок.
- Frontend: 29/29 (вкл. 4 интеграционных против живого daemon).

### 2026-10-07 — lead: live e2e slice 3 + переключение Dashboard sessions/alerts & History на real
- Backend `go test ./...` — **все зелёные** (включая ранее failing `TestSessionFailedProcessAndWatchdog` — исправлено).
- Каталог `agents/` с fixture-spec'ами (pi-lead/pi-reviewer/pi-worker) создан — live-сессии гоняются.
- **Live e2e slice 3 (real API client):** create session (role, `sleep 60`) → `state=running`,
  history `starting→running`, transcript форма, dashboard/sessions видит сессию → `DELETE` stop →
  `stopped` (повторный stop идемпотентен 200). Crash-сценарий: `sh -c 'exit 3'` → `state=failed`,
  `exit_code=3`, history `running→failed` (metadata.exit_code=3) + watchdog alert (`requires_action`).
- Frontend: добавлена группа `sessions` в Api-фасад (list/create/get/stop — real + mock).
  HistoryPage берёт реальные id task/session из dashboard (убран хардкод #1).
  Интеграционные тесты: 4 → **6** (slice 3: lifecycle + failed/watchdog). Итого **31/31** (25 unit + 6 integration).
- Slice 1 layout (blockers #8) — **закрыто**: topology `layout.relatives` заполнен; PATCH layout
  возвращает нормальные `SegmentLayout{segment_id, position{x,y,width,height}, collapsed}` и
  `previous_layout != new_layout` (old сохраняется). Create-ответы: layout возвращается, если
  передан в request (live-проверено) — полностью закрыто.
- Контракт 20 (лид): добавлен §3.6 Session lifecycle, §2.0 `GET /workflows`, уточнён `RelativeSpec` (from/to).
- Известный mismatch: `GET /sessions/:id/transcript` без `total` (контракт требует) — backend-фикс.
- `go test`/`tsc`/`vitest`/`build` — все зелёные; production build 78 KB gzip.

### 2026-10-07 — lead: Tasks lifecycle UI (страница /tasks)
- Frontend: новая страница `/tasks` (список с фильтрами team/state, create, inline-переходы
  state по карте переходов, handoff, done требует closure_reason, expandable → история + subtasks).
  Группа `tasks` в Api-фасад (list/create/get/updateState/handoff — real + mock), `lib/task.ts`
  (transitions, closure reasons). Работает в mock и real (backend slice 2 API готов).
- Контракт 20 (лид): §3.7 Task lifecycle (create/state/handoff, транзишены, closure_reason, handoff-semantics).
- Validation: `tsc`/`vitest` (34 теста, вкл. tasks lifecycle mock + 1 интеграционный против живого daemon)
  /`build` — все зелёные; production build 81 KB gzip.

## Next steps (sync)
- Frontend: после коммита slice 6 — финальный свип (55 тестов) и фиксация в статусе;
  при желании — ack subscribe в WS (сейчас задокументировано: event до subscribe теряется).
- Backend: **коммит slice 6** (WIP живёт в рабочем дереве; интеграционные прогоны — по WIP);
  chatroom `last_message` (optional, nice-to-have); Prometheus `/metrics` (по требованию);
  OpenAPI — в конце проекта; PG-миграция (`?`→`$N`) — при появлении PG-окружения
  (лид-решение B3: out-of-scope до этого).
