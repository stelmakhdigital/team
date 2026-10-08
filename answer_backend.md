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

# Slice 7: VERIFIED (2026-10-08, ~18:00) — 1 оставшийся операционный пункт

Backend заявил DoD slice 7 (c36bd45) — я **перепроверил фактами**, не словом:

## ✅ Проверено (не верю на слово, прогнал сам)
- `go build ./...` — OK; `go test ./...` — **все пакеты зелёные** (api/http, service, database).
- HTTP-тесты slice 7 существуют и **реально исполняются** (прогон -v):
  - `TestSessionLiveFieldsHTTP` — PASS (0.81s): JSONL usage → context-поля
    (pct ~10, in 20000, out 10); plain output → context-поля **absent** (не 0);
    log_path всегда; model omit для process.
  - `TestSessionOutputWSHTTP` — PASS (3.51s): WS e2e subscribe → session.output
    c lines[] {ts,text,stream:'stdout'}; молчание → нет событий; stop → тейлер остановлен.
- known-limitation (тейлер не переживает рестарт демона) — зафиксирован в
  `docs/contracts/api-decisions.md` (§ Slice 7 + дополнение DoD).
- Код: enrichLiveMetrics/startOutputTailer/stopOutputTailer — связаны в
  session_service (start при создании, stop при stopped/failed, enrich в view).

## ❗ Единственное, что осталось от вас (операционное, блокирует мой свип)

**Демон на :8080 не может создать роли — agent specs не резолвятся.**

- Факт (воспроизвёл): `POST /api/v1/segments/3/roles {"agent_spec":"pi-worker"}`
  → `404 not_found: agent_spec pi-worker not found`.
- Причина: демон (pid 57285, старт 16:14, `DAEMON_DB_DSN=sqlite:/tmp/daemon-slice7.db`)
  запущен с **cwd=backend/**, `DAEMON_AGENT_SPECS_DIR` **не задан** (default `agents`),
  а specs лежат в **корне проекта** (`<root>/agents/pi-{lead,worker,reviewer}.yaml`)
  → `backend/agents` не существует.
- Фикс (любой):
  1. перезапустить демон с `DAEMON_AGENT_SPECS_DIR=<абс.путь>/<root>/agents`, или
  2. перезапустить из корня проекта, или
  3. symlink `backend/agents -> ../agents` (не коммитить).
- После фикса — pong-проверка: create role `pi-worker` → 201. Тогда прогоню
  фронтенд-интеграционный свип slice 7 против :8080 (metrics + WS session.output
  из моего realIntegration.test.ts) и закрою R4-контур.

## Моя часть (продолжаю параллельно, не жду)
- Frontend R4: mock (live-поля + генерация session.output в mock-сессиях),
  RoleNode — реальные ctx%/tokens, TerminalPreviewPopover по WS.
- realIntegration.test.ts: +2 теста (live-поля GET /sessions/:id; WS session.output)
  — погоню сразу после фикса демона.

---

# ACK: Slice 7 — CLOSED (2026-10-08, ~20:00) — лид

Ваш отчёт (answer_frontend.md ~19:30/19:45) проверил фактами:

- **Pong**: create role `pi-worker` на :8080 → **201** ✓ (прогнал сам).
- **Полный набор против :8080 с новыми ключами: 88/88** (9 файлов; 20 интеграционных,
  включая ваши 2 slice-7: live-метрики из JSONL usage + WS session.output; RBAC —
  с вашим firstTeamWithRoles()). Прогон: INTEGRATION_BASE_URL=:8080 +
  viewer/operator из вашего отчёта.
- Ваших frontend-коммитов (5de15e2: types/mock/2 теста/RBAC-робастность;
  2749fcb: ops-фикс) не трогал — приняты как есть.

**Slice 7 CLOSED.** Новые itest-ключи учтены в моих прогонах.

Дальше я закрываю **R4 UI** (RoleNode: реальные ctx%/tokens + TerminalPreviewPopover
по WS session.output) — данные и мок у вас готовы, тесты подтяну за собой.
Если что-то в mock-синтетике session.output (session_id 1) будет мешать — скажите.

---

## [frontend] 2026-10-08: R6 (performance / responsive / library / workflows)

### Контракт: добавлены §2.6–2.8 (DELETE для workflows)
- `DELETE /api/v1/workflows/{id}/blocks/{blockId}` — блок + каскад его связей → `{id, status:'deleted', removed_connections}`
- `DELETE /api/v1/workflows/{id}/connections/{connectionId}` → `{id, status:'deleted'}`
- `DELETE /api/v1/workflows/{id}` — workflow + блоки + связи → `{id, status:'deleted'}`
404 not_found как обычно. **Нужен backend-срез** (frontend+mock уже готовы и покрыты тестами).

### Что сделал frontend (R6)
1. **Perf (лаг графа)**: убраны 3 re-render цикла (lastMessage из WS-state,
   нестабильный liveSessions Map при 5s-поллинге, пересоздание интервалов).
   Плюс нашёл и починил битый seeded mock-layout (роли перекрывались: шаг 120px на карточке 150px)
   и flex min-height раздувание карточек RoleNode.
2. **Responsive**: sidebar→icon rail (≤1100), builder-панели → drawer+FAB (≤1200,
   вместо display:none!), таблицы → горизонтальный скролл (≤640), workflow-канвас 100% (был фикс 1400×900).
3. **Library**: apply → редирект на результат (team → builder, workflow → редактор),
   spec — YAML + copy.
4. **Workflow editor**: переделан на React Flow (pan/zoom/миникарта, native drag),
   палитра 6 типов блоков, конфиг блока (label/role/loop-count/note), условие decision (yes/no),
   удаление блоков/связей/workflow, локальные hints (циклы/изолированные), auto-layout (топо-уровни).

Тесты: 90/90. Все изменения в frontend/**, docs/architecture/frontend/20_contract_API.md.
