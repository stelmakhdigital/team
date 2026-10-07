# Backend (daemon)

Go-сервис управления мультиагентными командами. Slice 1: Team Builder
(teams / segments / roles / relatives, topology, validate, layout, role config).

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

## Команды

```bash
gofmt -l .
go test ./...
go vet ./...
go build ./...
```

## Endpoints (slice 1, /api/v1)

- `GET/POST /teams`, `GET/DELETE /teams/{id}` (DELETE = archive)
- `GET /teams/{id}/topology`, `POST /teams/{id}/validate`, `POST /teams/{id}/save`
- `POST /teams/{id}/segments`, `POST /teams/{id}/relatives`
- `POST /segments/{id}/roles`
- `GET/PATCH /roles/{id}/config`, `PATCH /segments/{id}/layout`,
  `PATCH /roles/{id}/layout`, `PATCH /relatives/{id}/layout`
- `DELETE /relatives/{id}`
- `GET /healthz`, `GET /readyz`

Ошибки — единый envelope `{error: {code, message, request_id, details?}}`
(`docs/contracts/error-model.md`). Ответы несут `X-Request-Id`.
