# Backend status

## Current phase
implementation (slice 1 done — Team Builder + интеграционные правки по frontend; slice 2 — next)

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
- Role address вычисляется сервером: `team:segment.role`.
- Layout хранится в `config.layout` (контракт 21).

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
- `go test ./...` — PASS (service: 13 тестов; http: вертикаль Team Builder,
  error format, auth, health).
- `go vet ./...`, `gofmt -l .`, `go build ./...` — чисто.
- Smoke-тест живого daemon (curl create team с spec → get → validate → 404) — OK.

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
- OpenAPI spec — будет в slice 2.
- См. `_workspace/blockers.md`.

## Next step
- Slice 2: queue_tasks + history_status: `POST/GET /api/v1/tasks`,
  `PATCH /tasks/{id}/state`, `POST /tasks/{id}/handoff`, `GET /tasks/{id}/history`,
  `GET /api/v1/dashboard/summary` + `/dashboard/tasks`; OpenAPI-спецификация.
