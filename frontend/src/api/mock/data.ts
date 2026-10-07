// In-memory mock data store. All data strictly follows src/types/api.ts,
// which mirrors docs/architecture/frontend/20_contract_API.md.
import type {
  AgentSpec,
  AuditEntry,
  Chatroom,
  ChatroomMessage,
  DashboardSummaryResponse,
  GetMetricsResponse,
  LibraryItem,
  Message,
  PluginSpec,
  Relative,
  Role,
  Session,
  Segment,
  SkillSpec,
  Task,
  TaskHistoryEntry,
  Team,
  TopologyLayout,
  WatchdogAlert,
  Workflow,
  WorkflowBlock,
  WorkflowConnection,
} from '../../types/api';
import { validateTopologyGraph } from '../../lib/topology';

interface DB {
  teams: Team[];
  segments: Segment[];
  roles: Role[];
  relatives: Relative[];
  layouts: TopologyLayout | undefined;
  workflows: Workflow[];
  blocks: WorkflowBlock[];
  connections: WorkflowConnection[];
  tasks: Task[];
  sessions: Session[];
  alerts: WatchdogAlert[];
  messages: Message[];
  chatrooms: Chatroom[];
  chatMessages: ChatroomMessage[];
  library: LibraryItem[];
  audit: AuditEntry[];
  taskHistory: TaskHistoryEntry[];
  seq: number;
}

const now = () => new Date().toISOString();
const iso = (minsAgo: number) => new Date(Date.now() - minsAgo * 60_000).toISOString();

const rolePlugins: PluginSpec[] = [
  { name: 'git', enabled: true, config: {}, description: 'Git operations' },
  { name: 'github', enabled: true, config: {}, description: 'GitHub API' },
  { name: 'terminal', enabled: true, config: {}, description: 'Shell access' },
  { name: 'filesystem', enabled: true, config: {}, description: 'Filesystem access' },
  { name: 'http', enabled: false, config: {}, description: 'HTTP requests' },
  { name: 'mcp', enabled: false, config: {}, description: 'MCP servers' },
];

const roleSkills: SkillSpec[] = [
  { path: 'code-review', enabled: true, description: 'Code review skill' },
  { path: 'tdd', enabled: true, description: 'Test-driven development' },
  { path: 'go-style', enabled: false, description: 'Go code style' },
];

export function makeAgentSpec(name: string): AgentSpec {
  return {
    name,
    version: '1.2.0',
    runtime: { type: 'pi', version: '0.9.0' },
    pi_config: {
      model: 'claude-3-7-sonnet',
      temperature: 0.7,
      max_tokens: 8192,
      plugins: rolePlugins.map((p) => ({ ...p })),
      skills: roleSkills.map((s) => ({ ...s })),
      hooks: [{ path: 'hooks/pre-commit.sh', enabled: true, description: 'Pre-commit hook' }],
      mcp: { servers: [] },
    },
    resources: { cpu: '1.0', memory: '1G', gpu: false },
    startup: {
      files: [
        { path: 'guidance/role.md', orientation: 'role', delivery_hint: 'send_text' },
        { path: 'guidance/context.md', orientation: 'context', delivery_hint: 'project_file' },
      ],
      scripts: [],
    },
  };
}

function seed(): DB {
  const t: DB = {
    teams: [],
    segments: [],
    roles: [],
    relatives: [],
    layouts: undefined,
    workflows: [],
    blocks: [],
    connections: [],
    tasks: [],
    sessions: [],
    alerts: [],
    messages: [],
    chatrooms: [],
    chatMessages: [],
    library: [],
    audit: [],
    taskHistory: [],
    seq: 1000,
  };

  t.teams = [
    {
      id: 1,
      name: 'Dev Team',
      description: 'Backend + review topology',
      state: 'active',
      segments_count: 2,
      roles_count: 4,
      created_at: iso(60 * 24 * 3),
      updated_at: iso(60 * 5),
    },
    {
      id: 2,
      name: 'Platform Team',
      description: 'Infra and docs',
      state: 'active',
      segments_count: 1,
      roles_count: 2,
      created_at: iso(60 * 24 * 2),
      updated_at: iso(60 * 48),
    },
  ];

  t.segments = [
    {
      id: 1,
      team_id: 1,
      name: 'Backend',
      description: 'Backend roles',
      config: {},
      roles_count: 3,
      created_at: iso(60 * 24 * 3),
      updated_at: iso(60 * 5),
    },
    {
      id: 2,
      team_id: 1,
      name: 'Review',
      description: 'Review roles',
      config: {},
      roles_count: 1,
      created_at: iso(60 * 24 * 3),
      updated_at: iso(60 * 5),
    },
    {
      id: 3,
      team_id: 2,
      name: 'Infra',
      description: '',
      config: {},
      roles_count: 2,
      created_at: iso(60 * 24 * 2),
      updated_at: iso(60 * 48),
    },
  ];

  const mkRole = (
    id: number,
    team_id: number,
    segment_id: number,
    segment_name: string,
    name: string,
    agent_spec: string,
    state: Role['state'] = 'active',
    session?: Role['session'],
  ): Role => ({
    id,
    team_id,
    segment_id,
    segment_name,
    name,
    address: `team${team_id}:${segment_name}.${name}`,
    agent_spec,
    state,
    session,
    created_at: iso(60 * 24 * 3),
    updated_at: iso(60 * 5),
  });

  t.roles = [
    mkRole(1, 1, 1, 'Backend', 'Lead', 'pi-go-backend', 'active', { id: 1, state: 'running', started_at: iso(180) }),
    mkRole(2, 1, 1, 'Backend', 'Worker', 'pi-go-worker', 'active', { id: 2, state: 'running', started_at: iso(120) }),
    mkRole(3, 1, 1, 'Backend', 'Worker2', 'pi-go-worker', 'inactive'),
    mkRole(4, 1, 2, 'Review', 'Reviewer', 'pi-reviewer', 'active', { id: 3, state: 'running', started_at: iso(90) }),
    mkRole(5, 2, 3, 'Infra', 'Lead', 'pi-infra-lead'),
    mkRole(6, 2, 3, 'Infra', 'Docs', 'pi-docs'),
  ];

  t.relatives = [
    {
      id: 1,
      team_id: 1,
      from_role_id: 1,
      from_role_name: 'Lead',
      to_role_id: 2,
      to_role_name: 'Worker',
      type: 'delegates_to',
      created_at: iso(60 * 24 * 3),
    },
    {
      id: 2,
      team_id: 1,
      from_role_id: 1,
      from_role_name: 'Lead',
      to_role_id: 4,
      to_role_name: 'Reviewer',
      type: 'can_observe',
      created_at: iso(60 * 24 * 3),
    },
    {
      id: 3,
      team_id: 1,
      from_role_id: 2,
      from_role_name: 'Worker',
      to_role_id: 4,
      to_role_name: 'Reviewer',
      type: 'collaborates_with',
      created_at: iso(60 * 24 * 3),
    },
  ];

  t.layouts = {
    segments: [
      { segment_id: 1, position: { x: 60, y: 60, width: 420, height: 380 }, collapsed: false },
      { segment_id: 2, position: { x: 560, y: 60, width: 320, height: 220 }, collapsed: false },
      { segment_id: 3, position: { x: 60, y: 60, width: 360, height: 220 }, collapsed: false },
    ],
    roles: [
      { role_id: 1, segment_id: 1, position: { x: 90, y: 120 } },
      { role_id: 2, segment_id: 1, position: { x: 90, y: 240 } },
      { role_id: 3, segment_id: 1, position: { x: 300, y: 240 } },
      { role_id: 4, segment_id: 2, position: { x: 600, y: 140 } },
      { role_id: 5, segment_id: 3, position: { x: 90, y: 120 } },
      { role_id: 6, segment_id: 3, position: { x: 300, y: 120 } },
    ],
    relatives: [
      { relative_id: 1, from_role_id: 1, to_role_id: 2 },
      { relative_id: 2, from_role_id: 1, to_role_id: 4 },
      { relative_id: 3, from_role_id: 2, to_role_id: 4 },
    ],
  };

  t.workflows = [
    {
      id: 1,
      team_id: 1,
      name: 'Build & Review',
      description: 'Standard dev workflow',
      state: 'active',
      created_at: iso(60 * 24 * 2),
      updated_at: iso(60 * 6),
    },
    {
      id: 2,
      team_id: 1,
      name: 'Hotfix',
      description: '',
      state: 'draft',
      created_at: iso(60 * 20),
      updated_at: iso(60 * 20),
    },
  ];

  t.blocks = [
    { id: 1, workflow_id: 1, type: 'task', position: { x: 80, y: 120 }, config: { assignee: 'Backend.Lead' }, label: 'Implement', created_at: iso(60 * 24 * 2) },
    { id: 2, workflow_id: 1, type: 'agent', position: { x: 340, y: 120 }, config: { role: 'Backend.Worker' }, label: 'Worker runs', created_at: iso(60 * 24 * 2) },
    { id: 3, workflow_id: 1, type: 'decision', position: { x: 600, y: 120 }, config: {}, label: 'Review OK?', created_at: iso(60 * 24 * 2) },
    { id: 4, workflow_id: 1, type: 'task', position: { x: 860, y: 60 }, config: {}, label: 'Deploy', created_at: iso(60 * 24 * 2) },
    { id: 5, workflow_id: 2, type: 'task', position: { x: 100, y: 100 }, config: {}, label: 'Fix', created_at: iso(60 * 20) },
  ];

  t.connections = [
    { id: 1, workflow_id: 1, from_block_id: 1, to_block_id: 2, created_at: iso(60 * 24 * 2) },
    { id: 2, workflow_id: 1, from_block_id: 2, to_block_id: 3, created_at: iso(60 * 24 * 2) },
    { id: 3, workflow_id: 1, from_block_id: 3, to_block_id: 4, condition: 'yes', created_at: iso(60 * 24 * 2) },
    { id: 4, workflow_id: 1, from_block_id: 3, to_block_id: 2, condition: 'no', created_at: iso(60 * 24 * 2) },
  ];

  const mkTask = (
    id: number,
    title: string,
    state: Task['state'],
    destRole: string,
    priority: number,
    teamName = 'Dev Team',
    extra?: Partial<Task>,
  ): Task => ({
    id,
    team_id: 1,
    team_name: teamName,
    title,
    state,
    priority,
    destination_role_id: 2,
    destination_role_name: destRole,
    source_role_id: 1,
    source_role_name: 'Lead',
    created_at: iso(60 * 26),
    updated_at: iso(10),
    started_at: state === 'done' ? iso(60 * 25) : undefined,
    ...extra,
  });

  t.tasks = [
    mkTask(1, 'Implement auth middleware', 'in_progress', 'Backend.Worker', 1, 'Dev Team', { progress: 45 }),
    mkTask(2, 'Write integration tests', 'pending', 'Backend.Worker', 2, 'Dev Team'),
    mkTask(3, 'Review PR #142', 'in_progress', 'Review.Reviewer', 3, 'Dev Team', { progress: 80 }),
    mkTask(4, 'Fix flaky CI job', 'blocked', 'Backend.Worker2', 1, 'Dev Team', { is_blocked: true, is_stale: true, updated_at: iso(60 * 3) }),
    mkTask(5, 'Update deployment docs', 'done', 'Infra.Docs', 4, 'Platform Team', { destination_role_id: 6 }),
    mkTask(6, 'Migrate config schema', 'in_progress', 'Infra.Lead', 2, 'Platform Team', { destination_role_id: 5, progress: 20 }),
  ];

  t.sessions = [
    {
      id: 1,
      team_id: 1,
      team_name: 'Dev Team',
      role_id: 1,
      role_name: 'Lead',
      runtime_type: 'pi',
      state: 'running',
      queue_task_id: 1,
      queue_task_title: 'Implement auth middleware',
      started_at: iso(180),
      uptime_seconds: 10_800,
      cpu_percent: 12.4,
      memory_bytes: 512 * 1024 * 1024,
    },
    {
      id: 2,
      team_id: 1,
      team_name: 'Dev Team',
      role_id: 2,
      role_name: 'Worker',
      runtime_type: 'pi',
      state: 'running',
      queue_task_id: 1,
      queue_task_title: 'Implement auth middleware',
      started_at: iso(120),
      uptime_seconds: 7_200,
      cpu_percent: 48.1,
      memory_bytes: 1024 * 1024 * 1024,
    },
    {
      id: 3,
      team_id: 1,
      team_name: 'Dev Team',
      role_id: 4,
      role_name: 'Reviewer',
      runtime_type: 'tmux',
      state: 'idle',
      started_at: iso(90),
      uptime_seconds: 5_400,
      cpu_percent: 1.2,
      memory_bytes: 256 * 1024 * 1024,
    },
    {
      id: 4,
      team_id: 2,
      team_name: 'Platform Team',
      role_id: 5,
      role_name: 'Lead',
      runtime_type: 'process',
      state: 'failed',
      started_at: iso(60 * 12),
      uptime_seconds: 0,
    },
  ];

  t.alerts = [
    {
      id: 1,
      team_id: 1,
      team_name: 'Dev Team',
      event_type: 'stale',
      severity: 'high',
      description: "Task 'Fix flaky CI job' not updated for 3h",
      queue_task_id: 4,
      created_at: iso(90),
      is_read: false,
      requires_action: true,
    },
    {
      id: 2,
      team_id: 1,
      team_name: 'Dev Team',
      event_type: 'blocked',
      severity: 'critical',
      description: "Task 'Fix flaky CI job' blocked by CI",
      queue_task_id: 4,
      created_at: iso(80),
      is_read: false,
      requires_action: true,
    },
    {
      id: 3,
      team_id: 1,
      team_name: 'Dev Team',
      event_type: 'idle',
      severity: 'low',
      description: 'Session Reviewer idle for 45m',
      session_id: 3,
      created_at: iso(45),
      is_read: true,
      requires_action: false,
    },
  ];

  const mkMsg = (id: number, from: string, to: string | undefined, body: string, minsAgo: number, type: Message['type'] = 'direct', mine = false): Message => ({
    id,
    team_id: 1,
    from_role_name: from,
    to_role_name: to ?? undefined,
    type,
    body,
    is_read: minsAgo > 30,
    created_at: iso(minsAgo),
    is_mine: mine,
  });

  t.messages = [
    mkMsg(1, 'Lead', 'Worker', 'Start with the auth middleware, spec is in guidance/', 130),
    mkMsg(2, 'Worker', 'Lead', 'Understood. ETA ~2h.', 120),
    mkMsg(3, 'Watchdog', undefined, 'Task #4 is stale: no updates for 2h', 90, 'watchdog'),
    mkMsg(4, 'Reviewer', 'Lead', 'PR #142 looks good except error handling', 40),
    mkMsg(5, 'Lead', 'Worker', 'Please review the branch before deploy', 25, 'direct', true),
    mkMsg(6, 'Lead', undefined, 'Standup summary: all green except CI', 200, 'broadcast', true),
  ];

  t.chatrooms = [
    {
      id: 1,
      team_id: 1,
      segment_id: 1,
      name: 'backend-general',
      topic: 'Backend channel',
      last_message: { body: 'Understood. ETA ~2h.', from_role_name: 'Worker', created_at: iso(120) },
      unread_count: 2,
      members_count: 3,
    },
    {
      id: 2,
      team_id: 1,
      name: 'dev-team',
      topic: 'Whole team',
      last_message: { body: 'Standup summary: all green except CI', from_role_name: 'Lead', created_at: iso(200) },
      unread_count: 0,
      members_count: 4,
    },
  ];

  t.chatMessages = [
    { id: 1, chatroom_id: 1, from_role_name: 'Lead', body: 'Morning all', created_at: iso(140), is_mine: true },
    { id: 2, chatroom_id: 1, from_role_name: 'Worker', body: 'On it', created_at: iso(125), is_mine: false },
    { id: 3, chatroom_id: 1, from_role_name: 'Worker', body: 'Understood. ETA ~2h.', created_at: iso(120), is_mine: false },
    { id: 4, chatroom_id: 2, from_role_name: 'Lead', body: 'Standup summary: all green except CI', created_at: iso(200), is_mine: true },
  ];

  t.library = [
    {
      id: 1,
      type: 'team',
      name: 'Standard Dev Team',
      description: 'Lead + 2 workers + reviewer',
      group: 'Teams',
      version: '1.0.0',
      author: 'lead',
      is_public: true,
      downloads_count: 12,
      created_at: iso(60 * 24 * 30),
      updated_at: iso(60 * 24 * 3),
      tags: ['backend', 'review'],
      rating: 4.5,
    },
    {
      id: 2,
      type: 'workflow',
      name: 'Build & Review',
      description: '',
      group: 'Workflows',
      version: '1.2.0',
      author: 'lead',
      is_public: false,
      downloads_count: 4,
      created_at: iso(60 * 24 * 14),
      updated_at: iso(60 * 24),
      tags: ['ci'],
    },
    {
      id: 3,
      type: 'role',
      name: 'Go Worker',
      description: 'Backend worker preset',
      group: 'Roles',
      version: '0.3.0',
      author: 'platform',
      is_public: true,
      downloads_count: 31,
      created_at: iso(60 * 24 * 60),
      updated_at: iso(60 * 24 * 10),
      tags: ['go'],
      rating: 4.2,
    },
  ];

  t.audit = [
    { id: 1, timestamp: iso(10), user_name: 'lead', action: 'task.create', resource: 'task:6', severity: 'info' },
    { id: 2, timestamp: iso(40), user_name: 'lead', action: 'team.update', resource: 'team:1', severity: 'info' },
    { id: 3, timestamp: iso(70), user_name: 'daemon', action: 'session.fail', resource: 'session:4', severity: 'error' },
    { id: 4, timestamp: iso(95), user_name: 'watchdog', action: 'alert.create', resource: 'alert:1', severity: 'warning' },
    { id: 5, timestamp: iso(130), user_name: 'lead', action: 'library.save', resource: 'library:1', severity: 'info' },
  ];

  t.taskHistory = [
    { id: 1, queue_task_id: 1, from_state: undefined, to_state: 'pending', actor_type: 'role', actor_role_name: 'Lead', created_at: iso(60 * 26), icon: 'play', color: 'blue' },
    { id: 2, queue_task_id: 1, from_state: 'pending', to_state: 'in_progress', actor_type: 'role', actor_role_name: 'Worker', created_at: iso(60 * 25), icon: 'play', color: 'blue' },
    { id: 3, queue_task_id: 4, from_state: 'pending', to_state: 'in_progress', actor_type: 'role', actor_role_name: 'Worker2', created_at: iso(60 * 22), icon: 'play', color: 'blue' },
    { id: 4, queue_task_id: 4, from_state: 'in_progress', to_state: 'blocked', actor_type: 'watchdog', comment: 'No progress for 2h', created_at: iso(60 * 3), icon: 'alert', color: 'red' },
  ];

  return t;
}

let db: DB = seed();

export function resetMockDb(): void {
  db = seed();
}

export function nextId(): number {
  db.seq += 1;
  return db.seq;
}

export function getDb(): DB {
  return db;
}

export function recalcTeamCounts(): void {
  for (const team of db.teams) {
    team.segments_count = db.segments.filter((s) => s.team_id === team.id).length;
    team.roles_count = db.roles.filter((r) => r.team_id === team.id).length;
    team.updated_at = now();
  }
}

export function computeSummary(): DashboardSummaryResponse {
  const inProgress = db.tasks.filter((t) => t.state === 'in_progress');
  const running = db.sessions.filter((s) => s.state === 'running');
  return {
    teams: { total: db.teams.length, active: db.teams.filter((t) => t.state === 'active').length },
    tasks: {
      total: db.tasks.length,
      pending: db.tasks.filter((t) => t.state === 'pending').length,
      in_progress: inProgress.length,
      blocked: db.tasks.filter((t) => t.state === 'blocked').length,
      done_today: db.tasks.filter((t) => t.state === 'done').length,
    },
    sessions: {
      total: db.sessions.length,
      running: running.length,
      failed: db.sessions.filter((s) => s.state === 'failed').length,
    },
    alerts: {
      total: db.alerts.length,
      critical: db.alerts.filter((a) => a.severity === 'critical').length,
      warning: db.alerts.filter((a) => a.severity === 'medium' || a.severity === 'high').length,
    },
    updated_at: now(),
  };
}

export function computeMetrics(): GetMetricsResponse {
  const points = (base: number, variance: number) =>
    Array.from({ length: 24 }, (_, i) => {
      const t = new Date(Date.now() - (23 - i) * 3_600_000);
      return { timestamp: t.toISOString(), value: Math.max(0, Math.round(base + Math.sin(i / 2.5) * variance + (i % 5) * 0.5)) };
    });
  return {
    time_range: {
      start: new Date(Date.now() - 23 * 3_600_000).toISOString(),
      end: now(),
    },
    metrics: {
      tasks_created: points(2, 1.5),
      tasks_completed: points(1.5, 1),
      sessions_active: points(3, 2),
      queue_size: points(4, 3),
      llm_tokens: points(1200, 400),
    },
  };
}

/** Re-export for the mock adapter: server-side validation per contract 21. */
export function validateTeamTopology(teamId: number) {
  return validateTopologyGraph(
    db.roles.filter((r) => r.team_id === teamId),
    db.segments.filter((s) => s.team_id === teamId),
    db.relatives.filter((r) => r.team_id === teamId),
  );
}
