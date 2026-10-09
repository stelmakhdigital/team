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

---

## 6. Получение результата

Результат работы агентов собирается в нескольких плоскостях:

1. **Задача закрыта** — `GET /tasks/{id}` (`state: done` + `closure_reason`),
   `GET /tasks/{id}/history` (вся цепочка переходов и handoff'ов).
2. **Live-терминал сессии** — UI Session Detail: live-транскрипт
   (WS-событие `session.output`, батчи ≤500 мс), метрики контекста
   (модель, % контекста, токены), `log_path`.
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
