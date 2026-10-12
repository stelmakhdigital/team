# API Decisions (v1)

Единый контракт: `docs/architecture/frontend/20_contract_API.md` (source of truth,
см. `agents.md`) + `docs/architecture/frontend/21_team_builder.md`.

Зафиксированные решения backend по контракту:

1. **Базовый URL**: `/api/v1`. Даты — RFC3339 UTC. ID — JSON number (int64).
2. **`DELETE /api/v1/teams/{id}`** — archive (state → `archived`), soft delete.
   Ответ: `200 {"id": n, "status": "archived"}`. Повторный delete — 409 `conflict`.
3. **`DELETE /api/v1/relatives/{id}`** — hard delete (у связи нет soft-delete semantics).
   Ответ: `200 {"id": n, "status": "deleted", "from_role_name": "...", "to_role_name": "..."}`.
4. **Role address** вычисляется сервером: `team_name:segment_name.role_name`
   (контракт: `address` unique `team:segment.role`). Клиент не передаёт address.
5. **Layout** (Team Builder) хранится в `config`-JSON сущности:
   segment: `config.layout = {x, y, width, height, collapsed}`;
   role/relative: `config.layout = {x, y, path?...}`.
   Ответы create/topology возвращают `layout` как в контракте 21.
6. **`POST /api/v1/teams` с `spec`** — создание segments/roles/relatives одной
   транзакцией. Ошибка валидации spec → 400, команда не создаётся.
7. **`POST /api/v1/teams/{id}/validate`** — 200 с `is_valid: false` и списком
   ошибок (валидация — не ошибка HTTP).
8. **Пагинация**: `limit` (default 50, max 200) + `offset`; ответ `total` + `has_more` где задано контрактом.
9. **Error format**: `docs/contracts/error-model.md`.
10. **Auth**: фаза 0 — выключен (локальная разработка). Фаза 1 — ключ принимается
    двумя способами: `X-API-Key: <key>` **или** `Authorization: Bearer <key>`
    (второй — формат frontend, `VITE_API_KEY`).
    Header `X-Request-Id` (или backend генерирует) возвращается в каждом ответе.
11. **Idempotency**: POST create не идемпотентен; повтор с тем же unique-ключом → 409.
    PATCH layout/config идемпотентны.
12. **Статусы**: POST-создание ресурсов → `201 Created`; остальные мутации → `200`;
    `DELETE /teams/{id}` → `200 {"status":"archived"}`.
13. **`GET /api/v1/roles/{id}/config`** (21 §10): `agent_spec` читается daemon'ом
    с диска (yaml), пути ограничены `DAEMON_AGENT_SPECS_DIR` (default `agents`) и cwd
    (защита от traversal); файл не найден → stub `{name, available:false}`.
14. **`POST /api/v1/teams/{id}/save`** (21 §9): обновление name/description (опц.)
    + валидация; `library_item_id` будет возвращаться с slice 5 (Library),
    флаг `save_to_library` сейчас принимается и игнорируется.

Изменения контракта (относительно 20/21): нет — только уточнения.


## Slice 2: Tasks & History (2026-10-07)

- Переходы состояний: `pending → in_progress|done|blocked|canceled`; `in_progress → done|blocked|canceled`; `blocked → pending|in_progress|done|canceled`. Из терминальных (`done`/`canceled`) — 409 conflict.
- `PATCH /tasks/{id}/state` при `to_state=done` требует `closure_reason` из домена контракта → иначе 400 validation_failed.
- `handoff` = транзакция: закрытие исходной задачи (`done`/`handed_off_to`, `closure_target_id`=новая) + создание pending-задачи у целевой роли (`source_role_id`=исходная роль).
- Родительская задача закрывается автоматически (`done`/`no_follow_on`, actor=`daemon`), когда все subtasks в терминальных состояниях.
- `is_stale` = `in_progress` и `updated_at` старше 2 часов; `is_blocked` = state `blocked`.
- Dashboard: `summary` (teams/tasks/sessions=0/alerts=0 до slice 3), `tasks` = active (pending/in_progress/blocked) с team_name/role_name.
- Миграция 0002: `queue_tasks`, `history_status` (append-only). Колонки `project_id`/`session_id` добавятся с соответствующими slice'ами.
- История: `GET /tasks/{id}/history` — все записи, ASC (created, id), limit/offset + total.


## Slice 3: Sessions & Runtime (2026-10-07)

- `POST /api/v1/sessions?team_id={id}` — body: `role_id` (required), `command`/`args`
  (required для process/tmux; для `pi` command по умолчанию `pi`), `runtime_type`
  (process|tmux|pi; container/k8s → 400 "not supported in this build"),
  `queue_task_id?`, `working_dir?`, `config?`. Ответ 201 `{id, state, status:"started"}`.
- Одна активная сессия на роль → повтор 409 conflict.
- Сессия с `queue_task_id` переводит pending-задачу в `in_progress` (history: actor daemon).
- `DELETE /sessions/{id}` = stop (SIGTERM / tmux kill-session); для stopped — 200 (идемпотентно).
- Reaper (период `DAEMON_SESSION_POLL_SECS`, default 5s): exited-процесс → stopped
  (exit 0) / failed (exit != 0, `exit_code` сохраняется).
- Watchdog (период `DAEMON_WATCHDOG_SCAN_SECS`, default 30s):
  - in_progress без обновлений > `DAEMON_WATCHDOG_STALE_SECS` (2h) → `stale` (medium);
  - blocked > `DAEMON_WATCHDOG_BLOCKED_SECS` (1h) → `blocked` (high, requires_action);
  - failed-сессия → `drift` (high, requires_action).
  - Дедупликация: новый алерт не создаётся, если по той же задаче/сессии есть непрочитанный
    алерт того же типа.
- `dashboard/summary`: `sessions.running` = starting+running+idle; `alerts.warning`
  = severity `high` (в домене алертов severity: low/medium/high/critical, "warning" отсутствует).
- Transcript: process/tmux/pi пишут stdout+stderr в `DAEMON_LOGS_DIR/<session-id>.log`;
  `GET /sessions/{id}/transcript?limit=200` возвращает последние строки как
  `TranscriptEntry{message_type:"system", role:"system"}` (контракт 20 §6.4;
  семантика prompt/response для LLM-агентов придёт с интеграцией pi в slice 5+).
- Новые env: `DAEMON_SESSION_POLL_SECS`, `DAEMON_WATCHDOG_ENABLED`, `DAEMON_WATCHDOG_SCAN_SECS`,
  `DAEMON_WATCHDOG_STALE_SECS`, `DAEMON_WATCHDOG_BLOCKED_SECS`, `DAEMON_LOGS_DIR`,
  `DAEMON_SESSION_CONFIGS_DIR`.
- Миграция 0003: `sessions` (+exit_code, command — расширения 00_database.md),
  `session_history`, `watchdog_events` (+severity, is_read, requires_action — по контракту 20 §3.4).


## Интеграционные фиксы I1/I2/I4 (2026-10-08, backend)

По отчёту frontend (реалисты против живого daemon'а):

- **I1** `POST /teams/{id}/save`: body дополнительно принимает опциональные
  `segments` и `roles` — массивы объектов `{segment?: string, name: string}`
  (имена существующих в команде сущностей). Любое неизвестное имя → **400
  validation_failed**, `error.details.errors[] = {field: "segments[i].name" |
  "roles[i].name", reason: "unknown segment/role \"X\" in team \"Y\""}`.
  Без этих полей ответ не меняется (200 + validation) — существующий клиент
  frontend не пострадал.
- **I2** `POST /teams/{id}/validate` (и блок `validation` в ответе save):
  - добавлено поле **`valid`** (алиас `is_valid`, оба всегда синхронизированы);
  - новая базовая проверка **`NOT_EMPTY`**: команда без сегментов и ролей →
    `is_valid/valid = false`, error `{code: "NOT_EMPTY", severity: "error",
    location: {type: "team", id, name}}`.
- **I4** `POST /segments/{id}/roles`: `agent_spec` обязан указывать на
  существующий файл (ищется как `<path>` и `SpecsDir/<path>`,
  `SpecsDir = DAEMON_AGENT_SPECS_DIR`, default `agents`). Отсутствие файла →
  **404 not_found** (`agent_spec <path> not found`). Inline-роли в
  `POST /teams` (spec-создание команды) проверку НЕ проходят — команда
  создаётся, доступность spec проверяется на старте сессии (LoadAgentSpec → stub).
  Path-traversal-защита чтения (LoadAgentSpec) не меняется.
- **I3** `GET /teams/{id}`: `relatives` — уже присутствовал в билде с момента
  slice 1 (frontend тестировал устаревший бинарь). Изменений нет; при
  подключении использовать свежий `bin/daemon`.


## Ответ на report-файл answer_backend.md (2026-10-08, backend)

- **Punkt 1 (failed-состояние)**: найдена и закрыта гонка в process-адаптере —
  reaper мог зафиксировать процесс как `stopped/exit 0`, если `Status()` вызывался
  после смерти процесса, но до того, как фоновый `Wait` записал exit code.
  Теперь: `done`-канал (wait до 300мс) + флаг `stoppedByUs` (SIGTERM от daemon
  ≠ crash: stop → `stopped`, самопадение с exit != 0 → `failed`).
  `StopSession` после смерти процесса тоже смотрит реальный exit code
  (user-stop не затирает crash → `failed` + `exit_code` в history metadata).
- **Punkt 2 (agents/)**: добавлены fixture-spec'и в корне проекта:
  `agents/pi-lead.yaml`, `agents/pi-worker.yaml`, `agents/pi-reviewer.yaml`
  (формат LoadAgentSpec: name/pi_config/resources/startup).
  Дополнительно: `agent_spec` теперь резолвится с fallback по расширениям
  — `pi-lead` → `agents/pi-lead.yaml` (frontend шлёт имена без `.yaml`;
  кандидаты: `<p>`, `<p>.yaml`, `<p>.yml` + то же в `DAEMON_AGENT_SPECS_DIR`).
  README backend — раздел "Agent specs" (как поднять daemon со своим каталогом).
- **Punkt 3 (slice-1 контракт)**: всё закрыто:
  1. `topology.layout.relatives` — теперь ВСЕ relatives
     `[{relative_id, from_role_id, to_role_id, path?}]` (path — omitempty);
  2. `PATCH /segments/{id}/layout` → `previous_layout`/`new_layout` в формате
     контракта 21 §5 `SegmentLayout {segment_id, position{x,y,width,height}, collapsed}`
     (было плоское `{x,y,width,height}`); `PATCH /roles/{id}/layout` →
     `previous_position`/`new_position` `{x,y}`; баг `previous == new`
     (общий указатель в UpdateSegmentLayout) исправлен — previous теперь
     старое значение;
  3. create-ответы: `layout` больше не null — layout в config хранится как map
     (ранее struct → LayoutFromConfig возвращал nil до DB roundtrip).
- **Punkt 4 (slice 5)**: зафиксировано — WS `/ws` (subscribe channels,
  task.created|task.state_changed|session.started|session.stopped|message.sent|
  alert.created + timestamp) и `GET /workflows` (список; Lead обновляет контракт)
  — к slice 5. EventBus (задел) реализован в slice 4 — события уже эмитятся
  сервисами (tasks/sessions/watchdog/messages).
- Validation: `gofmt -l . && go vet ./... && go test ./...` — чисто (включая
  новые регресс-тесты: TestSessionCrashThenUserStop, TestSlice1LayoutRegressions,
  HTTP-ассерты layout-форматов). Live-проверка на 127.0.0.1:8080 (свежий бинарь,
  DAEMON_AGENT_SPECS_DIR=…/team/agents) — все пункты подтверждены.

## Slice 4: Message Center (2026-10-08)

Новые endpoint'ы (все под `/api/v1`), ничего существующего не меняется:

- `GET /messages?team_id=&queue_task_id=&from_role_id=&to_role_id=&type=&limit=&offset=`
  → `{messages[], total, has_more}`. Сортировка: новые раньше.
- `POST /messages` → `{id, status:'sent', delivered_to:[role_id]}`.
  `type`: `direct` (`to_role_id` обязателен) / `broadcast` (все роли команды) /
  `segment` (все роли сегмента `to_role_id`); `system|watchdog` — только сервер (API → 400).
- `GET /chatrooms?team_id=` → `{chatrooms[]}`: `last_message {from_role_name, body,
  created_at}`, `members_count`, `unread_count`.
- `GET /chatrooms/{id}/messages?limit=&offset=` → `{messages[], has_more}` (ASC, chat-order).
- `POST /chatrooms/{id}/messages` → `{id, status:'sent'}`.

Решения:

1. **Sender** — в контракте нет `from` в request'ах → `from_role_id = NULL` = оператор:
   `from_role_name = "You"`, `is_mine = true`. Опциональное расширение: `from_role_id`
   в POST-х (валидация: роль принадлежит команде).
2. **Auto-создание чатов** (контракт не задаёт endpoint создания): team-level чат при
   создании команды (имя команды, topic "Whole team"), `<segment>-general` при создании
   сегмента (topic "<Segment> channel"). В той же транзакции — атомарно.
3. **Delivery** — один message с `to_role_id` (direct) или NULL (broadcast/segment);
   `delivered_to` в ответе — расчётный набор ролей.
4. **`unread_count = 0`** до user-модели (RBAC, slice 6); `requires_reply = false` пока.
5. **`queue_task_id`** — опц. связь с задачей, фильтруется; задача должна быть из той же
   команды (иначе 400).
6. **EventBus** (in-memory pub/sub, задел под WS slice 5): `task.created`,
   `task.state_changed`, `session.started`, `session.stopped`, `message.sent`,
   `alert.created` (+timestamp). WS-хендлер `/ws` — slice 5.

Test: `message_service_test.go` (авто-чаты, direct/broadcast/segment delivery,
валидация, фильтры) + HTTP-вертикаль `TestMessageCenterVertical`.
## Slice 5a: WS /ws + Workflows (2026-10-08)

### WebSocket (контракт 20 §WebSocket)

- `GET /ws` — upgrade → read/write pump'ы. Клиент: `{"type":"subscribe","channels":["team:1",...]}`
  (повторный subscribe заменяет набор; без подписки — ничего не шлём).
- Каналы событий: `team:{id}`, `task:{id}`, `session:{id}`, `chatroom:{id}`,
  `watchdog:{id}` + **глобальный `dashboard`** (каждое событие дублируется туда;
  DashboardPage фронтенда подписан именно на `dashboard`).
- Chatroom-сообщения дублируются и на `team:{id}` (лента активности команды).
- Auth: при включённых API-ключих — `?api_key=...` (браузерный WebSocket не может
  шлать заголовки); для не-браузерных клиентов работают и X-API-Key/Authorization.
- Ограничение gorilla/websocket: после read-таймаута (pong wait 60s без трафика)
  соединение «corrupt» — все дальнейшие reads возвращают ошибку; клиент должен
  пересоединяться (frontend useWebSocket это делает: reconnect/backoff).
- Publish неблокирующий (буфер 64 на подписчика): медленный клиент может потерять
  события; источник истины — REST.

### Workflows (контракт 20 §2)

- `GET /workflows?team_id=&state=` → `{workflows, total}`.
- `GET /workflows/{id}` → `{workflow, blocks, connections}`.
- `POST /workflows` → `{id, name, status:'created'}`. **Уточнение:** в body
  `connections[].from_block_id/to_block_id` — **индексы** (0-based) массива
  `blocks` того же body (блоки создаются в той же транзакции и не имеют глобальных
  id на момент валидации). В `POST /workflows/{id}/connections` — реальные id блоков.
- `POST /workflows/{id}/blocks` → `{id, workflow_id, type, status:'created'}`.
- `POST /workflows/{id}/connections` → `{id, status:'created'}` (блоки должны
  принадлежать workflow → иначе 400).
- `PATCH /workflows/{id}/blocks/{blockId}` → `{id, status:'updated', changes:{position?{old,new}, config?{old,new}}}`.
- Валидация: types task|decision|parallel|loop|agent|manual; state draft|active|archived;
  unique (team_id, name) → 409; archived team → 409.
- WS-событий для workflow пока нет (их нет в контракте).

Slice 5b (впереди): Library (save/apply + `library_item_id` в `POST /teams/{id}/save`),
History Viewer (audit + transcripts), Prometheus `/metrics`.
## Slice 5b: Library + History Viewer (2026-10-08)

### Library (контракт 20 §5)

- `GET /library?type=&group=&search=&limit=&offset=` → `{items, total, groups[]}`.
- `POST /library` → `{id, status:'saved', library_item_id}`. Снапшот spec источника
  (team: segments/roles/relatives с layout'ами; workflow: blocks + connections
  (индексы)). Unique (type, name) → 409.
- `GET /library/{id}` → `{item, spec, versions[]}`.
- `POST /library/{id}/apply`:
  - **team**: без `target_team_id` → новая команда (`status:'applied'`,
    `created_resources.teams`); с `target_team_id` → **merge** отсутствующих
    сегментов/ролей/relatives (`status:'merged'`, `created_resources.segments/roles`).
    `overrides.name` — имя для новой команды.
  - **workflow**: `target_team_id` обязателен → workflow создаётся в этой команде.
  - **role/segment**: пока 400 (не поддержано — следующий шаг).
  - `downloads_count` +1 при каждом apply.
- `POST /teams/{id}/save` с `save_to_library: true` → снапшот команды в библиотеку
  (имя `team-<name>`), ответ += `library_item_id`.

### History Viewer (контракт 20 §6)

- **6.1/6.2** (task/session history) — уже были (slices 2/3).
- **6.3 audit log**: `GET /audit` (фильтры user_id/action/resource/start_time/end_time).
  Пишет middleware для успешных POST/PATCH/DELETE: action (team.create, task.create,
  task.state_update, session.start/stop, message.send, chatroom.message_send,
  workflow.*, library.*, ...), resource (team:1, task:2, ...), user_name
  ("operator" / "operator:<4 hex>"), ip_address, user_agent.
  `user_id`/`api_key_id` — появятся с RBAC (slice 6). `severity` = "info" (пока).
- **6.4 transcripts**: `GET /sessions/{id}/transcript` теперь возвращает `total`
  (контрактный mismatch закрыт).

### Metrics (контракт 20 §3.5)

- `GET /dashboard/metrics?range=1h|24h|7d` → `time_range` + 5 рядов по 12 точек:
  tasks_created, tasks_completed (count по бакетам), sessions_active, queue_size
  (snapshots на конец бакета), llm_tokens = 0 (LLM-агентов в сборке нет).
- События в «хвосте» окна (после truncation) вписываются в последний бакет.
## Slice 6: Security (RBAC + api_keys + secrets) (2026-10-08)

### Auth / RBAC (ТЗ 06 §2.1)

- **Auth включается**, если заданы env-ключи (`DAEMON_API_KEYS`) **или** в БД есть
  `api_keys` (проверка на старте после миграций). Без ключей — без auth (локальная
  разработка, как раньше).
- **Ключ: два уровня**:
  - env-ключи (`DAEMON_API_KEYS`) — **admin**, без user (audit: `user_name =
    "operator:<4 hex>"`, `user_id`/`api_key_id` отсутствуют);
  - ключи из БД (`sk_...`, создаются CLI `daemon admin keys create`) — привязаны к
    `users` + роли; audit получает `user_id`, `user_name`, `api_key_id`.
    В БД хранится только sha256-хэш ключа.
- **Роли** (seed в миграции 0007): `admin` (все permissions), `operator`
  (все кроме `config.update`), `viewer` (только `*.read`). Permissions —
  `resource.verb`: teams/tasks/sessions/messages/chatrooms/workflows/library/
  dashboard/audit + `config.update` (PATCH `/roles/{id}/config`).
- **RBAC**: mapping (method, path) → permission внутри daemon (таблица
  `requiredPermission`). Нет права → **403 `forbidden`** (новый код, см.
  error-model). `/ws` требует `dashboard.read` (auth как раньше: `?api_key=`).
  healthz/readyz без `/api/v1` — без ограничения.
- **CLI** (новый, slice 6):
  - `daemon admin keys create --name=<n> [--role=admin|operator|viewer] [--user=<u>] [--expires=<720h>]`
    (user создаётся автоматически, ключ показывается один раз);
  - `daemon admin keys list`;
  - `daemon admin keys revoke <id>`.
- Ключи из БД работают во всех местах, где раньше работал env-ключ
  (X-API-Key / Authorization: Bearer / ?api_key=).

### unread_count (контракт 20 §4.2)

- `Chatroom.unread_count` теперь **реальный** для аутентифицированного user
  (DB-ключ с user_id): `chatroom_reads(user_id, chatroom_id, last_read_id,
  last_read_at)` (миграция 0007). unread = сообщения с `id > last_read_id`.
- **Mark-read**: `GET /chatrooms/{id}/messages` для аутентифицированного user
  помечает чат прочитанным (side-effect, контрактом не описан — зафиксировано здесь).
- Для env-ключей (без user) и при выключенном auth — `unread_count = 0` (как в slice 4).

### Secrets (ТЗ 06 §4)

- Таблица `secrets` (миграция 0007) + `SecretsManager` (AES-256-GCM, ключ
  `DAEMON_SECRET_KEY` = 64 hex-символа; не задан — manager выключен).
  Store/Get/List/Delete — service-уровень; **HTTP-эндпоинтов нет** (их нет в
  контракте 20) — для хранения секртов сессий/интеграций внутри daemon.
- Вместо CFB из ТЗ 06 — GCM (authenticated encryption); ключ не пишется в БД.

### Миграция 0007 (sqlite + postgres)

`security_roles`, `permissions`, `role_permissions`, `users`, `api_keys`,
`secrets`, `chatroom_reads` + seed ролей/permissions/role_permissions.
Колонки `audit_log.user_id`/`api_key_id` уже были (0006) — заполняются с slice 6.

### Frontend impact

- **Ноль для mock-режима.** Real: если у daemon включён auth — frontend должен
  шлать ключ (X-API-Key, как раньше). Новый код ошибки `forbidden` (403) —
  flex-парсер frontend его обработает как generic (ветки по `code` — опционально).
- `unread_count` заполняется, только если frontend использует DB-ключ с user
  (через CLI); с env-ключом — 0 (безопасный дефолт).

### Slice 6 — дополнение (2026-10-08, перепроверка хвостов)

- **RBAC-mapping**: ресурсы Team Builder без собственного ресурса
  (`segments`/`roles`/`relatives`) мапятся на `teams.*`: GET → `teams.read`,
  PATCH layout → `teams.update` (исправление: ранее генерировались несуществующие
  permissions → 403 у operator/viewer).
- **`GET /messages`**: `from_role_name` для оператора — `"You"` (консистентно с
  chatroom-сообщениями; наблюдение 1 из answer_backend.md).
- **`save_to_library`**: имя снапшота `team-<team name>` (подтверждено по
  наблюдению 2; unique (type,name) → 409 при повторе).
- **WS**: событие, опубликованное до обработки subscribe, теряется (ack нет) —
  подтверждено; источник истины — REST (integration.md §4).

### Library apply для role/segment (2026-10-08, закрыт «not supported yet» из slice 5b)

- **`POST /library` (type segment)**: снапшот сегмента `{name, description, roles:[{name,
  agent_spec, profile?}]}` (роли сегмента на момент снапшота).
- **`POST /library` (type role)**: снапшот роли `{segment, name, agent_spec, profile?}`.
- **`POST /library/{id}/apply` (type segment)**: `target_team_id` обязателен.
  Семантика — **merge** (как team-merge): сегмент и отсутствующие по имени роли
  создаются (`status:'merged'`, `created_resources.segments/roles`), существующие
  пропускаются (идемпотентно). Сегменту создаётся chatroom `<name>-general`.
- **`POST /library/{id}/apply` (type role)**: `target_team_id` обязателен.
  Выбор сегмента (приоритет): `overrides.segment_id` (id, должен belong к команде,
  иначе 404/400) → `overrides.segment` (имя; отсутствует → **создаётся**) →
  имя сегмента из снапшота (существует в команде → туда, нет → создаётся) →
  единственный сегмент команды → (нет сегментов → создаётся `general`).
  Несколько сегментов и ни одного из кандидатов → 400 (уточнить overrides).
  Роль создаётся через обычный createRole: `agent_spec`-файл должен существовать
  (I4 → 404), дубль имени в сегменте → 409. `status:'applied'`.
- `downloads_count` +1 при каждом успешном apply (как раньше).
- Test: `TestLibraryRoleSegmentApply` (HTTP-вертикаль: снапшоты, merge/идемпотентность,
  overrides, segment_id, snapshot-сегмент, авто-создание сегментов, 400/409).

### Slice 7: live-метрики сессий + `session.output` (2026-10-08, редизайн UI R4)

- **`SessionDetail` += опциональные live-поля** (контракт 20 §3.6, additive,
  omit/null = «рантайм не знает» → UI «--»):
  - `model` — только для `runtime_type=pi`: из конфига сессии
    (`configs/sessions/session-<id>.yaml`, поле `model:`); иначе omit.
  - `context_total_input_tokens` — сумма `(input_tokens + cache_read_input_tokens)`
    по всем JSONL-записям с `usage` в transcript-логе (формат pi JSONL:
    `{"message":{"usage":{...}}}` или `{"usage":{...}}`);
  - `context_total_output_tokens` — сумма `output_tokens` по тем же записям;
  - `context_used_percentage` — последняя запись: `(input+cache_read)/окно*100`
    (capped 100); окно = `config.context_window` сессии, иначе 200000
    (документированное допущение — размер контекста модели не спрашивается у рантайма);
  - `log_path` — путь transcript-файла (`logs/sessions/session-<id>.log`), всегда.
  - **Честность**: ТUI-вывод pi (ANSI) usage не содержит → context-поля omit
    (не 0!). Заполняются, когда рантайм пишет JSONL с usage.
  - Парсинг хвоста файла (≤256KB) на каждый запрос списка — ок для масштабов UI.
- **WS `session.output`** (контракт 20 §4.3): тейлер transcript-лога, батчи
  ≤500ms, событие ТОЛЬКО при новых строках (молчание → тишина). Каналы
  `session:<id>` + `team:<id>` + `dashboard`. `lines[] = {ts, text, stream:"stdout"}`
  (stdout+stderr мержены в один лог → stream всегда "stdout"; ts — время батча,
  per-line ts из файла недоступен). Лимиты: 1MB/тик, 500 строк/событие,
  4000 символов/строка (обрезка). Тейлер живёт от start до stopped/failed
  (markSessionState) — после stop событий нет.
- Не ломает existing-клиентов: только новые опциональные поля и новый тип события
  (неподписанные на `session:<id>`/`dashboard` ничего не получат).
- Тесты: `session_live_test.go` (usage-парсинг, model из pi-конфига, window-override,
  cap 100, readNewLogLines: partial/truncate/idle), `TestSessionOutputEvents`
  (батчи/каналы/тишина после stop), `TestSessionLiveMetricsView`,
  `TestSessionLiveMetricsNoUsage`.

### Фикс (2026-10-08): `admin keys create` — существующий user с другой ролью → conflict

- Баг: `INSERT INTO users ... ON CONFLICT (username) DO NOTHING` — второй ключ для
  существующего user'а с другой ролью молча получал роль ПЕРВОГО ключа
  (operator-ключ → viewer-права, 403 на POST /teams).
- Фикс: если user существует и роль отличается → явная ошибка
  `user "X" already has role "Y" (requested "Z") — use another --user or change
  the user's role`; та же роль → ок (ещё один ключ тому же user'у).
- Регресс-тест: `TestCreateKeyExistingUserRole`. Live-проверено (viewer 403,
  operator 201, конфликт-сообщение).

### Slice 7 — дополнение (2026-10-08, DoD slice 7)

- **HTTP-интеграционные тесты** (`internal/api/http/session_live_handlers_test.go`):
  - `TestSessionLiveFieldsHTTP`: JSONL usage → context-поля присутствуют
    (pct ~10, input 20000, output 10); обычный вывод → context-поля **omit (не 0!)**;
    `log_path` всегда; `model` omit для process.
  - `TestSessionOutputWSHTTP`: WS e2e — subscribe `session:{id}`+dashboard →
    `session.output` с `lines[] {ts, text, stream:'stdout'}`; молчание → тишина
    (1.3s без событий); stop → тейлер остановлен (нет событий после).
- **Известное ограничение (зафиксировано, не баг)**: тейлер session.output живёт
  в памяти процесса daemon'а. После рестарта демона live-терминал у «выживших»
  сессий НЕ возобновляется (runtime-процессы при рестарте демона в любом случае
  теряют привязку к реестру daemon'а; transcript-файлы и REST-поля не страдают). Чинить не нужно
  (решение лида, 2026-10-08).
- Контракт 20 §4.3: канал `team:{id}` тоже получает `session.output`
  (суперсет контракта: `session:{id}` + `dashboard` — контракт; `team:{id}` —
  добавлен, лента активности команды).

### DELETE workflows (2026-10-08, R6.4; контракт 20 §2.6–2.8)

- **Эндпоинты** (были только контракт + frontend/mock; backend-срез доделан):
  - `DELETE /api/v1/workflows/{id}/blocks/{blockId}` — удаляет блок и ВСЕ его связи
    (каскад), отвечает `{id, status:'deleted', removed_connections:N}`.
  - `DELETE /api/v1/workflows/{id}/connections/{connectionId}` — `{id, status:'deleted'}`.
  - `DELETE /api/v1/workflows/{id}` — удаляет workflow + все блоки и связи.
  - 404 `not_found` — блок/связь/workflow нет ИЛИ чужеродный (из другого workflow).
- **Явный каскад в транзакции, НЕ FK-cascade**: в `:memory:` sqlite (тесты)
  `PRAGMA foreign_keys` выключен, поэтому полагаться на `ON DELETE CASCADE`
  из миграций нельзя (osиротевшие строки). Порядок: связи → блоки → workflow.
  `WorkflowConnectionRepo.DeleteByBlock` возвращает `RowsAffected` = `removed_connections`.
- **RBAC**: все три DELETE → `workflows.update` (дефолт `requiredPermission` для
  DELETE `workflows.*`), как у существующих POST/PATCH workflow-субресурсов.
- **Репозиторий**: добавлены `WorkflowRepo.Delete`, `WorkflowBlockRepo.Delete`/`DeleteByWorkflow`,
  `WorkflowConnectionRepo.GetByID`/`Delete`/`DeleteByBlock`/`DeleteByWorkflow` (+ `scanWorkflowConnection`).
- **Тесты**: `service` `TestWorkflowDelete` (каскад count=2, 404-пути, чужеродность);
  `http` `TestWorkflowDeleteHTTP` (status + response shape + 404) и `TestWorkflowDeleteRBAC`
  (viewer 403, operator 200). `newRBACTestServer` теперь шьёт `Workflows`.
  Frontend: интеграционный `slice 5c` (real client vs живой daemon) — 18/18 зелёный.

### OpenAPI-спека всего API (2026-10-08, договор «конец проекта»)

- **Источник правды по всему HTTP API — `docs/contracts/openapi.yaml`** (OpenAPI 3.0.3):
  все 57 операций из `server.go` (1-в-1), schemas моделей + единый error envelope
  (`ErrorEnvelope`/`Error` по `error-model.md`), `securitySchemes.apiKeyAuth` (`X-API-Key`)
  применён к `/api/v1/*` (healthz/readyz/ws — `security: []`).
- Схемы запросов/ответов сведены по контракту `docs/architecture/frontend/20_contract_API.md`
  и сверены с view-структурами `internal/service`/`internal/api/http`.
- **Автосверка покрытия** — `scripts/check_openapi.py` (PyYAML, без runtime-зависимостей
  проекта): сравнивает `method path` из `server.go` с `paths` в спеке, падает (exit 1)
  на расхождение. Запуск: `python3 scripts/check_openapi.py`. В финальном состоянии — PASS (57/57).
- Валидность: `python3 -c "import yaml; yaml.safe_load(open('docs/contracts/openapi.yaml'))"`. 

### U4 — Config subsystem / Settings (backend, 2026-10-09)

- **Контракт (source of truth)**: `.pi/pipeline/T-20261009005500/config-contract.md`
  (единый контракт backend+frontend, ориентир — OpenRIG).
- **API**: `GET /api/v1/config` (все ключи по секциям + meta + environment,
  source per key), `PUT /api/v1/config/{key}` (set override),
  `DELETE /api/v1/config/{key}` (reset override, идемпотентно).
  Тип-валидация value по `type` ключа (неверный тип → 400 validation_failed),
  secret/readonly-ключи → 403, unknown key → 404; `needs_restart` — честный
  сигнал (новое значение ≠ текущего эффективного).
- **Layered resolution**: `default < file < env < override`. File-слой —
  опциональный YAML (`DAEMON_CONFIG_FILE`, по умолчанию `daemon.yaml` в cwd);
  override-слой — таблица `config_overrides` (миграция 0008, sqlite+postgres).
  Демон грузит конфиг на старте из env+дефолтов (`config.Load()`). Слои file
  (`DAEMON_CONFIG_FILE`/`daemon.yaml`) и override (БД) читаются **только ConfigService**
  (Settings: просмотр/провенанс/валидация); runtime `config.Load()` их не потребляет,
  поэтому выставленное override **не вступает в силу** даже после рестарта (bootstrap:
  БД ещё нет на момент `Load()`). `requires_restart`/`needs_restart` — маркеры
  restart-ключей; применение override в runtime — follow-up (см. бэклог).
- **Реестр — полный охват**: 20 ключей, 7 секций (server/storage/sessions/
  watchdog/security/logging/workspace; workspace — новый, ключи добавлены в
  `internal/config` + `Load()`). Host-derived дефолты помечены
  `default_is_derived` (workspace.root=cwd, подпапки от root).
- **RBAC**: `config.read` (GET; admin+operator+viewer, новая permission в
  миграции 0008); `config.update` (PUT/DELETE `/api/v1/config`) — **admin-only**
  (осознанно НЕ выдаётся operator: та же permission охраняет
  `PATCH /roles/{id}/config` из slice 6 — выдача operator ломала бы RBAC slice 6;
  фикс-коммит `e9ceed2`).
- **Meta**: project/instance/version/uptime/git_commit/git_branch/started_at;
  `config_hash` = `sha256:` над отсортированным JSON эффективных значений
  всех ключей. Секция `environment` — env-provenance (secret-значения маскируются).
- **Опции**: env-длительности — секунды (существующая конвенция daemon);
  пустое env-значение трактуется как «слоя нет»; неизвестные ключи в
  `daemon.yaml` игнорируются.
- **OpenAPI**: +3 операции (всего 60), schemas ConfigKey/ConfigSection/
  ConfigMeta/ListConfigResponse/SetConfigRequest/SetConfigResponse/
  ResetConfigResponse (+ConfigEnvVar); `scripts/check_openapi.py` — PASS.

## U3-rename (round 2, 2026-10-09): PATCH /segments/{id}, /workflows/{id}, /library/{id}

**Цель** — довести U3 (редактируемость) до «все сущности редактируемы в UI» для
безопасных сущностей: переименование (name + опц. description).

- **Эндпоинты**: `PATCH /api/v1/segments/{id}`, `PATCH /api/v1/workflows/{id}`,
  `PATCH /api/v1/library/{id}` — body `{name (required, non-empty), description?}`;
  response `{id, name, status: "updated"}`; `updated_at` обновляется.
- **Семантика description**: omit → сохраняем текущее значение (не сбрасываем);
  пустая строка → очищаем (явное действие клиента).
- **Ошибки**: 400 (пустое имя), 404 (неизвестный id), 409 (дубликат имени, только
  segment — unique `(team_id, name)`); единый error-envelope.
- **RBAC** (`requiredPermission`, middleware.go):
  - `PATCH /segments/{id}` → `teams.update` (admin+operator; existing segments-branch).
  - `PATCH /workflows/{id}` → `workflows.update` (admin+operator; default `res.update`).
  - `PATCH /library/{id}` → **`library.create`** (admin+operator). В модели permissions
    нет `library.update`; переименование (write) гейтится существующим write-разрешением
    библиотеки `library.create`. (Альтернатива — новая permission `library.update` +
    миграция — сознательно не добавляем: minimal, без смены схемы; admin+operator —
    ожидаемые акторы write, viewer — read-only.)
- **Service**: `TeamService.UpdateSegmentMeta`, `WorkflowService.UpdateWorkflow`,
  `LibraryService.Update`; repo-методы `SegmentRepo.UpdateMeta`, `WorkflowRepo.UpdateName`,
  `LibraryRepo.UpdateName` (ErrNotFound → 404; `IsUniqueViolation` → 409).
- **OpenAPI**: +3 операции (`updateSegmentMeta`, `updateWorkflow`, `updateLibraryItem`),
  общие schemas `RenameRequest`/`RenameResponse`; всего 63 ops; `check_openapi.py` PASS.
- **Тесты**: service (unit: rename/persist/desc-omit/conflict/404/empty) +
  HTTP (`TestRenameEndpointsRBAC`: 200 operator, 403 viewer, 404 unknown, 409 dup).
- **Role/task НЕ переименовываются (read-only, обоснование)**:
  - **role** — имя роли = ключ `agent_spec` и фигурирует во внешних ссылках
    (chatroom-имена, relatives `segment.role`, session role_name, alert source);
    rename ломает связи/историю. Переименование роли требует каскада — out of scope.
  - **task** — `title`/`priority` задаются при create; нет внешних ID-зависимостей, но
    task-объект — единица работы с историей/аудитом; изменение title/priority в полёте
    не предусмотрено контрактом (изменения — через state/handoff). Оставляем read-only.

## P0.1 — Attention view «Что требует меня» (backend, 2026-10-09)

`GET /api/v1/attention?limit=&offset=` — единый операторский срез: всё, что требует
действия человека. Response `{ total, items[] }`; item = `{ kind, id, severity,
title, detail, created_at, suggested_action, link{path,label} }`.

**Источники (реальные данные, без новых миграций):**
- **`task_blocked`** — все задачи `state = 'blocked'`. severity = `high`.
- **`task_escalated`** — незавершённые (pending/in_progress/blocked) задачи с
  `queue_tasks.closure_reason = 'escalation'`. severity = `critical`.
  *Дедуп:* задача, которая одновременно blocked и escalated, эмитится только как
  `task_escalated` (более высокий severity), не дублируется.
  *Решение по детекции:* берём `closure_reason` с самого task (колонка), не парсим
  `history_status` — проще, один query, нет N+1. Если позже нужна детекция по
  комментариям/handoff — `history_status.closure_reason` уже несёт это на запись.
- **`alert_unread`** — watchdog-алерты `is_read = 0` (флаг уже есть — watchdog events
  read). severity по типу: `blocked`→`critical`, `stale`→`high`, остальные→`normal`.

**Оставлены в схеме, но сейчас пусты/отсутствуют (документировано):**
- **`workflow_attention`** — P1.4 (workflow status/attention) ещё не существует; вид
  в enum/схеме, возвращает ничего до реализации P1.4.
- **`session_failed`** — **опущено**: у `sessions` нет модели «непрочитано» (нет
  `is_read`-флага), отличить «свежий failed, требует внимания» от «старый failed»
  невозможно → не вводим, чтобы не засыпать оператор шумом. Добавим вместе с
  read-флагом на сессиях (отдельный бэклог).

**Сортировка:** severity desc (critical > high > normal) → `created_at` asc
(старое требует внимания дольше). Для task — «since» = `blocked_since`/`updated_at`
(escalated), для alert — `created_at` алерта.

**Bounded:** `limit` (default 50, max 200) / `offset` (default 0) поверх уже
отсортированного набора; `total` = полное число элементов (для badge-счётчика).
Внутренняя выборка по источникам капируется (500) как защита.

**RBAC** (`requiredPermission`): `GET /attention` → **`dashboard.read`** (как
dashboard/watchdog) — read-only операторский вид, доступен **viewer** (viewer имеет
все `*.read`). Новая permission не вводим — minimal, без смены схемы.

**Service**: `AttentionService` (new, `internal/service/attention_service.go`) —
агрегирует через существующие `TaskRepo.List` (+новый фильтр `ClosureReason`),
`WatchdogRepo.List` (+новый фильтр `UnreadOnly`), `TeamRepo.GetByID` (team-name в
detail). Repo-изменения аддитивные (nil/false по умолчанию → поведение существующих
вызовов не меняется).

**OpenAPI**: +1 операция (`listAttention`), schemas `ListAttentionResponse`/
`AttentionItem`/`AttentionLink`; всего 64 ops; `check_openapi.py` PASS.

**Тесты**: service (`TestAttentionAggregation`: комбинации источников, dedup,
severity-сортировка, created_at-asc в рамках severity, limit/offset/total, read-
алерт исключён; `TestAttentionEmpty`: total=0 + items=[]) +
HTTP (`TestAttentionEndpoint`: 401 без ключа, 200 admin, >=2 items, limit, **200 viewer**).

## P0.2 — Overdue / дедлайны задач (backend, 2026-10-09)

У задачи появляется честный дедлайн закрытия: вычисляется при переходе в
`in_progress`, виден в UI, просроченные — отдельный bounded-список и в Attention.

**Миграция 0009**: `queue_tasks.closure_required_at` (nullable). Храним как TEXT
(RFC3339Nano, UTC) — единая конвенция всех временных полей `queue_tasks`
(created_at/updated_at/started_at/...), НЕ `timestamptz` (spec-формулировка
адаптирована под существующую конвенцию; модель — `time.Time`). Индекс
`idx_queue_tasks_closure_due (state, closure_required_at)`.

**Правила выставления/пересчёта** (`TaskService.UpdateTaskState`):
- переход **в `in_progress`** (из pending, из blocked, из любого допустимого):
  `closure_required_at = now + tasks.closure_required_secs`. Это и первичное
  выставление, и **пересчёт** при повторном `blocked → in_progress`.
- `in_progress → blocked`: дедлайн **не трогается** (переносится; `is_overdue`
  учитывает и `blocked`).
- `in_progress → done/canceled`: дедлайн **не трогается** (история сохраняет;
  `is_overdue=false`, т.к. state вне (in_progress, blocked)).
- **Handoff-создание**: `HandoffTask` создаёт новую задачу в `pending` (НЕ в
  `in_progress`), поэтому дедлайн при handoff не выставляется — он появится, когда
  новая задача перейдёт в `in_progress` через обычный transition. Отдельного
  пути «handoff → in_progress» в модели нет.

**Config-key** `tasks.closure_required_secs` (duration, default `7200s`/2h,
`requires_restart=false`, editable, env `DAEMON_TASKS_CLOSURE_REQUIRED_SECS`).
- Новый section `tasks` в реестре (config_service.go). `ConfigService.ResolveDuration`
  (новый public) — layered value (default < file < env < override) **на момент
  перехода** (читается в `UpdateTaskState`, а не на старте демона).
- `TaskService.ClosureRequiredFunc` (новый, injectable) — main.go подвязывает к
  `cvc.ResolveDuration`; nil → дефолт 2h (тесты).

**`is_overdue` + `remaining_secs`** (в `TaskView`, и в списке, и в деталке):
- `is_overdue = state in (in_progress, blocked) AND closure_required_at < now`.
- `closure_required_at` — ISO (date-time); `remaining_secs = closure_required_at - now`
  (целое; отрицательное = просрочено). Вычисляются в `viewsFor` (Go, по
  `*time.Time` модели — полная точность), не в SQL.
- Сравнение «просрочено» — в Go (portable): TEXT-RFC3339Nano не сравним
  лексикографически надёжно для динамического `now`.

**`GET /api/v1/tasks/overdue?limit=&offset=`** (default 50, max 200, bounded):
- Возвращает `{tasks: [...с is_overdue, closure_required_at, remaining_secs], total}`
  (`tasks`, а не `items` из spec-примера — для консистентности с `GET /tasks`).
- Сортировка: самое просроченное (ранний дедлайн) — сверху. `total` = полное число.
- Реализация: `TaskService.ListOverdue` — выборка кандидатов
  (in_progress/blocked + `closure_required_at IS NOT NULL`, кап 500), фильтр
  «< now» + сортировка + limit/offset в Go.
- RBAC: `dashboard.read` (operator view, как attention; viewer-доступен).
  (остальные `GET /tasks` — `tasks.read`; только `/overdue` — `dashboard.read`).

**Attention (P0.1) — новый вид `task_overdue`** (severity `high`):
- Источник: in_progress/blocked с `closure_required_at < now`.
- detail — «Team X · просрочено на N»; suggested action — «Открыть задачу»;
  link на деталку.
- **Дедуп**: задача, уже выпущенная как `task_escalated`/`task_blocked`, НЕ
  дублируется как `task_overdue` (избегание повторяющихся строк). Т.е. вид добавляет
  новые строки только для задач, ещё не показанных (прежде всего in_progress с
  просроченным дедлайном). Существующие виды/тесты не сломаны (дополнение).

**`undelivered`** (задачи, чьё уведомление о создании не подтверждено) — **не в
scope этой карточки**: у нас нет модели nudge-доставки (нет «прочитано/доставлено»
для task-created nudge). Отложено с обоснованием (см. blockers/backlog) — появится,
когда будет модель подтверждения доставки.

**OpenAPI**: +1 операция (`listTasksOverdue`) → 65 ops; `Task` schema +`is_overdue`/
`closure_required_at`/`remaining_secs`; `check_openapi.py` PASS (65/65).

**Тесты**: service (`TestOverdueDeadlineSetOnInProgress`: выставление ~now+2h +
не-просрочена; `TestOverdueRecalcOnBlockedToInProgress`: не трогается на blocked,
пересчёт на входе в in_progress; `TestOverdueIsOverdueLogic`: in_progress/blocked +
past = true, done = false, закрытие не сбрасывает; `TestListOverdue`: пусто/с
данными/сортировка/limit) + HTTP (`TestTasksOverdueEndpoint`: 401/пусто/с данными/
limit/поля/is_overdue в деталке/**viewer 200**; `TestAttentionTaskOverdue`:
kind=task_overdue, severity=high, link).

## P0.3 — Block with wake (backend, 2026-10-09)

Заблокированная задача не «сидит молча»: у блока есть причина-триггер (**wake**)
и текст «как продолжить»; разблокировка — по срабатыванию wake или ручным resolve
с записью решения в историю.

**Миграция 0010**: `task_wakes` (id, task_id FK→queue_tasks ON DELETE CASCADE,
kind, ref, continuation, created_at, fired_at nullable). TIMESTAMP — TEXT
(RFC3339Nano, UTC), единая конвенция. Индексы: `(task_id)`, `(kind, fired_at)`
(арм. timer-wakes для watchdog), `(ref)` (task-wakes по referenced).

**`kind`**: `timer` | `task`. Вид **`watchdog` — запланирован (P1.6), НЕ
реализуется** в этой карточке: остаётся в схеме-документации, в коде пока только
timer|task (валидация отклоняет `watchdog` → 400). `ref` — единая колонка с
зависимым смыслом: **kind=task → task_id** (string) referenced-задачи;
**kind=timer → fire-at** (RFC3339Nano, = now + `after`), чтобы watchdog-проверка
была `ref < now`. `after` (duration, e.g. `30m`) — только вход; хранится fire-at.

**`fired_at` = «закрытие» wake** (fired ИЛИ resolve). Идемпотентность: fired wake
не срабатывает повторно (`MarkFired` = `UPDATE ... WHERE fired_at IS NULL`;
`fireWakeTx` — no-op, если задача уже не blocked / wake уже fired).

**Создание wake** (`TaskService.UpdateTaskState`, только при переходе **в `blocked`**):
- `wake` опциональный (без wake — как раньше, совместимость; в UI encouraged).
- Валидация → **400**: неверный kind; `timer` без валидного `after`; `task` без
  `task_id`. Продолжение `continuation` — string, «как продолжить».
- Если у задачи уже есть арм. wake — закрываем (идемпотентность re-block).

**Разблокировка (wake → blocked → pending + системный комментарий в history):**
- **timer**: watchdog-loop (`WatchdogService.Scan` → `FireTimerWakes` →
  `TaskService.FireArmedTimerWakes`): арм. timer-wakes с `ref < now` →
  `blocked → pending` + history «wake timer fired» + `task.state_changed`.
  Выбран существующий loop-механизм (watchdog, `ScanInterval` 30s).
- **task**: когда referenced-задача transition в `done`/`canceled`
  (`UpdateTaskState` И `HandoffTask`) → арм. task-wakes с `ref=<closed_id>` →
  `blocked → pending` + history «wake task #N closed». Срабатывание внутри той же
  tx закрытия (атомарно); publish — после commit.
- **ручной resolve**: `POST /api/v1/tasks/{id}/resolve` (body `decision` string,
  обязателен) → `blocked → pending` + history `type=resolve, decision` +
  `task.state_changed` (существующий EventBus, канал `team:<id>`). 409 на не-blocked,
  400 без decision. RBAC: `tasks.update` (admin/operator; viewer → 403).

**`GET /tasks/{id}`**: поле `wake` (самая свежая wake задачи): `{
kind, ref, continuation, fired, fired_at, remaining_secs (timer only = fire_at - now) }`.

**OpenAPI**: +1 op (`resolveTask`) → 66 ops; `GetTaskResponse` +`wake`;
`UpdateTaskStateRequest` +`wake`; схемы `TaskWake`/`TaskWakeSpec`;
`check_openapi.py` PASS (66/66).

**Тесты**: service (`TestWakeCreatedOnBlockTimer/Task`, `TestWakeTimerFires`,
`TestWakeTaskTriggerOnClose`, `TestWakeIdempotency`, `TestResolveTask` 200/409/400 +
wake закрыт, `TestBlockWakeValidation`) + HTTP (`TestResolveTaskEndpoint`
401/400/200/409/403-viewer; `TestBlockWithWakeEndpoint` timer/task/400/без-wake).

## P1.0 — Projects / мультипроектность (backend, 2026-10-09)

Сущность «проект» (OpenRIG work tree): реестр рабочих директорий, привязка к
командам/задачам/сессиям, автозаполнение `working_dir` при запуске сессии,
фильтры. «Где работать» — **свойство работы**, а не только параметр запуска.

### Хранилище (миграция 0011)
- Таблица `projects(id, name UNIQUE, path UNIQUE, kind CHECK('existing','new'),
  git, description, archived, created_at, updated_at)`. Timestamp — TEXT
  (RFC3339Nano), bool — INTEGER (0/1) — конвенции.
- Nullable-привязки (миграционная совместимость: команды/задачи/сессии без
  `project_id` работают как раньше):
  - `teams.project_id` — привязка команды к проекту;
  - `queue_tasks.project_id` — **свойство работы** задачи;
  - `sessions.project_id` — результат разрешения `working_dir`.
- SQLite: FK в `ALTER TABLE ADD COLUMN` не поддерживается → колонки без inline-FK
  (связь логическая, на уровне сервиса); postgres — с `REFERENCES ... ON DELETE SET NULL`.

### RBAC
- `projects.read` — viewer (GET); `projects.create` / `projects.update` — operator+
  (POST + DELETE=archive + PATCH). Route→perm через общий механизм `<res>.<verb>`.

### Создание проекта (service.CreateProject)
- `kind=existing`: `path` обязан существовать, быть каталогом и **writable**
  (иначе 400 с detail); `git` определяется (`.git` есть).
- `kind=new`: `parent_path` обязан существовать (иначе 400); target = `parent/name`;
  если target существует → 409; `git init` (если git доступен, иначе `git=false` +
  `warning` в ответе); README-заготовка (если каталог пуст).
- Дубликаты `name`/`path` → 409.

### Привязки + наследование
- `POST /teams` + `project_id` (опционально): проект обязан существовать (404) и
  быть не архивным (409). **Ре-ассайнмент команды** не поддерживается (PATCH команды
  — только name/description через rename) — осознанно: команда без проекта работает
  как раньше, смена проекта = новая команда (задокументировано в TЗ).
- `POST /tasks` + `project_id` (override): иначе наследуется от `team.project_id`
  (наличие проекта проверяется, отсутствующий → nil, т.е. без привязки).
- **Handoff**: новая задача наследует `project_id` от исходной.

### Разрешение working_dir (layered)
`session.working_dir` (явный) > `task.project` (через `queue_task_id`) >
`team.project` > (explicit/empty).

- Если проект нигде не задан (нет explicit, нет у task, нет у team) →
  `working_dir` = явный (или пустой) — **backward compatibility, без 400**
  (правило «сессии/команды/задачи без project_id работают как раньше»).
- **400** — референсированный проект (explicit `project_id`) не существует.
- **409** — проект archived (через explicit/task/team).
- `POST /sessions` + `project_id` (явный проект, опционально). Ответ/модель сессии
  получает `project_id`.
- Реализация — общая функция `resolveWorkingDir` (используется и ProjectService, и
  SessionService); вызывается в `CreateSession` **до** tx.

> **Примечание (reconciliation TЗ).** TЗ одновременно требует «400, если нигде не
> задан» и «команды/задачи/сессии без project_id работают как раньше». Чистый
> «absence → 400» сломал бы существующие сессии (команды без проектов + без
> working_dir). Поэтому 400 применён только к **референсированному, но отсутствующему**
> проекту; чистое отсутствие проекта сохраняет старое поведение (пустой working_dir).
> Это задокументировано как осознанное решение.

### Ограничения
- **`path` immutable**: `PATCH /projects/{id}` принимает только `name`/`description`;
  наличие `path` в body → 409 с обоснованием (смена пути = новые связи, требует
  миграции — не делаем).
- **DELETE = archive** (не удалить): идемпотентно (`archived=true`). Архивный проект:
  нельзя использовать для новых команд (409) / сессий (409); существующие связи
  сохраняются (nullable FK `ON DELETE SET NULL` в postgres).

### Сводка
- `GET /projects/{id}` — project + `counts{teams, tasks, active_sessions}`.
- `GET /projects?archived=true|include_archived=true|name=`.
- **P4 (2026-10-12) Artifacts**: `GET /projects/{id}/files[?path=<rel-dir>]` — список записей
  (файлы + каталоги) **на один уровень** внутри `project.path` или подкаталога (read-only;
  каталоги первыми, затем по имени; пропуск `.git`/`node_modules`/`__pycache__`; лимит
  1000 записей → `truncated=true`). Ответ: `{project_id, root, path, files[], truncated}`,
  где `path` — подкаталог списка ("" = корень). Дерево строит UI по раскрытию (lazy).
  `GET /projects/{id}/files/{rel...}` — содержимое файла (download, `Content-Disposition:
  attachment`; traversal-защита: путь только внутри корня, иначе 400; лимит 10 MB → 400).
  RBAC — `projects.read` (как прочие GET проекта).
- **P4 (2026-10-12) Agent specs**: `GET /agentspecs` — `{specs: string[]}` (верхний уровень
  `DAEMON_AGENT_SPECS_DIR`, имена .yaml/.yml без расширения; пусто при отсутствии каталога).
  Гейт — `teams.read` (Team Builder: автодополнение в форме роли).
- Фильтры: `GET /tasks?project_id=`, `GET /sessions?project_id=`.

### Тесты
- service (`project_service_test.go`): `TestProjectCreateExisting` (success/git/400
  no-path/400 file/409 dup-path/409 dup-name), `TestProjectCreateNew` (success/README/
  git/400 no-parent/409 exists/git-unavailable warning), `TestProjectArchive`
  (archive/idempotent/409 update/409 team-in-archived), `TestTaskProjectInheritance`
  (наследование/override/handoff), `TestResolveWorkingDir` (3 уровня + absence без 400
  + 400 missing), `TestResolveWorkingDirArchived` (409), `TestProjectSummaryCounts`,
  `TestProjectListFilter`.
- HTTP (`project_handlers_test.go`): `TestProjectsCRUDEndpoint` (401/201/400/200/404/
  patch 200/409/DELETE+идемпотентность), `TestProjectsRBAC` (viewer read-OK + create-403,
  operator create-201), `TestTaskFilterByProject` (наследование + фильтр).

## P1.4. Workflow engine: close-step → next task (T-20261009214429)

Исполняемый воркфлоу: каждый шаг = задача; закрытие задачи проецирует следующий шаг
(в том же tx). Engine opt-in (`engine_enabled`, off по умолчанию).

### Эндпоинты (7, tag `workflows`)
- `POST /workflows/{id}/run` (body `{root_objective?}`) → `201 {instance}`.
  `400` no/multiple entry blocks; `409` engine не включён / team не active.
- `GET /workflows/{id}/instances` → `{instances[], total}` (новые сверху).
- `GET /workflow-instances/{iid}` → `{instance, steps[], active_step_ids[]}`.
- `GET /workflow-instances/{iid}/trace` → `{steps[], total}` (ASC, append-only trail).
- `POST /workflow-instances/{iid}/resume` → `200 {instance}`; `409` не failed / нет failed-шага.
- `POST /workflow-instances/{iid}/route` (body `{to_role_id}`) → `200 {instance}`;
  `409` не running / нет active-шага с задачей.
- `POST /workflow-instances/{iid}/abort` → `200 {instance}` (active задачи → canceled);
  `409` уже завершён.

### Модель
- `workflow_instances`: state `running|waiting|completed|failed|aborted`.
- `workflow_steps`: state `pending|active|done|failed|skipped|routed`, exit
  `handoff|waiting|done|failed`. Append-only (UPDATE только состояния).
- `PATCH /workflows/{id}`: `engine_enabled` в `UpdateWorkflowRequest` (name обязателен).

### Решения
- **Projection в tx**: `ProjectStepClose` вызывается из `TaskService` (UpdateTaskState/
  HandoffTask/ResolveTask/fireWake) в том же tx — атомарность: если прожекция упала,
  состояние задачи откатывается. Idempotent: активный шаг по задаче уже закрыт → no-op.
- **Без новых RBAC-перм**: GET instances → `workflows.read`; POST run/resume/route/abort →
  `workflows.update`.
- **mapping closure_reason → exit_kind** (при закрытии задачи шага):
  `handed_off_to` → `handoff`; `no_follow_on` → `done`; `escalation` → `failed`
  (инстанс → failed, запись в trail); `blocked_on` → `waiting` (инстанс → waiting,
  работает P0.3 wake: unblock → projection в tx); `canceled` → `failed`. Route →
  шаг `routed` (актор обновляется, запись в trail).
- **Attention**: 5-й источник `workflow_attention` (failed → critical, waiting → high),
  link → `/workflows/{wf}/instances/{iid}`.
- **Loop**: body-задача на итерацию, cap=50; **agent/task без role_id** → skipped;
  **decision** — ветка по closure-comment ↔ connection.condition.

### Тесты
- service (`workflow_engine_service_test.go`, 12): run+chain, decision, parallel, loop,
  agent-skipped, waiting, failed, resume, route, abort, idempotent projection,
  transactionality, run-validation, compat-old-workflow.
- HTTP (`workflow_engine_handlers_test.go`): `TestWorkflowEngineVertical` (run/list/get/
  trace/route/abort + 409), `TestWorkflowEngineRBAC` (viewer read-200/run-403, operator run-201).

## P1.5. Session handover + compact-plan (T-20261009233009)

Сессия, упершаяся в «контекст-стену» (context_used_percentage >= порога), переводится
в новую сессию той же роли с переданным контекстом. Конфиг-ключ порога:
`sessions.context_wall_pct` (int, default 80, `requires_restart=false`, env
`DAEMON_SESSION_CONTEXT_WALL_PCT`), читается слоённо (ResolveInt).

### Эндпоинты (2, tag `sessions`)
- `GET /compact-plan` → `200 {sessions[], total, wall_pct}`. Живые сессии с
  context_used_percentage >= wall, sort desc; без usage — omit. `suggested_action`:
  `handover` / `wait_for_turn` (running-сессия — подождать завершения хода).
  RBAC: `sessions.read` (viewer+).
- `POST /sessions/{id}/handover` → `200 HandoverResult`. `dry_run=true` — план без
  мутаций (status `planned`); иначе полный прогон. RBAC: `sessions.update`
  (operator+). Ошибки: 400 (нет reason), 404 (нет сессии), 409 (stopped / повторный).

### Модель (HandoverResult)
`{status: succeeded|partial|failed|planned, dry_run, old_session_id, new_session_id?,
context_prepared, successor_started, task_reassigned, old_stopped, context_package?,
successor_preview?}` — каждый `*_step` = `{status: ok|failed|skipped|would, reason?,
detail?}` (честный per-step отчёт: partial — не скрываем, а показываем).

### Решения
- **Порядок выполнения**: `context_prepared` → `old_stopped` → `task_reassigned` →
  `successor_started`. Предшественник стопится ДО создания наследника — инвариант
  «одна активная сессия на роль» (иначе CreateSession → 409 conflict).
- **Контекст-пакет**: последние N=200 строк transcript (путь к полному файлу +
  working_dir + модель + задача). **Без LLM-summary** (out-of-scope; raw-хвост
  transcript + файл).
- **Инжекция контекста в наследника**: `pi` → `system_prompt` в pi-конфиг;
  `process`/`tmux` → env `HANDOVER_CONTEXT_FILE` (CreateSession теперь передаёт
  `adapter.Start` из `config.Env`).
- **Задача**: существующий `TaskService.HandoffTask` (та же роль, new task,
  old closed + comment `handover from session N — <reason>`); неприменим → `skipped`.
  Ограничение: args не персистятся в модели сессии → для process/tmux наследник
  получает только контекст-файл (pi — валиден, т.к. pi-конфиг несёт детали).
- **Ограничения**: только живая сессия (из stopped/повторный → 409); runtime
  `pi|tmux|process`; одна роль-наследник = та же роль.
- **Attention**: 6-й источник `session_context_wall` (severity high), из compact-plan,
  link → `/sessions/{id}`.

### Тесты
- service (`session_handover_test.go`, 7): compact-plan (порог/omit/сортировка),
  handover (dry-run без мутаций, полный, 409 stopped, 409 повторный, task skipped).
- HTTP (`session_handover_handlers_test.go`, 4): compact-plan + RBAC; handover
  dry-run/full/409; 400 (нет reason) + RBAC (viewer 403).
- attention: `TestAttentionSessionContextWall`.

## P1.6. Watchdog-джобы (T-20261010022307)

Повторяемые проверки по расписанию/состоянию: регистрация джоба с политикой +
целевой сессией + порогом; loop watchdog; attention; P0.3-wake.

### Сущность
- **`watchdog_jobs`** (migration 0013): `id, name, policy` (CHECK — v1 только
  `context-usage-threshold`), `target_session_id` FK→sessions ON DELETE SET NULL,
  `threshold_pct` (1..100), `message`, `enabled`, `created_by`, `created_at`,
  `fired_count`, `last_fired_at`, `last_fired_pct`.
- **`watchdog_job_events`** — история (bounded: последние 20 на дjob): `outcome`
  (fired/quiet/dead-target/stopped) + `detail` + `pct`.
- **Порог в pct** (не МБ): переиспользует существующий `context_used_percentage`
  (live-из transcript). Валидация 1..100.

### Loop + fire-политика
- `WatchdogService.Scan` → `checkContextThresholdJobs` → enabled-джобы → live
  context % target-сессии >= порога → fire (алерт + fired_count++ + история +
  fire parked-wakes).
- **Дедупликация**: повторный fire только если context вырос > 5 п.п. от
  `last_fired_pct` (иначе skip). Quiet-скапы НЕ логируются (OpenRIG).
- **Dead-target**: сессия stopped/failed/отсутствует → `enabled=false` +
  `dead-target` в историю (без бесконечных алертов).
- **P0.3-связка**: `task_wakes.kind='watchdog'`, `ref=job_id` (string). При fire —
  `FireArmedWatchdogWakes(job_id)` разблокирует parked-задачи (аналог timer-fire).

### Attention
- Новый alert-вид **`watchdog_job`** (high, «Посмотреть сессию», link на
  `/sessions/{id}`) — добавлен в CHECK `watchdog_events.event_type` (migration 0014:
  sqlite — пересоздание таблицы, postgres — ALTER CONSTRAINT).

### RBAC
- Новый перм **`watchdog.update`** (operator+). GET list/show — viewer+
  (`dashboard.read`). POST register/stop + DELETE — `watchdog.update`.

### API (5 ops, RBAC watchdog.update / dashboard.read)
| Метод | Path | RBAC | Ответ |
|---|---|---|---|
| POST | `/watchdog/jobs` | watchdog.update | 201 JobView; 400/404 |
| GET | `/watchdog/jobs` | dashboard.read | 200 { jobs[], total } |
| GET | `/watchdog/jobs/{id}` | dashboard.read | 200 { job, recent_evaluations[] }; 404 |
| POST | `/watchdog/jobs/{id}/stop` | watchdog.update | 200 JobView (enabled=false); 404 |
| DELETE | `/watchdog/jobs/{id}` | watchdog.update | 200; 404; 409 (fired>0) |

### Тесты
- service (`watchdog_job_service_test.go`, 9): register-валидации (name/threshold/
  policy/session 404), успех, loop-fire (context>=порога → fired_count + alert),
  дедупликация, dead-target (disabled + событие), stop, delete fired>0→404/409,
  watchdog-wake разблокировка, wake-kind валидация.
- HTTP (`watchdog_job_handlers_test.go`, 3): CRUD вертикаль, delete 409 (fired>0),
  RBAC (viewer read 200 / register 403, operator 201).
- migration 0014 (sqlite recreate + postgres alter) — watchdog_job в CHECK.

### Отложенные политики (follow-up, вне v1)
- **`stale-no-output`** — сессия не выдаёт output N минут (нужен parser
  transcript-активности; у OpenRIG — idle-gate).
- **`token-budget` / cost-порог** — сжигание токенов/денег (реализуется вместе
  с P2.14 Cost tracking).
- **`artifact-pool-ready`** — ждать появления артефактов (n файлов/условие) —
  нужен механизм проверки диска по project-path.
- **`periodic-reminder`** — периодический reminder (у OpenRIG есть; нам —
  низкий приоритет, покрывается timer-wake P0.3).

---

## P1.7. Session briefing — «входящий контекст при запуске» (T-20261010070318)

**Цель**: при запуске сессии (запуск из TeamBuilder, handover-наследник, engine)
агент получает компактный **briefing** — «что я делаю, где я, чего ждали, что
новое» — из task (если в контексте), team (адрес + соседи), project (git +
открытые задачи), последних сообщений (direct + chatroom) и unread-alerts.
Передаётся **по адаптеру** (pi → system_prompt; process/tmux → файл + env).

### Хранение
**Без миграции** — briefing-метаданные в `sessions.config` (JSON):
`briefing_applied` (builtin|override|none), `briefing_text` (canonical, для
detail-UI), `briefing_path` (process/tmux: путь к файлу). `SessionRepo.UpdateConfig`
персистит config после создания (UpdateState не трогает config).

### Сборка (`BriefingService.Build`) — read-only, bounded
- **task**: id/title/state/body + последние переходы (≤5, из history).
- **team**: имя, адрес `segment.role`, соседи (relatives).
- **project**: имя/путь; **git read-only** (`git -C <path> rev-parse/rev-list/branch`
  через `exec.CommandContext`, timeout 5s) — branch/head/≤5 коммитов; open tasks;
  топ-активных задач.
- **messages**: последние direct (≤3) + chatroom (≤2) (обрезка 500 симв.).
- **alerts**: unread (≤3).
- **empty/error → omit** (не ошибки, не ломают запуск); git-ошибка →
  `git.available=false` + причина.
- **canonical**: markdown (`briefing_text`) + JSON (`briefing_data`) — один источник
  для UI + передачи.

### Передача по адаптерам
- **pi** → `config["system_prompt"]` (merges: briefing ПЕРЕД existing handover-prompt
  P1.5 — тот не ломается). Пишется в pi-config yaml (session-N.yaml).
- **process/tmux** → файл `<logs>/briefings/<id>.md` + env `BRIEFING_FILE=<path>`.
- **override/append/disable** (config): `briefing` ("none"=выкл), `briefing_override`
  (вместо builtin), `briefing_append` (добавка). Priority: disable > override > append >
  builtin.

### Preview (dry-run)
`POST /api/v1/teams/{id}/briefing/preview` (RBAC sessions.read) — собирает briefing
(без мутаций) + возвращает `briefing_text` + `briefing_data` + `delivery`
(pi-system-prompt | file-env) + `briefing_applied`. Для launch-UI TeamBuilder.

### CreateSession response
`POST /api/v1/sessions` теперь возвращает `briefing_applied` + `briefing_path`.

### Тесты
- service core (`session_briefing_test.go`, 5): full assembly, empty→omit,
  git present (репо), git absent→omit, bounded (recent ≤5).
- service delivery (`session_briefing_delivery_test.go`, 5): process-доставка
  (файл+env+view), override, disabled, pi-config yaml (system_prompt), handover+briefing
  (наследник builtin).
- HTTP (`session_briefing_handlers_test.go`, 3): preview (200/400/404/401 + delivery),
  RBAC (viewer 200), create-session (briefing_applied + path).

### Follow-up (вне v1)
- live-refetch briefing через WS; другие политики брифинга (drift, cost);
  LLM-суммаризация (как у handover context wall).

---

## P1.8. Health/Doctor-панель (T-20261010070318 R2)

**Цель**: «the surface that shows the truth» — что daemon/tmux/seat/БД думают о
состоянии и **где расходятся**. Read-only: не чинит, а показывает расхождения.

### Эндпоинты
- `GET /api/v1/diagnostics` — быстрый срез (read-only).
- `POST /api/v1/diagnostics/run` — явный полный прогон (тот же результат).
- Оба — **RBAC `diagnostics.read`** (operator+; viewer не получает — new перм,
  не `%.read`). admin — через `AllPermissions`.

### Чек'и (per-check: `name`, `status` ok|warn|fail, `detail`, `evidence`)
- **daemon** — up + version + uptime (всегда ok, если сервер отвечает).
- **database.ping** — ping (ok/fail).
- **database.migrations** — applied vs available (из embedded FS). Расхождение
  (pending) → **warn**.
- **tmux.server** — `tmux list-sessions` (ok если сервер; warn если нет —
  process/pi не зависят).
- **sessions.divergence** — **КЛЮЧЕВОЙ (R3)**: где БД-статус расходится с реальной
  живостью, через `adapter.Status(runtime_ref)`:
  - **stale** — active в БД, `!Alive` в адаптере → **fail** (показываем «working»,
    а сессии нет);
  - **zombie** (R2) — stopped/failed в БД, жив в адаптере → **warn** (утёкший
    процесс/панель).
  - bounded (active ≤50, stopped/failed ≤10); structured evidence
    `stale_sessions`/`zombie_sessions` `[{id,runtime,label}]` (UI drill-down).
- **logs.dir** — dir существует + writable (fail если нет/не запись).
- **git** — по git-проектам (bounded 3, read-only `git -C <path> rev-parse`,
  timeout 5s, как P1.7). not-a-repo → warn; нет проектов → ok (skipped).

**Overall** = worst(checks): fail > warn > ok.

### Дизайн-решения
- **Read-only**: внешние вызовы (git/tmux) через `exec.CommandContext` + timeout
  (5s) — не блокируют/не зависают; ошибки отдельного чека → свой статус, не
  ломают весь прогон.
- **Каждый чек — отдельная функция** (`checkDaemon`, `checkDatabasePing`,
  `checkMigrations`, `checkTmux`, `checkSessionDivergence`, `checkLogsDir`,
  `checkGit`) → тестируются по отдельности (внутр. `diagnostics_service_checks_test.go`).
- **Divergence** (R3+R2): `sessions.divergence` использует `Adapter.Status`
  (process/tmux/pi) — ОБЕ стороны расхождения: stale (fail) + zombie (warn).
- **Migrations**: 0015 (RBAC `diagnostics.read` operator+); **0016 (R2)** —
  `diagnostics_fail` в `watchdog_events.event_type` CHECK (sqlite: пересоздание
  таблицы; postgres: DROP/ADD CONSTRAINT).
- **`database.MigrationStatus`** — helper (applied vs available из embedded FS).
- **Bounded**: sessions ≤50, stopped/failed ≤10, git ≤3 проекта.

### fail → Attention (P0.1) (R4, закрыт в раунде 2)
- **`ReportFail`**: при `POST /diagnostics/run`, если есть fail-чек'и → создаёт
  **ОДИН** unread `watchdog_events` `event_type='diagnostics_fail'` (critical,
  `requires_action`). **Дедуп**: если unread diagnostics_fail уже есть → не
  дублирует (`WatchdogRepo.ExistsUnreadByType`).
- **Attention**: новый kind **`diagnostics_fail`** (critical, link **`/diagnostics`**) —
  выпадает из unread-алертов (source `alert_unread`); `alertSeverityAndAction` +
  `alertItem` special-case.
- GET /diagnostics НЕ создаёт алерт (только POST /run). Alerт — best-effort
  (не ломает ответ прогона).

### Тесты
- service core (`diagnostics_service_test.go`): basic (7 чек'ов),
  **divergence-stale** (fail), **divergence-zombie** (warn + structured evidence),
  migration-pending (warn), git-repo (ok), logs-missing (fail).
- service per-check (`diagnostics_service_checks_test.go`, внутр.): daemon,
  db.ping (ok/fail), tmux, logs.dir (ok/warn/fail), git (no-projects),
  divergence (registry-nil), **ReportFail** (create/dedup/no-fails).
- attention: `TestAttentionDiagnosticsFail` (kind + critical + /diagnostics).
- HTTP (`diagnostics_handlers_test.go`, 3): GET/POST 200 + checks, RBAC
  (401/viewer-403/operator-200+run), **POST /run → unread diagnostics_fail + dedup**.

### Follow-up (вне v1)
- disk-space по ключевым путям; auto-heal (сейчас read-only).

---

## P2.7. Поиск по транскриптам (T-20261010140809)

**Цель**: оператор ищет фразу/решение по истории вывода всех сессий (транскрипты)
и попадает в конкретное место: сессия + контекст-окно → EntityLink.

### Эндпоинт
- `GET /api/v1/sessions/search?q=&role_id=&team_id=&state=&limit=&offset=`
- **RBAC `sessions.read`** (viewer-ok; через дефолт GET → `res.read`).
- `q` — обязательный, non-empty, **min 2 символа** (иначе **400**).
- Фильтры `role_id`/`team_id`/`state` — по сессиям-владелец (сузят набор файлов).
- `limit` (default 50, max 200 — число результатов), `offset`.

### Механика (наивный, bounded, read-only, честный)
Транскрипты — файлы `<LogsDir>/session-<id>.log` (см. `SessionService.LogsDir`).
- **Кандидаты**: сессии с фильтром, newest-first, **≤200** (`transcriptMaxSessions`).
- **Чтение**: transcript-файл **построчно**, **tail ≤20k строк** (кольцевой буфер —
  **не весь файл в память**). Свежие совпадения в хвосте (транскрипты растут вниз).
- **Матч**: substring, **case-insensitive**.
- **Контекст-окно**: **±3 строки** (`transcriptContext`) вокруг совпадения;
  **слияние** соседних окон (если окна ближе `transcriptMergeGap`).
- **Item**: `{session_id, role_name, team_name, state, line_number, match_line,
  context[], snippet}`. `line_number` = 1-based первичное совпадение в окне.
- **Сортировка**: `session_id desc` (новые сессии выше) → `line_number asc`
  (в сессии — по строкам).
- **Ответ**: `{results, total, has_more}`.

### Честность + производительность
- **Честность**: transcript-файла нет/нечитаем → сессия **пропускается** (не ошибка);
  `q` не найден → **пустой список** (не 404).
- **Производительность**: bounded по сессиям (≤200) + строкам (tail ≤20k/файл) +
  **время-бюджет скана 3s** (`transcriptMaxScan` — не блокируем API надолго).
  Решения задокументированы в константах (`session_transcript_search.go`).

### Тесты
- **service** pure-функции: `transcriptWindows` (матч/окно/case-insensitive/
  merge/sеparate/clamp/no-match) + `readTailLines` (fewer/bounded-tail/missing).
- **service** интеграция: `SearchTranscripts` (match+context+имена/фильтры
  role+team+state/пусто/файл-нет/**bounded-tail**).
- **HTTP**: 200+результаты, 400 (q пустой/короткий), 401, **viewer-200** (RBAC),
  фильтр role, limit/offset/has_more, пустой результат.

### Follow-up (вне v1)
- точный line-скролл в live-терминале; индексация транскриптов (FTS) для больших
  объёмов; поиск по всем файлам (не только tail).

## P2.8. Capture — стл-кадр экрана сессии (T-20261010153631)

**Цель**: оператор получает «стл-кадр» экрана/вывода сессии (последние N строк)
как артефакт — можно сохранить/прикрепить. Базис для P2.11 (send verify).
Read-only для сессии (не влияет на её работу).

### Эндпоинт
- `GET /api/v1/sessions/{id}/capture?lines=N`
- **RBAC `sessions.read`** (viewer-ok; дефолт GET → `res.read`, middleware без правки).
- `lines` — **default 20, max 200** (больше → **clamp к 200**, не 400 — задокум.).
- Ответ: `{session_id, runtime_type, lines[], captured_at, source,
  saved_path}`. `source` ∈ `tmux-pane` | `log-tail`.

### Механика: tmux-pane vs log-tail (асимметрия, задокументирована)
- **tmux / pi** (pi = tmux внутри, `daemon-session-<id>`): `tmux capture-pane -t <ref>
  -p -S -<N>` — **реальный экран** панели (не лог). Это «стл-кадр» в полном смысле.
- **process**: у process **нет «экрана»** — только лог. Возвращаем **log-tail**:
  tail transcript-файла `<LogsDir>/session-<id>.log` (последние N строк).
  Асимметрия осознанная: process-рантайм не имеет GUI/терминала.
- `Capture` вынесен в **интерфейс адаптера** (`Capture(ctx, ref, lines)
  ([]string, error)`): tmux → capture-pane; pi → delegate tmux; process →
  `ErrNoScreen` (service тогда использует log-tail). Это даёт мок в тестах.

### 409 на мёртвый tmux (не 500) + честность
- tmux **не установлен** (`ErrTmuxUnavailable`) или **сессия мертва/отсутствует**
  (`ErrTmuxSessionDead`) → **409** с понятной причиной (clear reason). Не 500.
- Дед-детекция: реальный tmux отдаёт `can't find pane/session` (проверка на
  `can't find` + `no session` — defensive).
- **Сессия не найдена** (нет в БД) → **404**.
- **process без лога** → **200 с пустыми `lines`** + `source=log-tail` (не ошибка —
  честность: у сессии ещё не было вывода). Задокументировано.

### Сохранение снапшота + bounded-cleanup
- Каждый capture пишется в `<LogsDir>/captures/session-<id>-<unixnano>.txt`.
- `saved_path` — в ответе (оператор видит, где лежит артефакт).
- **Bounded-cleanup**: держать **≤20 снапшотов на сессию** (`captureMaxPerSession`);
  старые удаляются (сортировка по unixnano из имени). Глобального потолка нет
  (20/сессию — предсказуемо + достаточно для v1). Задокументировано.

### Вложение в сообщение/задачу — отложено (не блокирует)
Возможность прикрепить последний capture в форме сообщения/комментария задачи —
**не сделано** в v1: потребовало бы отдельного media-контракта (storage, ссылки,
RBAC на артефакты) — большая правка, не по теме capture. Зафиксировано как
**follow-up** (см. ниже). Capture доступен + сохраняется (`saved_path`) — этого
достаточно для P2.11 (send verify) и ручной передачи артефакта.

### Тесты
- **runtime**: process → `ErrNoScreen`; tmux dead → `ErrTmuxSessionDead`; **tmux
  live-интеграция** (реальный tmux: start + capture → строки; `t.Skip` без tmux).
- **service**: process/log-tail (tail), process **без лога → пустые**, 404, **409
  (tmux dead)**, lines **clamp** (>200→200, 0→default 20), **bounded-cleanup**
  (25 capture → ≤20).
- **HTTP**: 200 (process) + source/saved_path, lines param, **404**, **409** (tmux
  dead), 400 (lines не число), 401, **RBAC viewer+operator → 200**.

### Follow-up (вне v1)
- вложение capture в сообщение/задачу (media-контракт);
- глобальный потолок снапшотов + TTL; capture с пометкой/комментарием;
- diff-снимков (что изменилось на экране с последнего capture).

---

## P2.9. Pulse feed — единая лента событий (T-20261010164511)

**Цель**: одна лента всего, что происходит в системе (задачи, сессии, сообщения,
алерты, воркфлоу) — история из БД + live-обновление по WS. Отдельно от audit (кто
через API что делал) и от WS-стрима (live): Pulse — persisted срез «что было».

### Эндпоинт
- `GET /api/v1/events?type=&team_id=&role_id=&entity_kind=&entity_id=&limit=&offset=`
- **RBAC `dashboard.read`** (viewer-ok; middleware `case "events" → dashboard.read`).
- `limit` — **default 50, max 200** (clamp). Ответ: `{events[], total, has_more}`,
  sort **created_at desc**.

### Источники: EventBus sink (не новый publish-site)
- В `EventBus` добавлен **`SetSink(SinkFunc)`**: sink вызывается **синхронно в
  `Publish()`** до WS-броадкаста. **Аддитивно** — WS-семантика и все
  publish-сайты не трогались. Sink → `EventService.SetSink` → `EventRepo.Record`.
- **`session.output` в ленту НЕ попадает** (live-поток транскрипта, не событие).
- `role_id` добавлен в `Data` у `session.started`/`session.stopped` (2 строки,
  аддитивно) — для role-фильтра. `team_id` извлекается из `Data["team_id"]`
  либо из канала `team:<id>`.

### Storage: bounded таблица `events` (migration 0017)
- Колонки: `id, type, team_id, role_id, entity_kind, entity_id, summary, payload,
  created_at` + индексы (created_at desc; фильтры).
- **Bounded-политика**: **≤5000 строк ИЛИ ≤7 дней** (что раньше), авто-cleanup при
  каждой записи (prune). Задокументировано (в отличие от audit — без жёсткого
  лимита; лента — operational-срез).

### Обогащение entity_title
- Batch-lookups (1 запрос на тип): teams / roles / `queue_tasks` → имя сущности
  (`entity_title`) для ленты. Не найденное → пусто (честность).

### Frontend: PulsePage (`/pulse` + nav)
- Feed: type-бейдж (цвет по категории) + summary + **EntityLink** (task/session/
  message/alert/workflow) + команда + relative time.
- Фильтры: **type / team / role** (select'ы; key-запрос меняется → refetch).
- **live-prepend по WS**: те же релевантные события (без session.output) конвертятся
  в EventItem + prepend (X3: throttle 3с + debounce 300мс); live-события тоже
  проходят активные фильтры (client-side). `workflow.instance_updated` добавлен в
  `WSServerEventType` (backend его публикует).
- PageSkeleton / EmptyState / ErrorState.

### Тесты
- **repo**: sort desc, фильтры (type/team/entity), **bounded 5001→5000**.
- **service (integration)**: 4 типа напрямую через EventBus + реальная `CreateTask`
  (sink → лента).
- **HTTP**: 200 + обогащение, 404-аналог (пусто), 401, **RBAC viewer → 200**.
- **frontend (vitest)**: рендер ленты (бейджи + EntityLink), фильтр по типу, empty.

## P2.11. Send to session + verify — ввод текста в терминал живой сессии (T-20261010183054)

**Цель**: оператор отправляет текст в терминал живой сессии (tmux/pi) и получает
честный статус доставки. Отличается от Message (закрытая команда → inbox, read):
Send — ввод в **живой процесс** (действие на работающий процесс).

### Механика (реальный tmux)
- `tmux send-keys -t <ref> -l -- <text>` — **`-l` = literal** (без интерпретации
  спец-символов; `--` = конец опций). `-t <ref>` — tmux-сессионная ссылка.
- Submit → **отдельный `tmux send-keys -t <ref> Enter`** (не в строку с `-l` —
  `send-keys -l Enter` ввёл бы литеральный "Enter").
- **tmux не установлен** → `ErrTmuxUnavailable`; **мёртвый tmux** (no such pane/session) →
  `ErrTmuxSessionDead` (409 с причиной). `Send` добавлен в `runtime.Adapter` (pi →
  делегирует TMux; process → `ErrNoScreen`).

### Verify по capture (3 × 300ms, честный статус)
- После `Send` — `Capture(25)` + поиск **заметной части** отправленного текста
  (посл. 25 символов, lowercase, без trim). Статусы:
  - **`delivered`** — часть текста найдена в capture (подтверждено), `verified=true`, `tail`.
  - **`staged`** — `send-keys` отдал 0 (введён), но capture не подтвердил за 3×300ms.
  - **`retained`** — не отправлено (guard), `sent=false`, `guard`-причина.
- 3×300ms — bounded + достаточно для синхронного эхо терминала (cat/shell). LLM-сессия
  может «поглотить» ввод → `staged` (честно: введено, не подтверждено).
- `sendVerifySleep` (package var) — в тестах = 0 (быстро).

### Interactive-prompt guard (эвристика)
- **Capture ~5 строк перед отправкой** (создаёт session как P2.8). Эвристика: строка
  содержит prompt-фразу (case-insensitive) ИЛИ заканчивается суффиксом (`>`, `#`, `$`).
- **`false-pos/neg допустимы** (эвристика, не детекция); false-negative безопасен
  (вводит обычный текст). False-positive → **`retained`** (text НЕ вводится, причина
  `interactive-prompt`).

### process runtime → 409 (нет экрана/терминала)
- У process нет терминала, куда «печатать» → всегда 409 «no terminal to send into».

### Эндпоинт
- `POST /api/v1/sessions/{id}/send` body `{text, submit?}`.
- **RBAC `sessions.update` (operator+)** — действие на работающий процесс опаснее
  read-only capture: viewer/operator могут читать сессию, но отправлять в неё могут
  только operator+. Migration **0018** (`sessions.update` → роль `operator`).
- `400` (text пустой / >2000), `404` (нет сессии), `409` (process / tmux недоступен/мертв).
- **Событие `session.send`** (P2.9) → Pulse feed (entity_kind session; status в summary).

### Frontend (Session Detail, tmux/pi)
- Блок «Send to session»: textarea + «Submit» checkbox + Send + **warning**
  (действие на живой процесс). Pending-state. Статус-бейдж (delivered=green /
  staged=yellow / retained=red) + guard-причина. Collapsible capture tail (verified).

### Тесты
- **runtime**: process → ErrNoScreen; tmux не установлен → ErrTmuxUnavailable;
  tmux live → text на экране (integration, skip без tmux).
- **service**: delivered (text на экране), staged (не подтверждён), **guard → retained**
  (text НЕ введён), process → 409, 404/400, `session.send` в events.
- **HTTP**: 200 delivered, 200 retained, 404, 400, 409 process, 409 tmux dead, 401,
  **RBAC viewer → 403 / operator → 200**.
- **frontend (vitest)**: delivered (badge + tail), staged, retained (guard), pending.

## P2.15. Whoami — «кто я и что мне делать» (T-20261010193009)

**Цель**: компактный срез агента (для `teamctl whoami` / API): кто я, у кого я, что
мне делать, кто ждёт от меня, кто передаёт/получает от меня.

### Идентификация (auth vs query)
- **API-ключ (auth)** определяет, *кто запрашивает* (RBAC).
- **Query** определяет, *какого агента описать*:
  - `session_id` → резолвится в role+team+статус сессии (**предпочтительно** —
    агент знает свой session id).
  - либо `role_id` (+опц. `team_id`).
  - **без параметров → 400**; нет сессии/роли/команды → **404**;
    role/team не совпадают → **400** (честность).

### Ответ (bounded)
- `agent` (role_id/role_name/team_id/team_name/address), `session` (только если
  session_id: id/state/model/context_used_pct).
- `my_tasks`: **pending / in_progress / blocked** (`destination_role_id = role`),
  blocked с `wake?` (kind/ref). Bounded ≤20/список (задокументировано).
- `waiting_for_me`: **unread** входящие к роли (`MessageFilter.UnreadOnly`, ≤20) —
  `{id, from_role_name, type, summary}` (summary = обрезка тела ≤120).
- `upstream` / `downstream`: relatives in/out (`{role_name, via}`, ≤20).

### RBAC
- `GET /api/v1/whoami` → **`dashboard.read`** (viewer-ok; читает задачи/сессии/
  сообщения/команду — все `*.read`).

### Тесты
- **service**: by session (полный: agent/session/tasks/waiting/upstream), by role
  (session=nil), 400 no-params, 404 (session/role), role-team mismatch (400),
  bounded (25→20), разные роли (разные my_tasks).
- **HTTP**: 200 by-session/by-role, 400, 404, **RBAC viewer-200**.

## P2.12. teamctl — агентский CLI поверх REST (T-20261010193009)

**Цель**: агент (pi-сессия, pipeline-воркер) работает с системой из shell без
curl-магии: `teamctl <cmd> [--json]` поверх существующих HTTP-эндпоинтов.

### Принципы
- **Клиент, не обход**: teamctl не содержит бизнес-логики — только парсинг flags +
  HTTP + печать. Все правила (RBAC, переходы, валидация) — на стороне демона.
- **Минимализм**: std `flag` + `net/http`, без внешних зависимостей.
- **Две поверхности**: `--json` (machine-parable, агенты парсят) / human (человеку).

### Конфигурация
- base URL: `--server` / `TEAMCTL_SERVER` (default `http://localhost:8080`).
- auth: `--api-key` / `TEAMCTL_API_KEY` → header `X-API-Key`.
- `--json` на все команды.

### UX / ошибки
- **Exit code**: `0` успех, `1` HTTP/сеть, `2` usage (bad flags / unknown cmd).
- Ошибки → **stderr** (человекочитаемые); при `--json` — **error envelope в stdout**
  (агент парсит и stdout). `teamctl help` + per-cmd справка.

### Команды (поверх существующих эндпоинтов)
whoami, task list/claim/update/handoff/block, session list/transcript/capture/stop,
attention list, config get/set/reset. `config set <key> <value>` — value парсится
как JSON (number/bool/obj), иначе строка (передать в `PUT /config/{key}`).

### Реализация (структура)
- `cmd/teamctl/main.go` → `cli.Run(args, stdout, stderr) int` (testable).
- `internal/cli/client.go` (HTTP + X-API-Key + resolve env/flag), `output.go`
  (--json/human + error envelope), `commands.go` (dispatch + команды + human).
- **`reorderFlagsFirst`**: std `flag` останавливается на первом non-flag, поэтому
  флаги после positional (`task handoff <id> --to-role-id M`) переупорядочиваются
  вперёд (известные value-флаги + их значения).

### Тесты (httptest)
- Для каждой команды: mock-демон → команда → assertion на **request** (method/path/
  body/`X-API-Key`) + **parse ответа** (`--json` и human).
- Auth-header presence; exit code на 404/403 (не нулевой); help + usage (exit 2).

## P2.13. Starter bundles — готовые стартеры в Library (T-20261010203207)

**Цель**: «проект из коробки» — готовый набор (team/workflow), который apply'ится в
один клик. Стартеры — pin-элементы библиотеки: их нельзя переименовать/изменить,
но можно apply'ить (копирование в новый объект).

### Механика (migration 0019 + seed)
- **Колонка `is_starter`** (0019, sqlite+postgres): метка pin-стартера на
  `library_items`. Стартеры — подмножество обычных элементов (все поля те же).
- **Seed = `service.SeedStarters(ctx, db)`** (не в миграции): детерминированный набор
  (2 шт): `dev-team` (type=team, роли lead/worker/reviewer на существующие
  agent-fixtures) + `review-workflow` (type=workflow, implement→review→done).
  Идемпотентно: `ON CONFLICT (type, name) DO NOTHING` (рестарт не дублирует).
  Вызывается в `main` при каждом старте (после миграций).
- **Почему не в миграции SQL**: seed-функция (Go) testable (идемпотентность проверяется
  двойным вызовом) + данные в одном месте; миграция только добавляет колонку.

### Pin-защита (неизменяемость)
- **`PATCH /library/{id}`** (Update): если `is_starter` → **409**
  ("pinned starter and cannot be modified").
- **`POST /library`** (Save) с именем, совпадающим со стартером → **409** (UNIQUE
  `(type, name)` уже ловит дубликат — проверено тестом).
- Delete стартера не существует (нет DELETE /library/{id}) — стартеры permanent.

### Apply + project_id (P1.0)
- **`POST /library/{id}/apply`** получает опц. `project_id`: для team-стартера
  (без `target_team_id`) новая команда привязывается к проекту
  (`applyTeam` → `CreateTeamRequest.ProjectID`). Workflow-стартер требует
  `target_team_id` (как и раньше).
- Ответ apply не меняется (`status` + `created_resources`).

### RBAC
- Без новых действий: apply/save/read идут через существующие `library.*` (apply =
  `library.create`, read = `library.read`). Pin-409 не зависит от роли.

### Frontend
- `LibraryPage`: **STARTER badge** (фиолетовый) + **стартеры первыми** (стабильная
  сортировка) + **banner «Starters»**; в detail — **no-edit** (lock, rename disabled)
  + **one-click apply** (для team-стартера — выбор проекта, опц.; кнопка
  «Apply this starter»). `LibraryItem.is_starter` + `ApplyLibraryItemRequest.project_id`.

### Тесты
- **Service** (7): seed (2 шт, is_starter=1, group=starters), идемпотентность
  (двойной seed → не дублирует), apply team-starter (3 роли), apply team-starter
  с project_id (привязка), apply workflow-starter (3 блока), pin-409 (Update),
  save-name-409 (дубль имени стартера).
- **HTTP** (5): list (2 стартера, is_starter, filter type=team), apply team,
  apply team+project, pin-409 (PATCH), apply workflow. Test helper `newTestServerSeeded`
  (+ Projects wired в Options).

## P2.14. Cost tracking — токены → деньги (T-20261010213901)

Оценка стоимости LLM-сессий/задач/проектов + бюджет-порог. **Честность**: без
token-данных или без цены модели показываем «нет данных» (`null` / `has_data=false`),
**никогда не 0** (0 = ложное «бесплатно»). Это оценка (токены × цена), не счётчик
биллинга → `estimate: true` (UI: «~»).

### Config-ключи (C1)
- **`cost.model_prices`** (StrType, `ValidateJSON=true`, default `{}`): JSON-объект цен
  `{"<model>":{"in_usd_per_mtok":N,"out_usd_per_mtok":N}}`. `{}` = цены не заданы →
  «нет данных». PUT валидирует JSON-объект (необъект → 400). `requires_restart=false`
  (читается в рантайме).
- **`cost.budget_alert_cents`** (IntType, default `0`): порог бюджета в ЦЕНТАХ.
  `0` = watchdog-алерт выключен. `requires_restart=false`.
- Новый механизм: `KeySpec.ValidateJSON` + `ResolveString` (строка для JSON-ключей).

### CostService (C2) — честная семантика
`CostInfo{cost_usd *float64, has_data bool, estimate bool}`; `NoDataCost()` = `{nil,false,-}`.
- `SessionCost(in, out, model)`: нет токенов (оба 0/nil) → «нет данных»; нет цены модели
  → «нет данных»; иначе `(in/1e6 × in_price) + (out/1e6 × out_price)`, `estimate=true`.
- `SumCost([]CostInfo)`: суммирует только `has_data` (estimate = ИЛИ); если ни одного
  с данными → «нет данных». Частичные данные → сумма тех, что есть (documented).
- `Prices(ctx)` / `BudgetAlertCents(ctx)` — чтение из Config.

### Вычисления + агрегация (bounded)
- `CostForSession` (log-tail: tokens + model), `CostForTask` (сессии задачи, limit 100),
  `CostForProject` (сессии проекта, limit 100), `CostForAll` (limit 50 — для бюджета).
- `CostSeries` (12 точек, dashboard, limit 50 сессий, пустые бакеты = 0 — как `llm_tokens`).
- Поля `estimated_cost` (null = «нет данных»): SessionView/SessionDetail, TaskDetail,
  GetProjectResponse. Metrics: `llm_cost_usd` series (nil-safe → все 0).

### Бюджет-порог → watchdog-алерт (C4)
`WatchdogService.checkBudgetAlert` (в `Scan`): `TotalCost` (all, limit 50) > порога →
событие `event_type="budget"`, severity `high`, `requires_action=true`. **Dedup**: cooldown
1ч (`ExistsRecentByType`, не спамит). `watchdog_events.team_id` — FK → привязываем к
первой команде (sentinel); нет команд → алерт не создаётся. **Migration 0020**: добавление
`'budget'` в CHECK `event_type` (sqlite — пересоздание таблицы, pg — DROP/ADD constraint).

### RBAC
Нет новых пермиссий: `estimated_cost` идёт в существующих read-ответах (sessions/tasks/
projects → их роли), `llm_cost_usd` в `/dashboard/metrics` (viewer-ok), config →
`config.update` (admin) / read (viewer). Бюджет-алерт — internal (watchdog, не API).

### Тесты
- **Service** (6 CostService): computed ($7.00), no-usage → null, no-price → null,
  SumCost, all-nodata → null, budget default/override. **Watchdog** (5): fires, dedup
  (cooldown), below-threshold, no-data, disabled. **HTTP** (4): config cost-ключи,
  metrics llm_cost_usd (12 точек), task detail cost, project detail cost.

## P3.12. Agent images / session snapshots — снимок настроенного агента (T-20261011032248)

OpenRIG-вдохновлённая фича: сохранить сессию как «образ агента» — снимок конфигурации
агента (модель, системный промпт, скиллы, tools, thinking, роль, runtime, working dir) +
opциональный summary. Из снимка можно **запустить новую сессию** с тем же настроенным
агентом (summary добавляется в системный промпт). Снимки живут в Library как новый тип
`agent_image` (отдельная секция, пин, prune).

### S1. Модель + migration 0021
- Новый тип библиотеки **`agent_image`**: расширение CHECK `type IN (...)` в таблице
  `library_items` (sqlite — пересоздание таблицы с копией, pg — DROP/ADD constraint).
- Новая колонка **`is_pinned`** (bool, default false) — пользовательский pin (защита от
  prune). Отдельно от `is_starter` (pin-стартер P2.13).
- Model: `LibAgentImage` spec, `Valid()` допускает `agent_image`, `LibraryItem.IsPinned`.
- Repo: `SetPinned`, `Delete`; SELECT/CREATE/scan обновлены под `is_pinned`.

### S2. Snapshot (создание снимка)
`SessionService.SnapshotSession(sessionID, req)`:
- Читает конфиг сессии (agent fields: model/system_prompt/skills/tools/thinking),
  role_name, runtime_type, command, working_dir; source_session_id.
- **Summary**: из `req.summary`, либо авто (tail транскрипта: не-JSON строки, bounded
  64KB/3 строки/300 символов), либо пусто.
- Создаёт `LibraryItem` type=`agent_image`, group=`agent-images`, version `1.0.0`,
  spec = `AgentImageSpec`. Дубликат по `UNIQUE(type,name)` → 409.
- **HTTP**: `POST /api/v1/sessions/{id}/snapshot` (RBAC sessions.update → operator+).

### S4. Launch (запуск из снимка)
`SessionService.LaunchFromSnapshot(libraryID, req)`:
- Читает spec, находит роль по имени в целевой команде (нет → 404/400), строит конфиг
  (agent fields + summary **в начале** системного промпта как `## Session context`).
- `CreateSession` (расширен: теперь читает thinking/skills/tools из config).
- **HTTP**: `POST /api/v1/library/{id}/launch` (RBAC sessions.update).

### S3. List / pin / prune
- List: `agent_image` — обычный тип (фильтр `type=agent_image`), отдельная секция в UI.
- **Pin/Unpin**: `POST /api/v1/library/{id}/pin|unpin` (RBAC library.create). Только
  `agent_image` (иначе 400).
- **Prune** (delete): `DELETE /api/v1/library/{id}` (RBAC library.create). Только
  `agent_image` (иначе 400); pinned → 409 (нельзя удалить закреплённый).

### RBAC
Нет новых пермиссий: snapshot/launch → `sessions.update` (operator+); pin/unpin/prune →
`library.create`; list/read → существующий read library (viewer-ok). `is_pinned` идёт в
существующих read-ответах library.

### Frontend
- Library: тип `agent_image` (фильтр), карточка с бейджем `PINNED` (is_pinned), detail
  с agent-config (модель/скиллы/tools/summary) + Pin/Unpin/Prune + Launch (team/project).
- Session Detail: кнопка `📸 Agent image` → модалка (name prefilled `agent-<role>`,
  summary, description) → POST snapshot.
- API client: `sessions.snapshot`, `library.pin/unpin/delete/launch`.

### Тесты
- **Service** (8 session_snapshot): snapshot-создание, дубликат 409, auto-summary,
  launch-создание сессии, launch-404 команда, pin/unpin, prune pinned-409.
- **HTTP** (4 session_snapshot): snapshot 200, snapshot 409 dup, launch 200, launch 400
  non-agent_image.

---

## P3.13. Proof/evidence на задачах — closure ≠ acceptance (T-20261011050110)

Ядро: «задача закрыта» (done) ≠ «результат принят». **Closure** — факт закрытия задачи;
**Acceptance** — отдельное состояние: есть ли принятый артефакт (verdict = accept).
Механика: к задаче прикладывают **typed-артефакты** (screenshot/test/log/other) с
**sha256-целостностью** (candidate-sha); **судья** (человек) выносит **verdict**
(accept/reject/withdraw) с атрибуцией.

### Модель (Migration 0022, sqlite + postgres)
`task_evidence`: id, task_id (FK queue_tasks ON DELETE CASCADE), type
(CHECK screenshot/test/log/other), name, content_ref, content_sha (sha256 =
candidate-sha), description, status (CHECK submitted/accepted/rejected/withdrawn),
submitted_by, judged_by, verdict_reason, judged_at, created_at, updated_at. Timestamps —
TEXT RFC3339Nano (project-конвенция). Индексы: (task_id), (task_id, status).

### Endpoints
- **Submit**: `POST /api/v1/tasks/{id}/evidence` (RBAC `tasks.update`, operator+). 201.
  - inline `content` → сервер считает **sha256** (candidate-sha); либо `content_ref`
    (+ `content_sha` для целостности). `type`+`name` обязательны.
- **List**: `GET /api/v1/tasks/{id}/evidence` (RBAC `dashboard.read`, viewer-ok). 200.
  Bounded (limit ≤100, default 50), newest first.
- **Verdict**: `POST /api/v1/tasks/{id}/evidence/{evid_id}/verdict` (RBAC `tasks.update`).
  - `accept` → status=accepted; `reject` → status=rejected (+ reason); `withdraw` →
    status=withdrawn (только если уже был verdict, иначе 400). Атрибуция:
    `judged_by` из `authContext` (UserName / "human").

### Readiness / Acceptance (агрегация в TaskView)
- `EvidenceReadiness`: **ready** (есть accepted) / **pending** (есть submitted) / **none**.
- `EvidenceAcceptance`: **accepted** (есть accepted) / **rejected** (есть rejected) /
  **pending** (есть submitted) / **none**.
- Поля на `TaskView`: `evidence_count`, `readiness`, `acceptance` — считаются **batched**
  (один `StatusSummaryByTasks` GROUP BY по списку задач, без N+1) в `viewsFor`.
- **Closure ≠ acceptance**: задача может быть `done` при `acceptance=pending` (закрыта,
  но артефакт не принят). Test фиксирует.

### RBAC
GET evidence → `dashboard.read` (viewer-ok); POST evidence + verdict → `tasks.update`
(operator+). `res=="tasks" && id!="" && sub=="evidence"` (verdict — `sub` = evidence,
`evid_id` — отдельный path-сегмент).

### Frontend
- Task Detail: панель **Evidence / proof** (список артефактов: type-чип, name, status-бейдж,
  sha, submitted_by, judged_by+at, reason + judge-кнопки Accept/Reject/Withdraw) +
  **+ Add evidence** (inline-форма: type/name/content|ref+sha/description).
- Readiness/acceptance бейдж в page-head (`✓ ready`/`… pending`/`∅ none` + `acceptance: X`).
- API client: `tasks.submitEvidence/listEvidence/verdictEvidence`.

### Тесты
- **Service** (8 evidence): submit inline (sha), submit ref (body-sha), validation, list,
  verdict accept/reject/withdraw, verdict errors (no verdict → withdraw 400, terminal),
  readiness/acceptance aggregation, **closure ≠ acceptance**.
- **HTTP** (4 evidence): submit 201, list 200, verdict 200, RBAC viewer 403/200.
- **Frontend vitest** (5): panel + readiness/acceptance, list artifacts, closure≠acceptance
  (done + pending), add evidence, judge accept.

---

## P3.15. Inbox/outbox — mailbox-семантика (T-20261011063105)

Сообщения — не просто лента, а **mailbox**: входящие (direct) требуют
**подтверждения** (absorb/deny/drop), исходящие — с записью (outbox).
**undelivered** = pending (неподтверждённые) входящие.

### Модель (Migration 0023, sqlite + postgres)
- `messages.ack_state` TEXT NOT NULL DEFAULT 'pending' CHECK IN
  ('pending','absorbed','denied','dropped') + `acked_by` (атрибуция) +
  `acked_at`. Существующие сообщения → 'pending'.
  - **sqlite**: CHECK не добавляется in-place → **REBUILD** таблицы (как 0021:
    messages_new → copy → drop → rename). Индексы: to_role, from_role, ack_state.
  - **pg**: ADD COLUMN + ADD CONSTRAINT in-place.
- `models.Message` + `AckState` (pending/absorbed/denied/dropped) + `AckAction`
  (absorb/deny/drop, `State()` → итоговое состояние) + `Valid()`.
- **`IsRead` не трогаем** (orthogonal): `is_read` = прочитано в ленте; `ack_state` =
  mailbox-подтверждение входящего. Разница задокументирована.
- **Скоуп inbox = direct-сообщения** (ToRoleID = роль, type=direct). **broadcast/segment
  НЕ в inbox** (идут в chatroom) — inbox это то, что адресовано именно роли и требует
  её реакции.

### Endpoints
- **`GET /roles/{id}/inbox`** (RBAC dashboard.read, viewer-ok): direct-входящие роли
  (ToRoleID=id, type=direct), bounded; + **`undelivered_count`** (число pending).
- **`POST /messages/{id}/ack`** (RBAC messages.update, operator+): body
  `{ action: absorb|deny|drop }`. `acked_by` (атрибуция из auth), `acked_at=now`,
  `ack_state` → absorbed/denied/dropped. Правила: **ack только из pending** (повторный
  → **409**); invalid action → **400**; 404 (нет сообщения).
- **`GET /roles/{id}/outbox`** (RBAC dashboard.read, viewer-ok): исходящие роли
  (FromRoleID=id), bounded, запись (как list).

### RBAC
- Новая пермиссия **`messages.update`** (operator+): оператор может ack'ить от имени
  роли (атрибуция фиксируется). **viewer не получает** (viewer = только `%.read`).
  - inbox/outbox (GET) → `dashboard.read` (viewer-ok); ack (POST) → `messages.update`.
- `res == "roles" && (sub=="inbox"||sub=="outbox")` → dashboard.read;
  `res=="messages" && sub=="ack"` → messages.update.

### Frontend
- **Messages page → Mailbox-секция**: селектор команды + роли → **MailboxPanel**.
- **Inbox**: каждый входящий с ack_state-бейджем; **pending выделен** (жёлтая
  рамка); счётчик **undelivered** (pending); на pending — кнопки **Absorb/Deny/Drop**
  → POST ack; показана атрибуция (acked_by + when).
- **Outbox**: исходящие (read-only list).
- В ленте direct-сообщений — ack_state-бейдж (опц.).
- API client: `mailbox.getInbox/getOutbox/ack`.

### Тесты
- **Service** (5): inbox direct-only+undelivered (broadcast/segment не в inbox),
  bounded+404, ack absorb/deny/drop (статус + атрибуция), повторный 409+invalid 400+404,
  outbox+404.
- **HTTP** (4): inbox, ack, outbox, RBAC (viewer inbox/outbox 200, ack 403).
- **Frontend vitest** (3): inbox pending+undelivered, ack absorb→бейдж меняется, outbox.

---

## P3.14. Walk — paced-доставка контекста в сессию (T-20261011074706)

**Проблема**: онбординг нового агента «одним дампом» нечитаем и теряется. **Walk** —
именованная **упорядоченная последовательность текстовых шагов**, доставляемых в
сессию **по одному** через существующий `SessionService.Send` (P2.11). Pacing =
**interval** (авто, из reaper-loop, не чаще poll) или **manual** (вручную).

### Модель (Migration 0024, sqlite + pg)
- **`walks`**: `session_id` (FK sessions, CASCADE), `name`, `description`, `mode`
  (CHECK interval/manual), `interval_secs` (для interval; NULL для manual), `status`
  (CHECK active/paused/completed/failed, default active), `current_seq` (следующий к
  отправке, default 1), `last_sent_at`. Индекс: `session_id`.
- **`walk_steps`**: `walk_id` (FK walks, CASCADE), `seq` (1-based), `content`, `state`
  (CHECK pending/sent/delivered/staged/retained/failed/skipped, default pending),
  `send_status` (delivered/staged/retained, из Send), `note` (guard/ошибка), `sent_at`.
  Индекс: `walk_id, seq`.
- Timestamps — TEXT RFC3339Nano (project-конвенция). Модель: `Walk`/`WalkStep` +
  `WalkMode`/`WalkStatus`/`WalkStepState` + `Valid()`.

### Service (WalkService)
- **`Sender`-интерфейс** (`Send(ctx, sessionID, text, submit)`): реализует
  `*SessionService`; в тестах — mock. **`WalkClock`** (injectable, для AdvanceDue).
- **`CreateWalk(session_id, name, mode, interval_secs, steps[])`**: валидация
  (session 404, ≥1 шаг, mode valid, interval_secs>0 если interval, content непустой
  ≤ 2000). Создаёт walk (active, current_seq 1) + steps (pending, seq 1..N) в tx.
- **`Advance(walkID)`**: `Send` следующего шага → step.state из `Send.status`:
  - **delivered/staged** → step sent, `current_seq++`, `last_sent_at=now`;
  - **retained** (guard: интерактивный prompt) → step retained + **walk paused**
    (current_seq **не** меняется — retry того же шага при resume);
  - **Send-ошибка** (409/нет экрана/мёртв) → step failed (note) + **walk failed**;
  - все шаги отправлены (current_seq > max) → **walk completed**.
  - paused/completed/failed → **409**.
- **`AdvanceDue(ctx)`** (interval-режим, из reaper-loop): по каждому active
  interval-walk — due, если `current_seq==1 && now-created_at ≥ interval` ИЛИ
  `current_seq>1 && now-last_sent_at ≥ interval` → **один шаг за тик** (pacing).
  manual-walks не трогаются.
- **`Pause/Resume`** (active↔paused; completed/failed → 409). **`Skip`**: текущий
  шаг → skipped, `current_seq++` (без Send). **`GetWalk`** (+ steps + progress),
  **`ListWalks`** (без steps, чтобы не N+1).
- **Progress** = `sent/total`, где sent = шаги с delivery-статусом
  (delivered/staged/retained/sent).

### Reaper-loop (piggyback)
- В `runReaper` (рядом с `ssvc.Reap`) каждый тик — `walkSvc.AdvanceDue(ctx)`.
  Pacing = не чаще poll-interval (не отдельный цикл).

### API + RBAC
- `POST /sessions/{id}/walks` (sessions.update, operator+) — create (201).
- `GET /sessions/{id}/walks` (dashboard.read, viewer-ok) — list.
- `GET /walks/{id}` (dashboard.read, viewer-ok) — detail (+ steps + progress).
- `POST /walks/{id}/advance|pause|resume|skip` (sessions.update, operator+).
- 404/400/409 честные. События (Pulse, P2.9): `walk.step_sent`, `walk.completed`.

### Frontend
- **Session Detail → Walk-блок** (`WalkPanel`): create-форма (name, mode
  manual/interval, interval_secs, **редактор последовательности** — добавить/удалить/
  изменить шаги, порядок) + список walk'ов (status-бейдж active/paused/completed/
  failed, progress `sent/total`, шаги с state-бейджами + sent_at + note (guard),
  действия Advance/Pause/Resume/Skip; при interval — пометка «авто (каждые Ns)»).

### Ключевые решения
- **retained → paused, current_seq не меняется**: guard-шаг не отправлен → при resume
  ретраим тот же шаг (prompt уже разобран). failed/current_seq — аналогично (шаг не
  сдвигается).
- **Sender/Clock — injectable**: тесты без runtime (mock Send) + детерминированный
  pacing (fake clock).
- **Нет новых RBAC-пермиссий**: reuse sessions.update (create/advance/pause/resume/
  skip) + dashboard.read (list/get) — как у Send/capture.

### Тесты
- **Service** (7): create (валидация + 404 сессии + whitespace-шаг), advance
  (delivered→seq++→completed), retained→paused (+retry при resume), failed, pause/
  resume/skip, AdvanceDue (interval due/not-due, clock, pacing, manual не авто),
  get/list/404.
- **HTTP** (3): create/get/advance (409 completed, 404), list, RBAC (viewer 403
  create/advance, 200 get).
- **Frontend vitest** (5): list+статус+progress, advance→completed, create-форма,
  retained→paused, pause/resume.

---

## P3.16. Mode/permission posture — delegated vs human-led (T-20261011092843)

Оператор выбирает **posture** (режим доверия) на уровне команды (база) + сессии
(override). **delegated** = чувствительные операции выполняются сразу (авто);
**human_led** = чувствительные операции **паркуются** → `human_request`
(approve/deny → **replay** оригинального запроса один раз).

### Модель (Migration 0025, sqlite + postgres)
- `teams.posture` TEXT NOT NULL DEFAULT 'delegated' CHECK IN
  ('delegated','human_led') — база.
- `sessions.posture` TEXT (nullable) CHECK IN ('delegated','human_led') — override;
  **NULL = наследует от команды**.
- `human_requests` (новая таблица): `scope_type` (team/session/global),
  `scope_id`, `team_id`, `method`, `path`, `body` (bounded ≤ 64KB), `auth_key`
  (API-ключ оригинала для replay-auth), `operation` ("session.stop" и т.д.),
  `reversible` (ADR-002), `status` (pending/approved/denied/replayed/expired),
  `decision_reason`, `decided_by`, `decided_at`, `replay_status` (HTTP-статус
  replay), created/updated. Индексы: status, (scope_type, scope_id).
- **`models.Posture`** + `Valid()`, `models.HumanRequest` + `HumanRequestStatus`.

### Sensitive-реестр (что считается чувствительным)
`service/sensitive_registry.go` — `MatchSensitive(method, path) → (operation,
reversible, ok)`. Правила (порядок — специфичные перед обобщённым):
- `PUT /config/{key}` → `config.set` (reversible)
- `POST /workflow-instances/{id}/abort` → `workflow.abort` (irreversible)
- `DELETE /sessions/{id}` → `session.stop` (irreversible)
- `POST /sessions/{id}/handover` → `session.handover` (reversible)
- `DELETE /library/{id}` → `library.prune` (irreversible)
- `DELETE /*` (обобщённый) → `delete` (irreversible: stop/прune/archive/relatives).
- **`decide`-endpoint НЕ sensitive** (иначе рекурсия).

### Scope resolution (effective posture)
`PostureService.resolve(path)`:
- **session scope** (`/sessions/{id}`) → `session.posture` если задано, иначе
  `team.posture` (от `session.team_id`).
- **team scope** (`/teams/{id}`) → `team.posture`.
- **task scope** (`/tasks/{id}`) → `team.posture` (от `task.team_id`).
- **global scope** (config/workflow-instances/library/прочее) → config
  `posture.default` (default `delegated`).

### PostureService (Check + Decide)
- **`Check(method, path, body, apiKey)`** → `(park, reqID, operation, err)`.
  Не sensitive → не паркуем. delegated → не паркуем. **human_led + sensitive →
  park**: создаём `human_request` (pending). **Дедуп**: если уже есть pending на
  ту же (method, path) → возвращаем его (не создаём дубль).
- **`Decide(id, decision, reason, decidedBy)`** → approve/deny. **Только из
  pending** (повтор → **409**); invalid decision → **400**; 404. Атрибуция
  `decided_by` + `decided_at`; audit `human_request.decided`.
- **`SetTeamPosture`** (admin) / **`SetSessionPosture`** (operator+, NULL =
  inherit). 404 team/session; invalid posture → 400.

### Интерцептор (postureMW) + replay
- **`postureMW`** вставляется **между auth и audit** (в `server.go`):
  - GET → пропуск. **`X-Posture-Approved: <id>`** → bypass (replay, one-time:
    `MarkReplayed` approved→replayed; иначе 403).
  - Не sensitive → пропуск. **human_led + sensitive → 202** (park) с
    `{parked, human_request_id, operation, message}`; **не выполняем**.
  - **fail-open**: при ошибке проверки (не RBAC) → log + пропуск (не блокируем).
- **Replay** (`DecideHumanRequest`, approve): реконструирует оригинальный
  запрос (method/path/body + `auth_key` → X-API-Key + `X-Posture-Approved`) и
  прогоняет через **полный chain** (`h.SetChain`). **auth+RBAC выполняются как
  обычно** (bypass только park) → `replay_status` = HTTP-статус replay. **Один
  раз** (MarkReplayed атомарный `WHERE status='approved'`).
- **RBAC не обходится**: replay с viewer-ключом → 403 (viewer не имеет права на
  операцию) — даже если human_request approved.

### Config (M5)
- `posture.default` (section `posture`, StrType, default `delegated`,
  Editable, `requires_restart=false` — читается на момент park/decide) — daemon
  default для global scope.

### Attention (M6)
- Pending `human_requests` → `AttentionItem{Kind:"human_request", Severity:"high"}`
  (source #7 в `AttentionService.List`). Live: EventBus `human_request.parked` /
  `human_request.decided` (channels team:{id} + dashboard).

### RBAC
- `PATCH /teams/{id}/posture` → **config.update** (admin-only, базовый режим
  команды).
- `PATCH /sessions/{id}/posture` → **sessions.update** (operator+).
- `GET /human-requests` + `GET /human-requests/{id}` → **dashboard.read**
  (viewer-ok).
- `POST /human-requests/{id}/decide` → **tasks.update** (operator+).
- **Нет новых пермиссий** (reuse существующих).

### Frontend
- **PosturePanel** (reusable): team (TeamBuilderPage) + session (SessionDetailPage).
  Effective-posture **бейдж** (🤖 Delegated / 👤 Human-led) + radio (team:
  delegated/human_led; session: inherit/delegated/human_led). **Optimistic** update
  (revert при ошибке).
- **Session detail**: `posture` (override) + `effective_posture` + `team_posture`
  в SessionView; `posture` в TeamView.
- **Attention**: pending human_request → строка с кнопками **Approve/Deny**
  (`HumanRequestActions`, reason через prompt) + link.
- **API client**: `posture.setTeam/setSession/getHumanRequest/
  decideHumanRequest/listHumanRequests`.

### Ключевые решения
- **Park до выполнения** (interceptor между auth и audit): sensitive op в
  human-led scope **не выполняется** до approve. delegated — как раньше.
- **Replay через полный chain** (auth+RBAC, bypass только park) → **RBAC не
  обходится**; one-time (MarkReplayed атомарный).
- **Scope по path** (session→team, task→team, team, global): global → config
  `posture.default` (default delegated).
- **Дедуп park**: повторный sensitive-запрос на ту же операцию → тот же
  human_request (не дубль).
- **fail-open** при ошибке проверки (не блокируем при сбое posture-check; RBAC/
  auth не трогаем).

### Тесты
- **Service**: matchSensitive (sensitive/non-sensitive + operations), scope
  resolution (session→team, task→team, global default), Check (delegated no-park,
  human_led park, non-sensitive, dedup), Decide (approve/deny, repeat 409, 404,
  invalid 400).
- **HTTP**: interceptor park (202) + replay (approve→200+replayed+replay_status=
  200, команда заархивирована), delegated no-park (200, сразу), deny (no replay,
  status=denied), **RBAC-not-bypassed** (approved от viewer → replay_status=403,
  команда НЕ изменена).
- **Frontend vitest** (4): team panel (badge + toggle optimistic+persist), session
  inherit→effective=team, human-request approve→replayed, mock setSession
  override+reset.
