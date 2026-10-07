
# Lead-агент

## Роль Lead-агента

**Lead-агент** — это центральный координатор задачи. Его основные функции:

1. **Получение задачи** — принимает root-задачу от daemon
2. **Анализ** — изучает контекст, ограничения, требования
3. **Декомпозиция** — разбивает на подзадачи
4. **Распределение** — назначает подзадачи воркерам по ролям
5. **Координация** — отслеживает выполнение, собирает результаты
6. **Завершение** — закрывает родительскую задачу

______________________________________________________________________

## Архитектура взаимодействия

```
┌──────────────────────────────────────────────────────────┐
│                     DAEMON                               │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Task Orchestrator                                 │ │
│  │                                                    │ │
│  │  1. Создаёт root-задачу (state=pending)           │ │
│  │  2. Запускает Lead-сессию                         │ │
│  │  3. Передаёт контекст задачи                     │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
                          │
                          │ HTTP/gRPC API
                          │ (инструменты для агента)
                          ▼
┌──────────────────────────────────────────────────────────┐
│              LEAD AGENT SESSION                          │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  LLM (Claude/GPT/локальная модель)                │ │
│  │                                                    │ │
│  │  Context:                                          │ │
│  │  - Role: "Lead Coordinator"                       │ │
│  │  - Task: title, description, project context      │ │
│  │  - Tools: create_subtask, assign_worker, ...      │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
                          │
                          │ Вызовы API к daemon
                          ▼
┌──────────────────────────────────────────────────────────┐
│                     DAEMON                               │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │   Create     │  │   Assign     │  │   Complete   │  │
│  │   Subtask    │  │   Worker     │  │   Task       │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
│                                                          │
│  Для каждой подзадачи:                                  │
│  - Создаёт queue_task (parent_id=root_id)              │
│  - Запускает Worker-сессию                             │
│  - Возвращает ID подзадачи Lead-агенту                 │
└──────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## Инструменты Lead-агента

Lead-агенту предоставляются **инструменты** (tools/functions) для взаимодействия с daemon:

### 1. `create_subtask`

Создание подзадачи.

```go
type CreateSubtaskRequest struct {
    Title       string            `json:"title"`
    Description string            `json:"description"`
    Role        string            `json:"role"`  // required role: "backend", "frontend", "reviewer", etc.
    Priority    int               `json:"priority,omitempty"`
    Context     map[string]interface{} `json:"context,omitempty"`  // файлы, ссылки, etc.
}

type CreateSubtaskResponse struct {
    SubtaskID int64  `json:"subtask_id"`
    Status    string `json:"status"`
}
```

**Пример вызова (из агента):**

```json
{
  "tool": "create_subtask",
  "arguments": {
    "title": "Реализовать HTTP handler для GET /api/users",
    "description": "Создать handler, который возвращает список пользователей из PostgreSQL. Использовать pagination (limit/offset).",
    "role": "backend",
    "priority": 1,
    "context": {
      "files": ["internal/handler/user.go", "internal/repository/user.go"],
      "related_task_id": 123
    }
  }
}
```

**Реализация на daemon:**

```go
func (s *taskService) CreateSubtask(ctx context.Context, parentTaskID int64, req CreateSubtaskRequest) (*Task, error) {
    return s.db.WithTransaction(ctx, func(tx *pgx.Tx) (*Task, error) {
        // 1. Получить родительскую задачу
        parentTask, err := s.taskRepo.GetByID(ctx, tx, parentTaskID)
        if err != nil {
            return nil, fmt.Errorf("parent task not found: %w", err)
        }
        
        // 2. Найти роль для этой подзадачи
        role, err := s.roleRepo.FindByTeamAndName(ctx, tx, parentTask.TeamID, req.Role)
        if err != nil {
            return nil, fmt.Errorf("role '%s' not found: %w", req.Role, err)
        }
        
        // 3. Создать подзадачу
        subtask := &Task{
            TeamID:          parentTask.TeamID,
            ParentTaskID:    parentTaskID,
            ProjectID:       parentTask.ProjectID,
            GoalID:          parentTask.GoalID,
            DestinationRoleID: role.ID,
            SourceRoleID:    parentTask.DestinationRoleID,  // Lead
            Title:           req.Title,
            Body:            req.Description,
            BodyContext:     req.Context,
            State:           StatePending,
            Priority:        req.Priority,
        }
        
        if err := s.taskRepo.Create(ctx, tx, subtask); err != nil {
            return nil, err
        }
        
        // 4. Записать в историю
        history := &HistoryStatus{
            QueueTaskID: subtask.ID,
            FromState:   "",
            ToState:     string(StatePending),
            ActorType:   "role",
            ActorRoleID: parentTask.DestinationRoleID,
        }
        
        if err := s.historyRepo.Create(ctx, tx, history); err != nil {
            return nil, err
        }
        
        // 5. Опубликовать событие
        s.events.Publish(TaskCreated{
            TaskID:    subtask.ID,
            TeamID:    subtask.TeamID,
            ParentID:  parentTaskID,
        })
        
        return subtask, nil
    })
}
```


______________________________________________________________________

### 2. `assign_worker`

Назначение конкретного воркера на подзадачу (опционально, если нужно выбрать конкретную сессию).

```go
type AssignWorkerRequest struct {
    SubtaskID int64  `json:"subtask_id"`
    SessionID int64  `json:"session_id"`  // ID существующей сессии
}
```


______________________________________________________________________

### 3. `complete_task`

Завершение задачи (для подзадач или root-задачи).

```go
type CompleteTaskRequest struct {
    TaskID        int64  `json:"task_id"`
    ClosureReason string `json:"closure_reason"`  // "no_follow_on", "handed_off_to", etc.
    TargetTaskID  *int64 `json:"target_task_id,omitempty"`  // для handed_off_to
    Comment       string `json:"comment,omitempty"`
}
```

**Реализация:**

```go
func (s *taskService) CompleteTask(ctx context.Context, req CompleteTaskRequest) error {
    return s.db.WithTransaction(ctx, func(tx *pgx.Tx) error {
        // 1. Получить задачу
        task, err := s.taskRepo.GetByID(ctx, tx, req.TaskID)
        if err != nil {
            return err
        }
        
        // 2. Проверить валидность closure_reason
        if !isValidClosureReason(req.ClosureReason) {
            return fmt.Errorf("invalid closure reason: %s", req.ClosureReason)
        }
        
        // 3. Обновить задачу
        task.State = StateDone
        task.ClosureReason = req.ClosureReason
        task.ClosureTargetID = req.TargetTaskID
        task.CompletedAt = time.Now()
        
        if err := s.taskRepo.Update(ctx, tx, task); err != nil {
            return err
        }
        
        // 4. Записать в историю
        history := &HistoryStatus{
            QueueTaskID:    task.ID,
            FromState:      string(task.State),
            ToState:        string(StateDone),
            ClosureReason:  req.ClosureReason,
            ClosureTargetID: req.TargetTaskID,
            ActorRoleID:    task.DestinationRoleID,
            Comment:        req.Comment,
        }
        
        if err := s.historyRepo.Create(ctx, tx, history); err != nil {
            return err
        }
        
        // 5. Если это подзадача, проверить, не завершены ли все sibling'и
        if task.ParentTaskID != nil {
            if err := s.checkParentTaskCompletion(ctx, tx, *task.ParentTaskID); err != nil {
                return err
            }
        }
        
        // 6. Опубликовать событие
        s.events.Publish(TaskCompleted{
            TaskID:   task.ID,
            TeamID:   task.TeamID,
            Reason:   req.ClosureReason,
        })
        
        return nil
    })
}

// Проверка, завершены ли все подзадачи родительской задачи
func (s *taskService) checkParentTaskCompletion(ctx context.Context, tx *pgx.Tx, parentTaskID int64) error {
    // Посчитать количество незавершённых подзадач
    pendingCount, err := s.taskRepo.CountPendingSubtasks(ctx, tx, parentTaskID)
    if err != nil {
        return err
    }
    
    // Если все подзадачи завершены, можно завершить родительскую
    if pendingCount == 0 {
        parentTask, err := s.taskRepo.GetByID(ctx, tx, parentTaskID)
        if err != nil {
            return err
        }
        
        // Автоматически завершить родительскую задачу
        parentTask.State = StateDone
        parentTask.ClosureReason = "no_follow_on"
        parentTask.CompletedAt = time.Now()
        
        return s.taskRepo.Update(ctx, tx, parentTask)
    }
    
    return nil
}
```


______________________________________________________________________

### 4. `send_message`

Отправка сообщения другой роли (для координации).

```go
type SendMessageRequest struct {
    ToRoleID int64  `json:"to_role_id"`
    Body     string `json:"body"`
}
```


______________________________________________________________________

### 5. `get_task_info`

Получение информации о задаче (для контекста).

```go
type GetTaskInfoResponse struct {
    TaskID      int64             `json:"task_id"`
    Title       string            `json:"title"`
    Description string            `json:"description"`
    State       string            `json:"state"`
    Subtasks    []SubtaskSummary  `json:"subtasks"`
    Project     ProjectSummary    `json:"project"`
}
```


______________________________________________________________________

## Промпт для Lead-агента

**Системный промпт:**

```
Ты — Lead Coordinator, центральный агент для управления задачами.

ТВОЯ РОЛЬ:
- Ты получаешь сложные задачи и декомпозируешь их на более мелкие подзадачи
- Ты назначаешь подзадачи специализированным воркерам (backend, frontend, reviewer, infra, etc.)
- Ты отслеживаешь выполнение и координируешь работу команды
- Ты завершаешь задачу, когда все подзадачи выполнены

ТВОИ ВОЗМОЖНОСТИ:
- create_subtask(title, description, role, priority, context) — создать подзадачу для воркера
- complete_task(task_id, closure_reason, target_task_id, comment) — завершить задачу
- send_message(to_role_id, body) — отправить сообщение другой роли
- get_task_info(task_id) — получить информацию о задаче

ПРАВИЛА:
1. ВСЕГДА декомпозируй задачу на логические подзадачи
2. Назначай подзадачи на соответствующие роли:
   - backend — API, БД, бизнес-логика
   - frontend — UI, компоненты, стили
   - reviewer — code review, тесты
   - infra — деплой, конфигурация, мониторинг
   - docs — документация
3. Указывай чёткое описание для каждой подзадачи
4. Добавляй контекст (файлы, ссылки, связанные задачи)
5. После создания всех подзадач, отслеживай их выполнение
6. Когда все подзадачи завершены, завершай родительскую задачу

ПРИМЕР ДЕКОМПОЗИЦИИ:
Задача: "Реализовать авторизацию через OAuth2"

Подзадачи:
1. backend: "Создать OAuth2 client для Google"
   - role: backend
   - context: [files: "internal/auth/oauth.go"]

2. backend: "Реализовать endpoint /api/auth/callback"
   - role: backend
   - context: [files: "internal/handler/auth.go"]

3. frontend: "Создать кнопку 'Login with Google'"
   - role: frontend
   - context: [files: "src/components/LoginButton.tsx"]

4. reviewer: "Проверить безопасность OAuth2 flow"
   - role: reviewer

5. docs: "Обновить документацию по авторизации"
   - role: docs
   - context: [files: "docs/auth.md"]
```


______________________________________________________________________

## Алгоритм декомпозиции

### Шаг 1: Получение задачи

```go
type LeadAgent struct {
    httpClient *http.Client
    sessionID  int64
    taskID     int64
    baseURL    string
}

func (a *LeadAgent) Start(ctx context.Context) error {
    // 1. Получить информацию о задаче
    taskInfo, err := a.getTaskInfo(ctx, a.taskID)
    if err != nil {
        return err
    }
    
    // 2. Сформировать промпт с контекстом
    prompt := a.buildPrompt(taskInfo)
    
    // 3. Отправить LLM
    response, err := a.callLLM(ctx, prompt)
    if err != nil {
        return err
    }
    
    // 4. Обработать ответ (распознать вызовы инструментов)
    return a.processResponse(ctx, response)
}
```


### Шаг 2: Анализ и декомпозиция

LLM возвращает структурированный ответ:

```json
{
  "analysis": "Задача требует реализации OAuth2 авторизации. Необходимо создать backend для обработки OAuth2 flow, frontend компонент для кнопки входа, проверить безопасность и обновить документацию.",
  "subtasks": [
    {
      "title": "Создать OAuth2 client для Google",
      "description": "Реализовать клиента для взаимодействия с Google OAuth2 API. Использовать библиотеку golang.org/x/oauth2.",
      "role": "backend",
      "priority": 1,
      "context": {
        "files": ["internal/auth/oauth.go"],
        "dependencies": []
      }
    },
    {
      "title": "Реализовать endpoint /api/auth/callback",
      "description": "Создать handler для обработки callback от Google OAuth2. Сохранять токен в сессию.",
      "role": "backend",
      "priority": 1,
      "context": {
        "files": ["internal/handler/auth.go"],
        "dependencies": ["oauth_client"]
      }
    },
    {
      "title": "Создать кнопку 'Login with Google'",
      "description": "React компонент кнопки для инициации OAuth2 flow.",
      "role": "frontend",
      "priority": 2,
      "context": {
        "files": ["src/components/LoginButton.tsx"],
        "dependencies": []
      }
    },
    {
      "title": "Проверить безопасность OAuth2 flow",
      "description": "Code review реализации OAuth2 на предмет уязвимостей (PKCE, state parameter, etc.)",
      "role": "reviewer",
      "priority": 3,
      "context": {
        "dependencies": ["oauth_client", "auth_callback"]
      }
    },
    {
      "title": "Обновить документацию по авторизации",
      "description": "Добавить описание OAuth2 авторизации в документацию.",
      "role": "docs",
      "priority": 4,
      "context": {
        "files": ["docs/auth.md"],
        "dependencies": ["oauth_client", "auth_callback", "login_button"]
      }
    }
  ]
}
```


### Шаг 3: Создание подзадач

```go
func (a *LeadAgent) processResponse(ctx context.Context, response LLMResponse) error {
    // 1. Распарсить ответ
    var result DecompositionResult
    if err := json.Unmarshal([]byte(response.Content), &result); err != nil {
        return err
    }
    
    // 2. Создать подзадачи
    createdSubtasks := make([]int64, 0, len(result.Subtasks))
    
    for _, subtask := range result.Subtasks {
        req := CreateSubtaskRequest{
            Title:       subtask.Title,
            Description: subtask.Description,
            Role:        subtask.Role,
            Priority:    subtask.Priority,
            Context:     subtask.Context,
        }
        
        resp, err := a.createSubtask(ctx, req)
        if err != nil {
            log.Error("failed to create subtask", "title", subtask.Title, "err", err)
            continue
        }
        
        createdSubtasks = append(createdSubtasks, resp.SubtaskID)
        log.Info("created subtask", "id", resp.SubtaskID, "title", subtask.Title)
    }
    
    // 3. Сохранить зависимости между подзадачами (опционально)
    // Можно добавить в body_context ссылки на dependencies
    
    // 4. Перейти в режим ожидания завершения подзадач
    return a.waitForCompletion(ctx, createdSubtasks)
}
```


### Шаг 4: Ожидание и координация

```go
func (a *LeadAgent) waitForCompletion(ctx context.Context, subtaskIDs []int64) error {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            // Проверить статус всех подзадач
            allDone, err := a.checkSubtasksCompletion(ctx, subtaskIDs)
            if err != nil {
                log.Error("failed to check completion", "err", err)
                continue
            }
            
            if allDone {
                // Все подзадачи завершены
                return a.completeParentTask(ctx)
            }
        }
    }
}

func (a *LeadAgent) checkSubtasksCompletion(ctx context.Context, subtaskIDs []int64) (bool, error) {
    for _, subtaskID := range subtaskIDs {
        info, err := a.getTaskInfo(ctx, subtaskID)
        if err != nil {
            return false, err
        }
        
        if info.State != "done" {
            // Если задача заблокирована или в progress слишком долго,
            // можно отправить wake-сообщение
            if time.Since(info.UpdatedAt) > 2*time.Hour {
                if err := a.sendWakeMessage(ctx, info); err != nil {
                    log.Error("failed to send wake message", "err", err)
                }
            }
            return false, nil
        }
    }
    
    return true, nil
}
```


### Шаг 5: Завершение родительской задачи

```go
func (a *LeadAgent) completeParentTask(ctx context.Context) error {
    req := CompleteTaskRequest{
        TaskID:        a.taskID,
        ClosureReason: "no_follow_on",
        Comment:       "Все подзадачи завершены",
    }
    
    if err := a.completeTask(ctx, req); err != nil {
        return err
    }
    
    log.Info("parent task completed", "task_id", a.taskID)
    return nil
}
```


______________________________________________________________________

## Обработка ошибок и edge cases

### 1. Если подзадача заблокирована

```go
func (a *LeadAgent) handleBlockedSubtask(ctx context.Context, subtaskID int64) error {
    // 1. Получить информацию
    info, err := a.getTaskInfo(ctx, subtaskID)
    if err != nil {
        return err
    }
    
    // 2. Если blocked_on, проверить блокирующую задачу
    if info.ClosureReason == "blocked_on" && info.ClosureTargetID != nil {
        blockerInfo, err := a.getTaskInfo(ctx, *info.ClosureTargetID)
        if err != nil {
            return err
        }
        
        // 3. Если блокирующая задача завершена, разблокировать
        if blockerInfo.State == "done" {
            // Нужно вызвать API daemon для обновления состояния
            return a.unblockTask(ctx, subtaskID)
        }
    }
    
    // 4. Отправить сообщение воркеру
    return a.sendMessage(ctx, SendMessageRequest{
        ToRoleID: info.DestinationRoleID,
        Body:     fmt.Sprintf("Задача #%d заблокирована. Нужна помощь?", subtaskID),
    })
}
```


### 2. Если воркер не справляется

```go
func (a *LeadAgent) reassignSubtask(ctx context.Context, subtaskID int64, newRole string) error {
    // 1. Создать новую подзадачу для другой роли
    newSubtask, err := a.createSubtask(ctx, CreateSubtaskRequest{
        Title:       fmt.Sprintf("Reassign: задача #%d", subtaskID),
        Description: "Переназначенная задача из-за проблем с выполнением",
        Role:        newRole,
        Priority:    1,
        Context: map[string]interface{}{
            "original_task_id": subtaskID,
            "reassignment_reason": "worker_unable_to_complete",
        },
    })
    
    if err != nil {
        return err
    }
    
    // 2. Заблокировать старую задачу на новую
    return a.completeTask(ctx, CompleteTaskRequest{
        TaskID:        subtaskID,
        ClosureReason: "blocked_on",
        TargetTaskID:  &newSubtask.SubtaskID,
        Comment:       "Переназначено на " + newRole,
    })
}
```


### 3. Если задача слишком сложная

```go
func (a *LeadAgent) escalateTask(ctx context.Context, reason string) error {
    // 1. Найти роль выше (например, human или senior lead)
    escalationRole, err := a.findEscalationRole(ctx)
    if err != nil {
        return err
    }
    
    // 2. Создать эскалационную задачу
    escalationTask, err := a.createSubtask(ctx, CreateSubtaskRequest{
        Title:       "Escalation: требуется вмешательство",
        Description: fmt.Sprintf("Задача #%d требует вмешательства. Причина: %s", a.taskID, reason),
        Role:        escalationRole.Name,
        Priority:    1,
        Context: map[string]interface{}{
            "original_task_id": a.taskID,
            "escalation_reason": reason,
        },
    })
    
    if err != nil {
        return err
    }
    
    // 3. Заблокировать текущую задачу на эскалационную
    return a.completeTask(ctx, CompleteTaskRequest{
        TaskID:        a.taskID,
        ClosureReason: "escalation",
        TargetTaskID:  &escalationTask.SubtaskID,
        Comment:       "Эскалировано: " + reason,
    })
}
```


______________________________________________________________________

## Интеграция с daemon API

**HTTP handler для создания подзадачи:**

```go
type TaskHandler struct {
    taskService service.TaskService
}

func (h *TaskHandler) CreateSubtask(w http.ResponseWriter, r *http.Request) {
    var req CreateSubtaskRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    // Получить task_id из URL или context
    taskID, err := getTaskIDFromContext(r.Context())
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    // Создать подзадачу
    subtask, err := h.taskService.CreateSubtask(r.Context(), taskID, req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Вернуть ответ
    response := CreateSubtaskResponse{
        SubtaskID: subtask.ID,
        Status:    "created",
    }
    
    json.NewEncoder(w).Encode(response)
}
```


______________________________________________________________________

## Оптимизации

### 1. Параллельное создание подзадач

```go
func (a *LeadAgent) createSubtasksParallel(ctx context.Context, subtasks []Subtask) ([]int64, error) {
    resultCh := make(chan CreateSubtaskResponse, len(subtasks))
    errCh := make(chan error, len(subtasks))
    
    for _, subtask := range subtasks {
        go func(s Subtask) {
            resp, err := a.createSubtask(ctx, CreateSubtaskRequest{
                Title:       s.Title,
                Description: s.Description,
                Role:        s.Role,
                Priority:    s.Priority,
                Context:     s.Context,
            })
            
            if err != nil {
                errCh <- err
                return
            }
            
            resultCh <- resp
        }(subtask)
    }
    
    createdIDs := make([]int64, 0, len(subtasks))
    
    for i := 0; i < len(subtasks); i++ {
        select {
        case resp := <-resultCh:
            createdIDs = append(createdIDs, resp.SubtaskID)
        case err := <-errCh:
            log.Error("failed to create subtask", "err", err)
        case <-ctx.Done():
            return createdIDs, ctx.Err()
        }
    }
    
    return createdIDs, nil
}
```


### 2. Кэширование информации о ролях

```go
type RoleCache struct {
    mu    sync.RWMutex
    roles map[int64][]*Role  // team_id -> roles
}

func (c *RoleCache) GetRoleByName(teamID int64, roleName string) (*Role, error) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    roles, ok := c.roles[teamID]
    if !ok {
        return nil, fmt.Errorf("team not found")
    }
    
    for _, role := range roles {
        if role.Name == roleName {
            return role, nil
        }
    }
    
    return nil, fmt.Errorf("role not found: %s", roleName)
}
```


