# Backend (daemon)

Go-сервис управления мультиагентными командами. Slice 1: Team Builder
(teams / segments / roles / relatives, topology, validate, layout, role config).
Slice 2: Tasks & History (queue, lifecycle, handoff, dashboard).
Slice 3: Sessions & Runtime (process/tmux/pi адаптеры, lifecycle+reaper, watchdog alerts, transcripts).

Архитектура: `docs/architecture/backend.md` · Контракт: `docs/contracts/api-decisions.md`,
`docs/architecture/frontend/20_contract_API.md` (source of truth).

## Запуск

```bash
export PATH=$PATH:~/sdk/go/bin   # Go 1.24 (установлен в ~/sdk/go)

go build -o bin/daemon ./cmd/daemon
DAEMON_DB_DSN=sqlite:./daemon.db ./bin/daemon
# → http://localhost:8080
```

Конфигурация (env):

| Env | Default | Описание |
|---|---|---|
| `DAEMON_LISTEN_ADDR` | `:8080` | HTTP listen |
| `DAEMON_DB_DSN` | `sqlite:./daemon.db` | `sqlite:<path>` или `postgres://user:pass@host:5432/db` |
| `DAEMON_API_KEYS` | — | список API-ключей через запятую (включает auth `X-API-Key`) |
| `DAEMON_LOG_LEVEL` | `info` | debug/info/warn/error |
| `DAEMON_AGENT_SPECS_DIR` | `agents` | корень чтения agent.yaml для GET /roles/{id}/config |
| `DAEMON_MIGRATE` | `true` | применить миграции при старте |
| `DAEMON_SESSION_POLL_SECS` | `5` | период проверки живости сессий |
| `DAEMON_WATCHDOG_ENABLED` | `true` | включить watchdog loop |
| `DAEMON_WATCHDOG_SCAN_SECS` | `30` | период скана watchdog |
| `DAEMON_WATCHDOG_STALE_SECS` | `7200` | порог stale (in_progress без обновлений) |
| `DAEMON_WATCHDOG_BLOCKED_SECS` | `3600` | порог blocked |
| `DAEMON_LOGS_DIR` | `logs/sessions` | transcript-файлы сессий |
| `DAEMON_SESSION_CONFIGS_DIR` | `configs/sessions` | конфиги pi-сессий |
| `DAEMON_SECRET_KEY` | — | 64 hex-символа (32 байта); AES-256-GCM ключ для `secrets` (slice 6; не задан — выключен) |

> **Важно (agent specs):** `DAEMON_AGENT_SPECS_DIR` по умолчанию — относительный `agents`
> (от cwd демона). Если демон запущен из `backend/`, а specs лежат в корне проекта, —
> create-role с `agent_spec` даст 404 (I4: файл не найден). Запускайте либо из корня
> проекта, либо с `DAEMON_AGENT_SPECS_DIR=<абс.путь>/<root>/agents` (рекомендуется;
> так работает live-демон :8080).

## Agent specs

`agent_spec` роли — путь к yaml-файлу (формат — `internal/service/agentspec.go`:
`name`, `pi_config`, `resources`, `startup`). Поиск: как `<path>` от cwd
и `DAEMON_AGENT_SPECS_DIR/<path>`; при отсутствии файла:
- `POST /segments/{id}/roles` → 404 not_found;
- `GET /roles/{id}/config` → stub (`available: false`).

В репо есть фикстуры: `../agents/pi-lead.yaml`, `pi-worker.yaml`, `pi-reviewer.yaml`
(относительно `backend/` — это каталог `agents/` в корне проекта).

Поднять daemon со своим specs-каталогом:

```bash
DAEMON_AGENT_SPECS_DIR=/abs/path/to/my-agents ./bin/daemon
# и agent_spec: "my-agent.yaml" в createRole → ищется /abs/path/to/my-agents/my-agent.yaml
```

## Команды

```bash
gofmt -l .
go test ./...
go vet ./...
go build ./...
```

## Postgres (опционально)

SQLite — дефолт. PostgreSQL — опциональная цель (ADR-002, B3):

```bash
# 1) скопировать данные из текущей sqlite-БД в пустой postgres
./bin/daemon migrate pg --to postgres://user:pass@host:5432/db [--from sqlite:/path/to.db] [--force]

# 2) переключить daemon
DAEMON_DB_DSN=postgres://user:pass@host:5432/db ./bin/daemon
```

- Репозитории написаны с sqlite-плейсхолдерами `?`; для PG зарегистрирован драйвер
  `pgx-rewrite` (автотрансляция `?` → `$1..$N`, `internal/database/pgx_rewriter.go`).
- `migrate pg` применяет postgres-миграции схемы и копирует все таблицы
  (FK-порядок, batch, cast'ы timestamptz); повтор — только с `--force` (TRUNCATE).
- Oграничение: e2e-проверка требует живого PG-сервера; локально (без PG) PG-DSN не используется.

## Security (slice 6)

- **Auth** включается, если заданы `DAEMON_API_KEYS` **или** в БД есть api_keys.
  Ключ: `X-API-Key` / `Authorization: Bearer` / `?api_key=` (WS). Нет права → 403 `forbidden`.
- **RBAC-роли**: `admin` (все права), `operator` (без `config.update`), `viewer` (read-only).
  Env-ключи — admin без user; ключи из БД — привязаны к user (audit: user_id/api_key_id).
- **Ключи из БД** (CLI, показываются один раз, в БД — sha256):

```bash
./bin/daemon admin keys create --name=frontend --role operator [--user=fe] [--expires=720h]
./bin/daemon admin keys list
./bin/daemon admin keys revoke <id>
```

- **Secrets** (AES-256-GCM, таблица `secrets`): service `SecretsManager`
  (Store/Get/List/Delete); HTTP-эндпоинтов нет (нет в контракте).
- **unread_count** (chatrooms): реальный для user (DB-ключ); `GET /chatrooms/{id}/messages`
  помечает чат прочитанным. Подробности: `docs/contracts/api-decisions.md` (Slice 6).

## Sessions live-метрики и live-терминал (slice 7)

- **`SessionDetail` += опциональные live-поля** (additive, omit = «неизвестно» → UI «--»):
  `model` (только pi, из конфига сессии), `context_used_percentage`, `context_total_input_tokens`,
  `context_total_output_tokens` (парсинг JSONL `usage` из transcript-лога; ТUI-вывод usage не содержит → omit),
  `log_path` (всегда). Подробности: `docs/contracts/api-decisions.md` (Slice 7).
- **WS `session.output`**: live-терминал — тейлер transcript-лога, батчи ≤500ms, только при новых
  строках. Каналы `session:<id>` + `team:<id>` + `dashboard`.

## Endpoints (slice 1, /api/v1)

- `GET/POST /teams`, `GET/DELETE /teams/{id}` (DELETE = archive)
- `GET /teams/{id}/topology`, `POST /teams/{id}/validate`, `POST /teams/{id}/save`
- `POST /teams/{id}/segments`, `POST /teams/{id}/relatives`
- `POST /segments/{id}/roles`
- `GET/PATCH /roles/{id}/config`, `PATCH /segments/{id}/layout`,
  `PATCH /roles/{id}/layout`, `PATCH /relatives/{id}/layout`
- `DELETE /relatives/{id}`
- `GET /healthz`, `GET /readyz`

## Endpoints (slice 5b, /api/v1)

- `GET /library?type=&group=&search=&limit=&offset=` — `{items, total, groups}`
- `POST /library` — снапшот в библиотеку (`type`, `source_id`, `name`, ...) → `{id, status:'saved', library_item_id}`
- `GET /library/{id}` — `{item, spec, versions}`
- `POST /library/{id}/apply` — `{target_team_id?, overrides?}` → `{status:'applied'|'merged', created_resources?, updated_resources?}`
- `GET /audit?user_id=&action=&resource=&start_time=&end_time=&limit=&offset=` — `{entries, total}`
- `GET /dashboard/metrics?range=1h|24h|7d` — `{time_range, metrics{tasks_created, tasks_completed, sessions_active, queue_size, llm_tokens}}` (12 точек)

`POST /teams/{id}/save` с `save_to_library: true` теперь возвращает `library_item_id`.
`GET /sessions/{id}/transcript` — теперь с `total`.

## Endpoints (slice 5, /api/v1)

- `GET /workflows?team_id=&state=` — список + total
- `GET /workflows/{id}` — workflow + blocks + connections
- `POST /workflows` — создать (`team_id`, `name`, `description?`, `blocks?`, `connections?`; в body connections = индексы blocks)
- `POST /workflows/{id}/blocks` — добавить блок (`type`, `position{x,y}`, `config?`, `label?`)
- `POST /workflows/{id}/connections` — связь (`from_block_id`, `to_block_id` — реальные id, `condition?`)
- `PATCH /workflows/{id}/blocks/{blockId}` — drag&drop/config → `changes {position?, config?} {old,new}`
- `GET /ws` — WebSocket: `{"type":"subscribe","channels":["team:1","dashboard",...]}` →
  `{type, data, timestamp}` по каналам (task./session./message./alert.-события + глобальный
  `dashboard`); auth: `?api_key=` (при включённых ключих)

Block types: task|decision|parallel|loop|agent|manual. Workflow states: draft|active|archived.

## Endpoints (slice 4, /api/v1)

- `GET /messages?team_id=&queue_task_id=&from_role_id=&to_role_id=&type=&limit=&offset=` — список + `total`/`has_more`
- `POST /messages` — отправить (`team_id`, `type` direct|broadcast|segment, `to_role_id` для direct/segment, `body`, опц. `from_role_id`, `queue_task_id?`) → `{id, status:'sent', delivered_to}`
- `GET /chatrooms?team_id=` — список чатов (last_message, members_count, unread_count)
- `GET /chatrooms/{id}/messages?limit=&offset=` — лента чата (ASC) + `has_more`
- `POST /chatrooms/{id}/messages` — отправить в чат (`body`, опц. `from_role_id`)

Chatrooms создаются автоматически (team-level при создании команды, `<segment>-general`
при создании сегмента). Оператор (без роли): `from_role_id` NULL → `from_role_name="You"`, `is_mine=true`.
`type system|watchdog` — серверные (API → 400). EventBus: task./session./message./alert.-события
(WS `/ws` — slice 5).

## Endpoints (slice 3, /api/v1)

- `POST /sessions?team_id={id}` — создать/запустить (`role_id`, `command`+`args?`,
  `runtime_type?` = process|tmux|pi, `queue_task_id?`, `working_dir?`, `config?`)
- `GET /sessions?team_id=&role_id=&state=&active=true` — список + total
- `GET /sessions/{id}` — сессия (team_name, role_name, uptime_seconds, exit_code)
- `DELETE /sessions/{id}` — stop (идемпотентно для stopped)
- `GET /sessions/{id}/history` — история переходов (ASC) + total
- `GET /sessions/{id}/transcript?limit=200` — последние строки вывода (stdout+stderr)
- `GET /dashboard/sessions` — активные сессии
- `GET /dashboard/alerts?team_id=` — алерты watchdog + total
- `GET /watchdog/events` — то же (ТЗ 01), `POST /watchdog/events/{id}/read` — mark read

Runtime-адаптеры: `process` (локальный процесс, SIGTERM), `tmux` (отдельная tmux-сессия),
`pi` (pi CLI в tmux + сгенерированный конфиг `configs/sessions/<id>.yaml`).
`container`/`k8s` → 400 (вне сборки, финальный проект).
Reaper (каждые `DAEMON_SESSION_POLL_SECS`, default 5s): exited → stopped/failed (+exit_code).
Watchdog (каждые `DAEMON_WATCHDOG_SCAN_SECS`, default 30s): stale/blocked задачи,
drift по failed-сессиям; дедупликация по непрочитанным алертам.

## Endpoints (slice 2, /api/v1)

- `GET /tasks?team_id=&state=&destination_role_id=&limit=&offset=` — список + total
- `POST /tasks` — создать (`team_id`, `destination_role_id`, `title`, `body?`, `parent_task_id?`, `source_role_id?`, `priority?`)
- `GET /tasks/{id}` — задача + subtasks
- `PATCH /tasks/{id}/state` — переход (`state`, `closure_reason` для `done`, `comment?`)
- `POST /tasks/{id}/handoff` — передать (`to_role_id`): закрыть исходную + создать новую
- `GET /tasks/{id}/history` — история переходов (ASC) + total
- `GET /dashboard/summary` — сводка (sessions/alerts = 0 до slice 3)
- `GET /dashboard/tasks` — активные задачи (pending/in_progress/blocked) с `is_stale`/`is_blocked`

Переходы: `pending → in_progress|done|blocked|canceled`; `in_progress → done|blocked|canceled`;
`blocked → pending|in_progress|done|canceled`. Из `done`/`canceled` — 409.
`done` требует `closure_reason`: `handed_off_to|blocked_on|denied|canceled|no_follow_on|escalation`.
Родитель закрывается автоматически (`no_follow_on`), когда все subtasks завершены.
`is_stale` = in_progress старше 2ч; `is_blocked` = state blocked.

Ошибки — единый envelope `{error: {code, message, request_id, details?}}`
(`docs/contracts/error-model.md`). Ответы несут `X-Request-Id`.
