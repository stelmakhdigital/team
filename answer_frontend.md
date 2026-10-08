# Ответ backend → лид (2026-10-08, ~15:40) — Slice 7 (live-метрики сессий) готов

## Коротко: форма `session.output` и поля `SessionDetail` — ок, реализованы как в контракте

Ваш вопрос «ок по форме?» — **да, без изменений**. Реализовано 1:1 по обновлённому
контракту 20 §3.6/§4.3, live-проверено на :8080. WS-подход (не отдельный
`GET /metrics`) оставлен — он уже есть в шине, и live-терминал по REST-поллингу
был бы хуже.

## Что сделано (коммит в origin/master — см. `git log`)

**1. `SessionDetail` += опциональные live-поля** (`internal/service/session_live.go`):
- `log_path` — всегда (`logs/sessions/session-<id>.log`);
- `model` — только `runtime_type=pi`, из конфига сессии
  (`configs/sessions/session-<id>.yaml`); для process/tmux — omit;
- `context_total_input_tokens` — Σ(input_tokens + cache_read_input_tokens) по всем
  JSONL-записям `usage` в transcript-логе;
- `context_total_output_tokens` — Σ(output_tokens);
- `context_used_percentage` — последняя запись: (input+cache_read)/окно*100,
  окно = `config.context_window`, иначе 200000 (допущение задокументировано).

**Честность (важно)**: ТUI-вывод pi (ANSI) usage НЕ содержит → context-поля **omit
(не 0!)**, UI покажет «--». Заполняются, когда рантайм пишет JSONL с usage
(формат pi JSONL `{"message":{"usage":{...}}}` / `{"usage":{...}}`). Всё additive —
existing-клиенты не ломаются.

**2. WS `session.output`** (`internal/service/session_tailer.go`):
- тейлер transcript-лога, батчи **≤500ms**, событие **только при новых строках**
  (молчание → тишина);
- каналы `session:<id>` + `team:<id>` + `dashboard`;
- `lines[] = {ts, text, stream:"stdout"}` — stdout+stderr мержены в один лог
  (адаптеры пишут `2>&1`), поэтому stream всегда "stdout"; ts — время батча
  (per-line ts из файла недоступен — задокументировано);
- лимиты: 1MB/тик, 500 строк/событие, 4000 символов/строка;
- тейлер живёт от start до stopped/failed → после stop тишина.

## Live-проверка (127.0.0.1:8080, свежий бинарь)
- WS (маскированный клиент, подписка `session:<id>`+`dashboard`): 5 батчей
  `lines=2/1/1/1/1` (live-1..live-6), `session_id`/`role_name`/`stream` корректны.
- `GET /sessions/{id}`: `log_path` есть; plain-sh → context-поля **отсутствуют**
  (omit); сессия с JSONL-usage → `context_used_percentage: 30`,
  `context_total_input_tokens: 60000`, `context_total_output_tokens: 300`
  (50000+10000 cache / 200000 = 30% ✓).

## Тесты
`go test -count=1 ./...` — PASS (http + service + database).
- `session_live_test.go`: usage-парсинг (JSONL top-level и message.usage),
  window-override, cap 100%, model из pi-конфига (pi/absent/process),
  `readNewLogLines` (partial строка, truncate/rotate, idle).
- `TestSessionOutputEvents`: батчи, каналы session+dashboard, stream/ts, тишина после stop.
- `TestSessionLiveMetricsView`, `TestSessionLiveMetricsNoUsage`: full path через GetSessionView.

## Доки
- `docs/contracts/api-decisions.md` — раздел «Slice 7: live-метрики сессий + session.output»
  (семантика полей, честность omit, лимиты, ts-семантика).
- `backend/README.md` — раздел «Sessions live-метрики и live-терминал (slice 7)».
- `_workspace/backend-status.md`, снапшет `backend.md` — обновлены.

## Для R4 (frontend)
Слоты «--» можно заменять реальными данными: подписка на `session:{id}` уже даёт
`session.output` (live-терминал), `GET /sessions` отдаёт context%/tokens. Если
рантайм usage не отдаёт — поля отсутсвуют (не 0), UI-логика «--» остаётся.
Вопрос по форме закрыт, можно строить R4 поверх.
