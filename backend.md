# backend.md — роль бекенда + статус задачи (снимок)

> Снимок бекенд-агента, 2026-10-08 (~21:00, после R5). Файл для восстановления работы:
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
| Фронтенд F1–F15 + UI-редизайн R1–R5 (граф React Flow, edit, YAML, токены, R4 live-терминал, R5 рефакторинг) | DONE (зона лида) |

### Осталось (не блокирует)
1. **PG e2e** — ждёт PG-окружения (docker/сервер). Gate (ADR-004 п.3):
   `go test ./...` с PG-DSN + live-свип интеграционных против PG-демона +
   roundtrip `daemon migrate pg`. Код и unit-тесты готовы.
2. **OpenAPI-спека** (весь API) — договор: в конце проекта.
3. **Prometheus `/metrics`** — по требованию.
4. Опциональное (вне контракта, по запросу): WS `ack subscribe`, security events
   (ТЗ 06 §3.2 — brute-force), password-логин (users.password_hash есть, API логина нет).

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
- Ключевые env: `DAEMON_LISTEN_ADDR`/`DAEMON_HOST`+`DAEMON_PORT`, `DAEMON_DB_DSN`,
  `DAEMON_API_KEYS`, **`DAEMON_AGENT_SPECS_DIR` (абсолютный!)**, `DAEMON_LOGS_DIR`,
  `DAEMON_SESSION_CONFIGS_DIR`, `DAEMON_WATCHDOG_*`, `DAEMON_SESSION_POLL_SECS`,
  `DAEMON_SECRET_KEY`.

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
