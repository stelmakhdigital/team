# Бэклог: порт функционала OpenRIG (P0–P3)

Источник: `docs/analysis/openrig-comparison.md` (2026-10-09). Каждое задание —
карточка с scope и критериями; исполнитель — pipeline (worker→planner→judge).
Статусы: `new` → `in_progress` → `done` (ссылка на T-id).

Порядок выполнения: P0.1 → P0.2 → P0.3 → **P1.0 Projects** → P1.4 → P1.5 → P1.6 → P2.* → P3.*.

---

## P0.5 → P1.0 Projects: сущность «проект» (мультипроектность, по запрос пользователя)

### P1.0 Projects entity + привязка к командам/сессиям — `new`
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

### P0.1 Attention view «Что требует меня» — `in_progress`
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

### P0.2 Overdue / дедлайны задач — `new`
- **Scope**: `closure_required_at` — вычисляется при переходе в `in_progress`
  (claim); базовое время из конфига (новый config-key, напр.
  `tasks.closure_required_secs`, layered resolution как у других); `GET /tasks`
  помечает `is_overdue`; отдельный overdue-список (bounded) на странице Tasks
  (фильтр) и в Attention. `undelivered` (задачи, чьё уведомление о создании не
  подтверждено) — optional в рамках той же карточки.
- **Критерии**: миграция (nullable timestamp), service-тесты вычисления и
  истечения, HTTP-тесты, UI-фильтр + скриншот, OpenAPI, конфиг-key в Settings.

### P0.3 Block with wake — `new`
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

### P1.4 Workflow engine: close-step → next task — `new`
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

---

## Не переносить (решение зафиксировано)
- Мультихост, TUI-модель, MCP-tools — позже/не в product-плане (см. анализ).

## Зависимости
- P0.3 (wake=watchdog) — опционально на P1.6 (timer/task-ветки работают без него).
- P1.5(b) handover — проще после P3.12 (snapshots), но не требует его.
- P2.11 — после проверки, реально ли runtime-адаптеры дают capture (P2.8 первый).
