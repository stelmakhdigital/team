
# Прокачиваем таблицу историй

## Твоя текущая схема

У тебя есть:

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

-- Индекс для быстрого получения истории задачи
CREATE INDEX idx_history_status_task ON history_status(queue_task_id, created_at DESC);
```

**Это уже отличная основа для observability!** ✅

______________________________________________________________________

## Что можно делать с этой таблицей

### 1. **Полная история задачи**

```sql
-- Вся история задачи #123
SELECT 
    from_state,
    to_state,
    closure_reason,
    actor_type,
    actor_role_id,
    comment,
    created_at
FROM history_status
WHERE queue_task_id = 123
ORDER BY created_at;
```

**Результат:**


| from_state | to_state | closure_reason | actor_type | actor_role_id | comment | created_at |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| (null) | pending | (null) | daemon | (null) | Task created | 2026-10-07 10:00:00 |
| pending | in_progress | (null) | role | 5 | Started work | 2026-10-07 10:05:00 |
| in_progress | done | handed_off_to | role | 5 | Handed to reviewer | 2026-10-07 11:30:00 |

✅ **Полный audit trail** — кто, что, когда сделал.

______________________________________________________________________

### 2. **Кто самый активный?**

```sql
-- Топ агентов по количеству действий
SELECT 
    r.name as role_name,
    COUNT(*) as actions_count,
    DATE_TRUNC('hour', hs.created_at) as hour
FROM history_status hs
JOIN roles r ON r.id = hs.actor_role_id
WHERE hs.created_at > NOW() - INTERVAL '24 hours'
GROUP BY r.name, hour
ORDER BY actions_count DESC
LIMIT 10;
```

✅ **Метрики активности** по ролям и времени.

______________________________________________________________________

### 3. **Где застряли задачи?**

```sql
-- Задачи, которые не двигаются больше 2 часов
SELECT 
    qt.id,
    qt.title,
    qt.state,
    qt.updated_at,
    EXTRACT(EPOCH FROM (NOW() - qt.updated_at))/3600 as hours_stuck
FROM queue_tasks qt
LEFT JOIN history_status hs ON hs.queue_task_id = qt.id
WHERE qt.state = 'in_progress'
  AND qt.updated_at < NOW() - INTERVAL '2 hours'
GROUP BY qt.id, qt.title, qt.state, qt.updated_at
ORDER BY hours_stuck DESC;
```

✅ **Watchdog query** — находить проблемные задачи.

______________________________________________________________________

### 4. **Среднее время выполнения**

```sql
-- Средняя длительность задач по ролям
SELECT 
    r.name as role_name,
    COUNT(*) as task_count,
    AVG(EXTRACT(EPOCH FROM (hs_done.created_at - hs_start.created_at))) as avg_duration_secs
FROM history_status hs_start
JOIN history_status hs_done 
    ON hs_done.queue_task_id = hs_start.queue_task_id
    AND hs_done.to_state = 'done'
JOIN queue_tasks qt ON qt.id = hs_start.queue_task_id
JOIN roles r ON r.id = qt.destination_role_id
WHERE hs_start.to_state = 'in_progress'
  AND hs_start.created_at > NOW() - INTERVAL '7 days'
GROUP BY r.name;
```

✅ **Performance metrics** — сколько времени занимают задачи.

______________________________________________________________________

### 5. **Error rate по ролям**

```sql
-- Сколько задач отклонено/отменено по ролям
SELECT 
    r.name as role_name,
    COUNT(*) as total_tasks,
    SUM(CASE WHEN hs.closure_reason IN ('denied', 'canceled') THEN 1 ELSE 0 END) as failed_tasks,
    ROUND(
        100.0 * SUM(CASE WHEN hs.closure_reason IN ('denied', 'canceled') THEN 1 ELSE 0 END) / COUNT(*),
        2
    ) as failure_rate_percent
FROM queue_tasks qt
JOIN roles r ON r.id = qt.destination_role_id
LEFT JOIN history_status hs ON hs.queue_task_id = qt.id AND hs.to_state = 'done'
WHERE qt.created_at > NOW() - INTERVAL '7 days'
GROUP BY r.name;
```

✅ **Quality metrics** — какие роли чаще ошибаются.

______________________________________________________________________

## Чего не хватает для 100% observability

У тебя **уже есть** append-only история для задач. Но для полной наблюдаемости нужно добавить:

### 1. **История для сессий**

```sql
CREATE TABLE session_history (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT NOT NULL REFERENCES sessions(id),
    
    from_state      TEXT,
    to_state        TEXT NOT NULL,
    
    actor_type      TEXT NOT NULL DEFAULT 'daemon',
    actor_role_id   BIGINT,
    
    comment         TEXT,
    metadata        JSONB,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_session_history_session ON session_history(session_id, created_at DESC);
```

**Зачем:** Видеть, когда сессия запускалась, останавливалась, падала.

______________________________________________________________________

### 2. **История для сообщений**

У тебя уже есть `messages` таблица, но можно добавить метаданные:

```sql
ALTER TABLE messages ADD COLUMN metadata JSONB;
-- Для хранения: delivered_at, read_at, retry_count
```


______________________________________________________________________

### 3. **Agent transcripts**

```sql
CREATE TABLE agent_transcripts (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT NOT NULL REFERENCES sessions(id),
    
    message_type    TEXT NOT NULL CHECK (
        message_type IN ('prompt', 'response', 'tool_call', 'tool_result', 'system')
    ),
    
    content         TEXT NOT NULL,
    metadata        JSONB,  -- model, tokens, latency
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_transcripts_session ON agent_transcripts(session_id, created_at DESC);
```

**Зачем:** Полная история диалогов агента.

______________________________________________________________________

### 4. **File changes**

```sql
CREATE TABLE file_changes (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT NOT NULL REFERENCES sessions(id),
    queue_task_id   BIGINT REFERENCES queue_tasks(id),
    
    file_path       TEXT NOT NULL,
    operation       TEXT NOT NULL CHECK (
        operation IN ('create', 'update', 'delete')
    ),
    
    diff            TEXT,  -- git diff или упрощённо
    checksum_before TEXT,
    checksum_after  TEXT,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_file_changes_session ON file_changes(session_id, created_at DESC);
CREATE INDEX idx_file_changes_task ON file_changes(queue_task_id, created_at DESC);
```

**Зачем:** Видеть, какие файлы менял агент.

______________________________________________________________________

### 5. **Tool calls**

```sql
CREATE TABLE tool_calls (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT NOT NULL REFERENCES sessions(id),
    queue_task_id   BIGINT REFERENCES queue_tasks(id),
    
    tool_name       TEXT NOT NULL,
    arguments       JSONB NOT NULL,
    result          TEXT,
    error           TEXT,
    
    duration_ms     INTEGER,
    
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tool_calls_session ON tool_calls(session_id, created_at DESC);
```

**Зачем:** Какие инструменты вызывал агент, с какими аргументами.

______________________________________________________________________

## Как использовать в коде

### Task Service (уже должно быть)

```go
func (s *taskService) UpdateTaskState(ctx context.Context, id int64, state string, reason string) error {
    return s.db.WithTransaction(ctx, func(tx *pgx.Tx) error {
        // 1. Получить текущее состояние
        oldState, err := s.taskRepo.GetState(ctx, tx, id)
        if err != nil {
            return err
        }
        
        // 2. Обновить задачу
        if err := s.taskRepo.UpdateState(ctx, tx, id, state); err != nil {
            return err
        }
        
        // 3. Записать в history_status (APPEND-ONLY!)
        history := &HistoryStatus{
            QueueTaskID:   id,
            FromState:     oldState,
            ToState:       state,
            ClosureReason: reason,
            ActorType:     "role",
            ActorRoleID:   authCtx.RoleID,
            CreatedAt:     time.Now(),
        }
        
        if err := s.historyRepo.Create(ctx, tx, history); err != nil {
            return err
        }
        
        // 4. Опубликовать событие
        s.events.Publish(TaskStateChanged{
            TaskID:    id,
            FromState: oldState,
            ToState:   state,
        })
        
        return nil
    })
}
```

✅ **Уже должно работать!**

______________________________________________________________________

### Session Service (нужно добавить)

```go
func (s *sessionService) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
    return s.db.WithTransaction(ctx, func(tx *pgx.Tx) (*Session, error) {
        // 1. Создать сессию
        session := &Session{
            TeamID:      req.TeamID,
            RoleID:      req.RoleID,
            QueueTaskID: req.QueueTaskID,
            RuntimeType: req.RuntimeType,
            State:       StateStarting,
        }
        
        if err := s.sessionRepo.Create(ctx, tx, session); err != nil {
            return nil, err
        }
        
        // 2. Записать в session_history
        history := &SessionHistory{
            SessionID: session.ID,
            FromState: "",
            ToState:   string(StateStarting),
            ActorType: "daemon",
            Comment:   "Session created",
        }
        
        if err := s.sessionHistoryRepo.Create(ctx, tx, history); err != nil {
            return nil, err
        }
        
        // 3. Запустить runtime
        runtimeRef, err := s.startRuntime(ctx, session)
        if err != nil {
            session.State = StateFailed
            
            // Записать в историю
            s.sessionHistoryRepo.Create(ctx, tx, &SessionHistory{
                SessionID: session.ID,
                FromState: string(StateStarting),
                ToState:   string(StateFailed),
                ActorType: "daemon",
                Comment:   fmt.Sprintf("Failed to start: %v", err),
            })
            
            return nil, err
        }
        
        // 4. Обновить сессию
        session.RuntimeRef = runtimeRef
        session.State = StateRunning
        s.sessionRepo.Update(ctx, tx, session)
        
        // 5. Записать в историю
        s.sessionHistoryRepo.Create(ctx, tx, &SessionHistory{
            SessionID: session.ID,
            FromState: string(StateStarting),
            ToState:   string(StateRunning),
            ActorType: "daemon",
            Comment:   "Session started successfully",
        })
        
        return session, nil
    })
}
```


______________________________________________________________________

## Queries для observability

### Dashboard: Active Tasks

```sql
-- Активные задачи по состояниям
SELECT 
    state,
    COUNT(*) as count
FROM queue_tasks
WHERE state IN ('pending', 'in_progress', 'blocked')
GROUP BY state;
```


### Dashboard: Task Flow

```sql
-- Сколько задач перешло в каждое состояние за последний час
SELECT 
    to_state,
    COUNT(*) as count
FROM history_status
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY to_state
ORDER BY count DESC;
```


### Dashboard: Session Health

```sql
-- Активные сессии по статусам
SELECT 
    state,
    COUNT(*) as count
FROM sessions
WHERE state IN ('running', 'idle', 'starting')
GROUP BY state;
```


### Alert: Stale Tasks

```sql
-- Задачи без изменений больше 2 часов
SELECT 
    qt.id,
    qt.title,
    MAX(hs.created_at) as last_activity
FROM queue_tasks qt
LEFT JOIN history_status hs ON hs.queue_task_id = qt.id
WHERE qt.state = 'in_progress'
GROUP BY qt.id, qt.title
HAVING MAX(hs.created_at) < NOW() - INTERVAL '2 hours';
```


______________________________________________________________________

## Итог

**Уже есть:**

✅ `history_status` таблица для append-only истории задач\
✅ Правильная структура (from_state, to_state, actor, timestamp)\
✅ Индексы для производительности

**Нужно добавить:**

⏳ `session_history` — для сессий\
⏳ `agent_transcripts` — для диалогов агентов\
⏳ `tool_calls` — для вызовов инструментов\
⏳ `file_changes` — для изменений файлов

**Результат:**

С этими таблицами + event streaming + метриками будет **95-100% observability** — сможем ответить на любой вопрос о системе через SQL queries.

