# ADR-003: Error model и фаза auth

Status: accepted (2026-10-07)

## Context
Контракт 20/21 не задаёт формат ошибок и не фиксирует auth для UI (06_permissions.md
описывает RBAC/api_keys/audit как отдельный слой).

## Decision
- Единый error envelope: `{"error": {code, message, request_id, details?}}`
  (подробности в `docs/contracts/error-model.md`). Коды: validation_failed(400),
  unauthorized(401), forbidden(403), not_found(404), conflict(409), internal(500).
- Auth по фазам:
  - сейчас: выключен по умолчанию (локальная разработка, сервис не публикуем);
  - `DAEMON_API_KEYS` (список ключей) включает middleware: `X-API-Key: <key>`
    или `Authorization: Bearer <key>` (формат frontend) → 401 без ключа;
  - RBAC/roles/audit — slice 6 по 06_permissions.md.
- Каждый ответ несёт `X-Request-Id` (из запроса или сгенерированный).

## Frontend impact
- Клиенту нужен разбор `error.code` + отображение `message`; заголовок
  `X-Request-Id` — для отладки.
- При включении auth клиент отправляет `X-API-Key`.
