# Ответ backend → лид (2026-10-08, ~18:30) — DoD slice 7 закрыт

## 1. ✅ HTTP-интеграционные тесты — готовы (`internal/api/http/session_live_handlers_test.go`)

- **`TestSessionLiveFieldsHTTP`**:
  - сессия с JSONL usage → `GET /sessions/:id`: `context_used_percentage ≈ 10`
    (20000/200000), `context_total_input_tokens = 20000`, `context_total_output_tokens = 10`;
  - сессия с обычным выводом (TUI-подобный, без usage) → context-поля **отсутствуют
    в JSON** (assert absence, не 0);
  - `log_path` — присутствует в обоих; `model` — omit для process (pi-ветка покрыта
    unit `TestSessionModelFromPiConfig` — реальный pi в HTTP-тесте не поднимаем).
- **`TestSessionOutputWSHTTP`** (WS e2e на httptest-сервере):
  - connect → `subscribe ['session:{id}', 'dashboard']` → сессия пишет строки →
    `session.output` c `lines[] = {ts ISO, text, stream:'stdout'}` (assert session_id,
    role_name, ts, stream, timestamp);
  - **молчание → тишина**: 1.3s без новых строк → 0 session.output;
  - **stop → тейлер остановлен**: после `DELETE /sessions/{id}` — 0 session.output.
- `wsCollector` дополнен `snapshot()` (безопасный скан событий).
- `go test -count=1 ./...` — PASS (http 4.5s + service + database).

## 2. ✅ :8080 — на новом бинаре (master)

Пересобрал и перезапустил (бинарь из кода master, включая фикс CreateKey ниже).
Текущее окружение:
- daemon: `127.0.0.1:8080`, DB `/tmp/daemon-slice7.db` (свежая), log `/tmp/daemon-slice7.log`
- `INTEGRATION_API_KEY` = `env-live-key` (admin)
- **ВНИМАНИЕ: БД была пересоздана — старые itest-ключи недействительны.** Новые:
  - viewer: `sk_a2cec42fb98906866e9cce9730e729b409cb67ff72b456b0`
  - operator: `sk_fdeddd04a882e6d3c4fba271d665e9b556b0ba249b1bb9ec`
- Live-проверено: viewer `POST /teams` → 403, operator `POST /teams` → 201.

## 3. ✅ Ограничение зафиксировано в доке

`docs/contracts/api-decisions.md` (Slice 7, дополнение): тейлер в памяти —
после рестарта демона live-терминал у выживших сессий не возобновляется
(runtime-процессы всё равно теряют привязку к реестру; REST/transcript не страдают).
Контракт 20 §4.3 дополнен: канал `team:{id}` — суперсет (lента активности команды).

## Бонус: найден и закоммичен регресс в `admin keys create` (6de6261)

Ваш F14-прогон случайно обнажил баг: `INSERT INTO users ... ON CONFLICT (username)
DO NOTHING` — второй ключ для **того же `--user`** с другой ролью молча наследовал
роль первого ключа (operator-ключ → viewer-права → 403 на POST /teams). Поймал его,
когда пересоздавал itest-ключи с общим `--user=fe-it`. Фикс: существующий user с
другой ролью → явная ошибка `user "X" already has role "Y" (requested "Z")`;
та же роль → ок. Регресс-тест `TestCreateKeyExistingUserRole`, live-проверено.

## Статус slice 7: **DONE** (DoD закрыт). Жду ваш фронтенд-свип против :8080.
