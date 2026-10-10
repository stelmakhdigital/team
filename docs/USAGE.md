# Гайд: как пользоваться сервисом «team»

Сервис — это **Go-демон** (`backend/`, REST + WebSocket, порт 8080) и **веб-UI**
(`frontend/`, React). Демон управляет мультиагентными командами: вы собираете
команду (сегменты → роли-агенты), задаёте задачи и воркфлоу, запускаете
агент-сессии (в т.ч. pi-агенты в tmux) в выбранной рабочей директории и
получаете результат: статусы задач, live-транскрипты сессий, сообщения,
алерты watchdog, дашборд и аудит.

```
        ┌─────────────┐   /api/v1 (REST)  ┌──────────────────┐
 Browser ──►  Web UI   ─────────────────► │   Go-демон :8080 │
        └─────────────┘   /ws (WebSocket) └────────┬─────────┘
                                                   │ runtime-адаптеры
                                        process / tmux / pi-CLI
                                                   │
                                          рабочая директория
                                          (git-репо проекта)
```

---

## 1. Установка и запуск

### 1.1. Демон (backend)

Требования: Go ≥ 1.24.

```bash
cd backend
go build -o bin/daemon ./cmd/daemon

# минимум: SQLite-БД рядом, UI-порт 8080
DAEMON_DB_DSN=sqlite:./daemon.db ./bin/daemon
# → http://localhost:8080
```

Важные env (полный список — `backend/README.md` и страница Settings в UI):

| Env | Default | Назначение |
|---|---|---|
| `DAEMON_LISTEN_ADDR` | `:8080` | HTTP-адрес |
| `DAEMON_DB_DSN` | `sqlite:./daemon.db` | `sqlite:<path>` или `postgres://user:pass@host:5432/db` |
| `DAEMON_CONFIG_FILE` | `daemon.yaml` (cwd) | опциональный YAML-слой конфигурации (см. §2) |
| `DAEMON_AGENT_SPECS_DIR` | `agents` | каталог agent.yaml-спеков ролей |
| `DAEMON_API_KEYS` | — | ключи через запятую; включает auth `X-API-Key` |
| `DAEMON_SECRET_KEY` | — | 64 hex; AES-256-GCM для secrets (не задан — выключен) |
| `DAEMON_LOGS_DIR` | `logs/sessions` | transcript-файлы сессий |
| `DAEMON_SESSION_CONFIGS_DIR` | `configs/sessions` | конфиги pi-сессий |
| `DAEMON_WATCHDOG_*` | enabled/30s/7200s/3600s | watchdog: период скана, пороги stale/blocked |

> ⚠️ **Частая ошибка**: `DAEMON_AGENT_SPECS_DIR` по умолчанию относительный
> `agents` (от cwd демона). Запускайте демон **из корня проекта** (там лежит
> `agents/` с pi-lead/pi-worker/pi-reviewer.yaml) либо задайте
> `DAEMON_AGENT_SPECS_DIR=/abs/path/to/project/agents`. Иначе роли с
> `agent_spec` создаются с 404.

### 1.2. Web UI (frontend)

```bash
cd frontend
npm install
npm run dev            # dev-сервер (mock-данные)
```

Режимы (env Vite):

| Env | Default | Назначение |
|---|---|---|
| `VITE_API_MODE` | `mock` | `mock` — demo-данные; `real` — живой демон |
| `VITE_API_BASE_URL` | same-origin | в dev Vite-прокси шлёт `/api` и `/ws` на `:8080` |
| `VITE_API_KEY` | — | API-ключ (header `X-API-Key`; для WS — `?api_key=`) |

Пример подключения к живому демону:

```bash
VITE_API_MODE=real VITE_API_KEY=<ключ> npm run dev
```

Браузер: `http://localhost:5173`.

### 1.3. Health-check

```bash
curl localhost:8080/healthz    # liveness
curl localhost:8080/readyz     # readiness (БД доступна)
```

---

## 2. Конфигурация

Конфигурация — типизированный реестр ключей с **layered resolution**
(приоритет low → high):

```
default  <  file (daemon.yaml)  <  env (DAEMON_*)  <  override (БД, из UI)
```

Для каждого ключа видно: тип, текущее значение, значение по умолчанию,
**источник** (какой слой выиграл) и имя env-переменной/пути файла.

### 2.1. Из UI (страница **Settings**)

- Секции: `server`, `storage`, `sessions`, `watchdog`, `security`,
  `logging`, `workspace` + блок **Meta** (readonly: версия, uptime,
  git-хед/ветка, хэш действующей конфигурации).
- **Workspace** — рабочая область: корневой путь и папки артефактов/логов.
- **Environment** — env-переменные видны в провенансе (source=env).
- **Set** — ставит override (наивысший слой, хранится в БД); **Reset** —
  сбрасывает override (возврат к env/file/default).
- Ключи, требующие рестарта демона, помечены; ответ set несёт
  `needs_restart: true`, если значение реально изменилось.
- Секрет-ключи (`security.api_keys`, `security.secret_key`) не редактируются.
- Host-derived дефолты помечены «(derived)» — это значения, выведенные под
  вашу машину, не универсальные константы.

### 2.2. По API

```bash
# все ключи + meta (секции, current/default/source per key)
curl -H "X-API-Key: <key>" localhost:8080/api/v1/config

# выставить override
curl -X PUT -H "X-API-Key: <key>" -d '{"value":"127.0.0.1:9000"}' \
  localhost:8080/api/v1/config/server.listen_addr

# сбросить override
curl -X DELETE -H "X-API-Key: <key>" \
  localhost:8080/api/v1/config/server.listen_addr
```

Неверный тип значения → 400; неизвестный ключ → 404.
`config.update` — право роли **admin** только (RBAC, §5).

### 2.3. Файл конфигурации

Опциональный YAML (путь из `DAEMON_CONFIG_FILE`, default `daemon.yaml` в cwd)
переопределяет default; env и override переопределяют файл. Слой `file`
видно в UI в колонке source (`source_detail` = путь файла).

---

## 3. Создание команды

UI: **Teams → «New Team»** → **Team Builder** (`/teams/:teamId`).

Модель: **team → segments (функциональные зоны) → roles (конкретные
агенты)** + **relatives** (связи между ролями — кто кому отдаёт/получает
работу).

1. **Сегмент**: `+ Segment` (напр. `backend`, `frontend`, `qa`). У сегмента —
   имя, описание, layout-позиция.
2. **Роли**: в сегменте `+ Role`. У роли:
   - `name` (входит в адрес: `team:segment.role` — после создания имя
     роли менять нельзя: это lookup-ключ для задач/чатов/сессий);
   - `agent_spec` — путь к agent.yaml (ищется в `DAEMON_AGENT_SPECS_DIR`;
     готовые фикстуры: `pi-lead`, `pi-worker`, `pi-reviewer`);
   - `pi_config`/`resources`/`startup` — из spec (модель, скиллы, tools);
   - роль редактируема через **ConfigPanel** (PATCH config) — всё, кроме имени.
3. **Relatives**: связи между ролями (кто кому — «relatives»); можно
   создавать/редактировать layout/удалять.
4. **Проверка и сохранение**: **Validate** (проверяет топологию: циклы,
   dangling-связи, адресность) → **Save** (`POST /teams/{id}/save`; с
   `save_to_library: true` команда одновременно сохраняется в Library).

API (эквивалент):

```bash
curl -X POST -d '{"name":"dev-team"}' localhost:8080/api/v1/teams
curl -X POST -d '{"name":"backend"}' localhost:8080/api/v1/teams/<id>/segments
curl -X POST -d '{"name":"go-dev","agent_spec":"pi-worker.yaml"}' \
  localhost:8080/api/v1/segments/<segId>/roles
curl -X POST -d '{}' localhost:8080/api/v1/teams/<id>/validate
curl -X POST -d '{}' localhost:8080/api/v1/teams/<id>/save
```

Спеку команды в YAML можно посмотреть в **SpecYamlPanel** (вкладка в Team
Builder) — её же можно использовать как «source» для Library.

**Library**: готовые команды/воркфлоу — `POST /library` (снапшот),
`POST /library/{id}/apply` (применить к новой/существующей команде с
overrides) — это «init-workspace» сервиса: не собирать с нуля, а применить
проверенный шаблон.

---

## 4. Воркфлоу

UI: **Workflows** → новый/открыть → **Workflow Editor** (React Flow,
drag&drop).

- **Блоки**: `task | decision | parallel | loop | agent | manual` —
  `+ Block`, у блока — `label`, `position`, `config` (напр. для `agent` —
  целевая роль/команда; для `task` — заголовок/тело).
- **Связи**: тяните от блока к блоку (`connection`, опц. `condition` —
  условие перехода для decision-веток).
- **Состояния воркфлоу**: `draft → active → archived`; rename — ✎ в шапке;
  delete — каскад (связи → блоки → workflow).

```bash
curl -X POST -d '{"team_id":1,"name":"build-and-test"}' \
  localhost:8080/api/v1/workflows
curl -X POST -d '{"type":"agent","position":{"x":0,"y":0},"label":"go-dev"}' \
  localhost:8080/api/v1/workflows/<id>/blocks
curl -X POST -d '{"from_block_id":"<b1>","to_block_id":"<b2>"}' \
  localhost:8080/api/v1/workflows/<id>/connections
```

---

## 5. Запуск работы: задачи и сессии

### 5.0. Проекты (мультипроектность)

UI: **Projects** (список + создание + archive) и **Project Detail** (счётчики +
связанные команды/задачи). Проект = «рабочее дерево» (репозиторий/директория),
к которому привязываются команды, задачи и сессии.

- **Создание**: `POST /projects` — `kind=existing` (существующий каталог: проверка
  существования/записи + авто-детект git) или `kind=new` (родительский каталог +
  `path` → mkdir + опционально `git init` + README). Ошибки: `404` (нет каталога),
  `409` (занятое имя/путь).
- **Привязки**: команда создаётся с `project_id` (в форме Teams → селект «Project»);
  задача наследует `project_id` команды (или override при создании); сессия
  наследует от задачи/команды. Переупорядочить команду в другой проект нельзя
  (только на создании).
- **Рабочая директория сессии**: `working_dir` разрешается по цепочке
  `explicit > task.project.path > team.project.path > explicit/пусто`. Явный
  `working_dir` (override) всегда приоритетнее.
- **Фильтры**: Tasks — по проекту (dropdown «Project»); API: `GET /tasks?project_id=`,
  `GET /sessions?project_id=`.
- **Archive**: `DELETE /projects/{id}` = архивация (идемпотентна); архивный проект
  нельзя использовать для новых команд/сессий (`409`).
- **Backward-compat**: команды/задачи/сессии без `project_id` работают как раньше;
  отсутствие проекта НЕ даёт 400 (400 — только для «проект указан, но не найден/архивен»).

### 5.1. Задачи ( Tasks)

UI: **Tasks** (список, детали, история переходов) и **Dashboard** (активные).

Жизненный цикл: `pending → in_progress → done | blocked | canceled`
(`blocked → pending/in_progress/...`); из `done/canceled` — 409.
Закрытие в `done` требует `closure_reason`:
`handed_off_to | blocked_on | denied | canceled | no_follow_on | escalation`.

- **Создание**: `POST /tasks` — `team_id`, `destination_role_id` (кому
  адресована), `title`, `body?`, `parent_task_id?` (subtask), `priority?`.
- **Handoff**: `POST /tasks/{id}/handoff` (`to_role_id`) — закрывает задачу
  и создаёт передачную у целевой роли (цепочка передачи между агентами).
- **Watchdog**: `in_progress` старше 2ч помечается `is_stale`, `blocked` —
  `is_blocked`; алерты видны в **Dashboard → Alerts** и `GET /watchdog/events`.

### 5.2. Сессии (агент-процессы)

Сессия = запуск конкретного агента (роли) в **выбранной рабочей
директории**. UI: сессии видны в **Dashboard → Sessions**, **Team Builder**
и **Session Detail** (live-терминал, метрики контекста, история).

```bash
# запуск: роль + команда + рабочая директория
curl -X POST "localhost:8080/api/v1/sessions?team_id=1" \
  -d '{
    "role_id": 42,
    "command": "pi",
    "args": ["--no-extensions"],
    "runtime_type": "pi",
    "working_dir": "/home/user/CODE/PROJECTS/my-project",
    "queue_task_id": 7
  }'
```

| Поле | Смысл |
|---|---|
| `role_id` | чья это сессия (из команды) |
| `command` / `args` | что запускать |
| `runtime_type` | `process` (локальный процесс) / `tmux` (отдельная tmux-сессия) / `pi` (pi-CLI в tmux + автогенерируемый конфиг из `agent_spec` роли); `container`/`k8s` — вне сборки (400) |
| `working_dir` | **рабочая директория, в которой работает агент** (передаёте свой проект сюда) |

> **Мультипроектность**: `working_dir` задаётся **на каждой сессии** (не только в
> Settings — секция Workspace в Settings это собственная рабочая область демона:
> артефакты, логи, конфиги сессий). Сессии одной или разных команд могут работать
> в разных директориях одновременно. Сущность «Project» (реестр проектов, привязка
> команды к проекту, автозаполнение working_dir, фильтры по проекту) — в бэклоге
> (`docs/backlog-openrig-port.md`, P1.0 Projects).
| `config` | доп. конфиг сессии |
| `queue_task_id` | привязка к задаче (статус задачи ↔ сессия) |

Runtime `pi` — основной путь «агент кодит проект»: демон берёт
`agent_spec` роли (модель/скиллы/системный промпт), генерирует
`configs/sessions/<id>.yaml` и запускает pi в tmux в `working_dir`.

Управление:

```bash
curl "localhost:8080/api/v1/sessions?team_id=1&active=true"   # список
curl localhost:8080/api/v1/sessions/<id>                      # статус, uptime, exit_code
curl "localhost:8080/api/v1/sessions/<id>/transcript?limit=200"  # вывод (stdout+stderr)
curl -X DELETE localhost:8080/api/v1/sessions/<id>            # stop (SIGTERM)
```

#### 5.2.1. Контекст-стена и handover (P1.5)

Когда сессия «упирается» в контекст (context_used_percentage >= порога), её
переводят в **новую сессию той же роли с переданным контекстом** (handover).

- **Порог** — config-ключ `sessions.context_wall_pct` (default `80`, можно менять
  в Settings без рестарта; env `DAEMON_SESSION_CONTEXT_WALL_PCT`).
- **Компактный план** — `GET /compact-plan`: живые сессии у контекст-стены
  (sort desc по %), `suggested_action` = `handover` / `wait_for_turn`.

```bash
curl "localhost:8080/api/v1/compact-plan"                          # кто у контекст-стены
curl -X POST localhost:8080/api/v1/sessions/<id>/handover \
  -d '{"reason": "context wall 88%"}'                               # handover
```

| Поле | Смысл |
|---|---|
| `reason` | **обязательная** причинa (попадает в комментарий handoff задачи) |
| `dry_run` | `true` — только план (без мутаций) |
| `target_working_dir` | опц. рабочая директория наследника (по умолчанию как у предшественника) |

Результат — честный **per-step отчёт**: `context_prepared` / `successor_started` /
`task_reassigned` / `old_stopped` (каждый `ok`/`failed`/`skipped`/`would`) +
`new_session_id`. Наследник получает контекст-пакет (хвост transcript + путь к
полному файлу + задача): `pi` — в `system_prompt`, `process`/`tmux` — через env
`HANDOVER_CONTEXT_FILE`. Задача (если есть) уходит существующим handoff → новая
задача той же роли. Ограничения: только живая сессия (из stopped/повторный → 409),
runtime `pi`/`tmux`/`process`.

**UI**: панель «Context wall» в Dashboard + контекст-стенa banner на сессии с
кнопкой «Продолжить в новой сессии» (модалка: reason → Показать план → Выполнить).

#### 5.2.2. Session briefing — «входящий контекст при запуске» (P1.7)

При запуске сессии агент получает компактный **briefing** — «что я делаю, где я,
чего ждали, что новое»: задача (если в контексте) + последние переходы, команда
(адрес + соседи), проект (git branch/head + открытые задачи), последние сообщения
(direct + chatroom) и unread-alerts.

**Как передать агенту** (по адаптеру):
- **pi** → `system_prompt` в pi-конфиге (merges с handover-prompt P1.5);
- **process/tmux** → файл `<logs>/briefings/<id>.md` + env `BRIEFING_FILE`.

**Персонализация** (config сессии): `briefing` (`"none"` = выключить),
`briefing_override` (свой текст вместо builtin), `briefing_append` (добавка в конец).

**UI**:
- **TeamBuilder** → кнопка «📋 Брифинг» → модалка «Предпросмотр брифинга»
  (dry-run, без запуска): что агент получит + override/append-поля + delivery-режим.
- **Session Detail** → блок «Briefing (startup context)»: applied-badge (builtin/override)
  + путь к файлу + полный текст брифинга.

**Preview API**: `POST /api/v1/teams/{id}/briefing/preview` (dry-run, без мутаций).

#### 5.2.3. Send to session + verify (P2.11)
Оператор может **отправить текст в терминал живой сессии** (tmux/pi) — ввод через
`tmux send-keys` + **честная верификация доставки** capture-м. Это действие на
работающий процесс (не read-only), поэтому требует **operator+** (`sessions.update`).

```bash
curl -X POST localhost:8080/api/v1/sessions/<id>/send \
  -d '{"text": "продолжай с миграцией", "submit": true}'
```

**Статусы** (честно, как есть):
- **`delivered`** — текст подтверждён capture-м (на экране), `verified=true`, `tail`.
- **`staged`** — `send-keys` отдал 0 (введён), но capture не подтвердил (напр. LLM
  «поглотил» ввод).
- **`retained`** — не отправлено: **interactive-prompt guard** (эвристика — сессия,
  похоже, ждёт ответа на вопрос; текст НЕ вводится, чтобы не сломать ввод).

**Ограничения**:
- `process` runtime → **409** (нет терминала, куда печатать).
- tmux не установлен / сессия мертва → **409** (с причиной).
- `text` ≤ 2000 символов (400 иначе).

**UI** (Session Detail, tmux/pi): блок «Send to session» — textarea + «Submit»
checkbox + Send + warning (действие на живой процесс). Статус-бейдж (delivered /
staged / retained) + guard-причина + collapsible capture tail. Событие `session.send`
попадает в **Pulse** (P2.9).

### 5.3. Workflow engine (close-step → next task, P1.4)

Воркфлоу можно **исполнять**: каждый шаг становится задачей, и закрытие задачи
автоматически создаёт следующий шаг. Engine включается тумблером **Run** в списке
воркфлоу (`Workflows`), далее кнопка **Run** запускает инстанс.

```bash
# включить engine (toggle в UI или PATCH)
curl -X PATCH localhost:8080/api/v1/workflows/1 \
  -d '{"name": "Build & Review", "engine_enabled": true}'

# запустить инстанс
 curl -X POST localhost:8080/api/v1/workflows/1/run \
  -d '{"root_objective": "build the feature"}'          # → 201 {instance}

# список / детали / trail
 curl "localhost:8080/api/v1/workflows/1/instances"       # → {instances, total}
 curl localhost:8080/api/v1/workflow-instances/1          # → {instance, steps, active_step_ids}
 curl localhost:8080/api/v1/workflow-instances/1/trace    # → {steps, total}

# управление
 curl -X POST localhost:8080/api/v1/workflow-instances/1/resume   # failed → running (re-arm)
 curl -X POST localhost:8080/api/v1/workflow-instances/1/abort    # active задачи → canceled
 curl -X POST localhost:8080/api/v1/workflow-instances/1/route \
  -d '{"to_role_id": 7}'                                    # active-шаг → другая роль
```

Состояния: инстанс `running | waiting | completed | failed | aborted`; шаг
`pending | active | done | failed | skipped | routed`. UI: страница **Workflow Instance**
(trail, Route active step, Resume/Abort). Состояния `failed`/`waiting` видны в
**Attention** («Что требует меня»). Ссылка: `docs/contracts/api-decisions.md` §P1.4.

---

### 5.4. Watchdog-джобы (политики слежения, P1.6)

Watchdog-джоб — повторяемая проверка по состоянию (v1: **context-usage-threshold**):
когда live-контекст целевой сессии пересекает порог — алерт в Attention + (если
поставлен) разбудить parked-задачу. В отличие от разового context wall (P1.5 —
статус сессии) это **зарегистрированная политика** с историей.

```bash
# зарегистрировать джоб: context сессии 3 >= 80% → алерт
 curl -X POST localhost:8080/api/v1/watchdog/jobs \
   -d '{"name": "worker ctx", "target_session_id": 3, "threshold_pct": 80, "message": "context high"}'  # → 201

# список / детали (+ последние оценки)
 curl "localhost:8080/api/v1/watchdog/jobs"                # → {jobs, total}
 curl localhost:8080/api/v1/watchdog/jobs/1                # → {job, recent_evaluations}

# остановить (enabled=false, история сохраняется) / удалить (только если ещё не fired)
 curl -X POST localhost:8080/api/v1/watchdog/jobs/1/stop   # → 200 (enabled=false)
 curl -X DELETE localhost:8080/api/v1/watchdog/jobs/1      # → 200; с историей → 409
```

Повторный fire — только при росте контекста > 5 п.п. от последнего (иначе skip,
без алерта). Если target-сессия умерла (stopped/failed/удалена) — дjob
автоматически останавливается + `dead-target` в историю. В **Attention** —
`watchdog_job` (high, ссылка на сессию). Sвязка с P0.3: blocked-задача с
`wake.kind='watchdog'` (ref=job id) разблокируется при fire. UI: страница **Watchdog**
(список + создание + stop/delete) + wake-форма на задаче. Ссылка:
`docs/contracts/api-decisions.md` §P1.6.

### 5.5. Health / Doctor-панель (P1.8)

«The surface that shows the truth» — что daemon/БД/tmux/сессии/logs/git думают о
состоянии и **где расходятся**. Read-only: показывает расхождения, не чинит.

**Чек'и** (status ok/warn/fail + detail + evidence):
- `daemon` — up + version + uptime;
- `database.ping` — БД отвечает;
- `database.migrations` — applied vs available (расхождение → warn);
- `tmux.server` — сервер жив (warn, если нет — process/pi не зависят);
- **`sessions.divergence`** — active в БД vs живость в адаптере (расхождение → **fail**);
- `logs.dir` — writable;
- `git` — read-only статус по git-проектам (branch/head).

**UI**: страница **Health** (nav) — overall-баннер (ok/warn/fail) + список чек'ов +
кнопка «Run full diagnostics» + **drill-down** (EntityLink на сессию-расхождение:
stale 👻 / zombie 🧟). **Aлерт**: при «Run full diagnostics» fail-чеги →
unread-алерт **diagnostics_fail** (critical, link на Health) в **Attention** (с
дедупом). **API**: `GET /api/v1/diagnostics`, `POST /api/v1/diagnostics/run`
(RBAC `diagnostics.read`, operator+). Ссылка:
`docs/contracts/api-decisions.md` §P1.8.

---

## 6. Получение результата

Результат работы агентов собирается в нескольких плоскостях:

1. **Задача закрыта** — `GET /tasks/{id}` (`state: done` + `closure_reason`),
   `GET /tasks/{id}/history` (вся цепочка переходов и handoff'ов).
2. **Live-терминал сессии** — UI Session Detail: live-транскрипт
   (WS-событие `session.output`, батчи ≤500 мс), метрики контекста
   (модель, % контекста, токены), `log_path`. При **контекст-стене**
   (>= порога `sessions.context_wall_pct`) — banner + кнопка
   «Продолжить в новой сессии» (handover, §5.2.1); такие сессии
   видны в Dashboard-панели «Context wall» и в Attention (`session_context_wall`).
3. **Файл-транскрипт** — `GET /sessions/{id}/transcript` (последние строки)
   + полный файл в `DAEMON_LOGS_DIR` (`logs/sessions/...`).
4. **Сообщения** — агенты пишут друг другу и оператору:
   - **Messages**: `direct | broadcast | segment` (`POST /messages`),
     фильтры по роли/типу/задаче;
   - **Chatrooms** (автосоздаются: team-level + `<segment>-general`):
     `GET /chatrooms?team_id=`, лента `GET /chatrooms/{id}/messages`
     (открытие чата помечает прочитанным), `POST /chatrooms/{id}/messages`
     (вы — `You`, `is_mine`).
   - Служебные события (`system|watchdog`) только из сервера.
5. **Dashboard** — сводка (`/dashboard/summary`), активные задачи
   (`/dashboard/tasks`), активные сессии (`/dashboard/sessions`), алерты
   (`/dashboard/alerts`), метрики по диапазону 1h/24h/7d
   (`/dashboard/metrics`: создано/завершено задач, активные сессии,
   очередь, LLM-токены) — всё обновляется live по WebSocket
   (`GET /ws`, каналы `dashboard`, `team:<id>`, `session:<id>`; события
   `task.* / session.* / message.* / alert.*`).
6. **Аудит** — `GET /audit` (кто/что/когда/с какими параметрами; фильтры
   user/action/resource/период) — в UI страница **History** с drill-down.
7. **Артефакты** — сам результат работы агента лежит в `working_dir`
   (git-коммиты, файлы, тесты) — это рабочая директория, которую вы
   передали при запуске сессии.

Типовой сценарий «от задачи к результату»:

```
Создали команду → задали воркфлоу/задачу (destination_role) →
запустили сессию роли (pi, working_dir=<ваш проект>) →
следите в Dashboard/Session Detail (live) →
агент завершает: задача → done (closure_reason), транскрипт + артефакты
в working_dir, итог в chat/audit → (опц.) сохраняете команду/воркфлоу
в Library и применяете к следующему проекту.
```

### 6.1. Поиск по транскриптам (P2.7)

Поиск фразы/решения по истории вывода всех сессий (транскрипты). Страница
**Search** (nav): вводите `q` (+фильтры команда/роль/состояние) → «Найти».
Результаты сгруппированы по сессии (роль/команда/состояние + **EntityLink** на
сессию), под каждой — совпадения с контекст-окном (±3 строки, подсветка).
Точный переход — на Session Detail (live-терминал). **API**: `GET /api/v1/sessions/search`
(RBAC sessions.read, viewer-ok). Механика: наивный bounded-поиск по transcript-
файлам (case-insensitive, tail-ограничение, read-only). Ссылка:
`docs/contracts/api-decisions.md` §P2.7.

### 6.2. Capture — стл-кадр экрана сессии (P2.8)

«Стл-кадр» экрана/вывода сессии — последние N строк как артефакт (можно
скопировать/сохранить). На **Session Detail** → кнопка **«📷 Capture»** (в карточке
Transcript) → модалка со снимком (mono, readonly), **source** (`tmux-pane`/
`log-tail`), `captured_at`, `saved_path` (путь к сохранённому снапшоту) + «📋 Copy».
**API**: `GET /api/v1/sessions/{id}/capture?lines=N` (RBAC sessions.read, viewer-ok;
lines default 20, max 200). Механика: **tmux/pi → `tmux capture-pane`** (реальный
экран; мёртвый tmux → 409 с причиной), **process → log-tail** (у process нет экрана
— лог; файла нет → пустой снимок). Снимок сохраняется в `<logs>/captures/` (bounded:
≤20 на сессию). Read-only. Ссылка: `docs/contracts/api-decisions.md` §P2.8.

### 6.3. Pulse — единая лента событий (P2.9)

На **Pulse** (nav, `/pulse`) — единая лента всего, что происходит в системе
(задачи, сессии, сообщения, алерты, воркфлоу): история из БД + live-обновление по
WS. Каждая запись: **type-бейдж** (цвет по категории), **summary**, **ссылка на
сущность** (EntityLink), команда, relative time. Фильтры: **тип / команда / роль**.
Лента bounded (≤5000 строк ИЛИ ≤7дн.), `session.output` (live-транскрипт) в неё не
попадает. Отдельно от **Attention** («что требует меня») и от **History**
(per-entity/аудит). **API**: `GET /api/v1/events` (RBAC dashboard.read, viewer-ok).
Ссылка: `docs/contracts/api-decisions.md` §P2.9.

### 6.4. Command palette — быстрый переход (P2.10)

**`Cmd+K` / `Ctrl+K`** открывает глобальную палитру (fuzzy-переход). Закрыть: `Esc`
или клик мимо. Стрелки ↑/↓ — выбор, `Enter` — переход/действие. Разделы:
**Страницы** (все маршруты), **Действия** (New team/task/project/workflow, Run
diagnostics, Go to Attention), **Сущности** (команды, задачи, сессии, воркфлоу,
проекты — lazy-поиск по name/title). Совпавшие символы подсвечены. Работает с
клавиатуры, с aria-разметкой. Ссылка: `frontend.md` §3q.

---

## 7. Безопасность (ключи и RBAC)

Auth включается, если заданы `DAEMON_API_KEYS` **или** в БД есть ключи.
Ключ передаётся: `X-API-Key` / `Authorization: Bearer` / `?api_key=` (WS).

- Роли: **admin** (всё, вкл. `config.update`), **operator** (без
  `config.update`), **viewer** (read-only). Нет права → 403 `forbidden`.
- Env-ключи (`DAEMON_API_KEYS`) — admin без привязки к user.
- Ключи из БД — с user и сроком; показываются один раз:

```bash
./bin/daemon admin keys create --name=frontend --role operator [--user=fe] [--expires=720h]
./bin/daemon admin keys list
./bin/daemon admin keys revoke <id>
```

- Все ошибки — единый envelope `{error: {code, message, request_id, details?}}`
  + header `X-Request-Id` (трейсинг по audit).

---

## 8. БД: SQLite / PostgreSQL

SQLite — дефолт. PG — опционально (ADR-002):

```bash
# 1) скопировать sqlite-данные в PG
./bin/daemon migrate pg --to postgres://user:pass@host:5432/db \
  [--from sqlite:/path/to.db] [--force]

# 2) переключить демон
DAEMON_DB_DSN=postgres://user:pass@host:5432/db ./bin/daemon
```

`migrate pg` применяет PG-миграции и копирует все таблицы (FK-порядок,
batch, cast'ы); повтор — только с `--force` (TRUNCATE).

---

## 9. Быстрая шпаргалка

| Хочу… | Где |
|---|---|
| Запустить сервис | `bin/daemon` + `npm run dev` (§1) |
| Настроить параметры/рабочую область | UI **Settings** (§2) |
| Создать команду | UI **Teams → Team Builder** (§3) |
| Повторить готовую команду | UI **Library → Apply** |
| Нарисовать процесс | UI **Workflows → Editor** (§4) |
| Дать агенту задачу | UI **Tasks** / `POST /tasks` (§5.1) |
| Запустить агента в своём проекте | `POST /sessions` c `working_dir` (§5.2) |
| Смотреть, что агент делает | Dashboard / Session Detail (live) (§6.2) |
| Получить результат | task done + transcript + артефакты в working_dir (§6) |
| Кто и что менял | UI **History** (audit) |
| Что не так (зависания) | Dashboard **Alerts** (watchdog) |
| Полный API | `docs/contracts/openapi.yaml` (63 операции, сверяется `scripts/check_openapi.py`) |

## 10. Полезные ссылки

- `docs/contracts/openapi.yaml` — полная REST-спека (source of truth)
- `docs/contracts/api-decisions.md` — решения по API (RBAC, config, workflows)
- `docs/contracts/error-model.md` — модель ошибок
- `docs/architecture/backend.md`, `docs/architecture/frontend.md` — архитектура
- `backend/README.md` — env, команды, security, миграции
- `agents/*.yaml` — образцы agent spec (pi-lead/pi-worker/pi-reviewer)
- `frontend.md`, `backend.md` (корень) — статусы ролей и бэклоги
