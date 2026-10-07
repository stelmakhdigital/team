
# Daemon

Daemon — центральная управляющая система.

## Архитектура daemon

```
┌─────────────────────────────────────────────────────────┐
│                      DAEMON (Go)                        │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │  HTTP API   │  │  gRPC API   │  │  MCP Server │     │
│  │  (REST)     │  │  (internal) │  │  (optional) │     │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘     │
│         │                │                │             │
│         └────────────────┼────────────────┘             │
│                          │                              │
│  ┌───────────────────────▼───────────────────────┐     │
│  │            API Gateway / Router               │     │
│  └───────────────────────┬───────────────────────┘     │
│                          │                              │
│  ┌───────────────────────▼───────────────────────┐     │
│  │           Application Services                │     │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐    │     │
│  │  │   Task   │  │ Session  │  │  Team    │    │     │
│  │  │ Service  │  │ Service  │  │ Service  │    │     │
│  │  └──────────┘  └──────────┘  └──────────┘    │     │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐    │     │
│  │  │  Watch-  │  │  Message │  │  Config  │    │     │
│  │  │   dog    │  │ Service  │  │ Service  │    │     │
│  │  └──────────┘  └──────────┘  └──────────┘    │     │
│  └───────────────────────┬───────────────────────┘     │
│                          │                              │
│  ┌───────────────────────▼───────────────────────┐     │
│  │             Repositories (DB)                 │     │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐    │     │
│  │  │   Task   │  │ Session  │  │   Role   │    │     │
│  │  │   Repo   │  │   Repo   │  │   Repo   │    │     │
│  │  └──────────┘  └──────────┘  └──────────┘    │     │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐    │     │
│  │  │  Team    │  │ Message  │  │  Config  │    │     │
│  │  │   Repo   │  │   Repo   │  │   Repo   │    │     │
│  │  └──────────┘  └──────────┘  └──────────┘    │     │
│  └───────────────────────┬───────────────────────┘     │
│                          │                              │
│  ┌───────────────────────▼───────────────────────┐     │
│  │              PostgreSQL                       │     │
│  │              (external)                       │     │
│  └───────────────────────────────────────────────┘     │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │          Background Workers (Goroutines)        │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐      │   │
│  │  │   Task   │  │ Watch-   │  │ Session  │      │   │
│  │  │ Orchestr │  │   dog    │  │ Monitor  │      │   │
│  │  └──────────┘  └──────────┘  └──────────┘      │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │           Runtime Adapters                      │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐      │   │
│  │  │ Container│  │  Process │  │   TMUX   │      │   │
│  │  │ Adapter  │  │  Adapter │  │  Adapter │      │   │
│  │  └──────────┘  └──────────┘  └──────────┘      │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## Структура проекта (Go modules)

```
cmd/
  daemon/
    main.go              # точка входа, инициализация
  cli/
    main.go              # CLI утилита для управления

internal/
  config/
    config.go            # загрузка конфигурации (файл + env + DB)
    validator.go         # валидация конфигов

  database/
    db.go                # подключение к PostgreSQL (pgx)
    migrate.go           # миграции
    transaction.go       # wrapper для транзакций

  models/
    team.go              # Go structs для teams
    segment.go           # для segments
    role.go              # для roles
    task.go              # для queue_tasks
    session.go           # для sessions
    message.go           # для messages
    ...

  repository/
    team_repo.go         # CRUD для teams
    segment_repo.go
    role_repo.go
    task_repo.go
    session_repo.go
    message_repo.go
    watchdog_repo.go
    config_repo.go

  service/
    team_service.go      # бизнес-логика для команд
    task_service.go      # оркестрация задач
    session_service.go   # управление сессиями
    message_service.go   # отправка сообщений
    watchdog_service.go  # логика watchdog
    config_service.go

  api/
    http/
      server.go          # HTTP сервер (chi/gin/echo)
      routes.go          # маршруты
      handlers/
        team_handler.go
        task_handler.go
        session_handler.go
        message_handler.go
      middleware/
        auth.go
        logging.go
        recovery.go
      dto/
        requests.go      # request structs
        responses.go     # response structs

    grpc/
      server.go
      proto/
        daemon.proto
      handlers/
        ...

    mcp/
      server.go          # MCP сервер (опционально)
      tools.go           # MCP tools

  session/
    manager.go           # Session Manager
    runtime/
      adapter.go         # интерфейс адаптера
      container.go       # Docker adapter
      process.go         # Process adapter
      tmux.go            # TMUX adapter
    types.go             # типы сессий

  orchestrator/
    task_orchestrator.go # жизненный цикл задач
    decomposer.go        # декомпозиция задач (Lead)
    worker.go            # распределение воркеров

  watchdog/
    scanner.go           # сканер состояния
    policies.go          # политики watchdog
    detector.go          # детектор аномалий
    intervener.go        # вмешательства (wake/refocus)

  events/
    bus.go               # event bus (in-memory или NATS)
    types.go             # типы событий
    handlers.go          # обработчики событий

  utils/
    logger.go            # логирование (zap)
    errors.go            # ошибки
    time.go              # утилиты времени
```


______________________________________________________________________

## Компоненты daemon

### 1. **API Gateway**

**Задачи:**

- Маршрутизация запросов к нужным сервисам
- Аутентификация/авторизация
- Валидация входных данных
- Логирование, метрики

**Интерфейсы:**

- **HTTP REST API** — основной интерфейс для CLI/UI
- **gRPC** — для внутреннего общения (если разделишь на микросервисы)
- **MCP Server** — опционально, для интеграции с другими агентами

**Пример HTTP endpoints:**

```
POST   /api/v1/teams                    # создать команду
GET    /api/v1/teams                    # список команд
GET    /api/v1/teams/{id}               # информация о команде
DELETE /api/v1/teams/{id}               # архивировать команду

POST   /api/v1/teams/{id}/segments      # создать сегмент
GET    /api/v1/teams/{id}/segments      # список сегментов

POST   /api/v1/segments/{id}/roles      # создать роль
GET    /api/v1/segments/{id}/roles      # список ролей

POST   /api/v1/tasks                    # создать задачу
GET    /api/v1/tasks                    # список задач (фильтры)
GET    /api/v1/tasks/{id}               # информация о задаче
PATCH  /api/v1/tasks/{id}/state         # изменить состояние
POST   /api/v1/tasks/{id}/handoff       # передать задачу

POST   /api/v1/sessions                 # создать сессию
GET    /api/v1/sessions                 # список сессий
DELETE /api/v1/sessions/{id}            # остановить сессию

POST   /api/v1/messages                 # отправить сообщение
GET    /api/v1/messages                 # получить сообщения

GET    /api/v1/watchdog/events          # события watchdog
```


______________________________________________________________________

### 2. **Task Service**

**Ответственность:**

- Жизненный цикл задач (создание, изменение состояния, завершение)
- Декомпозиция задач (через Lead-агента)
- Распределение подзадач воркерам

**Основные методы:**

```go
type TaskService interface {
    // Создание задачи
    CreateTask(ctx context.Context, req CreateTaskRequest) (*Task, error)
    
    // Получение задачи
    GetTask(ctx context.Context, id int64) (*Task, error)
    
    // Список задач
    ListTasks(ctx context.Context, filter TaskFilter) ([]*Task, error)
    
    // Изменение состояния
    UpdateTaskState(ctx context.Context, id int64, state TaskState, reason string) error
    
    // Handoff задачи
    HandoffTask(ctx context.Context, fromID int64, toRoleID int64, comment string) (*Task, error)
    
    // Завершение задачи
    CompleteTask(ctx context.Context, id int64, reason ClosureReason, targetID *int64) error
    
    // Отмена задачи
    CancelTask(ctx context.Context, id int64, reason string) error
}
```

**Логика:**

```go
func (s *taskService) CreateTask(ctx context.Context, req CreateTaskRequest) (*Task, error) {
    return s.db.WithTransaction(ctx, func(tx *pgx.Tx) (*Task, error) {
        // 1. Создать задачу
        task := &Task{
            TeamID:          req.TeamID,
            ParentTaskID:    req.ParentTaskID,
            ProjectID:       req.ProjectID,
            GoalID:          req.GoalID,
            DestinationRoleID: req.DestinationRoleID,
            SourceRoleID:    req.SourceRoleID,
            Title:           req.Title,
            Body:            req.Body,
            BodyContext:     req.BodyContext,
            State:           StatePending,
            Priority:        req.Priority,
        }
        
        if err := s.taskRepo.Create(ctx, tx, task); err != nil {
            return nil, err
        }
        
        // 2. Записать в историю
        history := &HistoryStatus{
            QueueTaskID: task.ID,
            FromState:   "",
            ToState:     string(StatePending),
            ActorType:   "daemon",
        }
        
        if err := s.historyRepo.Create(ctx, tx, history); err != nil {
            return nil, err
        }
        
        // 3. Опубликовать событие
        s.events.Publish(TaskCreated{
            TaskID: task.ID,
            TeamID: task.TeamID,
        })
        
        return task, nil
    })
}
```


______________________________________________________________________

### 3. **Session Service**

**Ответственность:**

- Запуск/остановка сессий агентов
- Мониторинг состояния сессий
- Управление runtime adapters

**Основные методы:**

```go
type SessionService interface {
    // Запуск сессии
    StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
    
    // Остановка сессии
    StopSession(ctx context.Context, id int64, reason string) error
    
    // Получение сессии
    GetSession(ctx context.Context, id int64) (*Session, error)
    
    // Список сессий
    ListSessions(ctx context.Context, filter SessionFilter) ([]*Session, error)
    
    // Перезапуск сессии
    RestartSession(ctx context.Context, id int64) (*Session, error)
}
```

**Session Manager:**

```go
type SessionManager struct {
    adapters map[RuntimeType]RuntimeAdapter
    repo     SessionRepository
    events   EventBus
}

type RuntimeAdapter interface {
    // Запуск сессии
    Start(ctx context.Context, cfg SessionConfig) (runtimeRef string, err error)
    
    // Остановка сессии
    Stop(ctx context.Context, runtimeRef string) error
    
    // Проверка статуса
    Status(ctx context.Context, runtimeRef string) (SessionState, error)
    
    // Отправка команды в сессию
    SendCommand(ctx context.Context, runtimeRef string, cmd string) error
}
```

**Пример запуска сессии:**

```go
func (m *SessionManager) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
    // 1. Создать запись в БД
    session := &Session{
        TeamID:      req.TeamID,
        RoleID:      req.RoleID,
        QueueTaskID: req.QueueTaskID,
        RuntimeType: req.RuntimeType,
        State:       StateStarting,
        WorkingDir:  req.WorkingDir,
        TargetDir:   req.TargetDir,
        Config:      req.Config,
    }
    
    if err := m.repo.Create(ctx, session); err != nil {
        return nil, err
    }
    
    // 2. Получить адаптер
    adapter, ok := m.adapters[req.RuntimeType]
    if !ok {
        return nil, fmt.Errorf("unknown runtime type: %s", req.RuntimeType)
    }
    
    // 3. Запустить сессию
    runtimeRef, err := adapter.Start(ctx, SessionConfig{
        TaskID:      req.QueueTaskID,
        Role:        req.Role,
        AgentSpec:   req.AgentSpec,
        WorkingDir:  req.WorkingDir,
        Environment: req.Environment,
    })
    
    if err != nil {
        session.State = StateFailed
        m.repo.Update(ctx, session)
        return nil, err
    }
    
    // 4. Обновить запись
    session.RuntimeRef = runtimeRef
    session.State = StateRunning
    session.StartedAt = time.Now()
    m.repo.Update(ctx, session)
    
    // 5. Опубликовать событие
    m.events.Publish(SessionStarted{
        SessionID: session.ID,
        TeamID:    session.TeamID,
    })
    
    return session, nil
}
```


______________________________________________________________________

### 4. **Task Orchestrator** (фоновый worker)

**Ответственность:**

- Автоматический запуск Lead-сессий для новых задач
- Декомпозиция задач через Lead-агента
- Распределение подзадач воркерам
- Завершение родительских задач

**Алгоритм:**

```go
type TaskOrchestrator struct {
    taskService    TaskService
    sessionService SessionService
    eventBus       EventBus
    config         OrchestratorConfig
}

func (o *TaskOrchestrator) Run(ctx context.Context) {
    ticker := time.NewTicker(o.config.PollInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            o.processPendingTasks(ctx)
        }
    }
}

func (o *TaskOrchestrator) processPendingTasks(ctx context.Context) {
    // 1. Найти все root-задачи в pending без Lead-сессии
    tasks, err := o.taskService.ListTasks(ctx, TaskFilter{
        State:     StatePending,
        Type:      "root",
        HasNoLead: true,
    })
    
    if err != nil {
        log.Error("failed to list pending tasks", "err", err)
        return
    }
    
    // 2. Для каждой задачи запустить Lead-сессию
    for _, task := range tasks {
        if err := o.startLeadSession(ctx, task); err != nil {
            log.Error("failed to start lead session", "task_id", task.ID, "err", err)
        }
    }
}

func (o *TaskOrchestrator) startLeadSession(ctx context.Context, task *Task) error {
    // Найти роль Lead для этой команды
    leadRole, err := o.findLeadRole(ctx, task.TeamID)
    if err != nil {
        return err
    }
    
    // Запустить сессию
    session, err := o.sessionService.StartSession(ctx, StartSessionRequest{
        TeamID:      task.TeamID,
        RoleID:      leadRole.ID,
        QueueTaskID: task.ID,
        RuntimeType: "container",
        Role:        "lead",
    })
    
    if err != nil {
        return err
    }
    
    // Обновить задачу
    return o.taskService.UpdateTask(ctx, task.ID, UpdateTaskRequest{
        SessionID: session.ID,
        State:     StateInProgress,
    })
}
```


______________________________________________________________________

### 5. **Watchdog Service** (фоновый worker)

**Ответственность:**

- Периодический скан состояния задач и сессий
- Детекция аномалий (stale, blocked, idle, drift)
- Генерация событий и вмешательств

**Алгоритм:**

```go
type WatchdogService struct {
    taskRepo     TaskRepository
    sessionRepo  SessionRepository
    eventRepo    WatchdogEventRepository
    messageService MessageService
    config       WatchdogConfig
}

func (w *WatchdogService) Run(ctx context.Context) {
    ticker := time.NewTicker(w.config.ScanInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            w.scan(ctx)
        }
    }
}

func (w *WatchdogService) scan(ctx context.Context) {
    // 1. Проверить stale задачи
    w.checkStaleTasks(ctx)
    
    // 2. Проверить blocked задачи
    w.checkBlockedTasks(ctx)
    
    // 3. Проверить idle сессии
    w.checkIdleSessions(ctx)
    
    // 4. Проверить drift от роли (опционально, сложнее)
    w.checkRoleDrift(ctx)
}

func (w *WatchdogService) checkStaleTasks(ctx context.Context) {
    // Найти задачи, которые не обновлялись дольше порога
    threshold := time.Now().Add(-w.config.StaleThreshold)
    
    tasks, err := w.taskRepo.FindStaleTasks(ctx, threshold)
    if err != nil {
        log.Error("failed to find stale tasks", "err", err)
        return
    }
    
    for _, task := range tasks {
        // Создать событие
        event := &WatchdogEvent{
            TeamID:      task.TeamID,
            QueueTaskID: task.ID,
            EventType:   "stale",
            Description: fmt.Sprintf("Task %d not updated since %s", task.ID, task.UpdatedAt),
        }
        
        w.eventRepo.Create(ctx, event)
        
        // Отправить wake-сообщение
        w.messageService.Send(ctx, SendMessageRequest{
            TeamID:       task.TeamID,
            ToRoleID:     task.DestinationRoleID,
            Type:         "watchdog",
            Body:         fmt.Sprintf("⚠️ Задача #%d не обновлялась более %s. Есть ли прогресс?", task.ID, w.config.StaleThreshold),
        })
    }
}

func (w *WatchdogService) checkBlockedTasks(ctx context.Context) {
    // Найти задачи, которые заблокированы дольше порога
    tasks, err := w.taskRepo.FindBlockedTasks(ctx, w.config.BlockedThreshold)
    if err != nil {
        log.Error("failed to find blocked tasks", "err", err)
        return
    }
    
    for _, task := range tasks {
        event := &WatchdogEvent{
            TeamID:      task.TeamID,
            QueueTaskID: task.ID,
            EventType:   "blocked",
            Description: fmt.Sprintf("Task %d blocked for too long", task.ID),
        }
        
        w.eventRepo.Create(ctx, event)
        
        // Если есть closure_target_id (блокирующая задача), проверить её
        if task.ClosureTargetID != nil {
            // Проверить, не застряла ли блокирующая задача
            blockerTask, err := w.taskRepo.GetByID(ctx, *task.ClosureTargetID)
            if err == nil && blockerTask.State == "done" {
                // Разблокировать
                w.taskService.UpdateTaskState(ctx, task.ID, StatePending, "")
            }
        }
    }
}
```


______________________________________________________________________

### 6. **Message Service**

**Ответственность:**

- Отправка сообщений между ролями
- Broadcast и segment-wide сообщения
- Системные уведомления

**Основные методы:**

```go
type MessageService interface {
    Send(ctx context.Context, req SendMessageRequest) (*Message, error)
    Broadcast(ctx context.Context, req BroadcastRequest) error
    GetMessages(ctx context.Context, filter MessageFilter) ([]*Message, error)
    MarkAsRead(ctx context.Context, id int64) error
}
```


______________________________________________________________________

### 7. **Event Bus**

**Ответственность:**

- Публикация событий (TaskCreated, SessionStarted, etc.)
- Подписка на события (для watchdog, оркестратора, etc.)

**Типы событий:**

```go
type Event interface {
    Type() string
    Timestamp() time.Time
}

type TaskCreated struct {
    TaskID    int64
    TeamID    int64
    Timestamp time.Time
}

type TaskStateChanged struct {
    TaskID     int64
    FromState  string
    ToState    string
    Timestamp  time.Time
}

type SessionStarted struct {
    SessionID int64
    TeamID    int64
    Timestamp time.Time
}

type WatchdogEventCreated struct {
    EventID   int64
    EventType string
    Timestamp time.Time
}
```

**Реализация (in-memory):**

```go
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
            }
        }
    }
}
```


______________________________________________________________________

## Конфигурация daemon

**Файл `config.yaml`:**

```yaml
server:
  http:
    host: "0.0.0.0"
    port: 8080
  grpc:
    host: "0.0.0.0"
    port: 9090

database:
  host: "localhost"
  port: 5432
  user: "daemon"
  password: "${DB_PASSWORD}"
  name: "daemon_db"
  ssl_mode: "disable"
  max_open_conns: 25
  max_idle_conns: 5

session:
  default_runtime: "container"
  container:
    docker_socket: "/var/run/docker.sock"
    network: "daemon-network"
  process:
    working_dir: "/tmp/sessions"

orchestrator:
  poll_interval: 5s
  max_concurrent_leads: 10

watchdog:
  enabled: true
  scan_interval: 30s
  stale_threshold: 2h
  blocked_threshold: 4h
  idle_threshold: 1h

logging:
  level: "info"
  format: "json"

events:
  type: "memory"  # или "nats"
  nats:
    url: "nats://localhost:4222"
```


______________________________________________________________________

## Жизненный цикл daemon

**main.go:**

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // 1. Загрузка конфигурации
    cfg, err := config.Load()
    if err != nil {
        log.Fatal("failed to load config", "err", err)
    }
    
    // 2. Инициализация логгера
    logger := utils.InitLogger(cfg.Logging)
    
    // 3. Подключение к БД
    db, err := database.Connect(cfg.Database)
    if err != nil {
        log.Fatal("failed to connect to database", "err", err)
    }
    defer db.Close()
    
    // 4. Применение миграций
    if err := database.Migrate(db); err != nil {
        log.Fatal("failed to migrate database", "err", err)
    }
    
    // 5. Инициализация репозиториев
    taskRepo := repository.NewTaskRepository(db)
    sessionRepo := repository.NewSessionRepository(db)
    teamRepo := repository.NewTeamRepository(db)
    // ...
    
    // 6. Инициализация сервисов
    taskService := service.NewTaskService(taskRepo, eventBus)
    sessionService := service.NewSessionService(sessionRepo, runtimeAdapters)
    messageService := service.NewMessageService(messageRepo)
    // ...
    
    // 7. Инициализация event bus
    eventBus := events.NewEventBus()
    
    // 8. Инициализация background workers
    orchestrator := orchestrator.NewTaskOrchestrator(taskService, sessionService, eventBus, cfg.Orchestrator)
    watchdog := watchdog.NewWatchdogService(taskRepo, sessionRepo, messageService, cfg.Watchdog)
    
    // 9. Запуск workers
    go orchestrator.Run(ctx)
    go watchdog.Run(ctx)
    
    // 10. Инициализация API
    httpServer := api.NewHTTPServer(cfg.Server.HTTP, taskService, sessionService, messageService)
    go httpServer.Start(ctx)
    
    // 11. Ожидание сигнала
    waitSignal(cancel)
    
    // 12. Graceful shutdown
    httpServer.Shutdown(ctx)
}
```
