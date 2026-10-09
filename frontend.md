# Frontend — роль и статус (точка восстановления сессии)

> Файл для восстановления работы. **Последнее обновление: 2026-10-08 (после R6.4).**
> Рабочая зона: `frontend/**`, контракт `docs/architecture/frontend/20_contract_API.md`
> + `21_team_builder.md` (владею как лид), `docs/architecture/integration.md`,
> статус-файлы `_workspace/*`, письма `answer_backend.md` (мне пишет бекенд в `answer_frontend.md`).
> **Роль: frontend-инженер + lead-интегратор** (меняю любые файлы для интеграции,
> backend-зону `backend/**` не трогаю — задачи бекенду отдаю в `answer_backend.md`).

## 1. Стек и конвенции
- Vite 5 + React 18 + TypeScript (strict) + react-router-dom v6 + vitest/jsdom.
- **Runtime-зависимости (добавлены редизайном, согласовано с пользователем):**
  `@xyflow/react` (React Flow v12 — топология и workflow-канвасы) + `yaml` (TeamSpec).
- Контракт — единственный источник истины: `docs/architecture/frontend/20_contract_API.md`
  (+ `21_team_builder.md`). Mirror типов: `src/types/api.ts`.
- API-фасад `src/api/index.ts`; адаптеры `src/api/mock/adapter.ts` (in-memory, latency,
  error-simulator) и `src/api/real.ts`. UI знает только `Api`.
- Режимы: `VITE_API_MODE=mock|real` (default mock). real: same-origin, vite-прокси
  `/api`,`/ws`,`/healthz` → `BACKEND_URL` (default :8080). Auth: `X-API-Key`
  (`VITE_API_KEY`); WS-auth: `?api_key=` в URL (F13).
- Commit: `stelmakhdigital <budaev.digital@gmail.com>`, remote `git@github.com:stelmakhdigital/team.git`,
  ветка `master`.
- **НЕ коммитить**: `backend/**`, `agents/`, `answer_*.md`, `_workspace/ui-screenshots/`
  (скриншоты — только локально, в `.gitignore`).

## 2. Что сделано

### 2.1 Базовый UI (F1–F15, июнь–08.10)
Каркас (8 маршрутов), типы 1-в-1 с контрактом, mock+real адаптеры, Dashboard,
Team Builder (SVG-эпоха), Messages/Library/History, Tasks lifecycle, WebSocket
(честный статус + mock-синтетика), интеграция с реальным backend (slice 1–6:
dashboard/tasks/sessions/alerts/history/workflows/library/audit/metrics/messages/WS),
RBAC-тесты, WS-auth fix, WS ping/pong live-проверка, ADR-004 (B3 CLOSED),
55/55 тестов, 37+18.

### 2.2 UI-редизайн в стиле openRIG (R1–R6, 2026-10-08) — текущее состояние UI
Решения пользователя: **hybrid editing** (auto-layout по умолчанию + edit-режим),
**React Flow** как стек графов, **full parity live-данных** (ctx%/tokens/терминал),
терминология segment/role/relative сохранена (визуально = pod/seat/edge).

- **R1** `1e5501c` — React Flow канвас топологии: auto-layout (топосорт по
  delegates_to/spawned_by, сегменты-контейнеры ≤3 колонок, карточки 240×150),
  RoleNode (activity-dot pulse, session state, runtime/profile, uptime),
  SegmentGroupNode (dashed glass), TaskEdge (5 цветов/стилей по типу связи),
  pan/zoom/миникарта/fitView, тёмная тема, reduced-motion.
- **R2** `37185c1` — hybrid edit-режим: drag ролей/сегментов (PATCH layout),
  connect handle→handle, палитра drag-drop, «✨ Auto layout» (пересчёт + PATCH всех).
- **YAML** `0aac0aa` — `lib/specYaml.ts` (teamToYaml/yamlToSpec/isSelfContained/
  buildMergePlan) + SpecYamlPanel: экспорт TopologySpec, создание команды из YAML,
  merge в существующую с планом; mock createTeam применяет spec.
- **R3** `28417b2`+`85f101c` — design-токены (:root --emerald/--sky/--amber/--violet/…
  в styles.css, topology.css на токенах), TopologyTableView (Graph|Table,
  авто-фолбэк ≤900px через useMediaQuery), dashboard-полировка (mono-статы, pulse),
  **удалён мёртвый SVG-канвас** (TeamCanvas.tsx).
- **Slice 7 (backend)** CLOSED: live-поля SessionDetail (model/context_used_percentage/
  context_total_input|output_tokens/log_path, контракт §3.6) + WS `session.output` (§4.3);
  мой ACK после live-верификации (pong 201 + 88/88 vs :8080, `25c9465`).
- **R4** `8015c81` — live на карточках: ctx% (пороги 60/80: зелёный/amber/красный+blink),
  compact tokens, модель («--» если рантайм не знает); **live-терминал**: hover на
  карточке → popover с последними строками WS `session.output` (ring 200);
  TeamBuilderPage: GET /sessions poll 5s + WS team:{id} (refetch на started/stopped);
  mock seeded-сессии с метриками (42%/67%).
- **R5** `72dc93b` — единый `Badge` (ui/States; task/entity/sev/type/warn) вместо ~10
  расписанных бейджей; **фикс 2 битых template literals** (AlertsPanel, LibraryPage —
  классы не подставлялись); CSS state-цвета; React.memo на 4 dashboard-панелях.
- **R6** `dd47d32`/`6a72bf8`/`0f84d63` (по запросу пользователя: «верстка фиксированная,
  workflows не работают, library не для чего»):
  - **R6.1 perf** — убраны 3 re-render цикла: `lastMessage` из state useWebSocket
    (никем не использовался, ререндерил страницу на каждый WS-тик), liveSessions Map
    стабилизирован (content-equality; 5s-поллинг больше не крутит ReactFlow),
    refetch через ref + стабильные deps эффектов. Нашёл и починил overlap карточек:
    flex min-height:auto раздувал RoleNode до 170px (фикс: height 150px + min-height:0)
    + перекрывающийся seeded mock-layout (удалён, дефолт — чистый auto-layout).
  - **R6.2 responsive** — sidebar → icon-rail ≤1100px; builder-панели (config/palette)
    — раньше `display:none` ≤1200px, теперь **drawer + FAB** (конфиг на любой ширине);
    таблицы в .table-wrap (h-scroll ≤640px); page-head flex-wrap; workflow-канвас 100%.
  - **R6.3 library** — apply (team new/merge, workflow/role/segment → team) с
    **редиректом на результат** (team → builder, workflow → его редактор, role/segment
    → builder команды); spec — YAML + copy-to-clipboard.
  - **R6.4 workflows** — редактор переделан на React Flow: pan/zoom/миникарта,
    нативный drag (нет лагов HTML5-DnD), палитра 6 типов блоков, конфиг блока
    (label/role/iterations/note), условие decision (yes/no на связях), удаление
    блока (каскад)/связи/workflow, локальные hints (цикл, изолированные),
    auto-layout по топологическим уровням (чистая функция + 7 тестов).
    **Контракт 20: новые §2.6–2.8 (DELETE block/connection/workflow)** — frontend+mock
    готовы, **ждём backend-срез** (письмо в `answer_backend.md`).

## 3. Текущий статус (2026-10-08, после R6.4)
- Git: `master` = `origin/master` = **`0f84d63`**. Working tree чист.
- Тесты: локально **90/90** (unit + smoke; интеграционные автоскип без ключей).
  Последний живой прогон vs :8080 — **88/88** (20 интеграционных, slice 7 ACK).
  R6-тесты (workflowLayout, mock delete, smoke editor) — unit/smoke, без демона.
- Backend-демон backend-агента на **:8080** (`env-live-key`, cwd backend/,
  sqlite `/tmp/daemon-slice7.db`, `DAEMON_AGENT_SPECS_DIR` выставлен) — **не убивать**.
- Dev-сервер: `http://localhost:5174` (mock; `npm run dev -- --port 5174`).
- Скриншоты (локально, не коммитить): `/tmp/r4-metrics.png` (live-метрики+терминал),
  `/tmp/r62-*.png` (responsive 4 ширины), `/tmp/r64-wf*.png` (workflow editor),
  `/tmp/final-*.png` (финальный свип).

### Бэклог (опциональное/внешнее)
1. **Backend (жду их)**: срез DELETE workflows (§2.6–2.8) — **DONE (2026-10-08)**: backend реализовал
   `DELETE /workflows/{id}/blocks/{blockId}` (каскад связи, `removed_connections`),
   `DELETE /workflows/{id}/connections/{connectionId}`, `DELETE /workflows/{id}`; 404 на чужеродные id;
   RBAC `workflows.update`. Проверено: backend unit+http + live curl + frontend интеграционный
   `slice 5c` (18/18 vs живой daemon).
2. PG e2e-свип по критерию ADR-004 §3 — при появлении PG-окружения.
3. WS ack subscribe (event до subscribe теряется; задокументировано, UI идемпотентен).
4. OpenAPI (`docs/contracts/openapi.yaml`) — в конце проекта, генерирует backend.
5. Prometheus `/metrics` (backend, по требованию).

## 3b. UI-доводка 2026-10-09 (task-ui: U1–U4)

### U1 — full-width адаптивный layout
- Убран потолок `.page { max-width: 1500px }` → страницы занимают всю ширину вьюпорта
  (нет пустой боковой зоны на ≥2560px). Сетки `auto-fit/auto-fill+minmax` перестраиваются
  по ширине; длинные текстовые блоки при необходимости ограничиваются отдельно.
- Library-grid: `auto-fill minmax(240,340)` — карточки читаемой ширины (не «размазываются»).
- Проверено скриншотами ≥2560px: dashboard / tasks / library / workflows / messages / history.

### U2 — выравнивание Dashboard
- `.dash-grid` пересобран из плоской `nth-child 3fr/2fr + align-items:start` в явные строки
  `.dash-row` (grid `3fr 2fr`, `align-items:stretch`) → карточки в строке одной высоты, ступенька ушла.
- SummaryCards — одна строка единой высоты (`align-items:stretch`, `height:100%`); gap 16px;
  responsive-collapse ≤1100px. До/после — скриншоты ≥2560px.

### U3 — аудит редактируемости (каждая сущность редактируема в UI)
| Сущность | Поле(а) | Редактируемо в UI? | Где / как | API | Пропуск? |
|---|---|---|---|---|---|
| Team | name, description | ✅ (добавлено в U3) | TeamBuilder → «✎ Team» (инлайн-форма, валидация) | `saveTopology` (Name/Description) | — |
| Team | segments | ✅ | палитра (drop) + drag | `createSegment`, `updateSegmentLayout` | — |
| Segment | name, description | ✅ (round 2) | TeamBuilder → выбрать сегмент → ConfigPanel «Apply» | `PATCH /segments/{id}` (`updateSegment`) | — |
| Role/член | config (model/tools/prompt) | ✅ | ConfigPanel | `updateRoleConfig` | — |
| Role/член | name, description | ⛔ read-only (обосновано) | — | нет `PATCH /roles/{id}` — rename ломает внешние ссылки (chatroom/relatives/session/alert = `segment.role`) | каскад, out of scope |
| Role/член | position | ✅ | drag | `updateRoleLayout` | — |
| Task | state (start/done/block/cancel/reopen) | ✅ | Tasks → действия | `updateState` | — |
| Task | assignment (handoff) | ✅ | Tasks → «handoff» | `handoff` | — |
| Task | title, priority | ⛔ read-only (обосновано) | форма «New task» (только при create) | нет `PATCH /tasks/{id}` — title/priority задаются при create; изменения в полёте не в контракте | — |
| Workflow | name | ✅ (round 2) | Workflow Editor → «✎ Rename» (в шапке) | `PATCH /workflows/{id}` (`updateWorkflow`) | — |
| Workflow | block (type/name/params) | ✅ | блок → config-панель | `updateBlock` | — |
| Workflow | connection (create/delete/label) | ✅ (create/delete; label у decision) | — | `createConnection`, `deleteConnection` | `PATCH` на label — нет |
| Library item | name | ✅ (round 2) | Library → выбрать элемент → «✎» в Details | `PATCH /library/{id}` (`updateLibraryItem`) | — |

Результат U3 (round 2): **безопасные сущности переименовываемы** — segment (ConfigPanel),
workflow (✎ в редакторе), library item (✎ в Details) — backend `PATCH` + inline-edit + тесты
(`tests/rename.test.tsx`). Role и task — **read-only по замыслу** (не «нужен эндпоинт»):
role-name = ключ agent_spec + внешние ссылки (rename ломает связи — каскад, out of scope);
task title/priority задаются при create (изменения в полёте не в контракте). Обоснование —
в `api-decisions.md` (U3-rename) + `backend.md` §3c. Тесты: `tests/editability.test.tsx`
(team meta) + `tests/rename.test.tsx` (segment/workflow/library: mock persist + 404/400/409 + UI).

### U4 — Settings / Configuration (OpenRIG-ориентир)
Страница Settings (секции: Daemon, Storage, Agents, Watchdog, API, …): типизированный
реестр ключей (key/type/current/default/source), layered resolution
default → file → env → override с провенансом «какой слой выиграл», set/reset per key,
маркировка restart-keys, секции Workspace / Environment / Meta (Meta — readonly:
версия, uptime, git-хед, хэш конфигурации). Host-derived дефолты помечены «(derived)».
Backend: `internal/config` (ConfigService), REST GET/PUT/DELETE /api/v1/config* +
RBAC (config.update admin-only) — см. backend.md / api-decisions.md. OpenAPI 63 ops.

### Доводка M1–M3 (2026-10-09, task-ui2, T-20261009041724)
- **M1** after-скриншоты ≥2560px: `_workspace/ui-screenshots/after-2560x1440-teams-builder.png`
  (Team Builder: full-width, segment ConfigPanel с Name/Description + Apply, «✎ Team» в шапке),
  `after-2560x1440-workflow-editor.png` (rename-инпут + Save/Cancel в шапке),
  `after-2560x1440-settings.png` (Settings: default per key + переносы по границам).
  Пустых боковых зон нет.
- **M2** Settings: под current value выводится «default: X» per key, host-derived —
  «(derived)»; тест `settings.test.tsx` «M2: renders the default value per key».
- **M3** Settings grid: `minmax(440px,1fr)` + `.cfg-key { word-break: normal }` + `<wbr>`
  по подчёркиваниям — имена ключей переносятся только по границам, не посреди слова.
- Коммит: `90cbec4` (M2+M3). Тесты: vitest 106 passed / 21 skipped; check_openapi 63/63.

### U4b — кликабельность и drill-down
Паттерн: `components/ui/EntityLink.tsx` — единая обёртка-ссылка для элементов, ссылающихся
на сущность; данные не дублируются — fetch по id. Новые деталки: `/tasks/:taskId`
(TaskDetailPage) и `/sessions/:sessionId` (SessionDetailPage).

| Было мёртвым | Теперь ведёт куда | API (fetch-by-id) |
|---|---|---|
| Dashboard: строка задачи (title) | `/tasks/:id` — деталка задачи + история | `tasks.get`, `getTaskHistory` |
| Dashboard: карточка сессии (role) | `/sessions/:id` — сессия: метрики, история, transcript | `sessions.get`, `getSessionHistory`, `getTranscript` |
| Dashboard: алерт watchdog | source → `/tasks/:id` (queue_task) / `/sessions/:id` | id из alert |
| Tasks: строка задачи (title) | `/tasks/:id` (полная деталка) + инлайн-раскрытие (TaskDetail с историей — уже было) | `tasks.get`, `getTaskHistory` |
| История: запись аудита | раскрытие → детали операции (актор/IP/params, ресурс) | `audit.details` |
| Сообщения: сообщение | related task → `/tasks/:id` (queue_task_id) | id из message |
| Task detail: subtask | `/tasks/:subId` (EntityLink) | `tasks.get` |

Не трогал (уже работали): Tasks inline-expand (TaskDetail), Library detail-pane, TeamBuilder
ConfigPanel, Workflow block config. Тесты: `tests/drilldown.test.tsx` (4). Скриншоты
detail-панелей: `tasks_1`, `sessions_1` @2560.

### UX-проход X1–X4 (2026-10-09, T-20261009100853) — состояния, обратная связь, доступность
Единый UX-стандарт по всем страницам. Коммиты: X1 `b6a3edc`, X2 `c7a7762`, X3 `90c58fd`,
X4 `6bebd70`. Тесты: vitest 112 passed / 21 skipped; `check_openapi.py` 63/63 (API не менялся).

**Единые примитивы** (`components/ui/States.tsx`, `ui/Toast.tsx`, `hooks/useQuery`/`useWebSocket`):
- **X1 EmptyState** — у каждого списка/панели; CTA там, где действие естественное
  (Tasks/Workflows/Teams). Заполнены пропуски: Messages (dm/chatrooms/chat-room).
- **X2 PageSkeleton/Skeleton** — скелетон (header + rows, `min-height`, shimmer,
  `prefers-reduced-motion` off) вместо голого full-page `<Spinner>`: Task/Session detail,
  Workflows, Teams, History (3 вкладки). Панели/действия — инлайн-`<Spinner>` (место
  зарезервировано, layout не дёргается). Кнопки мутаций — `disabled + pending` («Creating…» и т.п.).
- **X3** — `errorMessage()` мапит API-коды в конкретику (409→Conflict, 403→Not permitted,
  404→Not found, 0→Network, 500→Server) → все toast-ы мутаций самодостаточные.
  Глобальный WS-бейдж (auto-reconnect) в AppShell — на **каждой** странице (mock/connected/
  reconnecting/offline). Toast'ы (`aria-live`) на create/rename/delete/config set-reset.
  `CreateTaskModal`: ошибка API → инлайн form-error (было `alert()`).
- **X4** — глобальный `:focus-visible` outline (a/button/[role=button]/[tabindex]);
  `aria-label` на icon-кнопках (Workflow 🗑, Library ✎, Settings ✎ per key). Контраст статусных
  цветов (green/red/yellow/accent) + `--muted` проверен: все ≥ WCAG AA 4.5:1 на тёмной теме.

**Таблица покрытия** (empty / loading / error / a11y-аудит):

| Страница | Empty | Loading | Error | A11y / примечание |
|---|---|---|---|---|
| Dashboard | ✅ per-panel (tasks/sessions/alerts) | ✅ PageSkeleton (initial) + per-panel Spinner | ✅ per-panel ErrorState/Unavailable | EntityLink, progressbar, глоб. WS-бейдж |
| Attention | ✅ «Всё спокойно» | ✅ PageSkeleton | ✅ ErrorState+retry | EntityLink CTA, severity-бейджи, nav-счётчик (live WS) |
| Teams | ✅ +CTA | ✅ PageSkeleton | ✅ ErrorState+retry | ссылки-строки, focus |
| Team Builder | ✅ drop-hint (пустой сегмент) | ✅ Spinner (canvas) | ✅ ErrorState | Edit/Team — текстовые кнопки, focus |
| Workflows | ✅ +CTA | ✅ PageSkeleton | ✅ ErrorState+retry | ссылки-строки, focus |
| Workflow Editor | n/a (редактор сущности) | ✅ Spinner | ✅ ErrorState | rename/delete aria-label, focus |
| Tasks | ✅ +CTA («match filters») | ✅ PageSkeleton | ✅ ErrorState+retry | row-toggle aria, action-кнопки, focus |
| Task Detail | ✅ subtask/history инлайн | ✅ PageSkeleton | ✅ ErrorState+retry | EntityLink, focus |
| Session Detail | ✅ transcript/history инлайн | ✅ PageSkeleton | ✅ ErrorState+retry | focus |
| Messages | ✅ dm/chatrooms/chat | ✅ Spinner | ✅ ErrorState/Unavailable | chatroom-кнопки, input aria, focus |
| Library | ✅ «Nothing here» | ✅ PageSkeleton | ✅ ErrorState+retry | rename ✎ aria-label, focus |
| History | ✅ per tab | ✅ PageSkeleton ×3 | ✅ ErrorState/Unavailable | tabs (role/aria-selected), focus |
| Settings | n/a (реестр всегда) | ✅ Spinner | ✅ ErrorState+retry | edit ✎ aria-label, refresh, contrast AA |

**Глобально (все страницы):** WS-бейдж в AppShell · toast'ы (`aria-live`) на мутации ·
`:focus-visible` outline · контраст статусов AA.

**Скриншоты @2560** (`_workspace/ui-screenshots/`):
- `ux-2560x1440-empty-tasks-filtered.png` — Tasks (фильтр canceled) → EmptyState + CTA.
- `ux-2560x1440-empty-library-search.png` — Library (поиск без совпадений) → «Nothing here».
- `ux-2560x1440-focus.png` — keyboard-focus (focus-ring) на кнопке «+ New task».
- `ux-2560x1440-error-state.png` — ErrorState «Server error» + Retry (+ глоб. WS-бейдж).
**Тесты:** `tests/emptyStates.test.tsx` (4: dashboard/tasks/workflows/library),
`tests/feedback.test.tsx` (2: errorMessage code-mapping + глоб. WS-бейдж).

### 3c. Мобильные брейкпоинты (R1)

Проверено **программным overflow-аудитом** (Playwright, `documentElement.scrollWidth` vs viewport) по всем 12 страницам на 375/768/1100/320px. **375 и 768px — без горизонтального overflow по всем 12 страницам** (1100 всегда чисто; 2 минорных кейса на экстремально узких 320px → follow-up в report).

**Брейкпоинты:** 1200px (TeamBuilder: 1 колонка + drawer/FAB); 1100px (сайдбар→60px rail, dash-row/messages/library/detail → 1 колонка); 640px (padding ↓, `.table-wrap .table` min-width 560).

**Дополнено (R2):**
- `.table-wrap { overflow-x: auto }` — базовое правило (было только ≤640): широкие таблицы скроллятся на любой ширине, не ломая layout (фикс /tasks @768).
- `.settings-sections-grid > * { min-width: 0 }` + `@media ≤640 { minmax(0,1fr) }` (media ПОСЛЕ базового правила, каскад) — cfg-таблица Settings (6 колонок) скроллится в `.table-wrap`, 440px-трек не проталкивает грид шире вьюпорта (фикс /settings @375).
- `.filters { flex-wrap: wrap }` — селекты не вылезают на узких.

**Follow-up (320px) — закрыт в P4** (§3d): Dashboard `.metric` (SummaryCards) + `.library-save-row` фиксированы в `@media ≤480` (single-column / wrap).

Мобильные скриншоты в этой итерации не переснимались; десктопные зоны: `_workspace/ui-screenshots/after-2560x1440-*.png` (layout по брейкпоинтам ≥768 идентичен).

### 3d. UX-полиш P1–P4 (2026-10-09, T-20261009124055) — хвост UX-прохода

**P1 — Team Builder: PageSkeleton.** Initial load `/teams/:id` (topology) был full-page `<Spinner>` → теперь `<PageSkeleton rows={5}>` (единый паттерн X2). По-панельные спиннеры (topology canvas, ConfigPanel) оставлены — допустимо по spec. Тест: `tests/teamBuilderSkeleton.test.tsx` (удерживает getTopology через mock-latency, проверяет `.page-skeleton` без `.spinner-wrap`).

**P2 — mutation: no-double-submit + артефакты.** Паттерн disabled+pending теперь с re-entry-guard в `useMutation`: повторный `mutate()` во время in-flight возвращает тот же in-flight promise (не дублирует запрос) — держится даже если клик попал до ререндера disabled. Тест: `tests/mutation.test.tsx` (2: hook-level guard + button disabled/indicator/re-click). Скриншот loading-кнопки (Save → «Saving…» + disabled, mock-задержка 4s): `_workspace/ui-screenshots/ux-mutation-pending-2560x1440.png`.

**P3 — контраст статусов: верификация WCAG AA.** Скрипт `frontend/scripts/contrast-check.mjs` (relative-luminance WCAG 2.1, exit 0/1 для CI) прогоняет все статусные цвета. Цели: норм. текст ≥4.5:1, non-text (border/icon) ≥3:1. **2 пары не проходили → исправлено в styles.css:**
- белый на `--accent #4f8ef7` (`.btn-primary`) = 3.21:1 → `--accent-strong #2563eb` (5.17:1); hover темнеет (brightness .92), чтобы остаться ≥4.5.
- белый на `--red #f85149` (`.badge-sev-critical`) = 3.35:1 → `--red-strong #cf222e` (5.36:1).

Полная таблица (все 22 пары, после исправлений — `node frontend/scripts/contrast-check.mjs`):

| Пара | fg / bg | ratio | цель | AA |
|---|---|---|---|---|
| Text на странице | `#e6edf3`/`#0d1117` | 16.02:1 | 4.5 | ✅ |
| Text на панели | `#e6edf3`/`#161b22` | 14.64:1 | 4.5 | ✅ |
| Muted на странице | `#8b949e`/`#0d1117` | 6.15:1 | 4.5 | ✅ |
| Muted на панели | `#8b949e`/`#161b22` | 5.62:1 | 4.5 | ✅ |
| Muted на бейдже | `#8b949e`/`#1c2330` | 5.12:1 | 4.5 | ✅ |
| Green (ok) на бейдже | `#3fb950`/`#1c2330` | 6.20:1 | 4.5 | ✅ |
| Green (ok) на панели | `#3fb950`/`#161b22` | 6.81:1 | 4.5 | ✅ |
| Red (error) на бейдже | `#f85149`/`#1c2330` | 4.70:1 | 4.5 | ✅ |
| Red (error) на панели | `#f85149`/`#161b22` | 5.16:1 | 4.5 | ✅ |
| Yellow (warn) на бейдже | `#d29922`/`#1c2330` | 6.24:1 | 4.5 | ✅ |
| Yellow (warn) на панели | `#d29922`/`#161b22` | 6.85:1 | 4.5 | ✅ |
| Accent (info) на бейдже | `#4f8ef7`/`#1c2330` | 4.91:1 | 4.5 | ✅ |
| Accent (info) на панели | `#4f8ef7`/`#161b22` | 5.39:1 | 4.5 | ✅ |
| Accent на cfg-override chip | `#4f8ef7`/`#12233b` | 4.92:1 | 4.5 | ✅ |
| Purple на бейдже | `#a371f7`/`#1c2330` | 4.70:1 | 4.5 | ✅ |
| Sky на бейдже (cfg-source-file) | `#38bdf8`/`#1c2330` | 7.36:1 | 4.5 | ✅ |
| Yellow на mode-mock | `#d29922`/`#3b2f12` | 5.20:1 | 4.5 | ✅ |
| Green на mode-real | `#3fb950`/`#10291a` | 6.10:1 | 4.5 | ✅ |
| White на primary-кнопке | `#ffffff`/`#2563eb` | 5.17:1 | 4.5 | ✅ |
| White на critical-бейдже | `#ffffff`/`#cf222e` | 5.36:1 | 4.5 | ✅ |
| Dim border (transcript) | `#64748b`/`#161b22` | 3.63:1 | 3.0 | ✅ |
| Panel border (декор) | `#2d333b`/`#0d1117` | 1.49:1 | n/a | ℹ️ декор (1.4.11 n/a) |

**P4 — мобильный 320px (follow-up §3c).** Overflow-аудит (Playwright, scrollWidth vs viewport, 12 routes @320/360): реальных виновников всего 2 — Dashboard SummaryCards (`.metrics-grid` minmax(250px) > ~240px контента) и `.library-save-row`. Workflow Editor canvas, Settings grid, Team Builder topology — уже чисты (stack/scroll в своих контейнерах; топология на узких = Table view, граф скрыт). Фикс `@media ≤480`: `.metrics-grid` → 1 колонка; `.library-save-row` → wrap + input `min-width:0/flex:1`. **Результат: 320 и 360px — 0 горизонтального overflow на всех 12 routes.** Скриншоты (4 страницы @320): `_workspace/ui-screenshots/ux-mobile-320x720-{dashboard,library,workflow-editor,team-builder}.png`.

### 3e. Attention view «Что требует меня» (P0.1, 2026-10-09, T-20261009135335) — операторский срез

Единый список всего, что требует действия оператора: `GET /api/v1/attention` (backend) → страница `/attention` + пункт навигации с live-бейджем. Миграций нет — service агрегирует существующие таблицы.

**Источники (агрегация в `service.AttentionService`):**
- `task_blocked` (state=blocked) → high.
- `task_escalated` (closure_reason=escalation, не-терминальные) → critical; дедуп: blocked+escalated → один item (escalated).
- `alert_unread` (watchdog is_read=0) → critical/high/normal по event_type (blocked→critical, stale→high, else→normal).
- `workflow_attention` / `session_failed` — в схеме, но сейчас пусты (отсутствуют в ответе).
- Сортировка: severity desc → created_at asc; limit/offset (def 50 / max 200); `total` = полный счёт (для бейджа).
- RBAC: `dashboard.read` (viewer-доступен, без новой перми).

**UI:**
- Страница `/attention`: таблица (severity-бейдж / item+detail / elapsed / suggested action / CTA-EntityLink). Loading → `PageSkeleton`, ошибка → `ErrorState`+retry, пусто → `EmptyState` «Всё спокойно». Таблица в `.table-wrap` (внутренний горизонт. скролл на узких).
- Навигация: пункт «Attention» (⚑) + бейдж-счётчик (red, `--red-strong` = AA 5.36:1). Счётчик = `total`, обновляется live по WS-событиям task./session./alert. (дебаунс ~3с). В rail-режиме (≤1100px) бейдж — оверлей на иконке.
- `normal`-severity отображается существующим accent-классом `badge-sev-info` (новый цвет не введён; контраст — `contrast-check.mjs`).

**Тесты:** `tests/attention.test.tsx` (4: рендер списка + severity-бейджи, CTA-навигация → /tasks/:id, пустое состояние, бейдж-счётчик > 0). Контраст: `contrast-check.mjs` — 22 пары, exit 0 (бейдж = та же пара, что critical-бейдж).

**Скриншоты** (`_workspace/ui-screenshots/`): `attention-2560x1440.png` (список + бейдж), `attention-mobile-480x850.png` (rail + внутренний скролл таблицы), `attention-empty-2560x1440.png` (пусто, «Всё спокойно», бейдж скрыт).

## 4. Ключевые файлы
| Файл | Назначение |
|---|---|
| `frontend/src/types/api.ts` | mirror контракта |
| `frontend/src/api/{index,real,config,errors,http,types}.ts` | фасад + адаптеры |
| `frontend/src/api/mock/{adapter,data}.ts` | mock (seed, live-метрики, workflows, library apply) |
| `frontend/src/pages/TeamBuilderPage.tsx` | топология: poll sessions 5s, WS, live-merge, drawer/FAB |
| `frontend/src/pages/WorkflowEditorPage.tsx` | workflow-редактор (R6.4) |
| `frontend/src/pages/LibraryPage.tsx` | library: apply + редирект + YAML spec (R6.3) |
| `frontend/src/components/Topology/*` | TopologyCanvas, layout/autoLayout (ROLE_W=240/H=150), nodes/RoleNode (+ctx%/терминал), SegmentGroupNode, edges/TaskEdge, TopologyTableView |
| `frontend/src/components/Workflow/*` | WorkflowCanvas, layout (топо-уровни), nodes/BlockNode |
| `frontend/src/components/TeamBuilder/*` | Toolbar, ConfigPanel, SpecYamlPanel, BottomPanel, palette |
| `frontend/src/components/ui/States.tsx` | Spinner/Empty/Error/Unavailable + **Badge** (R5) |
| `frontend/src/components/layout/AppShell.tsx` | sidebar (rail ≤1100) |
| `frontend/src/lib/specYaml.ts` | YAML TeamSpec serialize/parse/merge |
| `frontend/src/hooks/{useQuery,useMutation,useWebSocket,useMediaQuery}.ts` | данные/мутации/WS/responsive |
| `frontend/src/{styles,topology}.css` | токены + темы (topology.css: topo + wf-*) |
| `frontend/tests/realIntegration.test.ts` | интеграционные (автоскип без daemon) |
| `frontend/tests/{topologyAutoLayout,specYaml,roleNode,badge,workflowLayout,mockAdapter,appSmoke}.test*` | unit/smoke |
| `docs/architecture/frontend/20_contract_API.md` | API-контракт (владею; §2.6–2.8 новые) |
| `answer_backend.md` / `answer_frontend.md` | почта с backend-агентом |
| `_workspace/ui-redesign-plan.md` | план R1–R6 (статусы + коммиты) |

## 5. Команды
```bash
cd frontend
npm run dev -- --port 5174          # mock (dev-сервер проекта; :5173 занят другим)
npm test                             # локально: 90/90 (интеграционные — автоскип)
npm run typecheck && npm run build
# интеграционные vs живой :8080 (демон backend-агента, НЕ убивать):
INTEGRATION_BASE_URL=http://localhost:8080 \
INTEGRATION_API_KEY=env-live-key \
INTEGRATION_VIEWER_KEY=sk_698b32cdac447a1f99f6e526c9a17896afcad2c1088e283f \
INTEGRATION_OPERATOR_KEY=sk_62ae7fa00cb2a6b4dd0931f1f6aa37dea873fa618e538ab8 \
npm test                              # 88/88 (20 интеграционных)
# headless-скриншоты (chrome-деbs в /tmp/chrome-libs, sudo недоступен):
NODE_PATH=/home/arkalaust/.npm/_npx/e41f203b7505f1fb/node_modules \
LD_LIBRARY_PATH=/tmp/chrome-libs/ext/usr/lib/x86_64-linux-gnu node /tmp/shot-*.cjs
```

## 6. Как восстановить сессию
1. Прочитать этот файл + `answer_backend.md` (последнее письмо) +
   `_workspace/ui-redesign-plan.md` (статусы R-фаз).
2. `cd frontend && npm test` — ожидается **106 passed / 21 skipped** (интеграционные — автоскип).
3. `git log --oneline -5` — HEAD = `90cbec4` (U4 M2+M3) или новее.
4. Дальше: бэклог (§3) — PG e2e при окружении; UI: UX-проход (пустые состояния/лоадеры/ошибки), доступность/мобильные брейкпоинты.
