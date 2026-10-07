# Error Model (API v1)

Все ошибки API возвращаются в едином формате:

```json
{
  "error": {
    "code": "not_found",
    "message": "human-readable description",
    "request_id": "9f1c2b3a",
    "details": { "field": "name", "reason": "required" }
  }
}
```

- `code` — стабильный машинный код (snake_case), frontend ветвит по `code`.
- `message` — человекочитаемое, НЕ для парсинга.
- `request_id` — совпадает с заголовком `X-Request-Id` ответа, для поиска в логах.
- `details` — опциональный объект (валидация: список проблем).

## Коды и статусы

| code | HTTP | Значение |
|---|---|---|
| `validation_failed` | 400 | невалидный request body/query/path param |
| `unauthorized` | 401 | отсутствует или неверный API-ключ (когда auth включён) |
| `forbidden` | 403 | нет прав (slice 6, RBAC) |
| `not_found` | 404 | ресурс или parent не существует |
| `conflict` | 409 | нарушение unique-ограничения (имя, адрес, дубль связи) |
| `internal` | 500 | непредвиденная ошибка сервера |

## Примеры

404:
```json
{ "error": { "code": "not_found", "message": "team 42 not found", "request_id": "..." } }
```

409:
```json
{ "error": { "code": "conflict", "message": "segment 'backend' already exists in team 1", "request_id": "..." } }
```

400:
```json
{ "error": { "code": "validation_failed", "message": "invalid request",
  "request_id": "...",
  "details": { "errors": [ { "field": "name", "reason": "required" } ] } } }
```
