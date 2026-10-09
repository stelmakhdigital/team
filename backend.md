# backend.md — роль бекенда + статус задачи (снимок)

> Снимок бекенд-агента, 2026-10-08 (~21:00, после R5) + 2026-10-08 (срез 8 DELETE workflows). Файл для восстановления работы:
> дочитать целиком перед продолжением. «Живые» статусы (могут обновлять лид/фронт —
> перечитывать перед правкой!): `_workspace/backend-status.md`,
> `_workspace/integration-status.md`, `_workspace/blockers.md`,
> `docs/contracts/api-decisions.md`, `docs/decisions/adr-*.md`.

## 1. Роль и правила
- Я — **backend-инженер проекта "team"**: Go-демон (модуль `daemon`) — оркестрация
  мультиагентных команд: team builder, tasks, sessions/runtime, messaging, WS,
  workflows, library, history/audit/metrics, security/RBAC, live-метрики сессий.
- **Зона ответственности**: `backend/**` + `agents/**` (fixture agent-spec'и)
  + `docs/architecture/backend.md`, `docs/contracts/**`, `docs/decisions/**`
  + `_workspace/*.md` (статусы). `frontend/**` — зона лида/фронта (тouched по явному
  требованию пользователя, как было со slice 7 frontend-частью).
- **Контракт (source of truth)**: `docs/architecture/frontend/20_contract_API.md`
  + `21_team_builder.md`. ТЗ backend: `docs/architecture/backend/00..10*.md`,
  сводка `docs/architecture/backend.md`. Решения: ADR-001..004 +
  `docs/contracts/api-decisions.md`.
- **Связь**: `answer_backend.md` → входящее от лида; мой ответ — `answer_frontend.md`
  (заменять целиком). Статусы — `_workspace/*.md`.
- **Коммиты**: по умолчанию — за пользователем/лидом; пользователь может поручить
  коммитить/пушить напрямую (так делалось 2026-10-08).

## 2. Среда
- Проект: `/home/arkalaust/CODE/PROJECTS/team/`. Git: `origin` = github
  (stelmakhdigital/team), ветка `master`.
- Go: `~/sdk/go/bin/go` (в PATH нет — всегда `export PATH=$PATH:~/sdk/go/bin`).
  Root/sudo нет, локально нет postgres/docker.
- Стек: stdlib `net/http` (без фреймворков, ADR-001); SQLite по умолчанию
  (`modernc.org/sqlite`, pure Go) + PostgreSQL опционально по DSN
  (`jackc/pgx/v5/stdlib` через драйвер `pgx-rewrite`); `gorilla/websocket` (WS);
  `gopkg.in/yaml.v3`.
- **Live-демон**: `127.0.0.1:8080`, DB `sqlite:/tmp/daemon-slice7.db`,
  log `/tmp/daemon-slice7.log`, auth ВКЛЮЧЁН. Запуск:
  ```
  cd backend && go build -o bin/daemon ./cmd/daemon && pkill -x daemon; sleep 0.5
  nohup env DAEMON_DB_DSN=sqlite:/tmp/daemon-slice7.db DAEMON_HOST=127.0.0.1 \
    DAEMON_PORT=8080 DAEMON_API_KEYS=env-live-key \
    DAEMON_AGENT_SPECS_DIR=/home/arkalaust/CODE/PROJECTS/team/agents \
    DAEMON_SECRET_KEY=<64hex> ./bin/daemon > /tmp/daemon-slice7.log 2>&1 &
  ```
  **Важно**: `DAEMON_AGENT_SPECS_DIR` — абсолютный путь (default `agents` — от cwd;
  при cwd=backend/ create-role с `agent_spec` даёт 404). `pkill -x daemon`
  (НЕ `pgrep/pkill -f` — ловит собственный shell, exit 143).
- Ключи live (локальные dev, в answer_frontend.md): admin `env-live-key` (env);
  itest-viewer/itest-operator — DB-ключи (создавать: `DAEMON_DB_DSN=... ./bin/daemon
  admin keys create --name=... --role viewer|operator --user=<уникальный user>`).
  Ключ показывает один раз (в БД — sha256).

## 3. Статус задачи (на момент снимка)
**Основная вертикаль + редизайн — DONE.** Всё закоммичено и запушено; slice 7
закрыт лидом фактами (pong 201, 88/88 против :8080).

| Срез | Статус |
|---|---|
| 1 Team Builder (CRUD, topology, validate, specs, I1–I4, layout) | DONE |
| 2 Tasks & History (CRUD/transitions/handoff, dashboard summary+tasks) | DONE |
| 3 Sessions & Runtime (lifecycle, reaper, watchdog, adapters process/tmux/pi, transcript) | DONE |
| 4 Messaging (messages direct/broadcast/segment, chatrooms авто-создание, EventBus) | DONE |
| 5a WS `/ws` (subscribe channels, ping/pong) + Workflows CRUD | DONE |
| 5b Library (все 4 типа apply) + Audit + Metrics + transcript `total` | DONE |
| 6 Security/RBAC (admin/operator/viewer, api_keys DB + CLI, secrets AES-256-GCM, unread per user) | DONE |
| PG-опция (B3, ADR-004): драйвер `pgx-rewrite` (`?`→`$N`) + `daemon migrate pg` | DONE (runtime+инструмент; e2e — ждёт PG-окружения) |
| 7 Live-метрики сессий + WS `session.output` (live-терминал) | DONE (контракт 20 §3.6/§4.3, closed лидом) |
| 8 DELETE workflows (контракт 20 §2.6–2.8, R6.4) | DONE (каскад связи→блоки→workflow в tx; RBAC `workflows.update`; 404; live + интеграционный `slice 5c`) |
| 9 OpenAPI-спека (весь API) | DONE (openapi.yaml 3.0.3, 57 ops 1-в-1; `scripts/check_openapi.py`) |
| U4 Backend: Config subsystem (Settings, layered resolution, overrides, RBAC) | DONE (backend; openapi 60 ops) |
| Фронтенд F1–F15 + UI-редизайн R1–R5 (граф React Flow, edit, YAML, токены, R4 live-терминал, R5 рефакторинг) | DONE (зона лида) |

### Осталось (не блокирует)
1. **PG e2e** — ждёт PG-окружения (docker/сервер). Gate (ADR-004 п.3):
   `go test ./...` с PG-DSN + live-свип интеграционных против PG-демона +
   roundtrip `daemon migrate pg`. Код и unit-тесты готовы.
   (NOTE 2026-10-08: срез 8 DELETE workflows уже сделан — frontend больше не ждёт backend-среза R6.4.)
2. **OpenAPI-спека** (весь API) — **DONE (2026-10-08)**: `docs/contracts/openapi.yaml`
   (OpenAPI 3.0.3, 57 операций 1-в-1 с `server.go`) + автосверка `scripts/check_openapi.py`. 
3. **Prometheus `/metrics`** — по требованию.
4. Опциональное (вне контракта, по запросу): WS `ack subscribe`, security events
   (ТЗ 06 §3.2 — brute-force), password-логин (users.password_hash есть, API логина нет).

## 3b. U4 — Config subsystem (backend, 2026-10-09)

Подсистема конфигурации по контракту `.pi/pipeline/T-20261009005500/config-contract.md`
(ориентир — OpenRIG). Frontend-часть (Settings UI) — зона лида.

- **Layered resolution** (приоритет low→high): `default < file < env < override`.
  - **file**: опциональный YAML (путь из `DAEMON_CONFIG_FILE`, по умолчанию
    `daemon.yaml` в cwd; формат `секция.ключ`, 2 уровня; неизвестные ключи игнорятся).
  - **env**: `DAEMON_*` (снапшот на старте — демон грузит конфиг на старте).
  - **override**: таблица `config_overrides` (миграция 0008, sqlite+postgres),
    выставляется через `PUT /api/v1/config/{key}`, сброс `DELETE /api/v1/config/{key}`.
  - Каждый ключ: `source` (победивший слой) + `source_detail` (env var / файл).
- **API**: `GET /api/v1/config` → `{meta, sections, environment}`; `PUT/DELETE
  /api/v1/config/{key}` (тип-валидация по `type` → 400; secret/readonly → 403;
  unknown → 404; `needs_restart` = новое ≠ текущего эффективного).
- **RBAC**: `config.read` (GET; admin+operator+viewer; миграция 0008), `config.update`
  (PUT/DELETE; **admin-only**). ВАЖНО: `config.update` НЕ выдаём operator — та же
  permission защищает `PATCH /roles/{id}/config` (slice 6), и выдача operator изменила бы
  RBAC slice 6 (operator смог бы редактировать role config). Поэтому U4 daemon-config
  write = admin-only (по существующей модели).
- **Meta**: project/instance/version/uptime/git_commit/git_branch/started_at +
  `config_hash` = `sha256:` над отсортированным JSON эффективных значений.
- **Ключевые файлы**: `internal/config/{config.go,filelayer.go}`,
  `internal/service/config_service.go` (реестр + resolution + meta),
  `internal/repository/config_repo.go` (`Stores.ConfigOverrides`),
  `internal/api/http/config_handlers.go`, миграции `0008_config_overrides.sql`.
- **Тесты**: `service` — `TestConfigRegistryCompleteness`,
  `TestConfigResolution{Default,FileLayer,EnvLayer,OverrideLayer}`,
  `TestConfigSetOverrideValidation`, `TestConfigSetOverrideForbiddenAndNotFound`,
  `TestConfigMetaAndHash`, `TestConfigEnvironmentSection`; `http` —
  `TestConfigListHTTP`, `TestConfigSetResetHTTP`, `TestConfigRBAC`.
- **Докс**: `docs/contracts/api-decisions.md` (секция U4), openapi.yaml
  (+3 операции, 60 всего; `scripts/check_openapi.py` PASS).

### Таблица ключей (полный реестр — 20 ключей, 7 секций)

| key | type | default | env-слой | restart | editable |
|---|---|---|---|---|---|
| server.listen_addr | string | `:8080` | DAEMON_LISTEN_ADDR | yes | yes |
| server.read_timeout | duration | `15s` | — (константа) | yes | no |
| server.write_timeout | duration | `30s` | — (константа) | yes | no |
| server.shutdown_timeout | duration | `10s` | — (константа) | yes | no |
| storage.dsn | string | `sqlite:./daemon.db` | DAEMON_DB_DSN | yes | yes |
| storage.migrate | boolean | `true` | DAEMON_MIGRATE | yes | yes |
| sessions.poll_interval | duration | `5s` | DAEMON_SESSION_POLL_SECS | yes | yes |
| sessions.configs_dir | path | `configs/sessions` | DAEMON_SESSION_CONFIGS_DIR | yes | yes |
| watchdog.enabled | boolean | `true` | DAEMON_WATCHDOG_ENABLED | yes | yes |
| watchdog.scan_interval | duration | `30s` | DAEMON_WATCHDOG_SCAN_SECS | yes | yes |
| watchdog.stale_after | duration | `2h` | DAEMON_WATCHDOG_STALE_SECS | yes | yes |
| watchdog.blocked_after | duration | `1h` | DAEMON_WATCHDOG_BLOCKED_SECS | yes | yes |
| security.api_keys | string-list | `[]` | DAEMON_API_KEYS | yes | no (secret) |
| security.secret_key | string | `""` | DAEMON_SECRET_KEY | yes | no (secret) |
| security.agent_specs_dir | path | `agents` | DAEMON_AGENT_SPECS_DIR | yes | yes |
| logging.level | string | `info` | DAEMON_LOG_LEVEL | yes | yes |
| workspace.root | path | cwd (**derived**) | DAEMON_WORKSPACE_ROOT | yes | yes |
| workspace.artifacts_dir | path | `workspace.root/artifacts` (**derived**) | DAEMON_WORKSPACE_ARTIFACTS | yes | yes |
| workspace.screenshots_dir | path | `workspace.root/screenshots` (**derived**) | DAEMON_WORKSPACE_SCREENSHOTS | yes | yes |
| workspace.logs_dir | path | `logs/sessions` (contract: derived) | DAEMON_LOGS_DIR | yes | yes |

Слои для каждого ключа: default (таблица) → file (`daemon.yaml`)
→ env (см. колонку) → override (БД). Env-длительности — в **секундах**
(конвенция daemon); в file/override — `Go duration` ("45s") или секунды.

### 3c. U3-rename (round 2, 2026-10-09): PATCH segment/workflow/library
Переименование (name + опц. description) для безопасных сущностей — доводит U3 до
«все сущности редактируемы в UI».

| Эндпоинт | RBAC permission | Актеры |
|---|---|---|
| `PATCH /api/v1/segments/{id}` | `teams.update` | admin, operator |
| `PATCH /api/v1/workflows/{id}` | `workflows.update` | admin, operator |
| `PATCH /api/v1/library/{id}` | **`library.create`** (нет `library.update` в модели) | admin, operator |

Body `{name (required), description?}`; response `{id, name, status: "updated"}`;
`updated_at` обновляется. Ошибки: 400 (пустое имя), 404 (нет id), 409 (дубликат имени —
toлько segment, unique `(team_id,name)`). description omit → текущее сохраняется.
Код: `SegmentRepo.UpdateMeta` / `WorkflowRepo.UpdateName` / `LibraryRepo.UpdateName` +
`TeamService.UpdateSegmentMeta` / `WorkflowService.UpdateWorkflow` / `LibraryService.Update`
+ 3 handler'а; `requiredPermission` — special-case `PATCH /library/{id}` → `library.create`.
OpenAPI: +3 операции (всего 63); `check_openapi.py` PASS.
Тесты: 3 service-unit + `TestRenameEndpointsRBAC` (HTTP: 200 operator / 403 viewer /
404 unknown / 409 dup).

**Role/task — read-only (обоснование, не «нужен эндпоинт»)**: role-name = ключ
`agent_spec` + внешние ссылки (chatroom/relatives/session/alert) → rename ломает связи
(каскад, out of scope); task title/priority задаются при create, изменения в полёте
не предусмотрены контрактом. Зафиксировано в `api-decisions.md` (U3-rename) + `frontend.md` §3b.

## 4. Как проверить
```
cd backend && export PATH=$PATH:~/sdk/go/bin
gofmt -l . && go vet ./... && go test -count=1 ./...   # http + service + database
go build -o bin/daemon ./cmd/daemon
```
Live-проверки: `curl -s -H "X-API-Key: env-live-key" http://127.0.0.1:8080/...`
(ответы pipe в `python3 -c "import json,sys; ..."`). WS-проверки — маскированным
клиентом (gorilla требует mask у клиентских фреймов) или через фронтенд-тесты.
Интеграционный набор фронта (20 тестов): `cd frontend && INTEGRATION_BASE_URL=...
INTEGRATION_API_KEY=env-live-key INTEGRATION_VIEWER_KEY=... INTEGRATION_OPERATOR_KEY=...
npx vitest run`. Анти-цикл: один чистый прогон в конце, не гонять без изменений.

## 5. Ключевые места кода
- `cmd/daemon/main.go` — wiring (все сервисы + EventBus + Options).
- `cmd/daemon/admin.go` — CLI `daemon admin keys create|list|revoke`.
- `cmd/daemon/migrate.go` — CLI `daemon migrate pg`.
- `internal/api/http/server.go` — роуты + цепочка (mux → auditMW → authMW →
  loggingMW → recoveryMW → requestIDMW); `middleware.go` — `authMW` + RBAC
  (`requiredPermission(method,path)`), `statusRecorder` **с Hijack()** (WS!).
- `internal/api/http/ws_handler.go` — WS pump'ы (readPump: subscribe; writePump:
  EventBus → фильтр каналов → WriteJSON; ping 30s).
- `internal/api/http/{message,workflow,library,session}_handlers.go`,
  `audit_middleware.go`, `handlers.go` — endpoint'ы.
- `internal/service/eventbus.go` — `WSEvent{type,data,timestamp,channels(json:"-")}`,
  `newEvent` (авто-дублирование на `dashboard`), non-blocking (буфер 64).
- `internal/service/session_service.go` — lifecycle (start/stop/reap), `SessionView`
  (+live-поля), `enrichLiveMetrics` hook в viewsFor.
- `internal/service/session_live.go` — live-метрики: `sessionLogPath`,
  `sessionModel` (pi-конфиг), парсинг JSONL `usage` (хвост ≤256KB), window override,
  pct cap 100.
- `internal/service/session_tailer.go` — тейлер transcript-лога для `session.output`
  (батчи ≤500ms, только при новых строках; truncate/rotate; лимиты 1MB/тик,
  500 строк/событие, 4000 символов/строку).
- `internal/service/auth_service.go` — AuthService (env→admin; DB→user/role/perms;
  CreateKey/ListKeys/RevokeKey; **существующий user с другой ролью → conflict**).
- `internal/service/secrets.go` — SecretsManager (AES-256-GCM, `DAEMON_SECRET_KEY`).
- `internal/service/{task,message,watchdog,workflow,library,metrics,audit}_service.go`
  — точки `Bus.Publish(newEvent(..., channels...))`.
- `internal/service/team_service.go` — `MergeSpec`, `applySpec`, I1–I4,
  `resolveAgentSpecPath` (без расширения: `pi-worker` → `pi-worker.yaml`).
- `internal/database/` — `db.go` (Open: sqlite | postgres→`pgx-rewrite`),
  `pgx_rewriter.go` (`?`→`$N`), `migrate_pg.go` (`MigratePG`: схема + все таблицы,
  FK-порядок, batch, cast'ы timestamptz), `migrations/{sqlite,postgres}/0001..0007`.
- `internal/repository/*` + `store.go`; `internal/models/*` (security.go —
  User/APIKey/AuthContext/Secret).
- `internal/service/config_service.go` — U4: реестр ключей, layered resolution
  (default<file<env<override), SetOverride/ResetOverride, meta+config_hash.
- `internal/config/{config.go,filelayer.go}` — Load + workspace-ключи +
  file-слой (YAML, DAEMON_CONFIG_FILE).
- Ключевые env: `DAEMON_LISTEN_ADDR`/`DAEMON_HOST`+`DAEMON_PORT`, `DAEMON_DB_DSN`,
  `DAEMON_API_KEYS`, **`DAEMON_AGENT_SPECS_DIR` (абсолютный!)**, `DAEMON_LOGS_DIR`,
  `DAEMON_SESSION_CONFIGS_DIR`, `DAEMON_WATCHDOG_*`, `DAEMON_SESSION_POLL_SECS`,
  `DAEMON_SECRET_KEY`, `DAEMON_CONFIG_FILE` (file-слой), `DAEMON_WORKSPACE_*` (U4).

## 6. Тесты (регресс-якоря)
- Slice 1: `TestSlice1LayoutRegressions`, `TestValidateTopologyEmpty`,
  `TestSaveTopologyUnknownNodes`, `TestCreateRoleMissingSpec`.
- Slice 3: `TestSessionFailedProcessAndWatchdog`, `TestSessionCrashThenUserStop`.
- Slice 4: `message_service_test.go`, `TestMessageCenterVertical`.
- Slice 5a: `TestWSSubscribeAndEvents` (wsCollector-паттерн!), `TestWSAuth`,
  `TestWorkflowFull/Validation`, `TestWorkflowVertical`.
- Slice 5b: `TestLibraryVertical`, `TestLibraryWorkflowApply`,
  `TestLibraryRoleSegmentApply`, `TestAuditAndMetrics`.
- Slice 6: `security_service_test.go` (auth/roles/expired/revoked/secrets/unread),
  `TestRBACViewerForbidden`, **`TestCreateKeyExistingUserRole`** (регресс: role
  inheritance при общем --user).
- Slice 7: `session_live_test.go` (usage-парсинг, model из pi-конфига, window,
  cap, `readNewLogLines` partial/truncate/idle), `TestSessionOutputEvents`
  (батчи/каналы/тишина), `TestSessionLiveMetricsView/NoUsage`; HTTP:
  `session_live_handlers_test.go` (`TestSessionLiveFieldsHTTP`,
  `TestSessionOutputWSHTTP`).
- PG: `internal/database/{pgx_rewriter,migrate_pg}_test.go`.
- Setup'ы: `newTestServer`/`newRBACTestServer` (http, все сервисы + bus + auth),
  `setupSecurityEnv`/`setupSessionEnv` (service), `specBody()` (http, команда
  "dev-team" — уникальное имя на тест-файл!).

## 7. Решения (не менять без ADR/договорённости)
- Stdlib HTTP без фреймворков (ADR-001); SQLite default / PG опционально
  (ADR-002 + ADR-004: rewriter принят, PG e2e — при окружении); error envelope
  + dual auth (ADR-003).
- Error: `{"error":{code,message,request_id,details}}`; коды
  validation_failed/unauthorized/forbidden/not_found/conflict/internal.
- WS: publish неблокирующий (буфер 64) — медленный подписчик теряет события;
  REST = источник истины; `dashboard` — глобальный канал; event до subscribe
  теряется (ack нет — задокументировано).
- **Slice 7 честность**: context-поля SessionDetail — OMIT (не 0) если рантайм
  не отдаёт usage (TUI/ANSI не содержит); окно контекста = `config.context_window`
  иначе 200000; тейлер in-memory (после рестарта демона live-терминал не
  возобновляется — зафиксировано, не баг); `session.output` — каналы
  `session:{id}`+`dashboard` (контракт) + `team:{id}` (суперсет).
- Auth: env-ключ = admin (без user, audit `operator:<4hex>`); DB-ключ — user+роль;
  ключи в БД — только sha256; `admin keys create` для существующего user с другой
  ролью → явный conflict (не молчаливая подмена роли).
- RBAC-маппинг: segments/roles/relatives → `teams.*` (у под-ресурсов своих
  permissions нет); `GET /chatrooms/{id}/messages` помечает чат прочитанным.
- Library apply: team — new/merge (идемпотентно); workflow — target обязателен;
  segment — merge по имени; role — приоритет сегмента overrides.segment_id →
  overrides.segment → снапшот → единственный → general; дубли → 409.
- Metrics: llm_tokens = 0 (LLM-агентов нет). OpenAPI — конец проекта.
  Prometheus `/metrics` — по требованию.

## 8. Питфолы (уроки сессий)
- `pgrep/pkill -f "bin/daemon"` ловит собственный shell (exit 143) —
  `pgrep/pkill -x daemon`.
- gorilla/websocket: после read-таймаута conn «corrupt» (все reads ошибаются) —
  в тестах один reader-goroutine (`wsCollector`); клиентские фреймы — маскировать.
- Любой middleware-обёртка ResponseWriter для WS должна реализовывать `Hijacker`.
- `_workspace/*.md` и `docs/contracts/*.md` переписывают параллельно —
  **всегда перечитывать перед правкой**; большие изменения — только `edit`
  (точечные замены) или append через python; полный `write` — после полного чтения.
- Транзакции/БД: sqlite `SetMaxOpenConns(1)` в тестах; `IsUniqueViolation` для 409.
- Пересозданная БД переиспользует session id — старые transcript-логи
  (`backend/logs/sessions/*.log`) дают ложные context-суммы; чистить при сбросе.
- Фронтенд/лид могут пользоваться тем же daemon'ом (:8080) — команды IT-* и
  probe-* в БД; перед live-проверками сверять `GET /teams`; RBAC-тесты фронты
  используют `firstTeamWithRoles()` (не `teams[0]`).
- httptest: WS-URL = `"ws://"+srv.Listener.Addr().String()+"/ws"`; `specBody()` —
  уникальное имя команды на тест-файл.
- `git add -A` подхватывает мусор (deb, bin) — проверять `git status` перед коммитом.

## 9. Формат ответа (чек-лист нового слайса)
1. `gofmt -l .` пусто, `go vet ./...` чисто, `go test -count=1 ./...` PASS.
2. Build `bin/daemon`, live-проверка ключевых путей curl'ем (+ WS при необходимости).
3. Обновить: `_workspace/backend-status.md`, `_workspace/integration-status.md`,
   `docs/contracts/api-decisions.md` (append-секция), `backend/README.md`,
   `docs/architecture/backend.md` (статус слайса).
4. Отчёт в `answer_frontend.md` (заменить целиком: что готово, endpoints,
   frontend impact, валидация, файлы, следующий шаг).
5. Обновить этот снапшет (backend.md) при смене роли/слайса или статусе задачи.
