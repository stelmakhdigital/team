# Ответ лид → backend (2026-10-08, ~11:05)

## ✅ Slice 4–5: интеграционные тесты frontend готовы, всё зелёное

`frontend/tests/realIntegration.test.ts`: **7 → 15 тестов** (45/45 с unit-тестами),
прогонено против вашего WIP-бинаря (slice 4–5, до slice 6):

- messages: direct (delivered_to=[role]) / broadcast (все роли), `system` → 400,
  list + фильтры team_id (формы: is_mine, to_role_name);
- chatrooms: авто-создание (team-level + `<segment>-general`), send + list (is_mine, "You");
- workflows: create ± blocks/connections (индексы 0-based в POST /workflows → реальные
  id), create block/connection (реальные id), PATCH block (changes.position/config old/new),
  list + фильтр team_id;
- library: save → 409 unique (type,name) → list (+groups) → get (spec segments/roles/relatives
  + versions) → apply без target (new team, overrides.name, downloads_count++) →
  apply c target (merged); `save_to_library` в `POST /teams/{id}/save` → `library_item_id`;
  workflow-apply: без target → 400, повтор в ту же команду → 409;
- audit: запись `library.save` после действия, фильтр `action`, формы
  (timestamp/user_name/ip_address);
- metrics: 12 точек × 5 серий для range 1h/24h/7d;
- WS `/ws`: subscribe `dashboard` → `task.created` при создании задачи (формат
  `{type, data{task_id, title, state}, timestamp}` — по контракту).

Ничего ломать в frontend для slice 4–5 не пришлось, кроме двух добавлений в фасад
(контракт уже предусматривал): `Api.library.applyLibrary` (POST /library/{id}/apply)
и `Api.dashboard.getMetrics({range})`.

## 🟡 Наблюдения (non-blocking, учесть в slice 6+/следующем прогоне)

1. **`GET /messages`: `from_role_name` для оператора omitempty** (отсутствует в JSON),
   тогда как chatroom-сообщения дают `"You"`. Контракт §4.2 ожидает `from_role_name?`
   — формально ок (optional), но для консистентности: заполнять `"You"` и там, либо
   зафиксировать в api-decisions. Приоритет низкий (уведёт RBAC-user model).
2. **`save_to_library` в `POST /teams/{id}/save`**: имя item = `"team-<team name>"`
   (не задокументировано в контракте 20 §5.2/5.3). Либо задокументировать как уточнение,
   либо принимать `name` из request. Учту в контракте как уточнение, если вы подтвердите.
3. **Chatroom `last_message`** (контракт §4.2, optional) — в live-ответах не возвращается
   даже после отправки сообщения. Nice-to-have.
4. **WS**: подтвердите, что событие, опубликованное до обработки `subscribe`, теряется
   (нет ack) — я задокументировал это в `docs/architecture/integration.md` §4
   (клиенты должны идемпотентно рефетчить при reconnect).

## 📄 Контракт 20: обновлён (лид)

- §3.5 `GET /dashboard/metrics?range=1h|24h|7d` (default 24h; всегда 12 точек) —
  зафиксирован параметр (у вас уже работает).
- Написан `docs/architecture/integration.md` (моя открытая обязанность, blockers B1).

## 🔧 Инфраструктура интеграционных тестов

- Тесты теперь принимают `INTEGRATION_BASE_URL` (default `http://localhost:8080`) и
  `INTEGRATION_API_KEY` (если daemon с `DAEMON_API_KEYS`). Порт можно свободно делить:
  я гонял свои тесты на :8081, не трогая ваш демон на :8080.
- Slice 6: когда будете готовы, прогоню 15 интеграционных тестов с DB-ключом
  (admin) и добавлю ветки RBAC (viewer → 403 `forbidden` на POST, operator → 403
  на config.update) — скажите, когда (и какой ключ/порт).

## Отчёт

- frontend: 45/45 (30 unit + 15 integration), typecheck OK, build OK (81.9 KB gzip).
- Статусы обновлены: `_workspace/{frontend,integration}-status.md`, `frontend.md`.
