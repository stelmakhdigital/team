# Backend Architecture

Owner: Backend Engineer
Status: implemented (slice 1) / planned (slices 2+)
Source of truth для API: `docs/architecture/frontend/20_contract_API.md` + `docs/architecture/frontend/21_team_builder.md` (по `agents.md` это единый контракт).

---

## 1. Назначение

Daemon — управляющая система мультиагентных команд: хранение топологий команд
(teams/segments/roles/relatives), очередь задач, сессии агентов (runtime
adapters: container/process/tmux/pi), сообщения, watchdog, observability.

Backend реализован на **Go**, идиоматично:

- контекст первым параметром;
- маленькие интерфейсы, DI через конструкторы;
- бизнес-логика в services, не в HTTP handlers;
- явная обработка ошибок, единая error-модель.

## 2. Технологические решения (см. ADR)

| Решение | Выбранный вариант | ADR |
|---|---|---|
| Язык/фреймворк | Go 1.24, стандартный `net/http` (ServeMux с паттернами Go 1.22+), без web-фреймворка | adr-001 |
| БД по умолчанию | SQLite (чистый Go драйвер `modernc.org/sqlite`, cgo не нужен) | adr-002 |
| БД для production | PostgreSQL (`pgx/v5/stdlib`), выбор через DSN в конфигурации | adr-002 |
| Миграции | numbered SQL-файлы в `internal/database/migrations/{sqlite,postgres}` + таблица `schema_migrations`, применяются при старте | adr-002 |
| Логирование | `log/slog` (JSON) | adr-001 |
| Метрики (slice 2+) | Prometheus (`/metrics`) — запланировано | — |
| Авторизация | Фаза 0: локальное доверие (auth выключен по умолчанию); фаза 1: API-ключи `X-API-Key` (включается конфигом); RBAC/audit — slice 3+ | adr-003 |

## 3. Структура пакетов

```
backend/
├── go.mod                       # module daemon
├── cmd/
│   └── daemon/
│       └── main.go              # wiring: config → db → repos → services → http server
└── internal/
    ├── config/
    │   └── config.go            # env-based config (DAEMON_*)
    ├── database/
    │   ├── db.go                # open sqlite/postgres по DSN, PRAGMA foreign_keys
    │   ├── migrate.go           # embedded SQL-миграции, schema_migrations
    │   └── migrations/
    │       ├── sqlite/0001_init.sql
    │       └── postgres/0001_init.sql
    ├── models/
    │   └── models.go            # доменные типы: Team, Segment, Role, Relative + states
    ├── repository/
    │   ├── store.go             # DBTX, интерфейсы репозиториев, NewStores
    │   ├── team_repo.go
    │   ├── segment_repo.go
    │   ├── role_repo.go
    │   └── relative_repo.go
    ├── service/
    │   ├── errors.go            # AppError (code/status/details), ErrNotFound/Conflict/Validation
    │   ├── team_service.go      # use cases: Team/Segment/Role/Relative, topology, validate
    │   └── topology.go          # валидация топологии: циклы, orphans, agent_spec
    └── api/
        └── http/
            ├── server.go        # NewServer: mux, middleware, routes
            ├── middleware.go    # request id, logging, recovery, auth (X-API-Key)
            ├── response.go      # JSON writer + error envelope
            └── handlers.go      # team builder handlers (dto + mapping)
```

## 4. Слои

```
HTTP handlers (dto, валидация входных данных, status codes)
        ↓
Services (use cases: транзакционные границы, доменные правила, history)
        ↓
Repositories (SQL, CRUD, только данные)
        ↓
database/sql (*sql.DB; sqlite или postgres)
```

Правила:

- handlers НЕ содержат бизнес-логики — только разбор/сериализация и маппинг ошибок;
- services получают репозитории через интерфейсы (тестируемы без БД, но основные тесты идут на SQLite in-memory);
- транзакционная граница = один use case (репозитории принимают `DBTX` — `*sql.DB` или `*sql.Tx`).

## 5. Domain-модель (slice 1: Team Builder)

- **Team** (`teams`): name (unique), description, spec_path, state ∈ {active, archived, stopped}.
- **Segment** (`segments`): group of roles inside a team, unique (team_id, name), `config` JSON (в т.ч. layout).
- **Role** (`roles`): agent seat, unique (team_id, segment_id, name), уникальный `address` = `team:segment.role`, `agent_spec` (путь к agent.yaml), `profile`, `config` JSON (в т.ч. layout), state ∈ {active, inactive, blocked}.
- **Relative** (`relatives`): directed edge between roles, type ∈ {delegates_to, spawned_by, can_observe, collaborates_with}, unique (from, to, type).

Далее (slice 2+): queue_tasks, history_status, sessions, messages, projects/goals,
watchdog_events, security/audit — схема в `docs/architecture/backend/00_database.md`.

## 6. Use cases (slice 1)

| Use case | Endpoint | Замечания |
|---|---|---|
| ListTeams | GET /api/v1/teams | counts segments/roles, limit/offset |
| CreateTeam | POST /api/v1/teams | опциональный `spec` (segments/roles/relatives) — transactional create |
| GetTeam | GET /api/v1/teams/{id} | team + segments + roles + relatives |
| ArchiveTeam | DELETE /api/v1/teams/{id} | state → archived (soft delete) |
| CreateSegment | POST /api/v1/teams/{id}/segments | layout в config |
| CreateRole | POST /api/v1/segments/{id}/roles | адрес вычисляется сервером |
| CreateRelative | POST /api/v1/teams/{id}/relatives | проверка ролей внутри team |
| DeleteRelative | DELETE /api/v1/relatives/{id} | |
| UpdateRoleConfig | PATCH /api/v1/roles/{id}/config | previous/new config в ответе |
| UpdateSegmentLayout / UpdateRoleLayout / UpdateRelativeLayout | PATCH .../layout | merge в config |
| GetTopology | GET /api/v1/teams/{id}/topology | team + entities + layout из config |
| ValidateTopology | POST /api/v1/teams/{id}/validate | errors/warnings: circular, orphans, no agent_spec |

## 7. Error model

Единый envelope (см. `docs/contracts/error-model.md`):

```json
{ "error": { "code": "not_found", "message": "team 42 not found", "request_id": "..." } }
```

| code | HTTP | Когда |
|---|---|---|
| validation_failed | 400 | невалидные body/params, неизвестный state/type |
| unauthorized | 401 | нет/неверный API-ключ (когда auth включён) |
| not_found | 404 | сущность/parent не найдена |
| conflict | 409 | дубликат unique (team name, segment name, role name, relative) |
| internal | 500 | необработанные ошибки (request_id для поиска в логах) |

Конфликты (unique constraint) маппятся в сервисе из ошибок драйвера (sqlite `1555`/postgres `23505`).

## 8. Конфигурация (env)

| Env | Default | Описание |
|---|---|---|
| `DAEMON_LISTEN_ADDR` | `:8080` | HTTP listen |
| `DAEMON_DB_DSN` | `sqlite:./daemon.db` | `sqlite:<path>` или `postgres://user:pass@host:5432/db` |
| `DAEMON_API_KEYS` | (пусто) | список ключей через запятую; пусто = auth выключен |
| `DAEMON_LOG_LEVEL` | `info` | debug/info/warn/error |
| `DAEMON_MIGRATE` | `true` | применять миграции при старте |

## 9. Testing strategy

- Unit + integration на SQLite in-memory (`file::memory:`): repositories, services (включая транзакции и конфликты).
- API-тесты на `httptest`: полная вертикаль Team Builder (создание команды с spec → чтение → валидация), error-формат, 404/409/400.
- Команды: `go test ./backend/...`, `go vet ./backend/...`, `gofmt -l backend`, `go build ./backend/...`.

## 10. Observability

- `slog` JSON-логи: request_id, method, path, status, duration для каждого запроса.
- Append-only история (history_status, session_history) — slice 2 (ТЗ 09/10).
- Prometheus `/metrics` — slice 3 (ТЗ 07).
- WebSocket events — slice 3 (контракт §WebSocket).

## 11. Migration strategy

Numbered SQL-файлы, применяются идемпотентно при старте (таблица `schema_migrations`).
Два набора диалектов: `migrations/sqlite/`, `migrations/postgres/` — одна семантика,
разный DDL. Никаких ручных изменений схемы в коде.

## 12. План реализации

- [x] **Slice 1 — Team Builder (текущий):** БД (sqlite/postgres, миграции),
  teams/segments/roles/relatives CRUD, topology, validate, error model,
  middleware, auth-hook, тесты, API-док.
- [x] **Slice 2 — Tasks & History (завершён 2026-10-07):** queue_tasks, history_status
  (миграция 0002), POST/GET/PATCH tasks, handoff, auto-close parent,
  task history, dashboard/summary+tasks. Тесты: lifecycle, transitions, handoff,
  dashboard, HTTP roundtrip (~20 тестов).
- [ ] **Slice 3 — Sessions & Runtime:** sessions + runtime adapters
  (process/tmux/pi, container позже), session lifecycle, watchdog events, alerts,
  dashboard/sessions+alerts, Prometheus `/metrics`.
- [ ] **Slice 4 — Messaging:** messages, chatrooms, message center.
- [ ] **Slice 5 — Workflows, Library, History Viewer, WebSocket:** workflows/blocks/connections,
  library save/apply, audit + transcripts, WS events.
- [ ] **Slice 6 — Security:** RBAC, api_keys users, audit_log, secrets (ТЗ 06).

## 13. Known risks / ограничения

1. `docs/contracts/**` отсутствовали — созданы `error-model.md` + `api-decisions.md`
   (рабочая копия из `20_contract_API.md`). OpenAPI — по договорённости с Lead
   будет сгенерирован в конце проекта (весь API), не в slice 2.
2. `docs/architecture/integration.md` отсутствует (за Lead) — зафиксировано в blockers.
3. Контракт не задавал error-схему — предложена (ADR-003), frontend должен
   синхронизировать (влияние: парсинг ошибок).
4. SQLite по умолчанию — для локальной разработки; concurrent writes при
   production-нагрузке — только PostgreSQL (WAL уже включён).
5. Auth выключен по умолчанию — до slice 6 API локальный (не публикуем наружу).
6. Layout хранится в `config` JSON — контракт сохраняет совместимость,
   но при росте требований к layout может понадобиться отдельная таблица.

## 14. Предположения

- A1: `DELETE /api/v1/teams/{id}` = archive (по 01_daemon.md), не hard delete.
- A2: цикличность топологии проверяется по всем типам relatives (контракт не уточняет).
- A3: `total` в списках = количество по filter'у без limit/offset.
- A4: даты — RFC3339 UTC (ISO8601), ids — JSON number (int64).
