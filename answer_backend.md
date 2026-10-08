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

# Новый срез (2026-10-08, ~15:00) — UI-редизайн: live-данные сессий (контракт R4)

Пользователь принял **редизайн UI в стиле openRIG** (топология — центр; деталь —
`_workspace/openrig-ui-research.md`, план — `_workspace/ui-redesign-plan.md`).
Frontend R1 (граф на React Flow: авто-layout, live-карточки ролей) — уже в worktree
(см. `_workspace/ui-redesign-plan.md` F1–R1). Дальше фазы R2–R5.

## Что прошу от backend — «live-метрики сессий» (отдельный срез, не блокирует R2/R3)

Контракт 20 §3.6 я **уже обновил** (см. diff `docs/architecture/frontend/20_contract_API.md`):

1. **`SessionDetail` += опциональные live-поля** (omit/null = неизвестно → UI покажет «--»):
   - `model?: string`
   - `context_used_percentage?: number` (0..100)
   - `context_total_input_tokens?: number`
   - `context_total_output_tokens?: number`
   - `log_path?: string`
2. **WS-событие `session.output`** (live-терминал): батчи строк лога, batch ≤ 500ms,
   каналы `session:{id}` и `dashboard`. Форма (контракт §4.3):
   ```
   { session_id: number, role_name?: string,
     lines: [{ ts, text, stream: 'stdout'|'stderr'|'log' }] }
   ```
   Источником строк — tail файла лога сессии (у нас transcript = строки лога файла).
   `subscribe`-канал `session:{id}` уже есть.

### Требования/критерии
- Опциональность: если рантайм не отдаёт context%/tokens — **omit**, не 0 (UI: «--»).
  Не ломать existing-клиентов (новые поля additive).
- `session.output` не должен заваливать канал при молчании сессии (батч-интервал ≤500ms,
  только при новых строках).
- Mock-режим frontend я покрываю сам (принцип «mock не отстаёт») — мне нужен только
  реальный контракт + интеграционные тесты.

### Порядок
- R2 (edit-режим графа) и R3 (визуальная консистентность) я делаю **параллельно**,
  не жду этот срез — слоты «--» уже на карточках.
- После вашего среза — R4 в UI (реальные context%/tokens + live-терминал popover).

Вопрос: ок по форме `session.output` и набору полей `SessionDetail`? Если хотите
по-другому (например, отдельный `GET /sessions/:id/metrics` вместо WS) — скажите,
контракт поправлю. Контракт — источник истины, меняю как лид, но согласовываю с вами.
