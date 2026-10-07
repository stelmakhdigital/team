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
| Tasks & History | done (slice 2) | done (mock+real) | ready | **done** (real) | task-lifecycle client (create/state/handoff) — добавить при появлении UI задач |
| Dashboard | done (slice 3: summary+tasks+sessions+alerts) | done (mock+real) | ready | **done** (summary, tasks, sessions, alerts) | metrics — slice 5 |
| Sessions (runtime) | done (slice 3: lifecycle, reaper, watchdog) | done (mock+real: Api.sessions list/create/get/stop, HistoryPage на реальных id) | ready (3.6 добавлен) | **done** (real, e2e: start→stop, crash→failed+alert) | transcript без `total` (контракт требует) |
| Message Center | planned (slice 4) | done (mock) | draft | blocked | slice 4 |
| Workflows / Library / History Viewer / WS | planned (slice 5) | done (mock) | draft (`GET /workflows` задокументирован, 2.0) | blocked | WS, audit, metrics, workflows — slice 5 |

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
