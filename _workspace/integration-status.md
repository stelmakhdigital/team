# Integration status

Таблица синхронизации backend/frontend (обновляют оба агента; источник контракта —
`docs/architecture/frontend/20_contract_API.md` + `21_team_builder.md`).

| Функция | Backend | Frontend | Контракт | Интеграция | Проблемы |
|---|---|---|---|---|---|
| Team Builder: teams CRUD | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: segments/roles/relatives CRUD | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: layout (PATCH) | done | done (mock+real) | ready | **done** (real) | previous_layout — плоский и неверный old (blockers #8) |
| Team Builder: topology + validate | done (исправлено лидом) | done (mock+real) | ready | **done** (real) | layout.relatives пока пусто (frontend не зависит) |
| Team Builder: role config (GET/PATCH) | done | done (mock+real) | ready | **done** (real) | — |
| Team Builder: save (POST /teams/{id}/save) | done | done (mock+real) | ready | **done** (real) | save_to_library — slice 5 |
| Error model (все endpoint) | done | done (flex-parse + validation_failed) | changed | **done** (real) | — |
| Tasks & History | planned (slice 2) | done (mock) | draft | blocked | backend slice 2 |
| Dashboard | planned (slice 2) | done (mock) | draft | blocked | backend slice 2 |
| Sessions (runtime) | planned (slice 3) | done (mock) | draft | blocked | slice 3 |
| Message Center | planned (slice 4) | done (mock) | draft | blocked | slice 4 |
| Workflows / Library / History Viewer / WS | planned (slice 5) | done (mock) | draft | blocked | нет `GET /workflows` в контракте (blockers #3) |

## Integration verification (lead, 2026-10-07)

Team Builder вертикаль проверена **на живом backend** (frontend real API client,
`tests/realIntegration.test.ts`, 3 теста): create team+spec → topology
(snake_case, layout с width/height) → validate → save; role config;
error envelope (404 not_found, 409 conflict). Все зелёные.

Режим работы frontend: `VITE_API_MODE=real` + `VITE_API_BASE_URL=http://localhost:8080`
→ Team Builder и teams-list работают на реальном API; остальные экраны — на mocks
(см. `frontend/.env`).

Backend endpoint slice 1 (все под `/api/v1`):
`GET/POST /teams` · `GET/DELETE /teams/{id}` · `GET /teams/{id}/topology` ·
`POST /teams/{id}/validate` · `POST /teams/{id}/save` · `POST /teams/{id}/segments` ·
`POST /teams/{id}/relatives` · `POST /segments/{id}/roles` · `PATCH /segments/{id}/layout` ·
`GET/PATCH /roles/{id}/config` · `PATCH /roles/{id}/layout` · `PATCH /relatives/{id}/layout` ·
`DELETE /relatives/{id}` · `GET /healthz` · `GET /readyz`

## API change log

### 2026-10-07 — lead: интеграционные фиксы backend (Team Builder unblock)
- `GET /teams/{id}/topology`: была сериализация raw-моделей (PascalCase, без
  `roles_count`/`segment_name`/`from_role_name`/`to_role_name`/счётчиков команды).
  Добавлен contract-вью `topologyView` в `internal/api/http/handlers.go`
  (snake_case + поля контракта + segment layout width/height). Тест расширен.
- `LoadAgentSpec` stub: nil-слайсы → пустые срезы (JSON `[]` вместо `null`
  для `plugins`/`skills`/`hooks`/`mcp.servers`/`startup`).
- Frontend: auth заголовок `X-API-Key` (вместо Bearer, решение blockers #9);
  code `validation_failed` в friendly-error map; интеграционные тесты real client.
- Validation: backend `go test ./...` OK; frontend `vitest` 28/28 (вкл. 3 интеграционных); build OK.

### 2026-10-07 — error model (backend)
- Endpoint: все
- Change: единый error envelope `{"error": {code, message, request_id, details?}}`
- Frontend impact: парсинг по `error.code` — реализован (flex-parse)
- Validation: интеграционные тесты frontend против живого backend

### 2026-10-07 — уточнения Team Builder (backend, без смены форматов)
- DELETE team = archive (200, повтор → 409); POST create → 201; layout в config,
  возвращается в create-ответах/topology/PATCH layout
- Spec relatives: `from`/`to` в формате `"segment.role"` (контракт 20 описывает
  `from_role`/`to_role`) — принято как уточнение, контракт 20 нужно обновить (blockers #8)
