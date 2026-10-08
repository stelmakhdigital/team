// All types mirror docs/architecture/frontend/20_contract_API.md and
// 21_team_builder.md 1:1. Do not invent extra response shapes here.

export interface Team {
  id: number;
  name: string;
  description?: string;
  state: 'active' | 'archived' | 'stopped';
  segments_count: number;
  roles_count: number;
  created_at: string;
  updated_at: string;
}

export interface TeamSpec {
  segments: SegmentSpec[];
  roles: RoleSpec[];
  relatives?: RelativeSpec[];
}

export interface SegmentSpec {
  name: string;
  description?: string;
  config?: Record<string, unknown>;
}

export interface RoleSpec {
  name: string;
  agent_spec: string;
  profile?: string;
  segment?: string;
  config?: Record<string, unknown>;
}

export interface RelativeSpec {
  // адресный формат "Segment.Role" (уточнение контракта 20, blockers #8)
  from: string;
  to: string;
  type: RelativeType;
  config?: Record<string, unknown>;
}

export interface GetTeamsResponse {
  teams: Team[];
  total: number;
}

export interface CreateTeamRequest {
  name: string;
  description?: string;
  spec?: TeamSpec;
}

export interface CreateTeamResponse {
  id: number;
  name: string;
  status: 'created';
}

export interface Segment {
  id: number;
  team_id: number;
  name: string;
  description?: string;
  config: Record<string, unknown>;
  roles_count: number;
  created_at: string;
  updated_at: string;
}

export type RoleState = 'active' | 'inactive' | 'blocked';

export interface RoleSession {
  id: number;
  state: 'running' | 'stopped' | 'failed';
  started_at?: string;
}

export interface Role {
  id: number;
  team_id: number;
  segment_id: number;
  segment_name: string;
  name: string;
  address: string;
  agent_spec: string;
  profile?: string;
  state: RoleState;
  session?: RoleSession;
  created_at: string;
  updated_at: string;
}

export type RelativeType =
  | 'delegates_to'
  | 'spawned_by'
  | 'can_observe'
  | 'collaborates_with';

export interface Relative {
  id: number;
  team_id: number;
  from_role_id: number;
  from_role_name: string;
  to_role_id: number;
  to_role_name: string;
  type: RelativeType;
  config?: Record<string, unknown>;
  created_at: string;
}

export interface GetTeamResponse {
  team: Team;
  segments: Segment[];
  roles: Role[];
  relatives: Relative[];
}

// ---- Team Builder layout (21_team_builder.md) ----

export interface Position {
  x: number;
  y: number;
}

export interface SegmentLayout {
  segment_id: number;
  position: Position & { width: number; height: number };
  collapsed: boolean;
}

export interface RoleLayout {
  role_id: number;
  segment_id: number;
  position: Position;
}

export interface RelativeLayout {
  relative_id: number;
  from_role_id: number;
  to_role_id: number;
  path?: Position[];
}

export interface TopologyLayout {
  segments: SegmentLayout[];
  roles: RoleLayout[];
  relatives: RelativeLayout[];
}

export interface GetTopologyResponse extends GetTeamResponse {
  layout?: TopologyLayout;
}

export interface CreateSegmentRequest {
  name: string;
  description?: string;
  config?: Record<string, unknown>;
  // UI layout (21_team_builder.md; see blockers #4)
  layout?: { x: number; y: number; width?: number; height?: number };
}

export interface CreateSegmentResponse {
  id: number;
  team_id: number;
  name: string;
  layout?: { x: number; y: number; width: number; height: number };
  status: 'created';
}

export interface CreateRoleRequest {
  name: string;
  agent_spec: string;
  profile?: string;
  config?: Record<string, unknown>;
  // UI layout (21_team_builder.md; see blockers #4)
  layout?: { x: number; y: number };
}

export interface CreateRoleResponse {
  id: number;
  segment_id: number;
  name: string;
  address: string;
  layout?: { x: number; y: number };
  status: 'created';
}

export interface CreateRelativeRequest {
  from_role_id: number;
  to_role_id: number;
  type: RelativeType;
  config?: Record<string, unknown>;
  layout?: {
    path?: Position[];
    label_position?: Position;
  };
}

export interface CreateRelativeResponse {
  id: number;
  from_role_name: string;
  to_role_name: string;
  type: string;
  status: 'created';
}

export interface UpdateSegmentLayoutRequest {
  position: Position;
  size?: { width: number; height: number };
  collapsed?: boolean;
}

export interface UpdateSegmentLayoutResponse {
  id: number;
  status: 'updated';
  previous_layout: SegmentLayout;
  new_layout: SegmentLayout;
}

export interface UpdateRoleLayoutRequest {
  position: Position;
}

export interface UpdateRoleLayoutResponse {
  id: number;
  status: 'updated';
  previous_position: Position;
  new_position: Position;
}

export type TopologyIssueType = 'role' | 'segment' | 'relative';

export interface TopologyLocation {
  type: TopologyIssueType;
  id: number;
  name: string;
}

export interface TopologyError {
  code: string;
  message: string;
  severity: 'error';
  location: TopologyLocation;
  fix_suggestion?: string;
}

export interface TopologyWarning {
  code: string;
  message: string;
  severity: 'warning';
  location?: TopologyLocation;
  fix_suggestion?: string;
}

export interface ValidateTopologyRequest {
  check_circular?: boolean;
  check_orphans?: boolean;
  check_required_fields?: boolean;
}

export interface ValidateTopologyResponse {
  is_valid: boolean;
  errors: TopologyError[];
  warnings: TopologyWarning[];
}

export interface SaveTopologyRequest {
  name?: string;
  description?: string;
  save_to_library?: boolean;
  library_group?: string;
  is_public?: boolean;
}

export interface SaveTopologyResponse {
  team_id: number;
  status: 'saved';
  library_item_id?: number;
  validation: ValidateTopologyResponse;
}

// ---- Role config (21_team_builder.md) ----

export interface PluginSpec {
  name: string;
  enabled: boolean;
  config: Record<string, unknown>;
  description: string;
}

export interface SkillSpec {
  path: string;
  enabled: boolean;
  description: string;
}

export interface HookSpec {
  path: string;
  enabled: boolean;
  description: string;
}

export interface MCPConfig {
  servers: { name: string; url: string; enabled: boolean }[];
}

export interface StartupFile {
  path: string;
  orientation: 'role' | 'context' | 'culture';
  delivery_hint: 'send_text' | 'project_file';
}

export interface StartupScript {
  path: string;
  description?: string;
}

export interface AgentSpec {
  name: string;
  version: string;
  runtime: { type: 'pi' | 'claude-code' | 'codex'; version: string };
  pi_config: {
    model: string;
    temperature: number;
    max_tokens: number;
    plugins: PluginSpec[];
    skills: SkillSpec[];
    hooks: HookSpec[];
    mcp: MCPConfig;
  };
  resources: { cpu: string; memory: string; gpu: boolean };
  startup: { files: StartupFile[]; scripts: StartupScript[] };
}

export interface Profile {
  name: string;
  description?: string;
}

export interface GetRoleConfigResponse {
  role: Role;
  agent_spec: AgentSpec;
  available_profiles: Profile[];
  available_skills: SkillSpec[];
  available_plugins: PluginSpec[];
}

export interface UpdateRoleConfigRequest {
  agent_spec?: string;
  profile?: string;
  pi_config?: {
    model?: string;
    temperature?: number;
    plugins?: Record<string, boolean>;
    skills?: string[];
  };
  resources?: { cpu?: string; memory?: string };
  startup?: { files?: StartupFile[] };
}

export interface UpdateRoleConfigResponse {
  id: number;
  status: 'updated';
  previous_config: AgentSpec;
  new_config: AgentSpec;
  requires_restart: boolean;
}

// ---- Workflow Editor ----

export interface Workflow {
  id: number;
  team_id: number;
  name: string;
  description?: string;
  state: 'draft' | 'active' | 'archived';
  created_at: string;
  updated_at: string;
}

export type WorkflowBlockType = 'task' | 'decision' | 'parallel' | 'loop' | 'agent' | 'manual';

export interface WorkflowBlock {
  id: number;
  workflow_id: number;
  type: WorkflowBlockType;
  position: Position;
  config: Record<string, unknown>;
  label?: string;
  created_at: string;
}

export interface WorkflowConnection {
  id: number;
  workflow_id: number;
  from_block_id: number;
  to_block_id: number;
  condition?: string;
  created_at: string;
}

export interface GetWorkflowResponse {
  workflow: Workflow;
  blocks: WorkflowBlock[];
  connections: WorkflowConnection[];
}

export interface GetWorkflowsResponse {
  workflows: Workflow[];
  total: number;
}

// Endpoint не зафиксирован в контракте (blockers #3): GET /api/v1/workflows
export interface GetWorkflowsParams {
  team_id?: number;
  state?: Workflow['state'];
}

export interface WorkflowBlockInput {
  type: WorkflowBlockType;
  position: Position;
  config: Record<string, unknown>;
  label?: string;
}

export interface WorkflowConnectionInput {
  from_block_id: number;
  to_block_id: number;
  condition?: string;
}

export interface CreateWorkflowRequest {
  team_id: number;
  name: string;
  description?: string;
  blocks?: WorkflowBlockInput[];
  connections?: WorkflowConnectionInput[];
}

export interface CreateWorkflowResponse {
  id: number;
  name: string;
  status: 'created';
}

export interface CreateBlockRequest {
  type: WorkflowBlockType;
  position: Position;
  config: Record<string, unknown>;
  label?: string;
}

export interface CreateBlockResponse {
  id: number;
  workflow_id: number;
  type: string;
  status: 'created';
}

export interface CreateConnectionRequest {
  from_block_id: number;
  to_block_id: number;
  condition?: string;
}

export interface CreateConnectionResponse {
  id: number;
  status: 'created';
}

export interface UpdateBlockRequest {
  position?: Position;
  config?: Record<string, unknown>;
  label?: string;
}

export interface UpdateBlockResponse {
  id: number;
  status: 'updated';
  changes: {
    position?: { old: Position; new: Position };
    config?: { old: Record<string, unknown>; new: Record<string, unknown> };
  };
}

// ---- Dashboard ----

export interface DashboardSummaryResponse {
  teams: { total: number; active: number };
  tasks: { total: number; pending: number; in_progress: number; blocked: number; done_today: number };
  sessions: { total: number; running: number; failed: number };
  alerts: { total: number; critical: number; warning: number };
  updated_at: string;
}

export type TaskState = 'pending' | 'in_progress' | 'blocked' | 'done' | 'canceled';

export type ClosureReason = 'handed_off_to' | 'blocked_on' | 'denied' | 'canceled' | 'no_follow_on' | 'escalation';

export interface Task {
  id: number;
  team_id: number;
  team_name: string;
  parent_task_id?: number;
  title: string;
  body?: string;
  body_context?: Record<string, unknown>;
  state: TaskState;
  priority: number;
  destination_role_id: number;
  destination_role_name: string;
  source_role_id?: number;
  source_role_name?: string;
  closure_reason?: ClosureReason;
  closure_target_id?: number;
  created_at: string;
  updated_at: string;
  started_at?: string;
  progress?: number;
  is_stale?: boolean;
  is_blocked?: boolean;
}

export interface GetTasksResponse {
  tasks: Task[];
  total: number;
}

export interface ListTasksParams {
  team_id?: number;
  state?: TaskState;
  destination_role_id?: number;
  limit?: number;
  offset?: number;
}

export interface CreateTaskRequest {
  team_id: number;
  parent_task_id?: number;
  destination_role_id: number;
  source_role_id?: number;
  title: string;
  body?: string;
  body_context?: Record<string, unknown>;
  priority?: number;
}

export interface CreateTaskResponse {
  id: number;
  state: TaskState;
  status: 'created';
}

export interface GetTaskResponse {
  task: Task;
  subtasks: Task[];
}

export interface UpdateTaskStateRequest {
  state: TaskState;
  closure_reason?: ClosureReason;  // обязателен для state=done (иначе 400)
  closure_target_id?: number;
  comment?: string;
}

export interface HandoffTaskRequest {
  to_role_id: number;
  comment?: string;
}

export interface HandoffTaskResponse {
  new_task_id: number;
  closed_task_id: number;
  task: Task;
  status: 'handed_off';
}

export type SessionState = 'starting' | 'running' | 'idle' | 'stopping' | 'stopped' | 'failed';
export type RuntimeType = 'container' | 'process' | 'tmux' | 'pi';

export interface Session {
  id: number;
  team_id: number;
  team_name: string;
  role_id: number;
  role_name: string;
  runtime_type: RuntimeType;
  state: SessionState;
  queue_task_id?: number;
  queue_task_title?: string;
  started_at?: string;
  uptime_seconds?: number;
  cpu_percent?: number;
  memory_bytes?: number;
}

export interface GetSessionsResponse {
  sessions: Session[];
  total: number;
}

export type AlertEventType =
  | 'wake'
  | 'refocus'
  | 'alignment_checkpoint'
  | 'stale'
  | 'blocked'
  | 'idle'
  | 'drift';

export type AlertSeverity = 'low' | 'medium' | 'high' | 'critical';

export interface WatchdogAlert {
  id: number;
  team_id: number;
  team_name: string;
  event_type: AlertEventType;
  severity: AlertSeverity;
  description: string;
  queue_task_id?: number;
  session_id?: number;
  role_id?: number;
  action_taken?: string;
  created_at: string;
  is_read: boolean;
  requires_action: boolean;
}

export interface GetAlertsResponse {
  alerts: WatchdogAlert[];
  total: number;
}

export interface TimeSeriesPoint {
  timestamp: string;
  value: number;
}

export interface GetMetricsResponse {
  time_range: { start: string; end: string };
  metrics: {
    tasks_created: TimeSeriesPoint[];
    tasks_completed: TimeSeriesPoint[];
    sessions_active: TimeSeriesPoint[];
    queue_size: TimeSeriesPoint[];
    llm_tokens: TimeSeriesPoint[];
  };
}

export interface GetMetricsParams {
  range?: '1h' | '24h' | '7d';
}

// ---- Session lifecycle (backend slice 3) ----

export interface CreateSessionRequest {
  role_id: number;
  queue_task_id?: number;
  runtime_type?: RuntimeType;
  command?: string;
  args?: string[];
  working_dir?: string;
  config?: Record<string, unknown>;
}

export interface CreateSessionResponse {
  id: number;
  state: SessionState;
  status: 'started';
}

export interface SessionDetail {
  id: number;
  team_id: number;
  team_name: string;
  role_id: number;
  role_name: string;
  runtime_type: RuntimeType;
  runtime_ref?: string;
  state: SessionState;
  command?: string;
  exit_code?: number;
  created_at: string;
  updated_at: string;
}

export interface ListSessionsParams {
  team_id?: number;
  role_id?: number;
  state?: SessionState;
}

export interface ListSessionsResponse {
  sessions: SessionDetail[];
  total: number;
}

export interface StopSessionResponse {
  id: number;
  state: SessionState;
  status: 'stopped';
}

// ---- Message Center ----

export type MessageType = 'direct' | 'broadcast' | 'segment' | 'system' | 'watchdog';

export interface Message {
  id: number;
  team_id: number;
  queue_task_id?: number;
  from_role_id?: number;
  from_role_name?: string;
  to_role_id?: number;
  to_role_name?: string;
  type: MessageType;
  body: string;
  is_read: boolean;
  created_at: string;
  is_mine?: boolean;
  requires_reply?: boolean;
}

export interface GetMessagesParams {
  team_id?: number;
  queue_task_id?: number;
  from_role_id?: number;
  to_role_id?: number;
  type?: MessageType;
  limit?: number;
  offset?: number;
}

export interface GetMessagesResponse {
  messages: Message[];
  total: number;
  has_more: boolean;
}

export interface SendMessageRequest {
  team_id: number;
  queue_task_id?: number;
  to_role_id?: number;
  type: 'direct' | 'broadcast' | 'segment';
  body: string;
  metadata?: Record<string, unknown>;
}

export interface SendMessageResponse {
  id: number;
  status: 'sent';
  delivered_to?: number[];
}

export interface Chatroom {
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

export interface GetChatroomsResponse {
  chatrooms: Chatroom[];
}

export interface ChatroomMessage {
  id: number;
  chatroom_id: number;
  from_role_id?: number;
  from_role_name?: string;
  body: string;
  created_at: string;
  is_mine: boolean;
}

export interface GetChatroomMessagesResponse {
  messages: ChatroomMessage[];
  has_more: boolean;
}

export interface SendChatroomMessageRequest {
  body: string;
}

export interface SendChatroomMessageResponse {
  id: number;
  status: 'sent';
}

// ---- Library ----

export type LibraryItemType = 'team' | 'workflow' | 'role' | 'segment';

export interface LibraryItem {
  id: number;
  type: LibraryItemType;
  name: string;
  description?: string;
  group?: string;
  version: string;
  author?: string;
  is_public: boolean;
  downloads_count: number;
  created_at: string;
  updated_at: string;
  thumbnail?: string;
  tags?: string[];
  rating?: number;
}

export interface LibraryGroup {
  name: string;
  items_count: number;
  icon?: string;
}

export interface GetLibraryParams {
  type?: LibraryItemType;
  group?: string;
  search?: string;
  limit?: number;
  offset?: number;
}

export interface GetLibraryResponse {
  items: LibraryItem[];
  total: number;
  groups: LibraryGroup[];
}

export interface SaveToLibraryRequest {
  type: LibraryItemType;
  source_id: number;
  name: string;
  description?: string;
  group?: string;
  is_public?: boolean;
  tags?: string[];
}

export interface SaveToLibraryResponse {
  id: number;
  status: 'saved';
  library_item_id: number;
}

export interface ApplyLibraryItemRequest {
  target_team_id?: number; // если применяем к существующей команде
  overrides?: Record<string, unknown>;
}

export interface ApplyLibraryItemResponse {
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

export interface LibraryVersion {
  version: string;
  created_at: string;
  changes?: string;
  author?: string;
}

export interface GetLibraryItemResponse {
  item: LibraryItem;
  spec: Record<string, unknown>;
  versions: LibraryVersion[];
}

// ---- History ----

export interface TaskHistoryEntry {
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
  metadata?: Record<string, unknown>;
  created_at: string;
  icon?: string;
  color?: string;
}

export interface GetTaskHistoryResponse {
  history: TaskHistoryEntry[];
  total: number;
}

export interface SessionHistoryEntry {
  id: number;
  session_id: number;
  from_state?: string;
  to_state: string;
  actor_type: string;
  actor_role_name?: string;
  comment?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface GetSessionHistoryResponse {
  history: SessionHistoryEntry[];
  total: number;
}

export interface AuditEntry {
  id: number;
  timestamp: string;
  user_id?: number;
  user_name?: string;
  api_key_id?: number;
  action: string;
  resource?: string;
  details?: Record<string, unknown>;
  ip_address?: string;
  user_agent?: string;
  icon?: string;
  severity?: 'info' | 'warning' | 'error';
}

export interface GetAuditLogParams {
  user_id?: number;
  action?: string;
  resource?: string;
  start_time?: string;
  end_time?: string;
  limit?: number;
  offset?: number;
}

export interface GetAuditLogResponse {
  entries: AuditEntry[];
  total: number;
}

export type TranscriptMessageType = 'prompt' | 'response' | 'tool_call' | 'tool_result' | 'system';

export interface TranscriptEntry {
  id: number;
  session_id: number;
  message_type: TranscriptMessageType;
  content: string;
  metadata?: {
    model?: string;
    tokens?: number;
    latency_ms?: number;
    tool_name?: string;
    tool_arguments?: Record<string, unknown>;
    tool_result?: string;
    tool_error?: string;
  };
  created_at: string;
  role?: 'user' | 'assistant' | 'system';
  is_collapsed?: boolean;
}

export interface GetTranscriptResponse {
  transcript: TranscriptEntry[];
  total: number;
  has_more: boolean;
}

// ---- WebSocket ----

export interface WSClientMessage {
  type: 'subscribe';
  channels: string[];
}

export type WSServerEventType =
  | 'task.created'
  | 'task.state_changed'
  | 'session.started'
  | 'session.stopped'
  | 'message.sent'
  | 'alert.created';

export interface WSServerMessage {
  type: WSServerEventType;
  data: unknown;
  timestamp: string;
}

export interface TaskCreatedEvent {
  task_id: number;
  team_id: number;
  title: string;
  state: string;
}

export interface TaskStateChangedEvent {
  task_id: number;
  from_state: string;
  to_state: string;
  updated_at: string;
}

export interface SessionStartedEvent {
  session_id: number;
  role_name: string;
  runtime_type: string;
}

export interface MessageSentEvent {
  message_id: number;
  from_role_name?: string;
  to_role_name?: string;
  body: string;
  type: string;
}

export interface AlertCreatedEvent {
  alert_id: number;
  event_type: string;
  severity: string;
  description: string;
  requires_action: boolean;
}
