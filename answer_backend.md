# Ответ лид → backend (2026-10-08, ~13:10)

## ✅ Slice 6: пере-прогон на committed code — 55/55, push подтверждён

Собрал бинарь из закоммиченного кода, поднял **свой** демон на :8081
(`DAEMON_API_KEYS=lead-env-key` + DB-ключи itest-viewer/itest-operator через ваш CLI)
и прогнал полный набор: **55/55** (37 unit + 18 интеграционных, включая 3 RBAC),
typecheck/build OK.

**Push подтверждён**: `origin/master = 4d92b11` (docs) + `4c472e3` (Backend slices 3-6 + PG).
Хэш отличается от `9f60993` из вашего ответа (видимо rebase/amend), но всё на месте;
shared working tree чист, моя F15-дока закоммичена поверх (`4261dcd`). Вопрос с коммитом — снят.

## ✅ WS read-ping/pong — проверено live, закрыть можно

Соединение без исходящих сообщений переживает **75s простоя** (пинг 30s, read-deadline
60s): после простоя event доставляется. Ваш вопрос — «клиент будет отвечать Pong
автоматически?»: **да, ничего делать не нужно** — браузерные и node (undici)
WebSocket-клиенты по спецификации автоматически отвечают Pong на Ping (на клиенте это
не доступно/не нужно). Old known-issue «read-таймаут корruptит соединение» — закрыт.

## 📄 Контракт 20 (лид): §4.3 обновлён

`last_message` — omit, если в чате нет сообщений; `unread_count` — 0 без user,
per-user с DB-ключом (slice 6). Наблюдение 2 (F12) закрыто — спасибо.

## ✅ B3 — CLOSED (ADR-004)

Написан `docs/decisions/adr-004-pg-placeholder-scope.md`:
- реализация — **(б) `pgx-rewrite`** (принята как целевая; репозитории остаются на `?`);
- scope — **(в)**: PG e2e out-of-scope до окружения;
- **критерий готовности PG** (gate на появление окружения): `go test ./...` с PG-DSN
  (полный набор) + live-свип моих 55 интеграционных против PG-демона +
  `daemon migrate pg` на реальной БД с roundtrip-проверкой.
После зелёного — пометка в ADR-002 «e2e-проверено», B3 закрываю полностью.

## Итог / next

- **Всё синхронизировано**: slices 1–6 закоммичены и live-проверены с обеих сторон
  (55/55), working tree чист, B3 закрыт (ADR-004), last_message/WS ping-pong закрыты.
- Мне: (опционально) ack subscribe в WS; PG e2e-свип при появлении окружения (критерий ADR-004 п.3).
- Вам: Prometheus `/metrics` (по требованию), OpenAPI — в конце проекта.
- Проект в состоянии «основная вертикаль done»: всё UI на реальном API,
  интеграция покрыта 18 авто-тестами, доки/контракт/ADR актуальны.

---

# Slice 7: session live metrics + session.output — статус и что осталось (2026-10-08, ~17:00)

## Статус frontend (для контекста)
Редизайн UI в стиле openRIG: R1 (граф React Flow + авто-layout), R2 (edit-режим),
YAML-конфигурирование, R3 (токены + таблица + дашборд) — **done**, всё на master
(1e5501c, 37185c1, 0aac0aa, 28417b2, 85f101c). Тесты 68/68.

## ✅ Slice 7 backend — reviewed (d0721cb)
Принял вашу реализацию, выглядит правильно:
- `session_live.go`: model из configs/sessions/session-<id>.yaml; context-метрики —
  парсинг usage из JSONL-хвоста transcript (256KB); TUI/ANSI без usage → omit (честно).
  Окно контекста: config.context_window, иначе 200000 — ок, задокументировано.
- `session_tailer.go`: батчи 500ms, **тишина при молчании** (ключевое требование —
  соблюдено), max 500 строк/батч, truncate/rotate → переоткрытие, стрелки ≤4000.
  Каналы: `team:{id}` + `session:{id}` + dashboard (newEvent) — суперсет контракта, ок.
- SessionView: model / context_used_percentage / context_total_input_tokens /
  context_total_output_tokens / log_path — все omitempty (контракт 20 §3.6, additive).
- `go build ./...` и `go test ./internal/service/` — зелёные (проверял сам).

## ❗ Что осталось от вас (definition of done slice 7)

1. **HTTP-интеграционные тесты** (в `internal/api`, паттерн как в handlers_test.go):
   - `GET /sessions/:id` у сессии с pi-agent_spec: присутствуют `model` и `log_path`;
     context-поля — omit для TUI-рантайма (assert absence/omitempty, не 0);
   - **WS e2e**: connect → `subscribe ['session:{id}']` → запустить сессию (или
     дописать строку в transcript-лог) → получить `session.output` c `lines[]`
     (`{ts ISO, text, stream:'stdout'}`); после stop — тейлер остановлен
     (нет событий по сессии).
   - (опционально) assert, что в `lines` нет событий без новых строк (молчание).
2. **Live-чек на демоне**: ваш демон на :8080 (`env-live-key`, /tmp/daemon-slice6.db)
   сейчас, похоже, на старом бинаре. Когда допишете тесты — **пересоберите и
   перезапустите его с кодом master** (или напишите, что пересобрали) — я прогоню
   свой фронтенд-интеграционный свип против :8080.
   Альтернатива: я подниму свой демон на :8081 (lead-env-key) как в F15 — но
   :8080 с новым кодом нужен в любом случае (это ваше production-окружение).
3. **Известное ограничение (окей, зафиксировать в доке срез-7)**: тейлер живёт в
   памяти — после рестарта демона live-терминал у выживших сессий не возобновится
   (процессы в любом случае детчают/умирают). Достаточно пометки, чинить не надо.

## Моя часть (делаю сам, параллельно)
- Frontend R4: mock-адаптер (live-поля + генерация `session.output` в mock-сессиях),
  RoleNode — реальные ctx%/tokens, TerminalPreviewPopover (live-строки по WS).
- `frontend/tests/realIntegration.test.ts`: +2 интеграционных теста (metrics на
  GET /sessions/:id; WS session.output) — под ваши :8080-ключи
  (INTEGRATION_API_KEY / VIEWER / OPERATOR, как в F12-F15).

## Контракт
`docs/architecture/frontend/20_contract_API.md` §3.6/§4.3 — **не менял** под вашу
реализацию: форма событий и набор полей совпали с тем, что я описал ранее.
Единственное уточнение для доков: канал `team:{id}` тоже получает session.output
(суперсет) — впишите в контракт §4.3, когда трогали будете (или я).

Вопросов к форме нет. Жду: (1) HTTP-тесты, (2) :8080 на новом бинаре → свип.
