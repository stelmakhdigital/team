# Бэклог: порт функционала OpenRIG (P0–P3)

Источник: `docs/analysis/openrig-comparison.md` (2026-10-09). Каждое задание —
карточка с scope и критериями; исполнитель — pipeline (worker→planner→judge).
Статусы: `new` → `in_progress` → `done` (ссылка на T-id).

Порядок выполнения: P0.1 → P0.2 → P0.3 → **P1.0 Projects** → P1.4 → P1.5 → P1.6 →
P1.7 (briefing) → P1.8 (doctor) → P2.* (включая P2.12–P2.15) → P3.*.

---

## P0.5 → P1.0 Projects: сущность «проект» (мультипроектность, по запрос пользователя)

### P1.0 Projects entity + привязка к командам/сессиям — `done` (T-20261009200032, 1 раунд; projects CRUD, layered working_dir, фильтры, 71 ops; миноры: UI-сессий страницы нет — API-фильтр готов)
- **Контекст**: сейчас `working_dir` задаётся вручную на каждой сессии; сущности
  Project нет (команда не привязана к директории; фильтр «по проекту» невозможен).
  Ориентир — OpenRIG work tree: workspace → project → mission → slice; seat работает
  в worktree slice'а, «где работать» — свойство работы, а не параметр запуска.
- **Scope (минимальный, без mission/slice-иерархии)**:
  - Сущность `project`: `{id, name, path (abs), kind: existing|new, git: bool,
    description}`; UI-страница **Projects**: зарегистрировать существующую
    директорию / создать новую (scaffold: mkdir + git init + README/шаблон);
    проверка при создании (существует? writable? git?); архивация.
  - Привязка **team → project** (nullable; команда может быть «универсальной»);
    при запуске сессии `working_dir` по умолчанию = путь проекта команды
    (override на сессии сохраняется); в UI запуске сессии — селект проекта/пути
    с автозаполнением.
  - **Project как свойство работы** (рекомендация командира, 2026-10-09):
    `task.project_id` — наследуется от team при создании, override при
    handoff/subtask; разрешение `working_dir`: `session.working_dir` (явное)
    → `task.project` → `team.project` (layered, как в Settings).
    Документировать порядок в api-decisions.md.
  - Фильтр «по проекту»: tasks/sessions/dashboard (team.project_id → фильтр);
    Attention/History учитывают проект в detail.
  - Артефакты: скриншоты/логи сессий привязываются к проекту (путь в
    workspace-секции конфига + project_id).
  - API: `GET/POST /projects`, `GET/DELETE /projects/{id}` (archive),
    `PATCH /projects/{id}`; team create/update — `project_id`; sessions —
    автозаполнение working_dir + `project_id` в модели сессии.
- **Критерии**: миграция; service + HTTP-тесты (создание existing/new, проверка
  директории, привязка, автозаполнение working_dir, фильтры); UI: Projects page
  + селект в team/session-формах + фильтры; OpenAPI (новые маршруты в спеке,
  check_openapi зелёный); USAGE.md §5.2 обновить (мультипроектная история);
  backend.md/frontend.md актуализированы; скриншоты Projects @2560.
- **Миграционная совместимость**: команды/сессии без project_id работают как
  раньше (path из working_dir); новые команды — с проектом.
- **Не в scope (решение зафиксировано, пересмотр по триггеру)**: mission/slice-
  иерархия (наш продукт — не их SDLC-миссии), per-slice git worktrees (пересмотр,
  когда P1.4 начнёт проецировать параллельные задачи на один репозиторий —
  тогда «per-task worktree on demand» в P3), proof/evidence (P3.13),
  workspace doctor-диагностика.

---

## P0 — операторский срез (малый объём, высокая ценность)

### P0.1 Attention view «Что требует меня» — `done` (T-20261009135335, 1 раунд; GET /attention, UI + nav-бейдж, 64 ops, 2026-10-09)
- **Scope**: единая страница/панель (или режим Dashboard) со всем, что требует
  человека: задачи `blocked` (особенно blocked_on-human/escalation),
  escalation-переходы, overdue (см. P0.2), waiting/stuck/failed инстансы
  воркфлоу, непрочитанные watchdog-алерты. Для каждой строки — suggested
  action (разблокировать / посмотреть сессию / открыть деталку) и CTA-ссылка
  (EntityLink на деталку).
- **Критерии**: страница в навигации; live-обновление по WS; тесты (mock:
  комбинации состояний); скриншот ≥2560; frontend.md.
- Backend при необходимости: агрегирующий эндпоинт `GET /attention`
  (или составление на клиенте — решить по архитектуре; если новый эндпоинт —
  OpenAPI + check_openapi).

### P0.2 Overdue / дедлайны задач — `done` (T-20261009161158, 1 раунд; closure_required_at, GET /tasks/overdue, task_overdue в Attention; миноры M1/M2 → P0.3)
- **Scope**: `closure_required_at` — вычисляется при переходе в `in_progress`
  (claim); базовое время из конфига (новый config-key, напр.
  `tasks.closure_required_secs`, layered resolution как у других); `GET /tasks`
  помечает `is_overdue`; отдельный overdue-список (bounded) на странице Tasks
  (фильтр) и в Attention. `undelivered` (задачи, чьё уведомление о создании не
  подтверждено) — optional в рамках той же карточки.
- **Критерии**: миграция (nullable timestamp), service-тесты вычисления и
  истечения, HTTP-тесты, UI-фильтр + скриншот, OpenAPI, конфиг-key в Settings.

### P0.3 Block with wake — `done` (T-20261009181739, 1 раунд; task_wakes timer|task, resolve, M1/M2 закрыты)
- **Scope**: при переходе задачи в `blocked` — обязательное `wake`-поле:
  `timer` (durations) | `watchdog` (id джоба, если P1.6 готов, иначе позже) |
  `task:<id>` (живая задача) + `continuation` (текст «как продолжить»).
  Разблокировка: по срабатыванию wake (timer — по сроку; task — когда
  referenced-задача закрыта) задача возвращается в `pending` с системным
  комментарием; ручной `resolve` (кнопка в деталке) записывает решение в
  history и будит (нотификация/алерт).
- **Критерии**: модель (поля/таблица), сервис-логика + тесты (timer/task
  ветки, идемпотентность), UI-форма блока + деталка wake-статуса,
  OpenAPI, docs (api-decisions.md).

---

## P1 — ядро ценности (средний/большой объём)

### P1.4 Workflow engine: close-step → next task — `done` (T-20261009214429, 1 раунд; instances/steps, projection в tx, resume/route/abort/trace, attention-вид, 78 ops; manual-блок не в v1)
- **Scope**: active-воркфлоу становится движком: `agent`/`task`-блоки
  привязываются к ролям; при старте инстанса (или при активации воркфлоу)
  создаётся задача для первого блока; closure задачи (done +
  closure_reason) в **одной транзакции** закрывает шаг и проецирует задачу
  следующего блока (по connection'ам; decision — по condition; parallel —
  ветки; loop — повтор по счётчику/условию). Состояния инстанса:
  `running|waiting|completed|failed|aborted`; `resume` (с failed-шага,
  completed не перезапускаются); `route` (перенаправить текущий шаг на другую
  роль — честный handoff); `trace` (append-only trail шагов); `status`
  (что требует внимания). UI: панель инстанса в Workflow Editor / отдельная
  деталка (текущий шаг, trail, действия resume/route/abort).
- **Критерии**: транзакционность (тест: crash-сценарий «закрыли шаг, но не
  создалась следующая» невозможен), покрывающие тесты на все 6 block types
  (минимум task/decision/parallel/agent), UI-панель + тесты, OpenAPI,
  api-decisions.md, backend.md.
- Замечание: существующие статические воркфлоу не должны ломаться (миграция
  additive; воркфлоу без engine-привязки работают как раньше).

### P1.5 Session handover + compact-plan — `done` (T-20261009233009, 1 раунд; context_wall_pct, GET /compact-plan, handover c dry-run + per-step отчётом, attention-вид, 80 ops; порядок context→stop→task→successor задокументирован)
- **Scope**: (a) `GET /compact-plan` (или раздел Dashboard): сессии у
  «стены контекста» (порог — config-key `sessions.context_wall_pct`, напр. 80)
  — у нас уже парсится `context_used_percentage`; (b) **handover**: для
  сессии — создать новую сессию той же роли (та же working_dir/runtime),
  доставить контекст: резюме транскрипта (последние N строк + summary,
  если доступен LLM-шаг — иначе честный «последние N строк + указание на
  transcript-файл»), перенос привязанной задачи (handoff-комментарий),
  остановка старой; `--dry-run`-аналог (кнопка «показать план»); причина
  handover обязательна; per-seat честный отчёт (resumed/rebuilt/fresh/failed).
- **Критерии**: тесты (контекст-порог, handover-транзакция, идемпотентность),
  UI (баннер на Session Detail при >порога: «стена контекста — handover?»,
  форма с причиной; список compact-plan на Dashboard), OpenAPI, docs.

### P1.6 Watchdog-джобы: context-usage-threshold + register — `done` (T-20261010022307, 1 раунд; watchdog_jobs+events, fire/дедуп/dead-target, wake=watchdog, alert-вид, 85 ops; миноры закрыты 06fa918)
- **Scope**: регистрируемые watchdog-джобы (хранилище, переживают рестарт):
  минимум policy `context-usage-threshold` (сессия + порог + сообщение) →
  алерт в Dashboard/Alerts + Attention; `list/show/status/stop`; при P0.3 —
  джоб может быть wake для parked-задачи.
- **Критерии**: миграция, loop-тесты (trigger + dедуп), UI-список джобов
  (Settings или отдельная панель) + тесты, OpenAPI, docs.

### P1.7 Session briefing (startup context, ориентир OpenRIG «what a seat knows when it arrives») — `done` (T-20261010070318, 1 раунд; BriefingService: task/team/project-git/messages/alerts, bounded+omit, pi→system_prompt / file+env, preview endpoint (86 ops), TeamBuilder-предпросмотр + Session Detail-блок, handover-наследник получает briefing)
- **Контекст**: агент не должен стартовать «в пустоту»: у OpenRIG сид получает
  собранный context pack при запуске/возобновлении (позиция в очереди, интент,
  последние решения, статус соседей).
- **Scope**: при запуске сессии (и при handover — P1.5) демон автоматически
  собирает **briefing**: тело привязанной задачи (+ parent), spec команды
  (сегменты/роли/relatives компактно), проект (P1.0: path, git-статус, recent
  commits/ветка), open tasks проекта, последние N сообщений роли, последние
  алерты; briefing передаётся агенту (pi: в конфиг/промпт сессии; process/tmux:
  аргумент/файл). Кнопка «показать брифинг» в UI запуска сессии + override/
  добавление вручную.
- **Критерии**: сборщик в service + тесты (комбинации источников, пустые
  источники), интеграция в создание сессии и handover, UI-просмотр + тест,
  OpenAPI (напр. `POST /sessions/{id}/briefing/preview`), docs.
- **Зависимости**: усиливает P1.5 (handover передаёт briefing наследнику);
  проект-часть — после P1.0 (до — из working_dir).

### P1.8 Health/Doctor-панель (ориентир OpenRIG troubleshooting surface) — `done` (реализован внутри T-20261010070318; верификация+fail→Attention T-20261010133944, 1 раунд; 7 чек'ов per-check-тесты + divergence stale/zombie, GET /diagnostics + POST /run, RBAC diagnostics.read, fail→Attention diagnostics_fail (миграция 0016), UI Health, 88 ops; минор типа-паритета закрыт aec2904)
- **Контекст**: у OpenRIG — «the surface that shows the truth»: что daemon/tmux/
  seat/БД думают о состоянии и где расходятся; workspace doctor — 8-check.
- **Scope**: `GET /diagnostics` (+ `POST /diagnostics/run` для полных проверок):
  демон (uptime, version, git-хед), БД (ping, размер, применённые миграции vs
  доступные), runtime-адаптеры (tmux жив? pi CLI доступен?), конфиг (хэш, ключи
  с pending restart, secret-статус), **расхождения** (БД говорит active, а
  процесса нет; сессия stopped, но tmux-сессия жива — bounded-список findings
  с severity), watchdog-статус, disk-space по ключевым путям. UI: страница
  **Health** (или вкладка Settings): findings ok/warn/fail, повторная проверка,
  drill-down на сущность.
- **Критерии**: каждый check — отдельная функция + тест (mock-состояния,
  включая расхождения), UI + тесты, OpenAPI, docs; fail-findings → Attention
  (P0.1) как алерт.
- **Зависимости**: P0.1 (куда падают fail-findings).

---

## P2 — удобства

### P2.7 Поиск по транскриптам — `done` (T-20261010140809, 2 раунда; GET /sessions/search, контекст-окно, bounded, UI, 89 ops; flaky-фикс R2)
- **Scope**: поиск по истории всех сессий (`GET /sessions/search?q=&role_id=`)
  с контекстными окнами (±N строк), фильтр по роли/команде/дате; UI — поиск
  на странице Sessions (результаты → EntityLink на сессию со скроллом к
  месту).
- **Критерии**: индекс/наивный поиск (решить по размеру данных), тесты,
  UI + тест, OpenAPI.

### P2.8 Capture — снимок экрана сессии — `done` (T-20261010153631; GET /sessions/{id}/capture, tmux-pane vs log-tail, 409 на мёртвый tmux, сохранение+bounded-cleanup, UI-модалка+Copy, 90 ops; вложение-в-сообщение — follow-up)
- **Scope**: `GET /sessions/{id}/capture?lines=N` — «стл-кадр» (последние N
  строк) как артефакт: кнопка в Session Detail + вложение в сообщение/задачу
  (если легко), сохранение снапшота в logs.
- **Критерии**: эндпоинт + тест, UI-кнопка + скриншот, OpenAPI.

### P2.9 Pulse feed — единая лента событий — `done` (T-20261010164511; GET /events, EventBus-sink (аддитивно), bounded-таблица ≤5000/≤7дн., без session.output, обогащение entity_title, UI: лента+фильтры type/team/role+EntityLink+live-prepend по WS, 91 ops; полный WS-стрим/FTS — follow-up)

### P2.10 Command palette (Cmd+K) — `done` (T-20261010180526; CommandPalette global (Cmd+K/Ctrl+K, Esc), fuzzy (subseq, своя функция lib/fuzzy.ts), разделы Pages/Entities(lazy API)/Actions, клавиатура ↑/↓/Enter + focus-trap + aria, 12 новых тестов (fuzzy unit + палитра + K3 P2.9 live-prepend); OpenAPI не меняется)

### P2.11 Send verify — `done`
- **Scope**: при отправке сообщения в сессию (runtime tmux/pi) — подтверждение
  доставки (панель получила текст) + отказ, если адресат на interactive
  prompt; статус в сообщении (`delivered|staged|retained`).
- **Критерии**: логика по runtime-адаптерам, тесты, UI-статус доставки,
  OpenAPI, docs.
- **Готово (T-20261010183054)**: `runtime.Adapter.Send` (tmux `send-keys -l --`
  + отдельный Enter; process → ErrNoScreen); `SessionService.Send` (interactive-prompt
  guard → retained; **verify по capture** 3×300ms → delivered/staged); `POST
  /sessions/{id}/send` (RBAC **sessions.update** operator+, миграция 0018); событие
  `session.send` (P2.9); UI блок в Session Detail (badge + tail) + 4 vitest; OpenAPI
  92; api-decisions §P2.11 + USAGE §5.2.3. process/мертов tmux → 409.

### P2.12 CLI-клиент `teamctl` (ориентир OpenRIG «CLI — surface для агентов») — `done`
- **Контекст**: golden rule OpenRIG: CLI — поверхность для агентов, UI — для
  людей; оба читают те же записи. У нас — только REST (curl-магия).
- **Scope**: CLI (`backend/cmd/teamctl`, отдельный бинарь): `task list/claim/
  update/handoff/block`, `session list/transcript/capture/stop`, `attention
  list`, `whoami`, `config get/set` — всё с `--json` (агенты парсят), auth через
  env/API-key, поверх существующих HTTP-эндпоинтов (клиент, не обход).
- **Критерии**: покрытие главных сценариев агента, `--json` на всё, тесты
  (httptest), README-секция «Для агентов» + USAGE.md, без дублирования
  бизнес-логики.
- **Готово (T-20261010193009)**: `backend/cmd/teamctl` + `internal/cli/` (std flag,
  без внешних зависимостей); команды whoami/task/session/attention/config; `--json`
  + human; auth `X-API-Key` (`--api-key`/`$TEAMCTL_API_KEY`); exit 0/1/2; help;
  `reorderFlagsFirst` (флаги после positional); httptest-тесты (15: request + parse
  + auth + exit codes); README «Для агентов» + USAGE §6.5; OpenAPI 93.

### P2.13 Starter bundles (ориентир OpenRIG RigBundle) — `done`
- **Scope**: готовые стартеры как pin'нутые library-предметы: напр. «dev-team:
  lead+worker+reviewer (pi-* specs)» + связанный workflow; `apply` в один клик
  (селект проекта, если P1.0 готов); метка `starter` в UI Library, защита от
  удаления (pin), версия стартера.
- **Критерии**: 2–3 стартера (fixture-снапшоты), apply-тесты, UI-метка +
  скриншот, секция «Быстрый старт» в USAGE.md.
- **DONE (T-20261010203207, 2026-10-10)**: migration 0019 `is_starter`; seed =
  `service.SeedStarters` (идемпотентный, `ON CONFLICT DO NOTHING`, в `main` при
  старте) — 2 шт: `dev-team` (team, lead/worker/reviewer на pi-* fixtures) +
  `review-workflow` (workflow, implement→review→done). Pin: `PATCH /library/{id}`
  на стартер → 409; save с именем стартера → 409 (UNIQUE). Apply: `project_id`
  (P1.0) для team-стартера → команда в проект. UI: STARTER badge + стартеры
  первыми + banner + one-click apply (project-select) + no-edit (lock). Тесты:
  service (7) + HTTP (5) + vitest (4). Скриншот `library-starter-2560x1440.png`.
  OpenAPI 93/93 (is_starter + project_id); api-decisions §P2.13; USAGE §3.1;
  backend §3r; frontend §3s.

### P2.14 Cost tracking (токены → деньги) — `done`
- **Scope**: цены моделей в конфиге (config-ключи, напр. `cost.models.<name>.
  in_usd_per_mtok` / `out_usd_per_mtok`); стоимость per session (из уже
  парсящихся usage-токенов), per task (по привязанным сессиям), per project
  (P1.0), период; в Dashboard metrics (график $), в Session/Task Detail;
  бюджет-порог → watchdog-алерт (связка с P1.6). Честность: «оценка по
  заявленным ценам», а не факт.
- **Критерии**: расчёт в service + тесты (нет usage → «нет данных», не 0),
  UI + скриншоты, конфиг-ключи в Settings, OpenAPI, docs.
- **DONE (T-20261010213901, 2026-10-11)**: config-ключи `cost.model_prices`
  (JSON-объект цен, `ValidateJSON`) + `cost.budget_alert_cents` (порог, 0=выкл).
  `CostService` (честная семантика: нет токенов/цены → `null`/`has_data=false`,
  **не 0**; `estimate=true`); `CostForSession/Task/Project/All` + `CostSeries`
  (12 точек, bounded 100/50). Поля `estimated_cost` (null=«нет данных»):
  SessionView/Detail, TaskDetail, GetProjectResponse; metrics `llm_cost_usd`.
  Бюджет-порог → watchdog-алерт `event_type="budget"` (dedup/cooldown 1ч,
  migration 0020). UI: Settings (JSON textarea + budget), Dashboard chart
  «LLM cost (USD)», Session/Task/Project detail (Cost). Тесты: service (6) +
  watchdog (5) + HTTP (4) + vitest (5). Скриншоты `cost-*.png`. OpenAPI 93/93
  (CostInfo + estimated_cost + llm_cost_usd + cost секция); api-decisions §P2.14;
  USAGE §6.6; backend §3s; frontend §3t.

### P2.15 `whoami` для агентов — `done`
- **Scope**: `GET /whoami` (по API-ключу/роли сессии): моя роль+команда, мои
  pending/in-progress/blocked задачи, что ждёт меня (входящие), кто мой
  upstream/downstream (relatives), статус моей сессии. Компактный JSON для
  агента (используется `teamctl whoami`, P2.12).
- **Критерии**: эндпоинт + тесты (разные роли/ключи), OpenAPI, docs;
  реализуется совместно с P2.12 (одна итерация).
- **Готово (T-20261010193009)**: `GET /api/v1/whoami?session_id=&role_id=&team_id=`
  (RBAC dashboard.read, viewer-ok); identity через query (session_id предпочтительно);
  agent/session/my_tasks(waiting/blocked+wake)/waiting_for_me(unread)/upstream/
  downstream; bounded ≤20; 400/404; `MessageFilter.UnreadOnly`; service-тесты (6) +
  HTTP-тесты (by-session/by-role, 400, 404, RBAC viewer-200); OpenAPI 93.

> **🏁 P0+P1+P2 ЗАВЕРШЕНЫ (09.10–10.10).** 17 карточек `done`: P0.1–3, P1.0/1.4/
> 1.5/1.6/1.7/1.8, P2.7–2.15. Остаток: P3.12–16 (долгосрочные, по решению) и
> PG e2e (нет окружения). Тесты: go все пакеты ok, vitest 197, OpenAPI 93 ops,
> контраст 22/22.

---

## P3 — долгосрочные проекты (если направление подтвердится)

### P3.12 Agent images / session snapshots — `done` (T-20261011032248, 2 раунда; Library agent_image, snapshot/launch, pin/prune, 98 ops)
- **Scope**: захват продуктивного состояния сессии (промпт/скиллы/накопленное
  резюме) как «изображение агента»: create/list/pin/prune/preview; запуск
  новой сессии из снимка. Интеграция с Library (тип `agent_image`).
- **Критерии**: формат снимка, хранилище, UI, тесты, docs.
- **РЕАЛИЗОВАНО (T-20261011032248 R1, тс-фикс R2, ожидает approve judge)**: новый
  тип Library `agent_image`
  (migration 0021: расширение CHECK + колонка `is_pinned`). `SnapshotSession`
  (снимок конфига агента: model/system_prompt/skills/tools/thinking, роль,
  runtime, working_dir + auto-summary из tail транскрипта) → `LibraryItem`
  type=agent_image, group=agent-images. `LaunchFromSnapshot` (новая сессия с
  тем же агентом; summary → `## Session context` в системном промпте). Pin/
  Unpin/Prune (delete; pinned → 409; только agent_image). API: `POST
  /sessions/{id}/snapshot`, `POST /library/{id}/pin|unpin|launch`, `DELETE
  /library/{id}`. UI: Library (тип + карточка PINNED + detail с agent-config
  + pin/prune/launch), Session Detail (`📸 Agent image` → модалка → snapshot).
  Тесты: service (9) + HTTP (4) + vitest (4). Скриншоты `agent-image-*.png`.
  OpenAPI 98/98 (SnapshotSession/LaunchSession + is_pinned + agent_image);
  api-decisions §P3.12; backend §3u; frontend §3u; USAGE §6.7. **R2**: tsc
  --noEmit 0 (фикс неиспользуемого импорта + сужение типа в тесте). Status
  станет `done` после approve.

### P3.13 Proof/evidence на задачах — `done` (T-20261011050110, R1; judge PASS; 101 ops)
- **Scope (реализовано)**: `task_evidence` (Migration 0022) — typed-артефакты
  (screenshot/test/log/other) с sha256 (candidate-sha) к задаче; **judge** verdict
  (accept/reject/withdraw) с атрибуцией; readiness/acceptance агрегация в TaskView
  (batched, без N+1); **closure ≠ acceptance** (done ≠ accepted).
- **API**: `POST/GET /tasks/{id}/evidence` + `POST /tasks/{id}/evidence/{evid_id}/verdict`
  (RBAC: read → dashboard.read, submit/verdict → tasks.update).
- **UI**: панель Evidence / proof на Task Detail (список + Add evidence + judge +
  readiness/acceptance-бейдж).
- **Docs**: OpenAPI 101/101; api-decisions §P3.13; backend §3v; frontend §3v; USAGE §6.8.
- **Тесты**: service 8 (вкл. closure≠acceptance) + HTTP 4 (RBAC) + vitest 5.
  Скриншоты: `task-evidence-2560x1440.png` (ready/accepted),
  `task-evidence-done-pending-2560x1440.png` (done + pending).
- **Scope (изначальный)**: привязка артефактов (скриншоты, тесты, логи) к задаче с typed
  frontmatter (type, verdict, candidate-sha) + `judge` (accept/reject/withdraw с причиной,
  атрибутированно); readiness задачи; «closure ≠ acceptance».

### P3.14 Walk — поэтапная онбординг-подача — `new`
- **Scope**: `walk` — последовательная (paced) доставка контекстных кусков в
  сессию (онбординг агента без «одного дампа»); UI: редактор последовательности
  + статус доставки.
- **Критерии**: логика pacing, тесты, UI, OpenAPI, docs.

### P3.15 Inbox/outbox — `in_progress` (T-20261011063105, R1; реализовано, ожидает approve; 104 ops)
- **Scope (реализовано)**: `messages.ack_state` (Migration 0023: pending/absorbed/
  denied/dropped + acked_by/acked_at); **Inbox** (direct ToRoleID + undelivered_count),
  **Outbox** (FromRoleID), **ack** (absorb/deny/drop, атрибуция, повторный→409);
  **undelivered** = pending; broadcast/segment не в inbox; RBAC `messages.update`
  (operator+).
- **API**: `GET /roles/{id}/inbox`, `GET /roles/{id}/outbox` (dashboard.read),
  `POST /messages/{id}/ack` (messages.update).
- **UI**: Messages → Role Mailbox (селектор команды + роли → MailboxPanel: inbox
  pending+undelivered+Absorb/Deny/Drop + outbox); ack_state-бейдж в ленте.
- **Docs**: OpenAPI 104/104; api-decisions §P3.15; backend §3w; frontend §3w; USAGE §6.9.
- **Тесты**: service 5 + HTTP 4 (RBAC) + vitest 3. Скриншот: role-inbox-2560x1440.png.
- **Scope (изначальный)**: mailbox-семантика: входящие с подтверждением (pending-входящие,
  absorb/deny/drop), исходящие с записью (outbox-record/list); `undelivered` (P0.2)
  подтягивается сюда.
- **Критерии**: модель, API, UI (панель входящих на сессии/роли), тесты,
  OpenAPI, docs.

### P3.16 Mode/permission posture (ориентир OpenRIG rig mode) — `new`
- **Контекст**: у OpenRIG — осознанное переключение «кто рулит»: human-led vs
  delegated по скоупам, effective-mode цитируем. Наш ответ на «агенты не
  спрашивают, но важные решения — должны».
- **Scope**: режим (posture) на уровне команды и/или сессии: `delegated` (авто)
  vs `human-led` (с подтверждением); список «чувствительных операций» (delete,
  force-операции, config-смены, секреты, работа вне project-path); в human-led
  такие операции уходят в Attention как **human-request** (вид `human_request`):
  описание операции, обратимость, approve/deny-кнопки; решение в audit;
  effective-posture — badge на команде/сессии.
- **Критерии**: модель (posture + sensitive-registry), interceptor в HTTP-слое
  (тесты: delegated проходит, human-led паркует + approve продолжает), UI
  (переключатель + human-request-деталка), OpenAPI, api-decisions.md.
- **Зависимости**: P0.1 (Attention с human-request).

---

## Не переносить (решение зафиксировано)
- Мультихост, TUI-модель, MCP-tools — позже/не в product-плане (см. анализ).

## Зависимости
- P0.3 (wake=watchdog) — опционально на P1.6 (timer/task-ветки работают без него).
- P1.5(b) handover — проще после P3.12 (snapshots), но не требует его.
- P1.7 briefing — усиливает P1.5; проект-часть — после P1.0.
- P1.8 doctor — fail-findings падают в Attention (P0.1).
- P2.15 whoami — вместе с P2.12 teamctl (одна итерация).
- P2.14 cost — алерт по бюджету — после P1.6.
- P3.16 posture — после P0.1 (human-request в Attention).
- P2.11 — после проверки, реально ли runtime-адаптеры дают capture (P2.8 первый).
