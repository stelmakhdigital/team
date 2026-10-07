# Конткракты для UI.

## Архитектура UI

```
┌──────────────────────────────────────────────────────────┐
│                      UI COMPONENTS                       │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  1. TEAM BUILDER                                  │ │
│  │     - Визуальный редактор команд                 │ │
│  │     - Drag & Drop блоков (Roles, Segments)       │ │
│  │     - Настройка связей (Relatives)               │ │
│  │     - Сохранение в Library                       │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  2. WORKFLOW EDITOR                               │ │
│  │     - Визуальное построение workflow             │ │
│  │     - Блоки: Task, Decision, Parallel, Loop      │ │
│  │     - Связи между блоками                        │ │
│  │     - Конфигурация каждого блока                 │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  3. DASHBOARD                                     │ │
│  │     - Активные задачи                            │ │
│  │     - Сессии агентов                             │ │
│  │     - Метрики в реальном времени                 │ │
│  │     - Watchdog alerts                            │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  4. MESSAGE CENTER                                │ │
│  │     - Сообщения между ролями                     │ │
│  │     - Chatrooms                                  │ │
│  │     - Уведомления от watchdog                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  5. LIBRARY                                       │ │
│  │     - Сохранённые конфигурации                   │ │
│  │     - Группы (Teams, Workflows, Roles)           │ │
│  │     - Версионирование                            │ │
│  │     - История изменений                          │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  6. HISTORY VIEWER                                │ │
│  │     - Audit log                                  │ │
│  │     - История задач                              │ │
│  │     - История сессий                             │ │
│  │     - Agent transcripts                          │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
└──────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## API Contracts

### 1. **Team Builder API**

#### 1.1 Получить список команд

```typescript
// GET /api/v1/teams
interface GetTeamsResponse {
  teams: Team[];
  total: number;
}

interface Team {
  id: number;
  name: string;
  description?: string;
  state: 'active' | 'archived' | 'stopped';
  segments_count: number;
  roles_count: number;
  created_at: string;  // ISO8601
  updated_at: string;
}
```


#### 1.2 Создать команду

```typescript
// POST /api/v1/teams
interface CreateTeamRequest {
  name: string;
  description?: string;
  spec?: TeamSpec;  // YAML/JSON спецификация
}

interface CreateTeamResponse {
  id: number;
  name: string;
  status: 'created';
}

interface TeamSpec {
  segments: SegmentSpec[];
  roles: RoleSpec[];
  relatives?: RelativeSpec[];
}
```


#### 1.3 Получить детальную информацию о команде

```typescript
// GET /api/v1/teams/:id
interface GetTeamResponse {
  team: Team;
  segments: Segment[];
  roles: Role[];
  relatives: Relative[];
}

interface Segment {
  id: number;
  team_id: number;
  name: string;
  description?: string;
  config: Record<string, any>;
  roles_count: number;
  created_at: string;
  updated_at: string;
}

interface Role {
  id: number;
  team_id: number;
  segment_id: number;
  segment_name: string;
  name: string;
  address: string;  // unique: "team:segment.role"
  agent_spec: string;
  profile?: string;
  state: 'active' | 'inactive' | 'blocked';
  session?: {
    id: number;
    state: 'running' | 'stopped' | 'failed';
    started_at?: string;
  };
  created_at: string;
  updated_at: string;
}

interface Relative {
  id: number;
  team_id: number;
  from_role_id: number;
  from_role_name: string;
  to_role_id: number;
  to_role_name: string;
  type: 'delegates_to' | 'spawned_by' | 'can_observe' | 'collaborates_with';
  config?: Record<string, any>;
  created_at: string;
}
```


#### 1.4 Добавить сегмент

```typescript
// POST /api/v1/teams/:teamId/segments
interface CreateSegmentRequest {
  name: string;
  description?: string;
  config?: Record<string, any>;
}

interface CreateSegmentResponse {
  id: number;
  team_id: number;
  name: string;
  status: 'created';
}
```


#### 1.5 Добавить роль

```typescript
// POST /api/v1/segments/:segmentId/roles
interface CreateRoleRequest {
  name: string;
  agent_spec: string;
  profile?: string;
  config?: Record<string, any>;
}

interface CreateRoleResponse {
  id: number;
  segment_id: number;
  name: string;
  address: string;
  status: 'created';
}
```


#### 1.6 Создать связь

```typescript
// POST /api/v1/teams/:teamId/relatives
interface CreateRelativeRequest {
  from_role_id: number;
  to_role_id: number;
  type: 'delegates_to' | 'spawned_by' | 'can_observe' | 'collaborates_with';
  config?: Record<string, any>;
}

interface CreateRelativeResponse {
  id: number;
  from_role_name: string;
  to_role_name: string;
  type: string;
  status: 'created';
}
```


#### 1.7 Обновить конфигурацию роли

```typescript
// PATCH /api/v1/roles/:id/config
interface UpdateRoleConfigRequest {
  agent_spec?: string;
  profile?: string;
  config?: Record<string, any>;
}

interface UpdateRoleConfigResponse {
  id: number;
  status: 'updated';
  previous_config: Record<string, any>;
  new_config: Record<string, any>;
}
```


______________________________________________________________________

### 2. **Workflow Editor API**

#### 2.1 Получить workflow

```typescript
// GET /api/v1/workflows/:id
interface GetWorkflowResponse {
  workflow: Workflow;
  blocks: WorkflowBlock[];
  connections: WorkflowConnection[];
}

interface Workflow {
  id: number;
  team_id: number;
  name: string;
  description?: string;
  state: 'draft' | 'active' | 'archived';
  created_at: string;
  updated_at: string;
}

interface WorkflowBlock {
  id: number;
  workflow_id: number;
  type: 'task' | 'decision' | 'parallel' | 'loop' | 'agent' | 'manual';
  position: { x: number; y: number };
  config: Record<string, any>;
  label?: string;
  created_at: string;
}

interface WorkflowConnection {
  id: number;
  workflow_id: number;
  from_block_id: number;
  to_block_id: number;
  condition?: string;  // для decision: "yes", "no"
  created_at: string;
}
```


#### 2.2 Создать workflow

```typescript
// POST /api/v1/workflows
interface CreateWorkflowRequest {
  team_id: number;
  name: string;
  description?: string;
  blocks?: WorkflowBlockInput[];
  connections?: WorkflowConnectionInput[];
}

interface WorkflowBlockInput {
  type: string;
  position: { x: number; y: number };
  config: Record<string, any>;
  label?: string;
}

interface WorkflowConnectionInput {
  from_block_id: number;
  to_block_id: number;
  condition?: string;
}

interface CreateWorkflowResponse {
  id: number;
  name: string;
  status: 'created';
}
```


#### 2.3 Добавить блок

```typescript
// POST /api/v1/workflows/:workflowId/blocks
interface CreateBlockRequest {
  type: 'task' | 'decision' | 'parallel' | 'loop' | 'agent' | 'manual';
  position: { x: number; y: number };
  config: Record<string, any>;
  label?: string;
}

interface CreateBlockResponse {
  id: number;
  workflow_id: number;
  type: string;
  status: 'created';
}
```


#### 2.4 Создать связь

```typescript
// POST /api/v1/workflows/:workflowId/connections
interface CreateConnectionRequest {
  from_block_id: number;
  to_block_id: number;
  condition?: string;
}

interface CreateConnectionResponse {
  id: number;
  status: 'created';
}
```


#### 2.5 Обновить блок (drag \& drop)

```typescript
// PATCH /api/v1/workflows/:workflowId/blocks/:blockId
interface UpdateBlockRequest {
  position?: { x: number; y: number };
  config?: Record<string, any>;
  label?: string;
}

interface UpdateBlockResponse {
  id: number;
  status: 'updated';
  changes: {
    position?: { old: Position; new: Position };
    config?: { old: Record<string, any>; new: Record<string, any> };
  };
}
```


______________________________________________________________________

### 3. **Dashboard API**

#### 3.1 Получить сводку

```typescript
// GET /api/v1/dashboard/summary
interface DashboardSummaryResponse {
  teams: {
    total: number;
    active: number;
  };
  tasks: {
    total: number;
    pending: number;
    in_progress: number;
    blocked: number;
    done_today: number;
  };
  sessions: {
    total: number;
    running: number;
    failed: number;
  };
  alerts: {
    total: number;
    critical: number;
    warning: number;
  };
  updated_at: string;
}
```


#### 3.2 Активные задачи

```typescript
// GET /api/v1/dashboard/tasks
interface GetTasksResponse {
  tasks: Task[];
  total: number;
}

interface Task {
  id: number;
  team_id: number;
  team_name: string;
  title: string;
  state: 'pending' | 'in_progress' | 'blocked' | 'done';
  priority: number;
  destination_role_id: number;
  destination_role_name: string;
  source_role_id?: number;
  source_role_name?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  
  // Для UI
  progress?: number;  // 0-100
  is_stale?: boolean;  // не обновлялась > 2h
  is_blocked?: boolean;
}
```


#### 3.3 Активные сессии

```typescript
// GET /api/v1/dashboard/sessions
interface GetSessionsResponse {
  sessions: Session[];
  total: number;
}

interface Session {
  id: number;
  team_id: number;
  team_name: string;
  role_id: number;
  role_name: string;
  runtime_type: 'container' | 'process' | 'tmux' | 'pi';
  state: 'starting' | 'running' | 'idle' | 'stopping' | 'stopped' | 'failed';
  queue_task_id?: number;
  queue_task_title?: string;
  started_at?: string;
  uptime_seconds?: number;
  cpu_percent?: number;
  memory_bytes?: number;
}
```


#### 3.4 Alerts от watchdog

```typescript
// GET /api/v1/dashboard/alerts
interface GetAlertsResponse {
  alerts: WatchdogAlert[];
  total: number;
}

interface WatchdogAlert {
  id: number;
  team_id: number;
  team_name: string;
  event_type: 'wake' | 'refocus' | 'alignment_checkpoint' | 'stale' | 'blocked' | 'idle' | 'drift';
  severity: 'low' | 'medium' | 'high' | 'critical';
  description: string;
  queue_task_id?: number;
  session_id?: number;
  role_id?: number;
  action_taken?: string;
  created_at: string;
  
  // Для UI
  is_read: boolean;
  requires_action: boolean;
}
```


#### 3.5 Метрики (для графиков)

```typescript
// GET /api/v1/dashboard/metrics
interface GetMetricsResponse {
  time_range: {
    start: string;
    end: string;
  };
  metrics: {
    tasks_created: TimeSeriesPoint[];
    tasks_completed: TimeSeriesPoint[];
    sessions_active: TimeSeriesPoint[];
    queue_size: TimeSeriesPoint[];
    llm_tokens: TimeSeriesPoint[];
  };
}

interface TimeSeriesPoint {
  timestamp: string;
  value: number;
}
```


______________________________________________________________________

### 4. **Message Center API**

#### 4.1 Получить сообщения

```typescript
// GET /api/v1/messages
interface GetMessagesRequest {
  team_id?: number;
  queue_task_id?: number;
  from_role_id?: number;
  to_role_id?: number;
  type?: 'direct' | 'broadcast' | 'segment' | 'system' | 'watchdog';
  limit?: number;
  offset?: number;
}

interface GetMessagesResponse {
  messages: Message[];
  total: number;
  has_more: boolean;
}

interface Message {
  id: number;
  team_id: number;
  queue_task_id?: number;
  from_role_id?: number;
  from_role_name?: string;
  to_role_id?: number;
  to_role_name?: string;
  type: string;
  body: string;
  is_read: boolean;
  created_at: string;
  
  // Для UI
  is_mine?: boolean;  // от меня
  requires_reply?: boolean;
}
```


#### 4.2 Отправить сообщение

```typescript
// POST /api/v1/messages
interface SendMessageRequest {
  team_id: number;
  queue_task_id?: number;
  to_role_id?: number;
  type: 'direct' | 'broadcast' | 'segment';
  body: string;
  metadata?: Record<string, any>;
}

interface SendMessageResponse {
  id: number;
  status: 'sent';
  delivered_to?: number[];  // role IDs
}
```


#### 4.3 Chatrooms

```typescript
// GET /api/v1/chatrooms
interface GetChatroomsResponse {
  chatrooms: Chatroom[];
}

interface Chatroom {
  id: number;
  team_id: number;
  segment_id?: number;
  name: string;
  topic?: string;
  last_message?: {
    body: string;
    from_role_name: string;
    created_at: string;
  };
  unread_count: number;
  members_count: number;
}

// GET /api/v1/chatrooms/:id/messages
interface GetChatroomMessagesResponse {
  messages: ChatroomMessage[];
  has_more: boolean;
}

interface ChatroomMessage {
  id: number;
  chatroom_id: number;
  from_role_id?: number;
  from_role_name?: string;
  body: string;
  created_at: string;
  
  // Для UI
  is_mine: boolean;
}

// POST /api/v1/chatrooms/:id/messages
interface SendChatroomMessageRequest {
  body: string;
}

interface SendChatroomMessageResponse {
  id: number;
  status: 'sent';
}
```


______________________________________________________________________

### 5. **Library API**

#### 5.1 Получить библиотеку

```typescript
// GET /api/v1/library
interface GetLibraryRequest {
  type?: 'team' | 'workflow' | 'role' | 'segment';
  group?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

interface GetLibraryResponse {
  items: LibraryItem[];
  total: number;
  groups: LibraryGroup[];
}

interface LibraryItem {
  id: number;
  type: 'team' | 'workflow' | 'role' | 'segment';
  name: string;
  description?: string;
  group?: string;
  version: string;
  author?: string;
  is_public: boolean;
  downloads_count: number;
  created_at: string;
  updated_at: string;
  
  // Для UI
  thumbnail?: string;
  tags?: string[];
  rating?: number;
}

interface LibraryGroup {
  name: string;
  items_count: number;
  icon?: string;
}
```


#### 5.2 Сохранить в библиотеку

```typescript
// POST /api/v1/library
interface SaveToLibraryRequest {
  type: 'team' | 'workflow' | 'role' | 'segment';
  source_id: number;  // ID команды/workflow/роли
  name: string;
  description?: string;
  group?: string;
  is_public?: boolean;
  tags?: string[];
}

interface SaveToLibraryResponse {
  id: number;
  status: 'saved';
  library_item_id: number;
}
```


#### 5.3 Получить из библиотеки

```typescript
// GET /api/v1/library/:id
interface GetLibraryItemResponse {
  item: LibraryItem;
  spec: Record<string, any>;  // YAML/JSON спецификация
  versions: LibraryVersion[];
}

interface LibraryVersion {
  version: string;
  created_at: string;
  changes?: string;
  author?: string;
}
```


#### 5.4 Применить из библиотеки

```typescript
// POST /api/v1/library/:id/apply
interface ApplyLibraryItemRequest {
  target_team_id?: number;  // если применяем к существующей команде
  overrides?: Record<string, any>;  // переопределения
}

interface ApplyLibraryItemResponse {
  status: 'applied' | 'merged';
  created_resources?: {
    teams?: number[];
    segments?: number[];
    roles?: number[];
  };
  updated_resources?: {
    teams?: number[];
    segments?: number[];
    roles?: number[];
  };
}
```


______________________________________________________________________

### 6. **History Viewer API**

#### 6.1 История задачи

```typescript
// GET /api/v1/tasks/:id/history
interface GetTaskHistoryResponse {
  history: TaskHistoryEntry[];
  total: number;
}

interface TaskHistoryEntry {
  id: number;
  queue_task_id: number;
  from_state?: string;
  to_state: string;
  closure_reason?: string;
  closure_target_id?: number;
  closure_target_title?: string;
  actor_type: 'role' | 'daemon' | 'watchdog' | 'human';
  actor_role_id?: number;
  actor_role_name?: string;
  actor_session_id?: number;
  comment?: string;
  metadata?: Record<string, any>;
  created_at: string;
  
  // Для UI
  icon?: string;  // 'play', 'check', 'x', 'alert'
  color?: string;  // 'green', 'red', 'yellow', 'blue'
}
```


#### 6.2 История сессии

```typescript
// GET /api/v1/sessions/:id/history
interface GetSessionHistoryResponse {
  history: SessionHistoryEntry[];
  total: number;
}

interface SessionHistoryEntry {
  id: number;
  session_id: number;
  from_state?: string;
  to_state: string;
  actor_type: string;
  actor_role_name?: string;
  comment?: string;
  metadata?: Record<string, any>;
  created_at: string;
}
```


#### 6.3 Audit log

```typescript
// GET /api/v1/audit
interface GetAuditLogRequest {
  user_id?: number;
  action?: string;
  resource?: string;
  start_time?: string;
  end_time?: string;
  limit?: number;
  offset?: number;
}

interface GetAuditLogResponse {
  entries: AuditEntry[];
  total: number;
}

interface AuditEntry {
  id: number;
  timestamp: string;
  user_id?: number;
  user_name?: string;
  api_key_id?: number;
  action: string;  // 'task.create', 'session.stop', etc.
  resource?: string;  // 'task:123', 'session:456'
  details?: Record<string, any>;
  ip_address?: string;
  user_agent?: string;
  
  // Для UI
  icon?: string;
  severity?: 'info' | 'warning' | 'error';
}
```


#### 6.4 Agent transcripts

```typescript
// GET /api/v1/sessions/:id/transcript
interface GetTranscriptResponse {
  transcript: TranscriptEntry[];
  total: number;
  has_more: boolean;
}

interface TranscriptEntry {
  id: number;
  session_id: number;
  message_type: 'prompt' | 'response' | 'tool_call' | 'tool_result' | 'system';
  content: string;
  metadata?: {
    model?: string;
    tokens?: number;
    latency_ms?: number;
    tool_name?: string;
    tool_arguments?: Record<string, any>;
    tool_result?: string;
    tool_error?: string;
  };
  created_at: string;
  
  // Для UI
  role?: 'user' | 'assistant' | 'system';
  is_collapsed?: boolean;  // для UI
}
```


______________________________________________________________________

## WebSocket Events (Real-time)

```typescript
// Подключение: ws://localhost:8080/ws

// Client → Server
interface WSClientMessage {
  type: 'subscribe';
  channels: string[];  // ['team:1', 'task:123', 'session:456']
}

// Server → Client
interface WSServerMessage {
  type: 'task.created' | 'task.state_changed' | 'session.started' | 
        'session.stopped' | 'message.sent' | 'alert.created';
  data: any;
  timestamp: string;
}

// Примеры событий
interface TaskCreatedEvent {
  task_id: number;
  team_id: number;
  title: string;
  state: string;
}

interface TaskStateChangedEvent {
  task_id: number;
  from_state: string;
  to_state: string;
  updated_at: string;
}

interface SessionStartedEvent {
  session_id: number;
  role_name: string;
  runtime_type: string;
}

interface MessageSentEvent {
  message_id: number;
  from_role_name?: string;
  to_role_name?: string;
  body: string;
  type: string;
}

interface AlertCreatedEvent {
  alert_id: number;
  event_type: string;
  severity: string;
  description: string;
  requires_action: boolean;
}
```


______________________________________________________________________

## UI Components Structure

```typescript
// Frontend структура компонентов

src/
├── components/
│   ├── TeamBuilder/
│   │   ├── TeamCanvas.tsx       // Drag & Drop холст
│   │   ├── SegmentBlock.tsx     // Блок сегмента
│   │   ├── RoleBlock.tsx        // Блок роли
│   │   ├── RelativeConnector.tsx // Линия связи
│   │   ├── ConfigPanel.tsx      // Панель конфигурации
│   │   └── LibraryPanel.tsx     // Панель библиотеки
│   │
│   ├── WorkflowEditor/
│   │   ├── WorkflowCanvas.tsx
│   │   ├── BlockNode.tsx
│   │   ├── ConnectionLine.tsx
│   │   └── BlockConfig.tsx
│   │
│   ├── Dashboard/
│   │   ├── SummaryCards.tsx
│   │   ├── TaskList.tsx
│   │   ├── SessionGrid.tsx
│   │   ├── MetricsChart.tsx
│   │   └── AlertsPanel.tsx
│   │
│   ├── MessageCenter/
│   │   ├── MessageList.tsx
│   │   ├── MessageThread.tsx
│   │   ├── ChatroomList.tsx
│   │   └── ChatView.tsx
│   │
│   ├── Library/
│   │   ├── LibraryGrid.tsx
│   │   ├── LibraryItemCard.tsx
│   │   └── LibraryGroups.tsx
│   │
│   └── History/
│       ├── Timeline.tsx
│       ├── AuditTable.tsx
│       └── TranscriptViewer.tsx
│
├── hooks/
│   ├── useWebSocket.ts
│   ├── useTasks.ts
│   ├── useSessions.ts
│   └── useMessages.ts
│
├── api/
│   ├── teams.ts
│   ├── workflows.ts
│   ├── tasks.ts
│   ├── sessions.ts
│   ├── messages.ts
│   ├── library.ts
│   └── history.ts
│
└── types/
    └── api.ts  // Все TypeScript интерфейсы
```


______________________________________________________________________

## Mock Server для разработки

**Для параллельной разработки фронтенда:**

```typescript
// mock-server.ts
import { createServer } from 'miragejs';

createServer({
  models: {
    team: Model,
    segment: Model,
    role: Model,
    task: Model,
    session: Model,
    message: Model,
  },
  
  seeds(server) {
    // Создать тестовые данные
    const team = server.create('team', {
      name: 'Dev Team',
      state: 'active',
    });
    
    server.createList('segment', 3, { team });
    server.createList('role', 7, { team });
    server.createList('task', 10, { team });
    server.createList('session', 5, { team });
    server.createList('message', 20, { team });
  },
  
  routes() {
    this.namespace = '/api/v1';
    
    this.get('/teams');
    this.post('/teams');
    this.get('/teams/:id');
    
    this.get('/dashboard/summary');
    this.get('/dashboard/tasks');
    this.get('/dashboard/sessions');
    this.get('/dashboard/alerts');
    
    this.get('/messages');
    this.post('/messages');
    
    this.get('/library');
    this.post('/library');
    
    this.get('/tasks/:id/history');
    this.get('/sessions/:id/history');
    this.get('/audit');
  },
});
```


______________________________________________________________________

## Итог

**API Contracts покрывают:**

✅ **Team Builder** — создание/редактирование команд\
✅ **Workflow Editor** — визуальные workflow\
✅ **Dashboard** — метрики, задачи, сессии, alerts\
✅ **Message Center** — сообщения, chatrooms\
✅ **Library** — сохранение/применение конфигураций\
✅ **History Viewer** — audit, transcripts\
✅ **WebSocket** — real-time updates

**Преимущества:**

- Фронтенд может разрабатываться **параллельно** с бекендом
- Чёткая спецификация для обоих команд
- Mock server для тестирования UI
- Легко документировать (OpenAPI/Swagger)

Хочешь, набросаю **OpenAPI спецификацию** (Swagger) для этих контрактов или примеры **React компонентов** для Team Builder?

---
