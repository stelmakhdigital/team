# Как достигается 100% observe (наблюдаемость)

## Что такое 100% Observability

**Observability = Telemetry + Context**

Состоит из трёх столпов:

### 1. **Metrics** (Метрики)

- **Что:** Агрегированные числовые данные
- **Примеры:** RPS, latency, error rate, queue size
- **Вопросы:** "Сколько?", "Как быстро?", "Как часто?"
- **Инструменты:** Prometheus, Grafana


### 2. **Logs** (Логи)

- **Что:** Текстовые записи о событиях
- **Примеры:** "Session started", "Task completed", "Error: timeout"
- **Вопросы:** "Что произошло?", "Когда?", "Где?"
- **Инструменты:** ELK, Loki, stdout


### 3. **Traces** (Трейсы)

- **Что:** Распределённая трассировка запросов
- **Примеры:** Request → Service A → Service B → DB
- **Вопросы:** "Какой путь?", "Где задержка?", "Что вызвало ошибку?"
- **Инструменты:** Jaeger, Tempo, OpenTelemetry

## Детальная реализация

### 1. Централизованное состояние (SQLite)

**Все сущности в одной БД:**

```sql
-- Полная история задач
SELECT 
    qt.id,
    qt.title,
    qt.body,
    qt.state,
    hs.from_state,
    hs.to_state,
    hs.actor_type,
    hs.actor_role_id,
    hs.created_at,
    hs.comment
FROM queue_tasks qt
LEFT JOIN history_status hs ON hs.queue_task_id = qt.id
WHERE qt.id = 123
ORDER BY hs.created_at;

-- Все сообщения
SELECT 
    m.id,
    m.from_role_id,
    m.to_role_id,
    m.body,
    m.type,
    m.created_at
FROM messages m
WHERE m.queue_task_id = 123
ORDER BY m.created_at;

-- Все сессии
SELECT 
    s.id,
    s.role_id,
    s.state,
    s.started_at,
    s.stopped_at,
    (SELECT COUNT(*) FROM history_status hs 
     WHERE hs.actor_session_id = s.id) as actions_count
FROM sessions s
WHERE s.team_id = 1;
```

**Преимущество:** Можно задать **любой вопрос** через SQL.

______________________________________________________________________

### 2. Append-only история

**Никаких UPDATE/DELETE для истории:**

```go
// В Task Service
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
        
        // 3. Записать в историю (APPEND, никогда не update)
        history := &HistoryStatus{
            QueueTaskID: id,
            FromState:   oldState,
            ToState:     state,
            ClosureReason: reason,
            ActorType:   "role",
            ActorRoleID: authCtx.RoleID,
            CreatedAt:   time.Now(),
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

**Результат:** Полная история всех изменений.

______________________________________________________________________

### 3. Event Streaming

**Event Bus в daemon:**

```go
package events

type EventBus struct {
    subscribers map[string][]chan Event
    mu          sync.RWMutex
}

func (b *EventBus) Subscribe(eventType string, ch chan Event) {
    b.mu.Lock()
    defer b.mu.Unlock()
    
    if _, ok := b.subscribers[eventType]; !ok {
        b.subscribers[eventType] = make([]chan Event, 0)
    }
    
    b.subscribers[eventType] = append(b.subscribers[eventType], ch)
}

func (b *EventBus) Publish(event Event) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    
    eventType := event.Type()
    if channels, ok := b.subscribers[eventType]; ok {
        for _, ch := range channels {
            select {
            case ch <- event:
            default:
                // Канал заполнен, пропускаем
                log.Warn("event channel full, dropping event", "type", eventType)
            }
        }
    }
}

// Типы событий
type TaskCreated struct {
    TaskID    int64
    TeamID    int64
    Timestamp time.Time
}

func (e TaskCreated) Type() string { return "task.created" }

type TaskStateChanged struct {
    TaskID    int64
    FromState string
    ToState   string
    Timestamp time.Time
}

func (e TaskStateChanged) Type() string { return "task.state_changed" }

type SessionStarted struct {
    SessionID int64
    TeamID    int64
    Timestamp time.Time
}

func (e SessionStarted) Type() string { return "session.started" }
```

**Подписка в TUI:**

```go
func (t *TUI) Start(ctx context.Context) {
    eventChan := make(chan events.Event, 100)
    
    t.eventBus.Subscribe("task.created", eventChan)
    t.eventBus.Subscribe("task.state_changed", eventChan)
    t.eventBus.Subscribe("session.started", eventChan)
    t.eventBus.Subscribe("session.stopped", eventChan)
    
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case event := <-eventChan:
                t.handleEvent(event)
            }
        }
    }()
}

func (t *TUI) handleEvent(event events.Event) {
    switch e := event.(type) {
    case events.TaskCreated:
        t.showMessage(fmt.Sprintf("✅ Task created: #%d", e.TaskID))
        t.refreshTaskList()
        
    case events.TaskStateChanged:
        t.showMessage(fmt.Sprintf("🔄 Task #%d: %s → %s", 
            e.TaskID, e.FromState, e.ToState))
        t.refreshTask(e.TaskID)
        
    case events.SessionStarted:
        t.showMessage(fmt.Sprintf("🚀 Session started: #%d", e.SessionID))
        t.refreshSessionList()
    }
}
```


______________________________________________________________________

### 4. Agent Transcripts

**Полная история диалогов:**

```go
type AgentTranscript struct {
    SessionID  int64
    Messages   []TranscriptMessage
    ToolCalls  []ToolCall
    FileChanges []FileChange
}

type TranscriptMessage struct {
    Role      string  // "user", "assistant", "system"
    Content   string
    Timestamp time.Time
}

type ToolCall struct {
    Name      string
    Arguments map[string]interface{}
    Result    string
    Timestamp time.Time
}

type FileChange struct {
    Path      string
    Operation string  // "create", "update", "delete"
    Diff      string
    Timestamp time.Time
}

// Сохранение в БД
func (r *SessionRepository) SaveTranscript(ctx context.Context, sessionID int64, transcript *AgentTranscript) error {
    return r.db.WithTransaction(ctx, func(tx *pgx.Tx) error {
        // Сохранить сообщения
        for _, msg := range transcript.Messages {
            _, err := tx.Exec(ctx, `
                INSERT INTO agent_transcript_messages 
                (session_id, role, content, timestamp)
                VALUES ($1, $2, $3, $4)
            `, sessionID, msg.Role, msg.Content, msg.Timestamp)
            
            if err != nil {
                return err
            }
        }
        
        // Сохранить вызовы инструментов
        for _, call := range transcript.ToolCalls {
            argsJSON, _ := json.Marshal(call.Arguments)
            
            _, err := tx.Exec(ctx, `
                INSERT INTO agent_tool_calls 
                (session_id, name, arguments, result, timestamp)
                VALUES ($1, $2, $3, $4, $5)
            `, sessionID, call.Name, argsJSON, call.Result, call.Timestamp)
            
            if err != nil {
                return err
            }
        }
        
        return nil
    })
}
```

**Query для анализа:**

```sql
-- Что делал агент в сессии?
SELECT 
    'message' as type,
    role,
    LEFT(content, 100) as preview,
    timestamp
FROM agent_transcript_messages
WHERE session_id = 123

UNION ALL

SELECT 
    'tool_call' as type,
    name as role,
    result as preview,
    timestamp
FROM agent_tool_calls
WHERE session_id = 123

ORDER BY timestamp;

-- Какие файлы менял?
SELECT 
    path,
    operation,
    LEFT(diff, 200) as diff_preview,
    timestamp
FROM agent_file_changes
WHERE session_id = 123
ORDER BY timestamp;
```


______________________________________________________________________

### 5. Snapshots (Time Travel)

**Периодические снапшоты:**

```go
type Snapshot struct {
    ID        int64
    TeamID    int64
    State     jsonb  // Полное состояние rig'а
    CreatedAt time.Time
}

func (s *SnapshotService) CreateSnapshot(ctx context.Context, teamID int64) (*Snapshot, error) {
    // Собрать полное состояние
    state, err := s.collectTeamState(ctx, teamID)
    if err != nil {
        return nil, err
    }
    
    // Сохранить в БД
    snapshot := &Snapshot{
        TeamID:    teamID,
        State:     state,
        CreatedAt: time.Now(),
    }
    
    if err := s.snapshotRepo.Create(ctx, snapshot); err != nil {
        return nil, err
    }
    
    log.Info("snapshot created", "team_id", teamID, "snapshot_id", snapshot.ID)
    
    return snapshot, nil
}

func (s *SnapshotService) collectTeamState(ctx context.Context, teamID int64) (jsonb, error) {
    // Собрать все сущности
    teams, _ := s.teamRepo.GetByID(ctx, teamID)
    segments, _ := s.segmentRepo.ListByTeam(ctx, teamID)
    roles, _ := s.roleRepo.ListByTeam(ctx, teamID)
    tasks, _ := s.taskRepo.ListByTeam(ctx, teamID)
    sessions, _ := s.sessionRepo.ListByTeam(ctx, teamID)
    messages, _ := s.messageRepo.ListByTeam(ctx, teamID)
    
    // Сериализовать в JSON
    state := map[string]interface{}{
        "team":     teams,
        "segments": segments,
        "roles":    roles,
        "tasks":    tasks,
        "sessions": sessions,
        "messages": messages,
        "timestamp": time.Now(),
    }
    
    return json.Marshal(state)
}
```

**Restore из снапшота:**

```bash
# rig down --snapshot
# rig restore my-team@2026-10-07T12:00:00Z
```


______________________________________________________________________

## Как достичь 100% Observability

### Checklist для полной наблюдаемости

#### 1. **Централизованное хранилище**

```sql
-- Все сущности в PostgreSQL или SQLite
-- Никакого состояния в памяти (кроме кэша)
```

✅ Все данные в БД\
✅ Foreign keys для связей\
✅ Индексы для частых запросов

______________________________________________________________________

#### 2. **Append-only логирование**

```go
// Никогда не UPDATE/DELETE историю
func (s *service) ChangeState(id, newState) {
    oldState := getState(id)
    
    UPDATE table SET state = newState WHERE id = id
    
    INSERT INTO history (from, to, timestamp) 
    VALUES (oldState, newState, NOW())  // ← APPEND
}
```

✅ История всех изменений\
✅ С timestamp и actor\
✅ Никогда не удаляется

______________________________________________________________________

#### 3. **Event streaming**

```go
// Публиковать все важные события
events.Publish("task.created", TaskCreated{...})
events.Publish("task.state_changed", TaskStateChanged{...})
events.Publish("session.started", SessionStarted{...})

// Подписаться в TUI/CLI
eventBus.Subscribe("task.*", handler)
```

✅ Real-time updates\
✅ Декoupled архитектура\
✅ Легко добавить новых подписчиков

______________________________________________________________________

#### 4. **Структурированное логирование**

```go
// Вместо: log.Println("Error occurred")
log.Error("task create failed",
    "task_id", taskID,
    "user_id", userID,
    "error", err,
    "duration_ms", duration.Milliseconds(),
)
```

✅ JSON формат\
✅ Контекст (IDs, duration)\
✅ Уровни (debug, info, warn, error)

______________________________________________________________________

#### 5. **Distributed tracing**

```go
import "go.opentelemetry.io/otel"

func (s *service) CreateTask(ctx context.Context, req Request) (*Task, error) {
    ctx, span := otel.Tracer("daemon").Start(ctx, "CreateTask")
    defer span.End()
    
    span.SetAttributes(
        attribute.String("task.title", req.Title),
        attribute.Int64("project.id", req.ProjectID),
    )
    
    task, err := s.createTaskInternal(ctx, req)
    
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
    
    return task, err
}
```

✅ Trace ID через все сервисы\
✅ Span для каждой операции\
✅ Интеграция с Jaeger/Tempo

______________________________________________________________________

#### 6. **Метрики для всего**

```go
// Counters
tasksCreated.Inc()
sessionFailures.Inc()

// Gauges
activeSessions.Set(float64(count))
queueSize.Set(float64(size))

// Histograms
taskDuration.Observe(duration.Seconds())
latency.Observe(latency.Seconds())
```

✅ Business metrics (tasks, sessions)\
✅ Technical metrics (latency, errors)\
✅ LLM metrics (tokens, cost)

______________________________________________________________________

#### 7. **Агентские транскрипты**

```go
// Сохранять всё:
// - Промпты и ответы
// - Вызовы инструментов
// - Изменения файлов
// - Git operations
```

✅ Полная история диалогов\
✅ Какие инструменты вызывал\
✅ Какие файлы менял

______________________________________________________________________

#### 8. **Query-ability**

```sql
-- Можно задать любой вопрос через SQL:

-- Какие задачи застряли?
SELECT * FROM queue_tasks 
WHERE state = 'in_progress' 
  AND updated_at < NOW() - INTERVAL '2 hours';

-- Кто самый активный агент?
SELECT r.name, COUNT(*) as actions
FROM history_status hs
JOIN roles r ON r.id = hs.actor_role_id
GROUP BY r.name
ORDER BY actions DESC
LIMIT 10;

-- Сколько токенов потратили?
SELECT model, SUM(tokens) as total_tokens
FROM llm_usage
WHERE timestamp > NOW() - INTERVAL '7 days'
GROUP BY model;
```

✅ SQL для любых вопросов\
✅ Агрегации и группировки\
✅ Time-based queries

______________________________________________________________________

## Пример: Debug проблемы с 100% observability

**Проблема:** "Задача \#123 застряла"

### С 100% observability:

```bash
# 1. Посмотреть историю задачи
curl http://localhost:8080/api/v1/tasks/123/history

# Ответ:
[
  {"from": "", "to": "pending", "timestamp": "2026-10-07T10:00:00Z"},
  {"from": "pending", "to": "in_progress", "timestamp": "2026-10-07T10:05:00Z", "actor": "backend-lead"},
  {"from": "in_progress", "to": "blocked", "timestamp": "2026-10-07T10:30:00Z", "reason": "blocked_on", "target": 120}
]

# 2. Посмотреть блокирующую задачу
curl http://localhost:8080/api/v1/tasks/120

# 3. Посмотреть сообщения
curl http://localhost:8080/api/v1/tasks/123/messages

# 4. Посмотреть транскрипт агента
curl http://localhost:8080/api/v1/sessions/456/transcript

# 5. Посмотреть метрики
curl http://localhost:9091/metrics | grep daemon_tasks_by_state
```

**Вывод:** Задача заблокирована на задаче \#120, которая завершена 2 часа назад. Watchdog должен был её разблокировать, но не сработал.

**Fix:** Проверить watchdog логи, найти баг.

______________________________________________________________________

### Без observability:

```
"Задача #123 застряла"
→ "Где логи?"
→ "В /var/log/daemon.log"
→ "А где история изменений?"
→ "Нету"
→ "А кто последний менял?"
→ "Не знаем"
→ "А что агент делал?"
→ "Не логируется"
→ 😭
```
