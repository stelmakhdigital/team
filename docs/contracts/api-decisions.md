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
