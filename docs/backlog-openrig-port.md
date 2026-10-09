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

### P1.4 Workflow engine: close-step → next task — `in_progress`
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

### P1.5 Session handover + compact-plan — `new`
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

### P1.6 Watchdog-джобы: context-usage-threshold + register — `new`
- **Scope**: регистрируемые watchdog-джобы (хранилище, переживают рестарт):
  минимум policy `context-usage-threshold` (сессия + порог + сообщение) →
  алерт в Dashboard/Alerts + Attention; `list/show/status/stop`; при P0.3 —
  джоб может быть wake для parked-задачи.
- **Критерии**: миграция, loop-тесты (trigger + dедуп), UI-список джобов
  (Settings или отдельная панель) + тесты, OpenAPI, docs.

### P1.7 Session briefing (startup context, ориентир OpenRIG «what a seat knows when it arrives») — `new`
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

### P1.8 Health/Doctor-панель (ориентир OpenRIG troubleshooting surface) — `new`
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

### P2.7 Поиск по транскриптам — `new`
- **Scope**: поиск по истории всех сессий (`GET /sessions/search?q=&role_id=`)
  с контекстными окнами (±N строк), фильтр по роли/команде/дате; UI — поиск
  на странице Sessions (результаты → EntityLink на сессию со скроллом к
  месту).
- **Критерии**: индекс/наивный поиск (решить по размеру данных), тесты,
  UI + тест, OpenAPI.

### P2.8 Capture — снимок экрана сессии — `new`
- **Scope**: `GET /sessions/{id}/capture?lines=N` — «стл-кадр» (последние N
  строк) как артефакт: кнопка в Session Detail + вложение в сообщение/задачу
  (если легко), сохранение снапшота в logs.
- **Критерии**: эндпоинт + тест, UI-кнопка + скриншот, OpenAPI.

### P2.9 Pulse feed — единая лента событий — `new`
- **Scope**: страница/панель «Pulse»: агрегированный поток WS-событий
  (task./session./message./alert.) с фильтрами по типу/команде/роли,
  история из БД (не только live), клики → EntityLink.
- **Критерии**: хранение событий (есть audit? решить — может, новый
  bounded-буфер/таблица), UI + тесты, скриншот.

### P2.10 Command palette (Cmd+K) — `new`
- **Scope**: fuzzy-палитра: навигация по страницам + сущностям (поиск
  команд/задач/сессий/воркфлоу) + быстрые действия (создать команду/задачу).
- **Критерии**: UX (клавиатура), тесты (fuzzy-матчинг, навигация), скриншот.

### P2.11 Send verify — `new`
- **Scope**: при отправке сообщения в сессию (runtime tmux/pi) — подтверждение
  доставки (панель получила текст) + отказ, если адресат на interactive
  prompt; статус в сообщении (`delivered|staged|retained`).
- **Критерии**: логика по runtime-адаптерам, тесты, UI-статус доставки,
  OpenAPI, docs.

### P2.12 CLI-клиент `teamctl` (ориентир OpenRIG «CLI — surface для агентов») — `new`
- **Контекст**: golden rule OpenRIG: CLI — поверхность для агентов, UI — для
  людей; оба читают те же записи. У нас — только REST (curl-магия).
- **Scope**: CLI (`backend/cmd/teamctl`, отдельный бинарь): `task list/claim/
  update/handoff/block`, `session list/transcript/capture/stop`, `attention
  list`, `whoami`, `config get/set` — всё с `--json` (агенты парсят), auth через
  env/API-key, поверх существующих HTTP-эндпоинтов (клиент, не обход).
- **Критерии**: покрытие главных сценариев агента, `--json` на всё, тесты
  (httptest), README-секция «Для агентов» + USAGE.md, без дублирования
  бизнес-логики.

### P2.13 Starter bundles (ориентир OpenRIG RigBundle) — `new`
- **Scope**: готовые стартеры как pin'нутые library-предметы: напр. «dev-team:
  lead+worker+reviewer (pi-* specs)» + связанный workflow; `apply` в один клик
  (селект проекта, если P1.0 готов); метка `starter` в UI Library, защита от
  удаления (pin), версия стартера.
- **Критерии**: 2–3 стартера (fixture-снапшоты), apply-тесты, UI-метка +
  скриншот, секция «Быстрый старт» в USAGE.md.

### P2.14 Cost tracking (токены → деньги) — `new`
- **Scope**: цены моделей в конфиге (config-ключи, напр. `cost.models.<name>.
  in_usd_per_mtok` / `out_usd_per_mtok`); стоимость per session (из уже
  парсящихся usage-токенов), per task (по привязанным сессиям), per project
  (P1.0), период; в Dashboard metrics (график $), в Session/Task Detail;
  бюджет-порог → watchdog-алерт (связка с P1.6). Честность: «оценка по
  заявленным ценам», а не факт.
- **Критерии**: расчёт в service + тесты (нет usage → «нет данных», не 0),
  UI + скриншоты, конфиг-ключи в Settings, OpenAPI, docs.

### P2.15 `whoami` для агентов — `new`
- **Scope**: `GET /whoami` (по API-ключу/роли сессии): моя роль+команда, мои
  pending/in-progress/blocked задачи, что ждёт меня (входящие), кто мой
  upstream/downstream (relatives), статус моей сессии. Компактный JSON для
  агента (используется `teamctl whoami`, P2.12).
- **Критерии**: эндпоинт + тесты (разные роли/ключи), OpenAPI, docs;
  реализуется совместно с P2.12 (одна итерация).

---

## P3 — долгосрочные проекты (если направление подтвердится)

### P3.12 Agent images / session snapshots — `new`
- **Scope**: захват продуктивного состояния сессии (промпт/скиллы/накопленное
  резюме) как «изображение агента»: create/list/pin/prune/preview; запуск
  новой сессии из снимка. Интеграция с Library (тип `agent-image`).
- **Критерии**: формат снимка, хранилище, UI, тесты, docs.

### P3.13 Proof/evidence на задачах — `new`
- **Scope**: привязка артефактов (скриншоты, тесты, логи) к задаче с typed
  frontmatter (type, verdict, candidate-sha) + `judge` (accept/reject/withdraw
  с причиной, атрибутированно); readiness задачи; «closure ≠ acceptance» —
  acceptance это вердикт по следующей строке.
- **Критерии**: модель, API, UI (панель evidence в Task Detail), тесты,
  OpenAPI, docs.

### P3.14 Walk — поэтапная онбординг-подача — `new`
- **Scope**: `walk` — последовательная (paced) доставка контекстных кусков в
  сессию (онбординг агента без «одного дампа»); UI: редактор последовательности
  + статус доставки.
- **Критерии**: логика pacing, тесты, UI, OpenAPI, docs.

### P3.15 Inbox/outbox — `new`
- **Scope**: mailbox-семантика: входящие с подтверждением (pending-входящие,
  absorb/deny/drop), исходящие с записью (outbox-record/list); `undelivered`
  (P0.2) подтягивается сюда.
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
