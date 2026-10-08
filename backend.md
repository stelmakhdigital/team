# backend.md — роль бекэндера + статус сессии (снимок для восстановления)

> Снимок сделан бекэнд-агентом, 2026-10-08 (после library apply role/segment, ~00:10+). Файл для
> восстановления работы: дочитай его целиком перед продолжением.
> Свежие «живые» статусы (могут быть обновлены lead/фронтом):
> `_workspace/backend-status.md`, `_workspace/integration-status.md`,
> `_workspace/blockers.md`, `docs/contracts/api-decisions.md`.

## 1. Роль и правила
- Я — **backend-инженер проекта "team"**: Go-демон (daemon) — оркестрация
  мультиагентных команд (team builder, tasks, sessions/runtime, messaging,
  workflows, library, history, security, observe).
- **Зона ответственности**: `backend/**` + `agents/**` (fixture agent-spec'и)
  + `docs/architecture/backend.md`, `docs/contracts/**`, `docs/decisions/**`
  + `_workspace/backend-status.md`, `_workspace/integration-status.md`,
  `_workspace/blockers.md`. `frontend/**` НЕ трогаю.
- **Контракт** (source of truth): `docs/architecture/frontend/20_contract_API.md`
  + `21_team_builder.md`. ТЗ backend: `docs/architecture/backend/00..10*.md`.
  Сводка: `docs/architecture/backend.md`. Решения: `docs/decisions/adr-*.md`,
  `docs/contracts/api-decisions.md`.
- **Связь**: обмены с lead/фронтом — через markdown-файлы в корне
  (`answer_backend.md` → мне; мой ответ — `answer_frontend.md`).
  Статусы — в `_workspace/*.md` (файлы могут переписываться параллельно —
  перечитывать перед правкой!).

## 2. Среда
- Проект: `/home/arkalaust/CODE/PROJECTS/team/`.
- Go: `~/sdk/go/bin/go` (1.26.0; в PATH нет — всегда
  `export PATH=$PATH:~/sdk/go/bin`). Root/sudo нет.
- Модуль: `daemon` (`backend/go.mod`), stdlib `net/http` (Go 1.22+ patterns),
  без веб-фреймворков. БД: SQLite по умолчанию (pure-Go `modernc.org/sqlite`),
  PostgreSQL по DSN. Зависимости: `modernc.org/sqlite`,
  `github.com/jackc/pgx/v5/stdlib`, `gopkg.in/yaml.v3`,
  **`github.com/gorilla/websocket v1.5.3`** (slice 5a, WS).
- Git: репозиторий существует. Коммиты — за пользователем/лидом, я не коммичу.
  На момент снимка: **много незакоммиченных изменений** (slices 2–6; slice 2
  был закоммичен лидом как «Backend slice 2 + lead integration»).

## 3. Что сделано (статус слайсов)
- **Slice 1 — Team Builder**: DONE (CRUD, topology, validate, agent specs,
  I1–I4, failed-гонка fix, layout-контракт).
- **Slice 2 — Tasks & History**: DONE (tasks CRUD/transitions/handoff/history,
  dashboard summary+tasks).
- **Slice 3 — Sessions & Runtime**: DONE (sessions lifecycle, reaper, watchdog,
  adapters process/tmux/pi — container/k8s → 400, transcript,
  dashboard sessions+alerts, watchdog/events).
- **Slice 4 — Messaging**: DONE (2026-10-08)
  - Миграция 0004 (sqlite+postgres): `chatrooms`, `chatroom_messages`, `messages`.
  - Chatrooms авто-создаются в транзакции команды/сегмента: team-level
    (имя команды, "Whole team") + `<segment>-general` ("<Segment> channel").
  - `GET/POST /messages`: direct (to_role_id обязателен) / broadcast (все роли
    team) / segment (все роли сегмента to_role_id); `system|watchdog` —
    серверные (API → 400). Ответ `{id, status:'sent', delivered_to}`.
  - Оператор: `from_role_id` NULL → `from_role_name:"You"`, `is_mine:true`;
    опц. `from_role_id` в body. `unread_count=0` до RBAC; `requires_reply:false`.
  - Chatroom-ленты ASC, `last_message`, `members_count` (роли team/сегмента).
  - `service/eventbus.go` — in-memory pub/sub (non-blocking, buffered 64,
    nil-safe Publish). Эмитят: tasks (created/state_changed), sessions
    (started/stopped), messages (sent), watchdog (alert.created).
  - `api/http/message_handlers.go` (5 endpoint'ов).
- **Slice 5a — WS + Workflows**: DONE (2026-10-08)
  - `GET /ws` (gorilla/websocket): `{"type":"subscribe","channels":[...]}`
    (повторный заменяет; без подписки — тишина) → `{type,data,timestamp}`.
    Каналы: `team:{id}`, `task:{id}`, `session:{id}`, `chatroom:{id}`,
    `watchdog:{id}` + **глобальный `dashboard`** (каждое событие дублируется —
    DashboardPage фронтенда подписана именно на него).
    Chatroom-сообщения дублируются и на `team:{id}`.
    Auth: `?api_key=` (браузерный WS не шлёт заголовки; authMW дополнен query).
    Ping 30s, pong-wait 60s, write-таймаут 10s.
  - EventBus: `newEvent(typ, data, channels...)` — routing-каналы;
    `dashboard` добавляется автоматически в `newEvent`.
  - **`statusRecorder` (loggingMW) — добавлен `Hijack()`** (без него WS upgrade
    падал: «response does not implement http.Hijacker» → 500).
  - **Workflows** (контракт 20 §2): миграция 0005 (workflows, workflow_blocks,
    workflow_connections), `service/workflow_service.go`, 6 endpoint'ов
    (`workflow_handlers.go`). Уточнение (в api-decisions): в `POST /workflows`
    `connections[].from_block_id/to_block_id` = **индексы** (0-based) массива
    `blocks` того же body; в `POST .../connections` — реальные id блоков.
    PATCH block → `changes.position/config {old,new}`.
  - **Питфолл gorilla (важно!)**: после read-таймаута соединение «corrupt»
    — все дальнейшие `ReadMessage` мгновенно ошибаются (documented в
    SetReadDeadline). В тестах WS — один reader-goroutine на всё
    тестирование (`wsCollector` в `ws_handler_test.go`), не читать
    «с таймаутом и потом снова».
- **Slice 5b — Library + History Viewer + Metrics**: DONE (2026-10-08)
  - Миграция 0006: `library_items`, `library_versions`, `audit_log`.
  - Library (`service/library_service.go`): `GET /library` (type/group/search +
    groups), `POST /library` (снапшот spec team/workflow; unique (type,name) →
    409; version 1.0.0 + versions[]), `GET /library/{id}` (item+spec+versions),
    `POST /library/{id}/apply`: team → новая команда (`applied`) или **merge** в
    существующую (`merged`; `TeamService.MergeSpec` — idempotent по имени,
    relative-conflict → skip), workflow → в target_team_id (обязателен),
    role/segment → 400 «not supported yet» (следующий шаг). `downloads_count` +1.
  - `POST /teams/{id}/save` с `save_to_library:true` → снапшот `team-<name>` +
    `library_item_id` в ответе (контракт 21 §9 закрыт).
  - **Audit log** (20 §6.3): `auditMW` (в цепочке mux → audit → auth → logging →
    recovery → requestID): успешные POST/PATCH/DELETE → запись (action по
    (method,path): team.create, task.state_update, session.start/stop,
    message.send, workflow.*, library.*...; resource `team:1`; user_name
    "operator"/"operator:<4hex>"; ip; user_agent). `GET /audit` с фильтрами.
    user_id/api_key_id — slice 6.
  - **Metrics** (20 §3.5): `GET /dashboard/metrics?range=1h|24h|7d` — 12 точек:
    tasks_created/completed (count по бакетам), sessions_active/queue_size
    (snapshots на конец бакета), llm_tokens=0. «Хвост» окна — в последний бакет.
  - **Transcript** (20 §6.4): `GET /sessions/{id}/transcript` += `total`
    (mismatch из slice 3 закрыт; `GetTranscript` возвращает
    `(entries, total, hasMore, err)`).
- **Slice 6 — Security: DONE (2026-10-08)**
  - Миграция 0007 (sqlite+postgres): `security_roles`, `permissions` (23 шт,
    `resource.verb`), `role_permissions`, `users`, `api_keys` (sha256-хэш, revoked,
    expires_at, доп. permissions JSON), `secrets`, `chatroom_reads(user_id,
    chatroom_id, last_read_id, last_read_at)` + seed ролей admin/operator/viewer.
  - `service/auth_service.go` — AuthService: Init (auth включён при env-ключах
    ИЛИ при api_keys в БД), ValidateAPIKey (env → admin "operator:<4hex>", без
    user; DB `sk_...` → user/role + permissions роли + доп.; revoked/expired →
    401), CreateKey/ListKeys/RevokeKey.
  - RBAC: `requiredPermission(method,path)` в `api/http/middleware.go`
    (таблица; /ws → dashboard.read; PATCH /roles/{id}/config → config.update;
    DELETE /sessions → sessions.stop; DELETE /relatives → teams.update;
    **segments/roles/relatives → teams.*** (GET → teams.read, PATCH layout →
    teams.update) — фикс: эти ресурсы НЕ имеют своих permissions, иначе
    operator 403 на layout, viewer 403 на GET role config).
    Нет права → 403 `forbidden` (новый код, в error-model.md). Роли: admin = все,
    operator = все кроме config.update, viewer = только *.read.
  - authMW теперь снаружи auditMW (цепочка: mux → authMW → auditMW → logging →
    recovery → requestID); AuthContext в ctx (`authContext(r.Context())`).
  - Audit: user_id/user_name/api_key_id из AuthContext; AuditEntryView +=
    user_id/api_key_id (omitempty) — `GET /audit`.
  - **CLI** `cmd/daemon/admin.go`: `daemon admin keys create --name=X
    [--role=admin|operator|viewer] [--user=U] [--expires=720h]` (user создаётся
    автоматически, ключ `sk_`+48 hex показывается один раз), `keys list`,
    `keys revoke <id>`.
  - **unread_count** (контракт §4.2): реальный для user (DB-ключ): unread =
    `chatroom_messages.id > last_read_id`; mark-read side-effect при
    `GET /chatrooms/{id}/messages` (закреплено в api-decisions). Без user → 0.
    Сигнатуры: `ListChatrooms(ctx, teamID, userID *int64)`,
    `GetChatroomMessages(ctx, id, limit, offset, userID *int64)`.
  - **Secrets** (`service/secrets.go`): SecretsManager AES-256-GCM (ключ
    `DAEMON_SECRET_KEY` = 64 hex; не задан — выключен). Store/Get/List/Delete;
    HTTP-эндпоинтов НЕТ (нет в контракте).
  - Live: viewer GET 200/POST 403, operator POST 201, PATCH config 403/200,
    audit user_id/api_key_id, unread 0→1→0. PASS.
  - Хвосты (перепроверка 2026-10-08): RBAC-mapping баг (layout/role config) —
    исправлен + регресс-тесты; `GET /messages` от оператора — `from_role_name="You"`
    (наблюдение 1 answer_backend.md); ответ на 4 наблюдения лида —
    answer_frontend.md (last_message — был старый бинарь; WS до-subscribe —
    подтверждено; save_to_library имя team-<name> — задокументировано);
    PG-плейсхолдеры — blocker B3 (нужно решение лида).
- **Library apply role/segment: DONE (2026-10-08, после slice 6)** — закрыт 400
  «not supported yet».
  - `snapshotSpec`: segment → `{name, description, roles:[{name,agent_spec,profile?}]}`;
    role → `{segment, name, agent_spec, profile?}` (LibraryService += `segments`
    segmentLister из stores).
  - `applySegment`: target_team_id обязателен → `teamSvc.MergeSpec` (merge-семантика,
    идемпотентно; chatroom для нового сегмента авто). Status "merged".
  - `applyRole`: target_team_id обязателен; `resolveRoleSegment`: overrides.segment_id
    (belong-чек) → overrides.segment (нет → `segmentByNameOrCreate`) → имя из снапшота
    (нет → создаётся) → единственный сегмент → `general`; несколько сегментов без
    кандидата → 400. Роль — через `teamSvc.CreateRole` (I4 agent_spec → 404, дубль → 409).
    Status "applied".
  - Test: `TestLibraryRoleSegmentApply` (HTTP-вертикаль: снапшоты, merge/идемпотентность,
    overrides.segment, snapshot-сегмент, авто-создание сегментов, 400/409, downloads).
  - Live: segment-apply merged (3 роли), повтор — 0 создано; role-apply в пустую команду
    → applied + создан сегмент; overrides.segment=ops → created segment; без target → 400.

## 4. Как проверить / как поднять
- Tests (один чистый прогон в конце, не зацикливаться):
  ```
  cd backend && export PATH=$PATH:~/sdk/go/bin
  gofmt -l . && go vet ./... && go test -count=1 ./...
  ```
  Последний прогон (после slice 5b): PASS (http + service).
- Build: `go build -o bin/daemon ./cmd/daemon`
- Запуск daemon (перезапуск: сначала `pgrep -x daemon | xargs -r kill`):
  ```
  cd backend && nohup env \
    DAEMON_LISTEN_ADDR=127.0.0.1:8080 \
    DAEMON_DB_DSN=sqlite:/tmp/daemon-slice6.db \
    DAEMON_AGENT_SPECS_DIR=/home/arkalaust/CODE/PROJECTS/team/agents \
    DAEMON_API_KEYS=env-live-key \
    ./bin/daemon > /tmp/daemon-slice6.log 2>&1 &
  ```
- **На момент снимка**: daemon запущен, 127.0.0.1:8080, DB
  `/tmp/daemon-slice6.db`, log `/tmp/daemon-slice6.log`, auth ВКЛЮЧЁН
  (env-ключ `env-live-key` = admin; в БД — DB-ключи operator `frontend-live`
  и viewer `viewer-live`, созданы CLI `./bin/daemon admin keys`).
  PID — `pgrep -x daemon` (**НЕ `pgrep -f bin/daemon`** — ловит свой shell).
  Если не жив — перезапустить по команде выше.
- **Важно**: фронтенд (Vite dev, real-режим) может работать параллельно на
  этом daemon'е и создавать свои команды (наблюдалось: команда
  `IT-1791409677215` с 2 сегментами). Перед live-проверками сверять
  `GET /api/v1/teams` — id ролей/сегментов чужих команд не путать.

## 5. Ключевые места кода
- `cmd/daemon/main.go` — wiring: EventBus (bus), все сервисы, Options
  (Sessions/Alerts/Messages/Workflows/Events/Library/Audit/Metrics).
- `internal/api/http/server.go` — роуты + цепочка middleware
  (mux → auditMW → authMW → loggingMW → recoveryMW → requestIDMW).
- `internal/api/http/ws_handler.go` — WS pump'ы (readPump: subscribe;
  writePump: EventBus → фильтр по каналам → WriteJSON; ping).
- `internal/api/http/audit_middleware.go` — auditMW + `auditAction(method,path)`.
- `internal/api/http/{message,workflow,library}_handlers.go` — endpoint'ы.
- `internal/api/http/handlers.go` — `handlers` struct (svc/tsvc/ssvc/msvc/wsvc/
  lsvc/asvc/msvcM/alerts/events); SaveTopology handler (+save_to_library →
  lsvc.Save, `library_item_id`).
- `internal/api/http/middleware.go` — `authMW` (auth + RBAC),
  `requiredPermission(method,path)`, `authContext(ctx)`/`withAuthContext`,
  `statusRecorder` **с Hijack()** (WS!), `recoveryMW`.
- `internal/service/eventbus.go` — WSEvent{type,data,timestamp,channels(json:"-")},
  `newEvent` (авто-dублирование на `dashboard`).
- `internal/service/{task,session,message,watchdog}_service.go` — точки
  `Bus.Publish(newEvent(..., channels...))`.
- `internal/service/library_service.go` — List/Save/Get/Apply; `snapshotSpec`
  (team: segments/roles/relatives; workflow: blocks+индексы-connections).
- `internal/service/team_service.go` — `MergeSpec` (+`MergeSpecResult`),
  `applySpec`, `SaveTopologyResult.LibraryItemID`.
- `internal/service/workflow_service.go` — CRUD workflows (types
  task|decision|parallel|loop|agent|manual; states draft|active|archived).
- `internal/service/metrics_service.go` — Get (1h/24h/7d, 12 точек, bucketize,
  sessions_active/queue_size snapshots).
- `internal/service/audit_service.go` — Record/List.
- `internal/repository/{message,workflow,library}_repo.go` + `store.go`
  (Stores: ... Messages/Chatrooms/ChatroomMessages/Workflows/WorkflowBlocks/
  WorkflowConnections/Library/LibraryVersions/Audit).
- `internal/models/{message,workflow,library}.go`.
- `internal/service/auth_service.go` — AuthService (env/DB-ключи, роли,
  CreateKey/ListKeys/RevokeKey, AllPermissions).
- `internal/service/secrets.go` — SecretsManager (AES-256-GCM).
- `internal/models/security.go` — User/APIKey/AuthContext/Secret.
- `cmd/daemon/admin.go` — CLI `daemon admin keys create|list|revoke`.
- Миграции: 0001 init, 0002 tasks, 0003 sessions, 0004 messaging,
  0005 workflows, 0006 library_audit, 0007 security (sqlite + postgres,
  TEXT RFC3339 — ADR-002; в 0007 — TIMESTAMPTZ, migrate pg кастит их).
- PG-опция: `internal/database/pgx_rewriter.go` (драйвер `pgx-rewrite`:
  `?` → `$N`, используется в `database.Open` для postgres DSN),
  `internal/database/migrate_pg.go` (`MigratePG` — sqlite → postgres),
  `cmd/daemon/migrate.go` (CLI `daemon migrate pg`).
- Ключевые env: `DAEMON_LISTEN_ADDR`, `DAEMON_DB_DSN`, `DAEMON_API_KEYS`,
  `DAEMON_AGENT_SPECS_DIR`, `DAEMON_SESSION_POLL_SECS`, `DAEMON_WATCHDOG_*`,
  `DAEMON_LOGS_DIR`, `DAEMON_SESSION_CONFIGS_DIR`, **`DAEMON_SECRET_KEY`**
  (slice 6, 64 hex; пусто = secrets выключен).
- Auth (slice 6): X-API-Key / Bearer / ?api_key=; auth включён при env-ключах
  или api_keys в БД; env-ключ = admin; DB-ключ = user+роль; 401 unauthorized,
  403 forbidden (RBAC). Ошибка: `{"error":{code,message,request_id,details}}`;
  коды validation_failed/unauthorized/**forbidden**/not_found/conflict/internal.
- Helpers: `rfc3339` (service), `limitOffset` (default 50, max 200),
  `parseIDParam`, `ptr[T]`, `joinAnd`, `marshalJSON`/`unmarshalConfig`,
  `nowTime/nowStr`, `IsUniqueViolation`.

## 6. Тесты (регресс-якоря)
- Slice 1: `TestSlice1LayoutRegressions`, `TestValidateTopologyEmpty`,
  `TestSaveTopologyUnknownNodes`, `TestCreateRoleMissingSpec`, I1/I2/I4-ассерты.
- Slice 3: `TestSessionFailedProcessAndWatchdog`, `TestSessionCrashThenUserStop`.
- Slice 4: `message_service_test.go` (авто-чаты, delivery, валидация, фильтры),
  `TestMessageCenterVertical` (HTTP).
- Slice 5a: `TestWSSubscribeAndEvents` (событие до subscribe не приходит;
  фильтры по каналам; wsCollector-паттерн!), `TestWSAuth`, `TestWorkflowFull/
  Validation` (service), `TestWorkflowVertical` (HTTP).
- Slice 5b: `TestLibraryVertical` (save/list/get/apply new+merge/409/400/404),
  `TestLibraryWorkflowApply` (+downloads_count), `TestAuditAndMetrics`
  (audit записи/фильтры; metrics 12 точек/суммы/400).
- Slice 6: `security_service_test.go` (TestAuthEnvKeyIsAdmin, DisabledWithoutKeys,
  DBKeysAndRoles, ExpiredAndRevoked, ListKeys, TestSecretsRoundtrip,
  TestChatroomUnreadCount), `TestRBACViewerForbidden` (HTTP: 401/403/201,
  config.update 403/200, audit user_id/api_key_id/user_name).
- PG-опция: `internal/database/{pgx_rewriter,migrate_pg}_test.go`
  (TestRewritePlaceholders, TestBuildPGInsertSQL, TestBatchInsertSQL,
  TestShiftPlaceholders, TestOpenPGDriverPath).
- Setup'ы: `newTestService`/`setupTaskEnv` (service), `newTestServer` (http —
  создаёт ВСЕ сервисы + bus + lsvc/asvc/msvcMetrics + **authSvc** (Init);
  SpecsDir с a.yaml/b.yaml), `newRBACTestServer` (http, только teams/tasks/audit/
  auth). Новые service-тесты — по образцу `setupMessageEnv`/`setupSecurityEnv`.

## 7. Решения (не менять без ADR/договорённости)
- Stdlib HTTP, нет фреймворков (ADR-001). SQLite default / PG по DSN (ADR-002).
  Error envelope + dual auth (ADR-003).
- OpenAPI-спека — генерировать **в конце проекта** (полный API), договор с lead'ом.
- Container/k8s runtime — отложено в ТЗ 08. Один active-session на роль (409).
- WS: publish неблокирующий (буфер 64) — медленный подписчик теряет события;
  источник истины — REST. `dashboard`-канал — глобальный (фронт на нём).
- Library apply: team-merge идемпотентен; role/segment — 400 (честно).
- Metrics: llm_tokens = 0 (LLM-агентов нет). Prometheus `/metrics` — вне
  slices 1–5 (отдельный шаг при необходимости).

## 8. Открытые дела / блокеры
- **Slice 6 — Security: DONE**. Остатки slice'а (опционально): security events
  (ТЗ 06 §3.2 — детекция brute force и т.п.), password-авторизация (password_hash
  есть в users, API логина нет — нет в контракте).
- **Library apply role/segment: DONE.**
- **Postgres-опция (B3): DONE как runtime+инструмент (2026-10-08 вечер)** —
  SQLite по умолчанию, PG опционально: драйвер `pgx-rewrite` (`?` → `$N`,
  pgx v5 без встроенного QueryRewriter) + CLI `daemon migrate pg --to
  postgres://... [--from sqlite:<path>] [--force]` (схема + все 25 таблиц, FK-порядок,
  batch 100, cast'ы timestamptz, пустая цель без --force). Unit-тесты: реврайтер,
  buildPGInsertSQL/batchInsertSQL/shiftPlaceholders, driver open/ping. **E2E — только
  на живом PG (окружения нет); до того PG-DSN не используется.** ADR-002 дополнен.
- **B3: РЕШЁН по варианту «SQLite по умолчанию + опциональная миграция» (2026-10-08):**
  rewriter + `daemon migrate pg` готовы (см. выше); остаток — e2e на живом PG
  (нужно окружение: docker/сервер). Подробности — `_workspace/blockers.md`.
- Library apply для `role`/`segment` (сейчас 400) — snapshotSpec + apply-ветки.
- Prometheus `/metrics` — по требованию (вне slices).
- `docs/architecture/integration.md` — ответственность lead'а, файла нет.
- Коммиты: я не коммичу; перед коммитом lead'а — `git status` (незакоммиченных
  изменений по slices 2–5b много; slice 2 коммит — уже в истории).
- Frontend: всё real (team builder…library/history/WS); unread_count — только
  при DB-ключе (user); новый код 403 `forbidden` (опц. ветка в парсере).

## 9. Питфолы (уроки сессий)
- **`pgrep -f "bin/daemon"` ловит собственный shell** (exit 143) — использовать
  `pgrep -x daemon | xargs -r kill` (имя бинарника = "daemon").
- **gorilla/websocket**: после read-таймаута conn «corrupt» (все reads ошибаются)
  — в тестах один reader; в проде — клиент рекоネクтит (фронт умеет).
- **Hijack**: любой middleware, оборачивающий ResponseWriter, должен
  реализовывать `http.Hijacker` (иначе WS → 500). statusRecorder — реализован.
- **`_workspace/*.md` и `docs/contracts/*.md`** переписывают параллельно —
  **всегда перечитывать перед правкой**. Урок slice 4: я перезаписал
  `api-decisions.md` (write) без полного чтения и потерял 100+ строк
  (незакоммиченных, их не было в git). Восстановил из истории сессий:
  `~/.pi/agent/sessions/--home-arkalaust-CODE-PROJECTS-team--/*.jsonl` —
  там ВСЕ tool-вызовы (write/edit/bash-heredocs) с полными текстами.
  Таймстемпы в jsonl — **UTC** (локальное = +3h). Правило: большие изменения
  доков — только `edit` (точечные замены) или append через python-скрипт;
  полный `write` — только после полного чтения.
- Анти-цикл: «один чистый прогон» в конце; не гонять тесты без изменения кода.
- Имена команд в live-сценариях — уникальные (иначе 409), но фронт может
  создать свою команду в той же БД — сверять `GET /teams`.
- `srv.URL` в httptest — string (Host через `srv.Listener.Addr().String()`).
- gorilla `Dial` возвращает 3 значения (conn, resp, err).
- `specBody()` в http-тестах создаёт команду "dev-team" (уникальное имя!) —
  вторая команда в тесте — с другим именем.
- Frontend шлёт `agent_spec` без расширения — fallback резолва есть, не чинить.
- REST-клиент live-проверок: `curl -s ... | python3 -c "import json,sys; ..."`.

## 10. Формат ответа (чек-лист нового слайса)
1. `gofmt -l .` пусто, `go vet ./...` чисто, `go test -count=1 ./...` PASS.
2. Build `bin/daemon`, live-проверка ключевых путей curl'ем.
3. Обновить: `_workspace/backend-status.md`, `_workspace/integration-status.md`
   (таблица + API change log), `docs/contracts/api-decisions.md`
   (append-секция slice), `backend/README.md` (Endpoints (slice N)),
   `docs/architecture/backend.md` (статус слайса).
4. Отчёт в `answer_frontend.md` (заменить целиком: что готово, endpoints,
   frontend impact, валидация, файлы, следующий шаг).
5. Снять новый снапкет этого файла (backend.md) при смене роли/слайса.
