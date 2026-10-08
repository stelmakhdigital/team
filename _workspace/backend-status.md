# Backend status

## Current phase
implementation (slices 1–6 done: 6 = Security/RBAC; дальше — library apply role/segment, Prometheus /metrics, OpenAPI в конце)

## Implemented
- Go-модуль `daemon` в `backend/` (Go 1.24, stdlib HTTP, slog).
- Storage: SQLite по умолчанию (pure-Go modernc.org/sqlite), PostgreSQL (pgx) через DSN.
  Миграции: `internal/database/migrations/{sqlite,postgres}/0001_init.sql`
  (teams, segments, roles, relatives), schema_migrations, идемпотентно при старте.
- Domain-модель: Team/Segment/Role/Relative + states (models).
- Use cases (service.TeamService): CreateTeam (± spec, transactional), ListTeams,
  GetTeam (с names/counts), ArchiveTeam, CreateSegment/Role/Relative, DeleteRelative,
  UpdateRoleConfig, Update{Segment,Role,Relative}Layout, GetTopology, ValidateTopology
  (ORPHAN_ROLE/NO_OUTGOING/NO_INCOMING/CIRCULAR_DEPENDENCY/ROLE_NO_AGENT_SPEC).
- HTTP API (slice 1, все endpoint контракта §1 Team Builder + layout/topology/validate из 21):
  - `GET/POST /api/v1/teams`, `GET/DELETE /api/v1/teams/{id}`
  - `GET /api/v1/teams/{id}/topology`, `POST /api/v1/teams/{id}/validate`,
    `POST /api/v1/teams/{id}/save`
  - `POST /api/v1/teams/{id}/segments`, `POST /api/v1/teams/{id}/relatives`
  - `POST /api/v1/segments/{id}/roles`
  - `GET/PATCH /api/v1/roles/{id}/config`, `PATCH /api/v1/segments/{id}/layout`,
    `PATCH /api/v1/roles/{id}/layout`, `PATCH /api/v1/relatives/{id}/layout`
  - `DELETE /api/v1/relatives/{id}`
  - `GET /healthz`, `GET /readyz`
- Middleware: X-Request-Id, logging (slog JSON), recovery, auth (X-API-Key **или**
  Authorization: Bearer — формат frontend; включается через `DAEMON_API_KEYS`).
- GET /roles/{id}/config: парсинг agent.yaml (yaml.v3) с диска, пути ограничены
  `DAEMON_AGENT_SPECS_DIR` (default `agents`) + cwd; stub при отсутствии файла.
- Error model: единый envelope, коды validation_failed/unauthorized/not_found/conflict/internal.

### Slice 2 — Tasks & History (завершён 2026-10-07)
- Миграция 0002_tasks (sqlite + postgres): `queue_tasks`, `history_status` (append-only).
- Domain: Task (pending/in_progress/done/blocked/canceled, closure_reason, parent/subtasks),
  HistoryEntry, AllowedTransitions.
- Use cases (service.TaskService): CreateTask (валидация team/roles/parent),
  UpdateTaskState (только валидные переходы; done требует closure_reason; blocked/started/completed
  служебные поля; append history), HandoffTask (транзакция: close handed_off_to + новая задача
  у целевой роли), авто-закрытие родителя (no_follow_on) когда все subtasks завершены,
  GetTask (+subtasks), ListTasks (фильтры team_id/state/destination_role_id + limit/offset/total),
  GetTaskHistory, DashboardSummary (teams/tasks; sessions/alerts = 0 до slice 3),
  DashboardTasks (active + is_stale > 2ч + is_blocked).
- HTTP: `GET/POST /api/v1/tasks`, `GET /tasks/{id}`, `PATCH /tasks/{id}/state`,
  `POST /tasks/{id}/handoff`, `GET /tasks/{id}/history`,
  `GET /api/v1/dashboard/summary`, `GET /api/v1/dashboard/tasks`.
- 409 на переход из терминального состояния и повторный handoff; 400 на done без closure_reason.
- Role address вычисляется сервером: `team:segment.role`.
- Layout хранится в `config.layout` (контракт 21).

### Slice 3 — Sessions & Runtime (завершён 2026-10-07)
- Миграция 0003 (sqlite+postgres): `sessions` (+exit_code, command), `session_history`,
  `watchdog_events` (+severity/is_read/requires_action по контракту 20 §3.4).
- Runtime-адаптеры (`internal/runtime`): `process` (реальный subprocess, SIGTERM,
  exit code, transcript-файл), `tmux` (отдельная tmux-сессия, kill-session),
  `pi` (pi CLI в tmux + сгенерированный конфиг configs/sessions/<id>.yaml).
  `container`/`k8s` → явная 400 (вне сборки, ТЗ 08).
- Session lifecycle: starting → running → stopped/failed; reaper (background loop,
  `DAEMON_SESSION_POLL_SECS` default 5s) — exited процесс → stopped (exit 0) /
  failed (exit != 0, exit_code в БД + history metadata).
- Одна активная сессия на роль (409); сессия с queue_task_id переводит
  pending-задачу в in_progress (history actor=daemon).
- Watchdog (background loop, `DAEMON_WATCHDOG_SCAN_SECS` default 30s):
  stale (in_progress > 2h), blocked (> 1h), drift (failed-сессия);
  дедупликация по непрочитанным алертам того же типа.
- HTTP: `POST/GET /sessions` (+`GET /sessions/{id}`, `DELETE` = stop,
  `GET /sessions/{id}/history`, `GET /sessions/{id}/transcript?limit=`),
  `GET /dashboard/sessions`, `GET /dashboard/alerts`,
  `GET /watchdog/events`, `POST /watchdog/events/{id}/read`.
- Dashboard summary: sessions (total/running/failed) и alerts (total/critical/warning=high)
  считаются по реальным данным (ранее — 0).
- Transcript: stdout+stderr процесса → DAEMON_LOGS_DIR/<id>.log; endpoint возвращает
  последние строки как TranscriptEntry (message_type=system).

### Library apply role/segment (закрыт 2026-10-08, после slice 6)
- `POST /library` type `segment` (снапшот `{name, description, roles[]}`) и `role`
  (снапшот `{segment, name, agent_spec, profile?}`) — раньше 400 «not supported yet».
- Apply segment → target_team_id обязателен, merge-семантика (сегмент + отсутствующие
  роли; существующие — skip, идемпотентно). Apply role → target_team_id обязателен,
  сегмент: overrides.segment_id → overrides.segment (нет → создаётся) → имя из
  снапшота (нет → создаётся) → единственный сегмент → general; дубль роли → 409;
  agent_spec-файл проверяется (I4 → 404). Подробности — api-decisions.
- Test: TestLibraryRoleSegmentApply (HTTP-вертикаль). Live: merged 3 роли; role apply
  → applied (авто-создание сегмента), overrides.segment → created segment.

### Slice 6 — Security: RBAC + api_keys + secrets (завершён 2026-10-08)
- Миграция 0007 (sqlite+postgres): `security_roles`, `permissions`, `role_permissions`,
  `users`, `api_keys` (sha256-хэш), `secrets`, `chatroom_reads` + seed:
  роли admin/operator/viewer, 23 permissions, role_permissions.
- **AuthService** (`service/auth_service.go`): auth включается при env-ключах
  (`DAEMON_API_KEYS`) или при api_keys в БД (Init после миграций). Env-ключ →
  admin без user ("operator:<4 hex>"); DB-ключ `sk_...` → user/role + permissions
  роли (+ доп. из ключа); revoked/expired → 401.
- **RBAC**: `requiredPermission(method,path)` (таблица в `api/http/middleware.go`):
  teams/tasks/sessions/messages/chatrooms/workflows/library/dashboard/audit +
  `config.update` (PATCH /roles/{id}/config); /ws → `dashboard.read`.
  Нет права → **403 `forbidden`** (новый код, error-model.md обновлён).
- **Audit**: `user_id`/`user_name`/`api_key_id` из AuthContext (authMW теперь снаружи
  auditMW); `GET /audit` += `user_id`, `api_key_id` в записи.
- **CLI** `daemon admin keys create|list|revoke` (user создаётся автоматически;
  ключ показывается один раз).
- **unread_count** (контракт §4.2): реальный для user — `chatroom_reads(user_id,
  chatroom_id, last_read_id, last_read_at)`; mark-read side-effect при
  `GET /chatrooms/{id}/messages` (закреплено в api-decisions). Env-ключ/без auth → 0.
- **Secrets** (`service/secrets.go`): `SecretsManager` AES-256-GCM (ключ
  `DAEMON_SECRET_KEY`, 64 hex), Store/Get/List/Delete; HTTP-эндпоинтов нет (нет в контракте).
- Тесты: `security_service_test.go` (env-key admin, disabled, DB-ключи/роли, expired,
  revoked, list; secrets roundtrip; unread flow), `TestRBACViewerForbidden` (HTTP:
  401/403/201/403 config.update/200 admin; audit user_id/api_key_id/user_name).
- Live-проверка 2026-10-08: CLI create/list, viewer GET 200 + POST 403 forbidden,
  operator 201, audit (user_id=1, api_key_id=1, user_name=frontend-live),
  unread_count: 0 → 1 (после send) → 0 (после read). PASS.

### Slice 5b — Library + History Viewer + Metrics (завершён 2026-10-08)
- **Library** (контракт 20 §5): миграция 0006 (library_items, library_versions,
  audit_log); `LibraryService`: List (type/group/search + groups), Save
  (снапшот spec team/workflow; unique (type,name) → 409; version 1.0.0 + versions[]),
  Get (item+spec+versions), Apply: team → новая команда (`applied`) или **merge**
  в существующую (`merged`, отсутствующие сегменты/роли/relatives; существующие
  пропускаются; relative-conflict — skip), workflow → в target_team_id; role/segment →
  400 (следующий шаг). `downloads_count` +1 при apply.
- **`TeamService.MergeSpec`** — merge spec в команду (library apply); chatroom для
  новых сегментов; idempotent по имени.
- **`POST /teams/{id}/save`**: `save_to_library: true` → снапшот `team-<name>` в
  библиотеку + `library_item_id` в ответе (ранее — флаг игнорировался).
- **Audit log** (контракт 20 §6.3): `auditMW` (middleware) — успешные POST/PATCH/DELETE
  → запись (action по (method, path): team.create, task.state_update, session.start/stop,
  message.send, workflow.*, library.*; resource: team:1, task:2; user_name:
  "operator" / "operator:<4 hex>" при API-ключе; ip, user_agent). `GET /audit` с фильтрами.
  user_id/api_key_id — slice 6 (RBAC).
- **Metrics** (контракт 20 §3.5): `GET /dashboard/metrics?range=1h|24h|7d` — 12 точек:
  tasks_created/completed (count по бакетам из queue_tasks/history_status),
  sessions_active/queue_size (snapshots на конец бакета), llm_tokens = 0.
  «Хвост» окна (после truncation) — в последний бакет.
- **Transcript** (6.4): `GET /sessions/{id}/transcript` += `total` (mismatch с контрактом
  из slice 3 закрыт).
- Тесты: TestLibraryVertical (save/list/get/apply new+merge/валидация),
  TestLibraryWorkflowApply (workflow save+apply+downloads), TestAuditAndMetrics
  (audit записи/фильтры; metrics ряды/суммы/400).
- Live-проверка 127.0.0.1:8080: всё выше + metrics created/completed=1 после
  create+done задачи; save_to_library → library_item_id.

### Slice 5a — WebSocket + Workflows (завершён 2026-10-08)
- **WS `/ws`** (`api/http/ws_handler.go`, gorilla/websocket): upgrade → read pump
  (subscribe: `{type, channels}`, повторный заменяет) + write pump (EventBus → фильтр
  по каналам → `{type, data, timestamp}`; ping 30s, write-таймаут 10s).
  Без подписки — ничего не шлём. Auth: `?api_key=` (браузерный WS не шлёт заголовки;
  authMW дополнен query-параметром).
- **EventBus** дополнен routing-каналами: task.created/state_changed → team:{id}+task:{id};
  session.started/stopped → team:{id}+session:{id}; message.sent → team:{id} (+task:{id}/
  chatroom:{id}); alert.created → watchdog:{id}+team:{id}. Глобальный канал `dashboard`
  — каждое событие (DashboardPage фронтенда подписан на него).
- **statusRecorder** (loggingMW) — добавлен `Hijack()` (иначе WS upgrade ломался:
  «response does not implement http.Hijacker»). Ограничение gorilla: после read-таймаута
  соединение «corrupt» (документировано в api-decisions; клиент рекоネクтит).
- **Workflows** (контракт 20 §2): миграция 0005 (sqlite+postgres), models,
  repositories, `service/workflow_service.go`: List (team_id/state), Get (+blocks+conns),
  Create (± blocks/connections; connections в body = индексы blocks), CreateBlock,
  CreateConnection (валидация: блоки из того же workflow), UpdateBlock (drag&drop,
  changes position/config old/new).
  HTTP: 6 endpoint'ов (workflow_handlers.go), wiring в main.go.
- Тесты: `ws_handler_test.go` (TestWSSubscribeAndEvents: событие до subscribe не
  приходит; фильтры по каналам; TestWSAuth), `workflow_service_test.go` (Full/Validation),
  `workflow_handlers_test.go` (HTTP-вертикаль).
- Live-проверка 127.0.0.1:8080: workflows CRUD (создание с блоками, list, get,
  patch block), WS: task.created + message.sent (broadcast) доставлены по каналам
  dashboard+team:1.

### Slice 4 — Message Center (завершён 2026-10-08)
- Миграция 0004 (sqlite+postgres): `chatrooms` (team_id, segment_id?, name, topic),
  `chatroom_messages` (chatroom_id, from_role_id?, body), `messages` (team_id,
  queue_task_id?, from_role_id?, to_role_id?, type, body, metadata, is_read).
- Chatrooms — авто-создание в транзакции команды/сегмента: team-level (имя команды,
  "Whole team") + `<segment>-general` ("<Segment> channel").
  `GET /chatrooms?team_id=`: last_message (имя отправителя, оператор → "You"),
  members_count (роли team/сегмента), unread_count=0 (до user-модели, RBAC slice 6).
  Лента: `GET/POST /chatrooms/{id}/messages` (ASC, limit/offset/has_more).
- Messages: `GET /messages` (фильтры team_id/queue_task_id/from_role_id/to_role_id/type,
  DESC, total/has_more), `POST /messages`: direct (to_role_id обязателен) /
  broadcast (все роли team) / segment (все роли сегмента to_role_id);
  `system|watchdog` — серверные (API → 400). Ответ `{id, status:'sent', delivered_to}`.
  Оператор: from_role_id NULL → from_role_name "You", is_mine=true; опц. `from_role_id`
  в request (валидация: роль принадлежит команде).
- EventBus (`service/eventbus.go`): in-memory pub/sub (non-blocking, buffered 64, nil-safe);
  каналы team:{id}/session:{id}/watchdog:{id}/task:{id}/chatroom:{id}. Эмитят: tasks
  (created/state_changed), sessions (started/stopped), messages (sent), watchdog (alert.created).
  WS-хендлер `/ws` (subscribe channels) — slice 5.
- Handlers: `message_handlers.go` (5 endpoint'ов), роуты в server.go; main.go — wiring.
- Тесты: `message_service_test.go` (авто-чаты, delivery, валидация, фильтры) +
  HTTP-вертикаль `TestMessageCenterVertical`.
- Live-проверка 127.0.0.1:8080: команда → 3 чата (1 team + 2 сегмента), отправка
  в чат (You/is_mine), last_message, direct/broadcast/segment (delivered_to 1/3/2),
  фильтры, 400/404 — всё подтверждено.

### Интеграционные фиксы I1/I2/I4 (завершён 2026-10-08, по отчёту frontend)
- **I1** `POST /teams/{id}/save`: принимается опц. `segments`/`roles` (массив
  `{segment?, name}`); неизвестные имена → 400 validation_failed, `details.errors[]`
  (`field: segments[i].name / roles[i].name`). Без этих полей поведение не изменилось.
- **I2** `POST /teams/{id}/validate` + `validation` в save: новое поле `valid` (алиас
  `is_valid`); пустая команда → error `NOT_EMPTY` (severity error) → is_valid/valid = false.
- **I4** `POST /segments/{id}/roles`: `agent_spec` должен указывать на существующий файл
  (`<path>` или `SpecsDir/<path>`) → иначе 404 not_found. Inline-роли в `POST /teams` (spec)
  проверку не проходят (создание команды не блокируется отсутствующими spec-файлами).
- **I3** `relatives` в `GET /teams/{id}` — уже был в текущем билде, подтверждено live
  (frontend тестировал старый бинарь). Без изменений.
- Тесты: +TestValidateTopologyEmpty, +TestSaveTopologyUnknownNodes, +TestCreateRoleMissingSpec
  (service), +I1/I2/I4-ассерты в HTTP-тесте (save/validate/createRole). Все существующие
  тесты, создающие роли, переведены на реальные spec-файлы (temp SpecsDir).

## API
- Все endpoint slice 1 соответствуют `20_contract_API.md` §1 + `21_team_builder.md`
  (§2–§7, §8). Уточнения зафиксированы в `docs/contracts/api-decisions.md`
  (archive semantics, 201 на создание, layout в config, error envelope — новое).

## Database
- Миграция 0001_init (sqlite + postgres диалекты): 4 таблицы + индексы, FK,
  CHECK-ограничения, UNIQUE-ограничения.
- Timestamps — TEXT (RFC3339), для единообразия диалектов (ADR-002).

## Commands
```bash
export PATH=$PATH:~/sdk/go/bin
cd backend
go test ./...        # unit + API-тесты (sqlite in-memory)
go vet ./...
gofmt -l .
go build -o bin/daemon ./cmd/daemon
DAEMON_DB_DSN=sqlite:./daemon.db ./bin/daemon   # http://localhost:8080
```

## Validation
- `gofmt -l . && go vet ./... && go test ./...` — PASS (2026-10-08, свежий прогон
  после slice 5b: +TestLibraryVertical, +TestLibraryWorkflowApply,
  +TestAuditAndMetrics; регресс-тесты: I1/I2/I4, TestSessionCrashThenUserStop,
  TestSlice1LayoutRegressions, slices 1–5a).
- Live-проверка 2026-10-08 (sвежий bin/daemon, 127.0.0.1:8080,
  DAEMON_AGENT_SPECS_DIR=…/team/agents): crash-сессия → failed ✓; createRole
  `pi-lead` (без .yaml) → 201 + layout ✓; GET /roles/{id}/config available:true ✓;
  layout.relatives = все relatives ✓; PATCH layout previous≠new (контракт-формат) ✓;
  create layout не null ✓; I4 control → 404 ✓.

### Ответ на answer_backend.md (завершён 2026-10-08)
- **Punkt 1** (failed): гонка process-адаптера (reaper раньше Wait) — done-канал
  + stoppedByUs; StopSession смотрит реальный exit code. Crash → failed всегда.
- **Punkt 2** (agents/): `agents/pi-{lead,worker,reviewer}.yaml` в корне проекта;
  fallback резолва `pi-lead` → `pi-lead.yaml` (.yaml/.yml); README: "Agent specs".
- **Punkt 3** (slice-1 контракт): закрыто — (1) layout.relatives = все relatives;
  (2) PATCH-ответы в формате 21 §5 + фикс previous==new (UpdateSegmentLayout);
  (3) layout в config как map → create-ответы без layout:null.
- **Punkt 4**: зафиксировано к slice 5 (WS /ws, GET /workflows).
- `go vet ./...`, `gofmt -l .`, `go build ./...` — чисто.
- Smoke-тест живого daemon (curl): create team → session (running, runtime_ref=PID)
  → dashboard/sessions (team_name/role_name/state) → transcript ("agent-working")
  → stop (stopped) → summary (sessions.total=1) — OK.

## Frontend impact
- Frontend (F1–F8) готов на mocks (typecheck/tests/build OK). Для real mode
  (`VITE_API_MODE=real`) Team Builder-вертикаль теперь полностью покрыта backend'ом.
- Auth: `Authorization: Bearer <VITE_API_KEY>` (или X-API-Key); без ключей — без auth.
- Error format: frontend flex-парсер совместим с envelope. Known mismatch: backend-код
  `validation_failed` vs frontend-ветка `validation` — к Lead.
- Остальные страницы (dashboard/workflows/messages/library/history/WS) — на mocks
  до соответствующих backend-слайсов.

## Blockers
- `docs/architecture/integration.md` отсутствует (Lead) — работаю по agents.md + 20/21.
- OpenAPI spec — договорено с Lead: сгенерировать в конце проекта (весь API).
- См. `_workspace/blockers.md`.

### Postgres-опция: rewriter + migrate (завершён 2026-10-08, B3)
- SQLite остаётся дефолтом; PG — опционально (ADR-002 дополнен).
- `internal/database/pgx_rewriter.go` — драйвер `pgx-rewrite`: pgx stdlib +
  трансляция `?` → `$1..$N` (pgx v5 без встроенного QueryRewriter). Unit-тесты
  (литералы/идентификаторы, multi-плейсхолдеры). `database.Open` для postgres DSN
  использует его — runtime-путь PG проходим.
- `daemon migrate pg --to postgres://... [--from sqlite:<path>] [--force]`
  (`internal/database/migrate_pg.go`, `cmd/daemon/migrate.go`): postgres-миграции
  схемы + копирование всех 25 таблиц (FK-порядок, batch 100, cast'ы timestamptz,
  цель обязана быть пустой без --force). Unit-тесты: buildPGInsertSQL,
  batchInsertSQL, shiftPlaceholders, open/ping driver-пути.
- Ограничение: e2e (реальный PG-сервер) — при появлении окружения; до того
  PG-DSN не используется (всё на sqlite).

### Slice 7: live-метрики сессий + `session.output` (завершён 2026-10-08, редизайн R4)
- `SessionDetail` += опциональные live-поля (контракт 20 §3.6, additive, omit = «--»):
  `model` (pi, из конфига сессии), `context_used_percentage`, `context_total_input_tokens`,
  `context_total_output_tokens`, `log_path` (всегда).
  - context-поля: парсинг JSONL `usage` из transcript-лога (хвост ≤256KB);
    total_input = Σ(input+cache_read), total_output = Σ(output),
    pct = последняя (input+cache_read)/окно*100 (окно = config.context_window, иначе 200000);
    ТUI-вывод pi usage не содержит → omit (честно, не 0).
- WS `session.output` (live-терминал, контракт 20 §4.3): тейлер transcript-лога
  (`internal/service/session_tailer.go`), батчи ≤500ms, только при новых строках,
  каналы session:<id>+team:<id>+dashboard; после stop — тишина.
- Тесты: `session_live_test.go` (8 unit) + `TestSessionOutputEvents` (батчи/каналы/тишина),
  `TestSessionLiveMetricsView`, `TestSessionLiveMetricsNoUsage`.
- Live-проверено (:8080): WS-батчи (2+1+1+1+1), `log_path` в view,
  context-поля из JSONL (pct 30, input 60000, output 300), omit без usage.

## Next step
- Фронтенд R1–R5 **закрыты** (8015c81 R4, 72dc93b R5, 07ff0a1 план) — backend-хвостов по UI нет.
- PG (опционально): rewriter + `daemon migrate pg` реализованы (B3, 2026-10-08);
  e2e-проверка — при появлении PG-окружения (критерий ADR-004 п.3). До того PG-DSN не используется.
- Prometheus `/metrics` (при необходимости; договор с lead'ом).
- OpenAPI-спека — в конце проекта (полный API).

