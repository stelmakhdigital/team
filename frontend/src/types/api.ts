// All types mirror docs/architecture/frontend/20_contract_API.md and
// 21_team_builder.md 1:1. Do not invent extra response shapes here.

export interface Team {
  id: number;
  name: string;
  description?: string;
  state: 'active' | 'archived' | 'stopped';
  project_id?: number; // P1.0
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
  project_id?: number; // P1.0: привязка к проекту (опционально)
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
  engine_enabled?: boolean; // P1.4: исполняемый воркфлоу
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

// ---- P1.4: Workflow engine (instances + trail) ----
export type WorkflowInstanceState = 'running' | 'waiting' | 'completed' | 'failed' | 'aborted';
export type WorkflowStepState = 'pending' | 'active' | 'done' | 'failed' | 'skipped' | 'routed';

export interface WorkflowInstance {
  id: number;
  workflow_id: number;
  state: WorkflowInstanceState;
  root_objective?: string;
  current_block_id?: number;
  created_at: string;
  updated_at: string;
  finished_at?: string;
}

export interface WorkflowStep {
  id: number;
  block_id: number;
  block_type: string;
  block_label?: string;
  task_id?: number;
  task_title?: string;
  state: WorkflowStepState;
  exit_kind?: string;
  actor_name?: string;
  result_note?: string;
  entered_at?: string;
  exited_at?: string;
}

export interface WorkflowInstanceDetail {
  instance: WorkflowInstance;
  steps: WorkflowStep[];
  active_step_ids?: number[];
}

export interface ListWorkflowInstancesResponse {
  instances: WorkflowInstance[];
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

// R6.4 (контракт 20 §2.6–2.8): удаление блоков/связей/workflow
export interface DeleteWorkflowBlockResponse {
  id: number;
  status: 'deleted';
  removed_connections: number;
}

export interface DeleteWorkflowConnectionResponse {
  id: number;
  status: 'deleted';
}

export interface DeleteWorkflowResponse {
  id: number;
  status: 'deleted';
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
  project_id?: number; // P1.0: свойство работы
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
  // P0.2: дедлайн закрытия
  is_overdue?: boolean;
  closure_required_at?: string;
  remaining_secs?: number;
}

// P0.3: wake для заблокированной задачи (block with wake).
export type WakeKind = 'timer' | 'task' | 'watchdog';
export interface TaskWake {
  kind: WakeKind; // timer | task | watchdog
  ref: string; // kind=task: task_id; kind=timer: fire-at (RFC3339); kind=watchdog: job id
  job_id?: number; // kind=watchdog: id watchdog-дjоба
  continuation?: string; // «как продолжить»
  fired: boolean; // закрыт (fired ИЛИ resolve)
  fired_at?: string;
  remaining_secs?: number; // timer only: fire_at - now (neg = fired)
}
export interface TaskWakeSpec {
  kind: WakeKind;
  after?: string; // timer: duration, e.g. "30m"
  task_id?: number; // task: referenced task_id
  job_id?: number; // watchdog: id watchdog-дjоба (ref)
  continuation?: string;
}

export interface GetTasksResponse {
  tasks: Task[];
  total: number;
}

export interface ListTasksParams {
  team_id?: number;
  state?: TaskState;
  destination_role_id?: number;
  project_id?: number; // P1.0: фильтр по проекту
  limit?: number;
  offset?: number;
}
export interface GetOverdueParams {
  limit?: number;
  offset?: number;
}

export interface CreateTaskRequest {
  team_id: number;
  project_id?: number; // P1.0: override; иначе наследуется от team.project
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
  wake?: TaskWake; // P0.3: current/most-recent wake
}

export interface UpdateTaskStateRequest {
  state: TaskState;
  closure_reason?: ClosureReason;  // обязателен для state=done (иначе 400)
  closure_target_id?: number;
  comment?: string;
  wake?: TaskWakeSpec; // P0.3: при blocked (опционально)
}
export interface ResolveTaskRequest {
  decision: string; // P0.3: обязательно — почему задача снова в работу
}
export interface ResolveTaskResponse {
  task: Task;
  status: string;
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
  project_id?: number; // P1.0
  team_name: string;
  role_id: number;
  role_name: string;
  runtime_type: RuntimeType;
  state: SessionState;
  working_dir?: string; // P1.5: handover/compact-plan
  queue_task_id?: number;
  queue_task_title?: string;
  started_at?: string;
  uptime_seconds?: number;
  cpu_percent?: number;
  memory_bytes?: number;
  // Live-метрики (slice 7, контракт 20 §3.6) — опциональные, omit = «--»
  model?: string;
  context_used_percentage?: number;
  context_total_input_tokens?: number;
  context_total_output_tokens?: number;
  log_path?: string;
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

// P0.1 — Attention view «Что требует меня» (GET /api/v1/attention).
export type AttentionKind =
  | 'task_blocked'
  | 'task_escalated'
  | 'task_overdue'
  | 'alert_unread'
  | 'workflow_attention'
  | 'session_context_wall'
  | 'session_failed';
export type AttentionSeverity = 'critical' | 'high' | 'normal';
export interface AttentionLink {
  path: string;
  label: string;
}
export interface AttentionItem {
  kind: AttentionKind;
  id: string;
  severity: AttentionSeverity;
  title: string;
  detail: string;
  created_at: string;
  suggested_action: string;
  link: AttentionLink;
}
export interface GetAttentionParams {
  limit?: number;
  offset?: number;
}
export interface GetAttentionResponse {
  total: number;
  items: AttentionItem[];
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
  project_id?: number; // P1.0: явный проект (опционально)
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
  project_id?: number; // P1.0
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
  // Live-метрики (slice 7, контракт 20 §3.6): опциональные, omit = «рантайм не знает» → UI «--»
  model?: string;
  context_used_percentage?: number;
  context_total_input_tokens?: number;
  context_total_output_tokens?: number;
  log_path?: string;
}

export interface ListSessionsParams {
  team_id?: number;
  role_id?: number;
  project_id?: number; // P1.0: фильтр по проекту
  state?: SessionState;
}

// ---- P1.5: Session handover + compact-plan ----
export interface CompactPlanEntry {
  session_id: number;
  team_id: number;
  team_name: string;
  role_id: number;
  role_name: string;
  runtime_type: string;
  state: SessionState;
  model?: string;
  working_dir?: string;
  queue_task_id?: number;
  context_used_percentage?: number;
  suggested_action: 'handover' | 'wait_for_turn';
}

export interface CompactPlanResponse {
  sessions: CompactPlanEntry[];
  total: number;
  wall_pct: number;
}

export interface HandoverRequest {
  reason: string;
  target_working_dir?: string;
  dry_run?: boolean;
}

export interface HandoverStep {
  status: 'ok' | 'failed' | 'skipped' | 'would';
  reason?: string;
  detail?: string;
}

export interface HandoverTaskRef {
  id: number;
  title: string;
  state: string;
  new_id?: number;
}

export interface HandoverContextPackage {
  context_file: string;
  transcript_path?: string;
  lines: number;
  task?: HandoverTaskRef;
}

export interface HandoverPreview {
  role_id: number;
  role_name: string;
  team_id: number;
  team_name: string;
  runtime_type: string;
  working_dir: string;
  handoff_comment?: string;
}

export interface HandoverResult {
  status: 'succeeded' | 'partial' | 'failed' | 'planned';
  dry_run: boolean;
  old_session_id: number;
  new_session_id?: number;
  context_prepared: HandoverStep;
  successor_started: HandoverStep;
  task_reassigned: HandoverStep;
  old_stopped: HandoverStep;
  context_package?: HandoverContextPackage;
  successor_preview?: HandoverPreview;
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

export interface RenameRequest {
  name: string;
  description?: string;
  engine_enabled?: boolean; // P1.4 (workflows)
}

export interface RenameResponse {
  id: number;
  name: string;
  status: 'updated';
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
  | 'session.output'
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

// session.output (slice 7, контракт 20 §4.3): live-терминал — батчи строк лога
// (batch ≤ 500ms, только при новых строках; каналы session:{id}+dashboard+team:{id})
export interface SessionOutputEvent {
  session_id: number;
  role_name?: string;
  lines: Array<{ ts: string; text: string; stream: 'stdout' | 'stderr' | 'log' }>;
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

// ---------- U4: Settings / Configuration (OpenRIG-style) ----------
export type ConfigKeyType = 'string' | 'integer' | 'duration' | 'boolean' | 'path' | 'string-list';
export type ConfigSource = 'default' | 'file' | 'env' | 'override';

export interface ConfigKeyView {
  key: string;
  section: string;
  type: ConfigKeyType;
  description: string;
  current_value: unknown;
  default_value: unknown;
  source: ConfigSource;
  source_detail?: string;
  default_is_derived: boolean;
  requires_restart: boolean;
  editable: boolean;
}

export interface ConfigSection {
  name: string;
  description: string;
  keys: ConfigKeyView[];
}

export interface ConfigMeta {
  project: string;
  instance: string;
  version: string;
  uptime_seconds: number;
  git_commit?: string;
  git_branch?: string;
  config_hash: string;
  started_at: string;
}

export interface ConfigEnvVar {
  env_var: string;
  value: string;
  feeds_key: string;
  source_active: boolean;
}

export interface ListConfigResponse {
  meta: ConfigMeta;
  sections: ConfigSection[];
  environment: ConfigEnvVar[];
}

export interface SetConfigResponse {
  key: string;
  value: unknown;
  source: string;
  requires_restart: boolean;
  needs_restart: boolean;
}

export interface ResetConfigResponse {
  key: string;
  source: string;
  value: unknown;
}

// ---- P1.0: Projects (мультипроектность) ----

export type ProjectKind = 'existing' | 'new';

export interface Project {
  id: number;
  name: string;
  path: string;
  kind: ProjectKind;
  git: boolean;
  description?: string;
  archived: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProjectResponse {
  project: Project;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}

export interface ListProjectsParams {
  limit?: number;
  offset?: number;
  name?: string;
  archived?: boolean;
  include_archived?: boolean;
}

export interface CreateProjectRequest {
  name: string;
  kind: ProjectKind;
  path?: string;        // kind=existing
  parent_path?: string; // kind=new
  description?: string;
}

export interface CreateProjectResponse {
  project: Project;
  warning?: string;
}

export interface ProjectCounts {
  teams: number;
  tasks: number;
  active_sessions: number;
}

export interface GetProjectResponse {
  project: Project;
  counts: ProjectCounts;
}

export interface UpdateProjectRequest {
  name?: string;
  description?: string;
  path?: string; // если задан → 409 (immutable)
}

// ---------- P1.6: watchdog-джобы (политики слежения + пороги) ----------

export type WatchdogJobPolicy = 'context-usage-threshold';
export type WatchdogJobEventOutcome = 'fired' | 'quiet' | 'dead-target' | 'stopped';

export interface WatchdogJob {
  id: number;
  name: string;
  policy: WatchdogJobPolicy;
  target_session_id: number;
  threshold_pct: number;
  message?: string;
  enabled: boolean;
  created_by: string;
  created_at: string;
  fired_count: number;
  last_fired_at?: string;
  last_fired_pct?: number;
}

export interface WatchdogJobEvent {
  id: number;
  job_id: number;
  outcome: WatchdogJobEventOutcome;
  detail?: string;
  pct?: number;
  created_at: string;
}

export interface WatchdogJobDetail {
  job: WatchdogJob;
  recent_evaluations: WatchdogJobEvent[];
}

export interface WatchdogJobRegisterRequest {
  name: string;
  target_session_id: number;
  threshold_pct: number;
  message?: string;
}

export interface WatchdogJobListResponse {
  jobs: WatchdogJob[];
  total: number;
}
