
# Cхема БД

По дефолту нужно чтобы БД создавалась в локальной SQLite (или что порекомендуешь). Но с возможностью конфигурации на PostgreSQL (локальной или внешней).

## Общая структура

**Ключевые сущности:**

- **Team** — команда агентов (аналог rig)
- **Segment** — логическая группа внутри команды (аналог pod)
- **Role** — конкретная роль/агент в сегменте (аналог seat)
- **Relative** — связь между ролями (аналог edge)
- **Queue tasks** — задачи в очереди
- **History status** — история переходов задач
- **Projects** — проекты
- **Goals** — цели внутри проектов


## Схема БД (PostgreSQL)

### 1. `teams` — команды

```sql
CREATE TABLE teams (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,      -- например, 'dev-team', 'research-team'
    spec_path       TEXT NOT NULL,             -- путь к YAML-спецификации команды
    description     TEXT,
    
    -- жизненный цикл
    state           TEXT NOT NULL DEFAULT 'active' 
                    CHECK (state IN ('active', 'archived', 'stopped')),
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE teams IS 'Команда агентов с общей топологией и конфигурацией';
```


______________________________________________________________________

### 2. `segments` — сегменты (группы ролей)

```sql
CREATE TABLE segments (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,             -- например, 'backend', 'review', 'infra'
    description     TEXT,
    
    -- конфигурация сегмента
    config          JSONB,                     -- startup файлы, guidance, политики
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    UNIQUE(team_id, name)
);

COMMENT ON TABLE segments IS 'Логическая группа ролей внутри команды (bounded context)';
```


______________________________________________________________________

### 3. `roles` — роли (агенты)

```sql
CREATE TABLE roles (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    segment_id      BIGINT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
    
    -- идентификаторы
    name            TEXT NOT NULL,             -- например, 'lead', 'backend-dev', 'reviewer'
    address         TEXT NOT NULL,             -- уникальный адрес: 'dev-team:backend.lead'
    
    -- конфигурация
    agent_spec      TEXT NOT NULL,             -- путь к agent.yaml (спецификация агента)
    profile         TEXT,                      -- профиль из agent_spec
    config          JSONB,                     -- дополнительные настройки роли
    
    -- состояние
    state           TEXT NOT NULL DEFAULT 'active'
                    CHECK (state IN ('active', 'inactive', 'blocked')),
    
    -- сессия (если роль активна)
    session_id      BIGINT,                    -- ссылка на текущую сессию
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    UNIQUE(team_id, segment_id, name)
);

COMMENT ON TABLE roles IS 'Конкретная роль/агент в сегменте команды';
```


______________________________________________________________________

### 4. `relatives` — связи между ролями

```sql
CREATE TABLE relatives (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    
    -- кто с кем связан
    from_role_id    BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    to_role_id      BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    
    -- тип связи
    type            TEXT NOT NULL CHECK (
        type IN (
            'delegates_to',      -- делегирует задачи
            'spawned_by',        -- порождён другой ролью
            'can_observe',       -- может наблюдать за выходом
            'collaborates_with'  -- равноправное сотрудничество
        )
    ),
    
    -- метаданные
    config          JSONB,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- защита от дубликатов
    UNIQUE(from_role_id, to_role_id, type)
);

COMMENT ON TABLE relatives IS 'Связи между ролями: делегирование, наблюдение, сотрудничество';
```


______________________________________________________________________

### 5. `projects` — проекты

```sql
CREATE TABLE projects (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT REFERENCES teams(id) ON DELETE SET NULL,
    
    -- идентификаторы
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL UNIQUE,      -- краткое имя для URL/адресов
    
    -- описание
    description     TEXT,
    readme          TEXT,                      -- подробное описание проекта
    
    -- конфигурация
    config          JSONB,                     -- настройки проекта (репозитории, доступы)
    
    -- состояние
    state           TEXT NOT NULL DEFAULT 'active'
                    CHECK (state IN ('active', 'archived', 'on_hold')),
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE projects IS 'Проект с целями, задачами и ресурсами';
```


______________________________________________________________________

### 6. `goals` — цели

```sql
CREATE TABLE goals (
    id              BIGSERIAL PRIMARY KEY,
    project_id      BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_goal_id  BIGINT REFERENCES goals(id),  -- для декомпозиции целей
    
    -- идентификаторы
    title           TEXT NOT NULL,
    slug            TEXT NOT NULL,
    
    -- описание
    description     TEXT,
    
    -- приоритет и статус
    priority        INTEGER NOT NULL DEFAULT 0,  -- чем выше, тем важнее
    state           TEXT NOT NULL DEFAULT 'pending'
                    CHECK (state IN ('pending', 'in_progress', 'blocked', 'done', 'canceled')),
    
    -- метрики
    progress        INTEGER NOT NULL DEFAULT 0 
                    CHECK (progress >= 0 AND progress <= 100),  -- процент выполнения
    
    -- сроки
    due_date        TIMESTAMPTZ,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    UNIQUE(project_id, slug)
);

COMMENT ON TABLE goals IS 'Цель в рамках проекта, может иметь подцели';
```


______________________________________________________________________

### 7. `queue_tasks` — задачи в очереди

```sql
CREATE TABLE queue_tasks (
    id                  BIGSERIAL PRIMARY KEY,
    team_id             BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    
    -- иерархия задач
    parent_task_id      BIGINT REFERENCES queue_tasks(id) ON DELETE SET NULL,
    
    -- привязка к проекту/цели
    project_id          BIGINT REFERENCES projects(id) ON DELETE SET NULL,
    goal_id             BIGINT REFERENCES goals(id) ON DELETE SET NULL,
    
    -- кто отвечает
    destination_role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    source_role_id      BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    
    -- сессия исполнителя
    session_id          BIGINT,
    
    -- содержание задачи
    title               TEXT NOT NULL,
    body                TEXT NOT NULL,             -- подробное описание
    body_context        JSONB,                     -- контекст (файлы, коммиты, ссылки)
    
    -- состояние
    state               TEXT NOT NULL DEFAULT 'pending'
                        CHECK (
                            state IN (
                                'pending',
                                'in_progress',
                                'done',
                                'blocked',
                                'canceled'
                            )
                        ),
    
    -- причина завершения (для state = 'done')
    closure_reason      TEXT CHECK (
        closure_reason IS NULL OR closure_reason IN (
            'handed_off_to',    -- передано другой роли
            'blocked_on',       -- заблокировано на другой задаче
            'denied',           -- отклонено получателем
            'canceled',         -- отменено
            'no_follow_on',     -- завершено окончательно
            'escalation'        -- эскалировано выше
        )
    ),
    
    -- ссылка на целевую задачу (для handed_off_to, blocked_on, escalation)
    closure_target_id   BIGINT REFERENCES queue_tasks(id),
    
    -- таймеры (для blocked задач)
    blocked_since       TIMESTAMPTZ,
    park_timeout_secs   INTEGER,
    
    -- метрики
    priority            INTEGER NOT NULL DEFAULT 0,
    estimated_secs      INTEGER,
    actual_secs         INTEGER,
    
    -- временные метки
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ
);

COMMENT ON TABLE queue_tasks IS 'Задача в очереди с явным владельцем (ролью)';

-- Индексы для частых запросов
CREATE INDEX idx_queue_tasks_team_state ON queue_tasks(team_id, state);
CREATE INDEX idx_queue_tasks_destination ON queue_tasks(destination_role_id, state);
CREATE INDEX idx_queue_tasks_project ON queue_tasks(project_id, state);
CREATE INDEX idx_queue_tasks_goal ON queue_tasks(goal_id, state);
```


______________________________________________________________________

### 8. `history_status` — история переходов задач

```sql
CREATE TABLE history_status (
    id                  BIGSERIAL PRIMARY KEY,
    queue_task_id       BIGINT NOT NULL REFERENCES queue_tasks(id) ON DELETE CASCADE,
    
    -- переход состояний
    from_state          TEXT,
    to_state            TEXT NOT NULL,
    
    -- причина завершения (если есть)
    closure_reason      TEXT,
    
    -- ссылка на целевую задачу (если применимо)
    closure_target_id   BIGINT REFERENCES queue_tasks(id),
    
    -- кто инициировал переход
    actor_role_id       BIGINT REFERENCES roles(id),
    actor_session_id    BIGINT,
    actor_type          TEXT NOT NULL DEFAULT 'role'
                        CHECK (actor_type IN ('role', 'daemon', 'watchdog', 'human')),
    
    -- комментарий (опционально)
    comment             TEXT,
    
    -- метаданные (опционально)
    metadata            JSONB,
    
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE history_status IS 'Append-only история переходов состояний задачи';

-- Индекс для быстрого получения истории задачи
CREATE INDEX idx_history_status_task ON history_status(queue_task_id, created_at DESC);
```


______________________________________________________________________

### 9. `sessions` — сессии агентов

```sql
CREATE TABLE sessions (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role_id         BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    
    -- привязка к задаче (опционально)
    queue_task_id   BIGINT REFERENCES queue_tasks(id) ON DELETE SET NULL,
    
    -- тип сессии
    runtime_type    TEXT NOT NULL
                    CHECK (runtime_type IN ('container', 'process', 'tmux', 'k8s', 'other')),
    runtime_ref     TEXT,                      -- имя контейнера, PID, etc.
    
    -- состояние
    state           TEXT NOT NULL DEFAULT 'starting'
                    CHECK (
                        state IN (
                            'starting',
                            'running',
                            'idle',
                            'stopping',
                            'stopped',
                            'failed'
                        )
                    ),
    
    -- рабочая директория
    working_dir     TEXT,
    target_dir      TEXT,
    
    -- метаданные
    config          JSONB,
    
    -- временные метки
    started_at      TIMESTAMPTZ,
    stopped_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE sessions IS 'Сессия агента (контейнер, процесс, tmux)';

-- Индекс для поиска активных сессий
CREATE INDEX idx_sessions_team_state ON sessions(team_id, state);
CREATE INDEX idx_sessions_role ON sessions(role_id, state);
```


______________________________________________________________________

### 10. `messages` — сообщения между ролями

```sql
CREATE TABLE messages (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    
    -- контекст (опционально)
    queue_task_id   BIGINT REFERENCES queue_tasks(id) ON DELETE SET NULL,
    goal_id         BIGINT REFERENCES goals(id) ON DELETE SET NULL,
    
    -- отправитель и получатель
    from_role_id    BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    from_session_id BIGINT REFERENCES sessions(id) ON DELETE SET NULL,
    
    -- получатель (для direct сообщений)
    to_role_id      BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    to_session_id   BIGINT REFERENCES sessions(id) ON DELETE SET NULL,
    
    -- тип сообщения
    type            TEXT NOT NULL DEFAULT 'direct'
                    CHECK (
                        type IN (
                            'direct',        -- конкретному получателю
                            'broadcast',     -- всем в team/segment
                            'segment',       -- всем в segment
                            'system',        -- системное уведомление
                            'watchdog'       -- от watchdog
                        )
                    ),
    
    -- содержание
    body            TEXT NOT NULL,
    
    -- метаданные
    metadata        JSONB,
    
    -- прочитано ли (опционально)
    is_read         BOOLEAN NOT NULL DEFAULT FALSE,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE messages IS 'Сообщения между ролями (без обязательств, в отличие от задач)';

-- Индексы для частых запросов
CREATE INDEX idx_messages_team ON messages(team_id, created_at DESC);
CREATE INDEX idx_messages_task ON messages(queue_task_id, created_at DESC);
CREATE INDEX idx_messages_to_role ON messages(to_role_id, is_read, created_at DESC);
```


______________________________________________________________________

### 11. `chatrooms` — чаты для команд/сегментов

```sql
CREATE TABLE chatrooms (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    segment_id      BIGINT REFERENCES segments(id) ON DELETE SET NULL,
    
    -- идентификаторы
    name            TEXT NOT NULL,
    topic           TEXT,
    
    -- настройки
    config          JSONB,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    UNIQUE(team_id, segment_id, name)
);

COMMENT ON TABLE chatrooms IS 'Общий чат для команды или сегмента';
```


______________________________________________________________________

### 12. `chatroom_messages` — сообщения в чатах

```sql
CREATE TABLE chatroom_messages (
    id              BIGSERIAL PRIMARY KEY,
    chatroom_id     BIGINT NOT NULL REFERENCES chatrooms(id) ON DELETE CASCADE,
    
    -- отправитель
    from_role_id    BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    from_session_id BIGINT REFERENCES sessions(id) ON DELETE SET NULL,
    
    -- содержание
    body            TEXT NOT NULL,
    
    -- метаданные (опционально)
    metadata        JSONB,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE chatroom_messages IS 'Сообщения в общем чате';

-- Индекс для получения последних сообщений
CREATE INDEX idx_chatroom_messages_chatroom ON chatroom_messages(chatroom_id, created_at DESC);
```


______________________________________________________________________

### 13. `watchdog_events` — события от watchdog

```sql
CREATE TABLE watchdog_events (
    id              BIGSERIAL PRIMARY KEY,
    team_id         BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    
    -- контекст
    queue_task_id   BIGINT REFERENCES queue_tasks(id) ON DELETE SET NULL,
    session_id      BIGINT REFERENCES sessions(id) ON DELETE SET NULL,
    role_id         BIGINT REFERENCES roles(id) ON DELETE SET NULL,
    
    -- тип события
    event_type      TEXT NOT NULL
                    CHECK (
                        event_type IN (
                            'wake',
                            'refocus',
                            'alignment_checkpoint',
                            'stale',
                            'blocked',
                            'idle',
                            'drift'
                        )
                    ),
    
    -- описание
    description     TEXT,
    
    -- действия (опционально)
    action_taken    TEXT,                    -- что сделал watchdog
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE watchdog_events IS 'События от системы мониторинга (watchdog)';

-- Индексы
CREATE INDEX idx_watchdog_events_team ON watchdog_events(team_id, created_at DESC);
CREATE INDEX idx_watchdog_events_task ON watchdog_events(queue_task_id, created_at DESC);
```


______________________________________________________________________

### 14. `config` — глобальная конфигурация daemon

```sql
CREATE TABLE config (
    id              BIGSERIAL PRIMARY KEY,
    key             TEXT NOT NULL UNIQUE,
    value           JSONB NOT NULL,
    description     TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE config IS 'Глобальная конфигурация daemon (настройки, политики, пороги)';
```


______________________________________________________________________

## Связи между таблицами (кратко)

```
teams (1) ──< (N) segments
teams (1) ──< (N) roles
teams (1) ──< (N) relatives
teams (1) ──< (N) queue_tasks
teams (1) ──< (N) sessions
teams (1) ──< (N) messages
teams (1) ──< (N) chatrooms
teams (1) ──< (N) watchdog_events
teams (1) ──< (N) projects

segments (1) ──< (N) roles
segments (1) ──< (N) chatrooms

roles (1) ──< (N) relatives (as from_role)
roles (1) ──< (N) relatives (as to_role)
roles (1) ──< (N) queue_tasks (as destination)
roles (1) ──< (N) queue_tasks (as source)
roles (1) ──< (N) sessions
roles (1) ──< (N) messages

projects (1) ──< (N) goals
projects (1) ──< (N) queue_tasks

goals (1) ──< (N) goals (recursive, для подцелей)
goals (1) ──< (N) queue_tasks

queue_tasks (1) ──< (N) queue_tasks (recursive, parent_task)
queue_tasks (1) ──< (N) history_status
queue_tasks (1) ──< (N) messages
queue_tasks (1) ──< (N) watchdog_events

sessions (1) ──< (N) messages
sessions (1) ──< (N) chatroom_messages

chatrooms (1) ──< (N) chatroom_messages
```


______________________________________________________________________

## Дополнительные рекомендации

### 1. Триггеры для `updated_at`

```sql
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Пример для teams
CREATE TRIGGER update_teams_updated_at
    BEFORE UPDATE ON teams
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Повторить для segments, roles, projects, goals, queue_tasks, sessions, chatrooms
```


### 2. RLS (Row Level Security) — опционально

Если планируешь мульти-тенантность:

```sql
ALTER TABLE teams ENABLE ROW LEVEL SECURITY;

CREATE POLICY team_isolation ON teams
    USING (team_id = current_setting('app.current_team_id')::BIGINT);
```


### 3. Миграции

Используй `golang-migrate` или `atlas` для управления миграциями схемы.

