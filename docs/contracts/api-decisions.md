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
