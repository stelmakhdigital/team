import type { Api } from '../types';
import type {
  AgentSpec,
  AttentionItem,
  GetAttentionParams,
  GetAttentionResponse,
  CreateRoleRequest,
  CreateSegmentRequest,
  CreateTeamRequest,
  Message,
  Profile,
  Project,
  ProjectKind,
  RenameRequest,
  Segment,
  Session,
  SessionDetail,
  HandoverStep,
  Task,
  TaskWake,
  UpdateRoleConfigRequest,
  Workflow,
  WatchdogJob,
} from '../../types/api';
import { canTransition, isTerminalTaskState } from '../../lib/task';
import { CONFIG_REGISTRY, resolveConfig, normalizeSetValue } from './configRegistry';
import {
  computeMetrics,
  computeSummary,
  getDb,
  makeAgentSpec,
  nextId,
  recalcTeamCounts,
  validateTeamTopology,
} from './data';
import { ApiClientError } from '../errors';

// ---- error simulator (dev) ----
export type MockErrorKind = 'unauthorized' | 'forbidden' | 'not_found' | 'conflict' | 'server' | 'network';

let mockError: MockErrorKind | null = null;

export function setMockError(kind: MockErrorKind | null): void {
  mockError = kind;
}

export function getMockError(): MockErrorKind | null {
  return mockError;
}

const ERROR_STATUS: Record<MockErrorKind, number> = {
  unauthorized: 401,
  forbidden: 403,
  not_found: 404,
  conflict: 409,
  server: 500,
  network: 0,
};

function maybeFail(): void {
  if (!mockError) return;
  const status = ERROR_STATUS[mockError];
  if (status === 0) {
    throw new ApiClientError(0, 'network', 'Simulated network error (mock)');
  }
  throw new ApiClientError(status, mockError, `Simulated ${mockError} error (mock)`);
}

function latency(): Promise<void> {
  // Dev/test override: `window.__MOCK_LATENCY_MS = <ms>` makes every mock request
  // take that long (used to capture a mutation button's pending/loading state).
  const override =
    typeof window !== 'undefined' ? (window as { __MOCK_LATENCY_MS?: number }).__MOCK_LATENCY_MS : undefined;
  const ms = typeof override === 'number' ? override : 120 + Math.random() * 280;
  return new Promise((r) => setTimeout(r, ms));
}

const now = () => new Date().toISOString();

function toSessionDetail(s: Session): SessionDetail {
  const at = s.started_at ?? now();
  return {
    id: s.id,
    team_id: s.team_id,
    team_name: s.team_name,
    role_id: s.role_id,
    role_name: s.role_name,
    runtime_type: s.runtime_type,
    state: s.state,
    created_at: at,
    updated_at: at,
    // slice 7: live-метрики (мок: стабильные значения для UI; у process — omit)
    model: s.model,
    context_used_percentage: s.context_used_percentage,
    context_total_input_tokens: s.context_total_input_tokens,
    context_total_output_tokens: s.context_total_output_tokens,
    log_path: s.log_path,
  };
}
function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T;
}

// компактный elapsed для detail (мок): "5м", "2ч 10м", "3д"
function humanDur(ms: number): string {
  if (ms < 0) ms = 0;
  const m = Math.round(ms / 60000);
  if (m < 1) return 'только что';
  if (m < 60) return `${m}м`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  if (h < 24) return rm ? `${h}ч ${rm}м` : `${h}ч`;
  const d = Math.floor(h / 24);
  const rh = h % 24;
  return rh ? `${d}д ${rh}ч` : `${d}д`;
}

// P0.2: is_overdue + remaining_secs из closure_required_at (мок, зеркалит backend).
function decorateOverdue(t: Task): Task {
  if (t.closure_required_at && (t.state === 'in_progress' || t.state === 'blocked')) {
    const ms = new Date(t.closure_required_at).getTime() - Date.now();
    t.is_overdue = ms < 0;
    t.remaining_secs = Math.round(ms / 1000);
  }
  return t;
}

// ---------- P0.3: wake (мок, зеркалит backend) ----------

// parseDurMs — "30m"/"2h"/"1h30m"/"45s"/"1d" → ms (мок; Go-длительности).
function parseDurMs(s: string): number {
  const re = /(\d+)\s*(d|h|m|s|ms)/g;
  let total = 0;
  let m: RegExpExecArray | null;
  let matched = false;
  while ((m = re.exec(s)) !== null) {
    matched = true;
    const n = parseInt(m[1], 10);
    switch (m[2]) {
      case 'd': total += n * 86400000; break;
      case 'h': total += n * 3600000; break;
      case 'm': total += n * 60000; break;
      case 's': total += n * 1000; break;
      case 'ms': total += n; break;
    }
  }
  return matched ? total : 0;
}

function addMockHist(
  db: ReturnType<typeof getDb>,
  h: { queue_task_id: number; from_state?: string; to_state: string; actor_type: string; comment?: string; icon?: string; color?: string; metadata?: Record<string, unknown> },
) {
  db.taskHistory.push({
    id: nextId(),
    queue_task_id: h.queue_task_id,
    from_state: h.from_state,
    to_state: h.to_state,
    actor_type: h.actor_type as never,
    comment: h.comment,
    created_at: now(),
    icon: h.icon,
    color: h.color,
    metadata: h.metadata,
  });
}

// fireDueTimerWakes — арм. timer-wakes с наступившим сроком → blocked → pending (мок).
function fireDueTimerWakes(db: ReturnType<typeof getDb>): void {
  const nowMs = Date.now();
  for (const w of db.taskWakes) {
    if (w.fired || w.kind !== 'timer') continue;
    const fireAt = new Date(w.ref).getTime();
    if (Number.isNaN(fireAt) || fireAt >= nowMs) continue;
    const t = db.tasks.find((x) => x.id === w.task_id);
    if (!t || t.state !== 'blocked') continue;
    w.fired = true;
    w.fired_at = new Date(fireAt).toISOString();
    t.state = 'pending';
    t.updated_at = now();
    addMockHist(db, {
      queue_task_id: t.id, from_state: 'blocked', to_state: 'pending', actor_type: 'daemon',
      comment: 'wake timer fired', icon: 'play', color: 'blue',
      metadata: { type: 'wake_fired', wake_kind: 'timer' },
    });
  }
}

// fireTaskWakes — referenced-задача закрыта → её task-wakes срабатывают (мок).
function fireTaskWakes(db: ReturnType<typeof getDb>, closedId: number): void {
  for (const w of db.taskWakes) {
    if (w.fired || w.kind !== 'task' || w.ref !== String(closedId)) continue;
    const t = db.tasks.find((x) => x.id === w.task_id);
    if (!t || t.state !== 'blocked') continue;
    w.fired = true;
    w.fired_at = now();
    t.state = 'pending';
    t.updated_at = now();
    addMockHist(db, {
      queue_task_id: t.id, from_state: 'blocked', to_state: 'pending', actor_type: 'daemon',
      comment: `wake task #${closedId} closed`, icon: 'play', color: 'blue',
      metadata: { type: 'wake_fired', wake_kind: 'task' },
    });
  }
}

// latestWakeFor — самая свежая wake задачи (мок).
function latestWakeFor(db: ReturnType<typeof getDb>, taskId: number): (TaskWake & { id: number; created_at: string }) | undefined {
  const list = db.taskWakes.filter((w) => w.task_id === taskId);
  return list.length ? list.reduce((a, b) => (a.created_at >= b.created_at ? a : b)) : undefined;
}

// wakeView — TaskWake в контрактном виде (remaining_secs для timer, live).
function wakeView(w: TaskWake & { id: number; created_at: string }): TaskWake {
  const out: TaskWake = { kind: w.kind, ref: w.ref, continuation: w.continuation, fired: w.fired };
  if (w.fired_at) out.fired_at = w.fired_at;
  if (w.kind === 'timer' && !w.fired) {
    const ms = new Date(w.ref).getTime() - Date.now();
    out.remaining_secs = Math.round(ms / 1000);
  }
  return out;
}

export function createMockAdapter(): Api {
  return {
    teams: {
      async getTeams() {
        maybeFail();
        await latency();
        const db = getDb();
        return { teams: clone(db.teams), total: db.teams.length };
      },

      async createTeam(req: CreateTeamRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const id = nextId();
        db.teams.push({
          id,
          name: req.name,
          description: req.description,
          state: 'active',
          segments_count: 0,
          roles_count: 0,
          created_at: now(),
          updated_at: now(),
        });
        // spec (контракт 20/21): создать segments/roles/relatives при создании команды
        if (req.spec) {
          for (const s of req.spec.segments ?? []) {
            const sid = nextId();
            db.segments.push({
              id: sid,
              team_id: id,
              name: s.name,
              description: s.description,
              config: s.config ?? {},
              roles_count: 0,
              created_at: now(),
              updated_at: now(),
            });
            db.layouts = db.layouts ?? { segments: [], roles: [], relatives: [] };
            db.layouts.segments.push({
              segment_id: sid,
              position: { x: 60 + (db.layouts.segments.length % 3) * 480, y: 60 + Math.floor(db.layouts.segments.length / 3) * 440, width: 420, height: 260 },
              collapsed: false,
            });
          }
          for (const r of req.spec.roles ?? []) {
            const seg = r.segment ? db.segments.find((s) => s.team_id === id && s.name === r.segment) : undefined;
            if (!seg) continue;
            const rid = nextId();
            db.roles.push({
              id: rid,
              team_id: id,
              segment_id: seg.id,
              segment_name: seg.name,
              name: r.name,
              address: `team${id}:${seg.name}.${r.name}`,
              agent_spec: r.agent_spec,
              profile: r.profile,
              state: 'inactive',
              created_at: now(),
              updated_at: now(),
            });
            db.layouts = db.layouts ?? { segments: [], roles: [], relatives: [] };
            db.layouts.roles.push({ role_id: rid, segment_id: seg.id, position: { x: 80, y: 120 } });
          }
          const findRoleId = (addr: string): number | undefined => {
            const dot = addr.indexOf('.');
            if (dot <= 0) return undefined;
            const segName = addr.slice(0, dot);
            const roleName = addr.slice(dot + 1);
            const seg = db.segments.find((s) => s.team_id === id && s.name === segName);
            if (!seg) return undefined;
            return db.roles.find((r) => r.segment_id === seg.id && r.name === roleName)?.id;
          };
          for (const rel of req.spec.relatives ?? []) {
            const fromId = rel.from ? findRoleId(rel.from) : undefined;
            const toId = rel.to ? findRoleId(rel.to) : undefined;
            const f = rel.from ? rel.from.split('.').slice(1).join('.') : '';
            const t = rel.to ? rel.to.split('.').slice(1).join('.') : '';
            if (fromId === undefined || toId === undefined || fromId === toId) continue;
            db.relatives.push({
              id: nextId(),
              team_id: id,
              from_role_id: fromId,
              from_role_name: f,
              to_role_id: toId,
              to_role_name: t,
              type: rel.type,
              created_at: now(),
            });
          }
        }
        recalcTeamCounts();
        return { id, name: req.name, status: 'created' };
      },

      async getTeam(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const team = db.teams.find((t) => t.id === id);
        if (!team) throw new ApiClientError(404, 'not_found', `Team ${id} not found`);
        return {
          team: clone(team),
          segments: clone(db.segments.filter((s) => s.team_id === id)),
          roles: clone(db.roles.filter((r) => r.team_id === id)),
          relatives: clone(db.relatives.filter((r) => r.team_id === id)),
        };
      },

      async getTopology(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const team = db.teams.find((t) => t.id === id);
        if (!team) throw new ApiClientError(404, 'not_found', `Team ${id} not found`);
        return {
          team: clone(team),
          segments: clone(db.segments.filter((s) => s.team_id === id)),
          roles: clone(db.roles.filter((r) => r.team_id === id)),
          relatives: clone(db.relatives.filter((r) => r.team_id === id)),
          layout: db.layouts ? clone(db.layouts) : undefined,
        };
      },

      async createSegment(teamId, req: CreateSegmentRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const team = db.teams.find((t) => t.id === teamId);
        if (!team) throw new ApiClientError(404, 'not_found', `Team ${teamId} not found`);
        const id = nextId();
        db.segments.push({
          id,
          team_id: teamId,
          name: req.name,
          description: req.description,
          config: req.config ?? {},
          roles_count: 0,
          created_at: now(),
          updated_at: now(),
        });
        db.layouts = db.layouts ?? { segments: [], roles: [], relatives: [] };
        db.layouts.segments.push({
          segment_id: id,
          position: {
            x: req.layout?.x ?? 60 + (db.layouts.segments.length % 3) * 480,
            y: req.layout?.y ?? 60 + (db.layouts.segments.length % 3) * 440,
            width: req.layout?.width ?? 420,
            height: req.layout?.height ?? 260,
          },
          collapsed: false,
        });
        recalcTeamCounts();
        return {
          id,
          team_id: teamId,
          name: req.name,
          layout: {
            x: db.layouts.segments[db.layouts.segments.length - 1].position.x,
            y: db.layouts.segments[db.layouts.segments.length - 1].position.y,
            width: db.layouts.segments[db.layouts.segments.length - 1].position.width,
            height: db.layouts.segments[db.layouts.segments.length - 1].position.height,
          },
          status: 'created' as const,
        };
      },

      async createRole(segmentId, req: CreateRoleRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const segment = db.segments.find((s) => s.id === segmentId);
        if (!segment) throw new ApiClientError(404, 'not_found', `Segment ${segmentId} not found`);
        if (!req.name?.trim()) throw new ApiClientError(422, 'validation', "Field 'name' is required");
        if (!req.agent_spec?.trim()) throw new ApiClientError(422, 'validation', "Field 'agent_spec' is required");
        const id = nextId();
        db.roles.push({
          id,
          team_id: segment.team_id,
          segment_id: segment.id,
          segment_name: segment.name,
          name: req.name,
          address: `team${segment.team_id}:${segment.name}.${req.name}`,
          agent_spec: req.agent_spec,
          profile: req.profile,
          state: 'inactive',
          created_at: now(),
          updated_at: now(),
        });
        db.layouts = db.layouts ?? { segments: [], roles: [], relatives: [] };
        db.layouts.roles.push({
          role_id: id,
          segment_id: segment.id,
          position: { x: req.layout?.x ?? 80, y: req.layout?.y ?? 120 },
        });
        recalcTeamCounts();
        return {
          id,
          segment_id: segmentId,
          name: req.name,
          address: `team${segment.team_id}:${segment.name}.${req.name}`,
          layout: { x: req.layout?.x ?? 80, y: req.layout?.y ?? 120 },
          status: 'created' as const,
        };
      },

      async createRelative(teamId, req) {
        maybeFail();
        await latency();
        const db = getDb();
        if (req.from_role_id === req.to_role_id) {
          throw new ApiClientError(422, 'validation', 'A role cannot be connected to itself');
        }
        const from = db.roles.find((r) => r.id === req.from_role_id && r.team_id === teamId);
        const to = db.roles.find((r) => r.id === req.to_role_id && r.team_id === teamId);
        if (!from || !to) throw new ApiClientError(404, 'not_found', 'Role not found');
        const dup = db.relatives.find(
          (r) => r.team_id === teamId && r.from_role_id === from.id && r.to_role_id === to.id && r.type === req.type,
        );
        if (dup) throw new ApiClientError(409, 'conflict', 'Connection already exists');
        const id = nextId();
        db.relatives.push({
          id,
          team_id: teamId,
          from_role_id: from.id,
          from_role_name: from.name,
          to_role_id: to.id,
          to_role_name: to.name,
          type: req.type,
          config: req.config,
          created_at: now(),
        });
        db.layouts = db.layouts ?? { segments: [], roles: [], relatives: [] };
        db.layouts.relatives.push({ relative_id: id, from_role_id: from.id, to_role_id: to.id });
        return {
          id,
          from_role_name: from.name,
          to_role_name: to.name,
          type: req.type,
          status: 'created' as const,
        };
      },

      async updateSegmentLayout(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const l = db.layouts?.segments.find((s) => s.segment_id === id);
        if (!l) throw new ApiClientError(404, 'not_found', `Segment layout ${id} not found`);
        const prev = clone(l);
        l.position = {
          x: req.position.x,
          y: req.position.y,
          width: req.size?.width ?? l.position.width,
          height: req.size?.height ?? l.position.height,
        };
        if (req.collapsed !== undefined) l.collapsed = req.collapsed;
        return { id, status: 'updated', previous_layout: prev, new_layout: clone(l) };
      },

      async updateSegment(id, req: RenameRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const seg = db.segments.find((s) => s.id === id);
        if (!seg) throw new ApiClientError(404, 'not_found', `Segment ${id} not found`);
        if (!req.name || req.name.trim() === '') throw new ApiClientError(400, 'validation_failed', 'name is required');
        const dup = db.segments.find((s) => s.id !== id && s.team_id === seg.team_id && s.name === req.name);
        if (dup) throw new ApiClientError(409, 'conflict', `segment "${req.name}" already exists in team`);
        seg.name = req.name;
        if (req.description !== undefined) seg.description = req.description;
        return { id, name: seg.name, status: 'updated' };
      },

      async updateRoleLayout(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        // R6.2: upsert — при первом сохранении позиции entry ещё нет
        // (seeded layout удалён; real backend ведёт layout lazy)
        db.layouts = db.layouts ?? { segments: [], roles: [], relatives: [] };
        const role = db.roles.find((r) => r.id === id);
        if (!role) throw new ApiClientError(404, 'not_found', `Role ${id} not found`);
        let l = db.layouts.roles.find((r) => r.role_id === id);
        const prev = l ? { ...l.position } : undefined;
        if (!l) {
          l = { role_id: id, segment_id: role.segment_id, position: { x: 0, y: 0 } };
          db.layouts.roles.push(l);
        }
        l.position = { ...req.position };
        return { id, status: "updated", previous_position: prev ?? { ...req.position }, new_position: { ...req.position } };
      },

      async deleteRelative(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const rel = db.relatives.find((r) => r.id === id);
        if (!rel) throw new ApiClientError(404, 'not_found', `Relative ${id} not found`);
        db.relatives = db.relatives.filter((r) => r.id !== id);
        if (db.layouts) db.layouts.relatives = db.layouts.relatives.filter((r) => r.relative_id !== id);
        return { id, status: 'deleted', from_role_name: rel.from_role_name, to_role_name: rel.to_role_name };
      },

      async getRoleConfig(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const role = db.roles.find((r) => r.id === id);
        if (!role) throw new ApiClientError(404, 'not_found', `Role ${id} not found`);
        const agent_spec: AgentSpec = makeAgentSpec(role.agent_spec);
        const profiles: Profile[] = [
          { name: 'project-x', description: 'Project X profile' },
          { name: 'default', description: 'Default profile' },
        ];
        return {
          role: clone(role),
          agent_spec,
          available_profiles: profiles,
          available_skills: agent_spec.pi_config.skills,
          available_plugins: agent_spec.pi_config.plugins,
        };
      },

      async updateRoleConfig(id, req: UpdateRoleConfigRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const role = db.roles.find((r) => r.id === id);
        if (!role) throw new ApiClientError(404, 'not_found', `Role ${id} not found`);
        const prev = makeAgentSpec(role.agent_spec);
        if (req.agent_spec) role.agent_spec = req.agent_spec;
        if (req.profile) role.profile = req.profile;
        role.updated_at = now();
        const next = makeAgentSpec(role.agent_spec);
        if (req.pi_config) {
          if (req.pi_config.model) next.pi_config.model = req.pi_config.model;
          if (req.pi_config.temperature !== undefined) next.pi_config.temperature = req.pi_config.temperature;
          if (req.pi_config.plugins) {
            for (const p of next.pi_config.plugins) {
              const v = req.pi_config.plugins[p.name];
              if (v !== undefined) p.enabled = v;
            }
          }
          if (req.pi_config.skills) {
            const enabled = new Set(req.pi_config.skills);
            for (const s of next.pi_config.skills) s.enabled = enabled.has(s.path);
          }
        }
        if (req.resources) {
          if (req.resources.cpu) next.resources.cpu = req.resources.cpu;
          if (req.resources.memory) next.resources.memory = req.resources.memory;
        }
        return {
          id,
          status: 'updated',
          previous_config: prev,
          new_config: next,
          requires_restart: !!req.agent_spec && req.agent_spec !== role.agent_spec,
        };
      },

      async validateTopology(id, _req) {
        maybeFail();
        await latency();
        const db = getDb();
        if (!db.teams.some((t) => t.id === id)) throw new ApiClientError(404, 'not_found', `Team ${id} not found`);
        return validateTeamTopology(id);
      },

      async saveTopology(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const team = db.teams.find((t) => t.id === id);
        if (!team) throw new ApiClientError(404, 'not_found', `Team ${id} not found`);
        // U3: persist team meta (name/description) — mirrors backend SaveTopology Name/Description.
        if (req?.name) team.name = req.name;
        if (req?.description !== undefined) team.description = req.description;
        team.updated_at = now();
        const validation = validateTeamTopology(id);
        if (req?.save_to_library) {
          const libraryId = nextId();
          db.library.push({
            id: libraryId,
            type: 'team',
            name: req.name ?? team.name,
            description: req.description ?? team.description,
            group: req.library_group ?? 'Teams',
            version: '1.0.0',
            author: 'you',
            is_public: req.is_public ?? false,
            downloads_count: 0,
            created_at: now(),
            updated_at: now(),
          });
          return { team_id: id, status: 'saved', library_item_id: libraryId, validation };
        }
        return { team_id: id, status: 'saved', validation };
      },
    },

    workflows: {
      async getWorkflows(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let list: Workflow[] = db.workflows;
        if (params?.team_id) list = list.filter((w) => w.team_id === params.team_id);
        if (params?.state) list = list.filter((w) => w.state === params.state);
        return { workflows: clone(list), total: list.length };
      },

      async getWorkflow(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const wf = db.workflows.find((w) => w.id === id);
        if (!wf) throw new ApiClientError(404, 'not_found', `Workflow ${id} not found`);
        return {
          workflow: clone(wf),
          blocks: clone(db.blocks.filter((b) => b.workflow_id === id)),
          connections: clone(db.connections.filter((c) => c.workflow_id === id)),
        };
      },

      async createWorkflow(req) {
        maybeFail();
        await latency();
        const db = getDb();
        const id = nextId();
        db.workflows.push({
          id,
          team_id: req.team_id,
          name: req.name,
          description: req.description,
          state: 'draft',
          created_at: now(),
          updated_at: now(),
        });
        return { id, name: req.name, status: 'created' };
      },

      async createBlock(workflowId, req) {
        maybeFail();
        await latency();
        const db = getDb();
        if (!db.workflows.some((w) => w.id === workflowId)) throw new ApiClientError(404, 'not_found', 'Workflow not found');
        const id = nextId();
        db.blocks.push({
          id,
          workflow_id: workflowId,
          type: req.type,
          position: { ...req.position },
          config: req.config ?? {},
          label: req.label,
          created_at: now(),
        });
        return { id, workflow_id: workflowId, type: req.type, status: 'created' };
      },

      async createConnection(workflowId, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const from = db.blocks.find((b) => b.id === req.from_block_id && b.workflow_id === workflowId);
        const to = db.blocks.find((b) => b.id === req.to_block_id && b.workflow_id === workflowId);
        if (!from || !to) throw new ApiClientError(404, 'not_found', 'Block not found');
        const dup = db.connections.find(
          (c) => c.workflow_id === workflowId && c.from_block_id === from.id && c.to_block_id === to.id,
        );
        if (dup) throw new ApiClientError(409, 'conflict', 'Connection already exists');
        const id = nextId();
        db.connections.push({
          id,
          workflow_id: workflowId,
          from_block_id: from.id,
          to_block_id: to.id,
          condition: req.condition,
          created_at: now(),
        });
        return { id, status: 'created' };
      },

      async updateBlock(workflowId, blockId, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const block = db.blocks.find((b) => b.id === blockId && b.workflow_id === workflowId);
        if (!block) throw new ApiClientError(404, 'not_found', 'Block not found');
        const changes: { position?: { old: { x: number; y: number }; new: { x: number; y: number } }; config?: { old: Record<string, unknown>; new: Record<string, unknown> } } = {};
        if (req.position) {
          changes.position = { old: { ...block.position }, new: { ...req.position } };
          block.position = { ...req.position };
        }
        if (req.config) {
          changes.config = { old: block.config, new: req.config };
          block.config = req.config;
        }
        if (req.label !== undefined) block.label = req.label;
        return { id: block.id, status: 'updated', changes };
      },

      async updateWorkflow(id, req: RenameRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const wf = db.workflows.find((w) => w.id === id);
        if (!wf) throw new ApiClientError(404, 'not_found', `Workflow ${id} not found`);
        if (!req.name || req.name.trim() === '') throw new ApiClientError(400, 'validation_failed', 'name is required');
        wf.name = req.name;
        if (req.description !== undefined) wf.description = req.description;
        if (req.engine_enabled !== undefined) wf.engine_enabled = req.engine_enabled;
        return { id, name: wf.name, status: 'updated' };
      },

      // R6.4 (контракт 20 §2.6–2.8)
      async deleteBlock(workflowId, blockId) {
        maybeFail();
        await latency();
        const db = getDb();
        const block = db.blocks.find((b) => b.id === blockId && b.workflow_id === workflowId);
        if (!block) throw new ApiClientError(404, 'not_found', 'Block not found');
        const removed = db.connections.filter(
          (c) => c.workflow_id === workflowId && (c.from_block_id === blockId || c.to_block_id === blockId),
        );
        db.connections = db.connections.filter(
          (c) => !(c.workflow_id === workflowId && (c.from_block_id === blockId || c.to_block_id === blockId)),
        );
        db.blocks = db.blocks.filter((b) => b.id !== blockId);
        return { id: blockId, status: 'deleted', removed_connections: removed.length };
      },

      async deleteConnection(workflowId, connectionId) {
        maybeFail();
        await latency();
        const db = getDb();
        const conn = db.connections.find((c) => c.id === connectionId && c.workflow_id === workflowId);
        if (!conn) throw new ApiClientError(404, 'not_found', 'Connection not found');
        db.connections = db.connections.filter((c) => c.id !== connectionId);
        return { id: connectionId, status: 'deleted' };
      },

      async deleteWorkflow(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const wf = db.workflows.find((w) => w.id === id);
        if (!wf) throw new ApiClientError(404, 'not_found', 'Workflow not found');
        db.blocks = db.blocks.filter((b) => b.workflow_id !== id);
        db.connections = db.connections.filter((c) => c.workflow_id !== id);
        db.workflows = db.workflows.filter((w) => w.id !== id);
        return { id, status: 'deleted' };
      },
      // P1.4: engine (mock simulation)
      async runWorkflow(id, req) {
        maybeFail(); await latency();
        const db = getDb();
        const wf = db.workflows.find((w) => w.id === id);
        if (!wf) throw new ApiClientError(404, 'not_found', 'Workflow not found');
        if (!wf.engine_enabled) throw new ApiClientError(409, 'engine_disabled', 'Engine не включён для workflow');
        const firstBlock = db.blocks.find((b) => b.workflow_id === id);
        const inst = {
          id: Math.max(0, ...db.workflow_instances.map((i) => i.id)) + 1,
          workflow_id: id, state: 'running' as const,
          root_objective: req?.root_objective ?? '',
          current_block_id: firstBlock?.id,
          created_at: now(), updated_at: now(),
        };
        db.workflow_instances = [inst, ...db.workflow_instances];
        const stepId = Math.max(0, ...db.workflow_steps.map((s) => s.id)) + 1;
        if (firstBlock) {
          db.workflow_steps = [...db.workflow_steps, { id: stepId, block_id: firstBlock.id, block_type: firstBlock.type, block_label: firstBlock.label ?? '', state: 'active', entered_at: now() }];
        }
        return { instance: clone(inst) };
      },
      async listWorkflowInstances(id) {
        maybeFail(); await latency();
        const db = getDb();
        const list = db.workflow_instances.filter((i) => i.workflow_id === id);
        return { instances: clone(list), total: list.length };
      },
      async getWorkflowInstance(iid) {
        maybeFail(); await latency();
        const db = getDb();
        const inst = db.workflow_instances.find((i) => i.id === iid);
        if (!inst) throw new ApiClientError(404, 'not_found', 'Instance not found');
        const steps = clone(db.workflow_steps);
        return { instance: clone(inst), steps, active_step_ids: db.workflow_steps.filter((s) => s.state === 'active').map((s) => s.id) };
      },
      async getWorkflowInstanceTrace(iid) {
        maybeFail(); await latency();
        const db = getDb();
        const inst = db.workflow_instances.find((i) => i.id === iid);
        if (!inst) throw new ApiClientError(404, 'not_found', 'Instance not found');
        const steps = clone(db.workflow_steps);
        return { steps, total: steps.length };
      },
      async resumeWorkflowInstance(iid) {
        maybeFail(); await latency();
        const db = getDb();
        const inst = db.workflow_instances.find((i) => i.id === iid);
        if (!inst) throw new ApiClientError(404, 'not_found', 'Instance not found');
        if (inst.state !== 'failed') throw new ApiClientError(409, 'instance_not_failed', 'Instance не в failed');
        inst.state = 'running'; inst.updated_at = now();
        return { instance: clone(inst) };
      },
      async routeWorkflowInstance(iid, toRoleId) {
        maybeFail(); await latency();
        const db = getDb();
        const inst = db.workflow_instances.find((i) => i.id === iid);
        if (!inst) throw new ApiClientError(404, 'not_found', 'Instance not found');
        if (inst.state !== 'running') throw new ApiClientError(409, 'instance_not_running', 'Instance не в running');
        void toRoleId;
        inst.updated_at = now();
        return { instance: clone(inst) };
      },
      async abortWorkflowInstance(iid) {
        maybeFail(); await latency();
        const db = getDb();
        const inst = db.workflow_instances.find((i) => i.id === iid);
        if (!inst) throw new ApiClientError(404, 'not_found', 'Instance not found');
        if (inst.state === 'completed' || inst.state === 'aborted') throw new ApiClientError(409, 'instance_terminal', 'Instance уже завершён');
        inst.state = 'aborted'; inst.updated_at = now(); inst.finished_at = now();
        db.workflow_steps = db.workflow_steps.map((s) => s.state === 'active' ? { ...s, state: 'skipped', exit_kind: 'done', exited_at: now() } : s);
        return { instance: clone(inst) };
      },
    },

    dashboard: {
      async getSummary() {
        maybeFail();
        await latency();
        return computeSummary();
      },
      async getTasks() {
        maybeFail();
        await latency();
        const db = getDb();
        return { tasks: clone(db.tasks), total: db.tasks.length };
      },
      async getSessions() {
        maybeFail();
        await latency();
        const db = getDb();
        return { sessions: clone(db.sessions), total: db.sessions.length };
      },
      async getAlerts() {
        maybeFail();
        await latency();
        const db = getDb();
        return { alerts: clone(db.alerts), total: db.alerts.length };
      },
      async getMetrics() {
        maybeFail();
        await latency();
        return computeMetrics();
      },
    },

    attention: {
      async list(params?: GetAttentionParams): Promise<GetAttentionResponse> {
        maybeFail();
        await latency();
        const db = getDb();
        const now = Date.now();
        const items: AttentionItem[] = [];
        const escalated = new Set<number>();
        const isTerminal = (s: string) => s === 'done' || s === 'canceled';
        // 1. escalated (active + closure_reason=escalation) -> critical
        for (const t of db.tasks) {
          if (t.closure_reason === 'escalation' && !isTerminal(t.state)) {
            escalated.add(t.id);
            const since = new Date(t.updated_at).getTime();
            items.push({
              kind: 'task_escalated', id: String(t.id), severity: 'critical',
              title: t.title,
              detail: `Команда ${t.team_name} · ждёт ${humanDur(now - since)}`,
              created_at: t.updated_at,
              suggested_action: 'Разобрать эскалацию',
              link: { path: `/tasks/${t.id}`, label: t.title },
            });
          }
        }
        // 2. blocked (skip already-escalated) -> high
        for (const t of db.tasks) {
          if (t.state === 'blocked' && !escalated.has(t.id)) {
            const since = new Date(t.updated_at).getTime();
            items.push({
              kind: 'task_blocked', id: String(t.id), severity: 'high',
              title: t.title,
              detail: `Команда ${t.team_name} · ждёт ${humanDur(now - since)}`,
              created_at: t.updated_at,
              suggested_action: 'Разблокировать',
              link: { path: `/tasks/${t.id}`, label: t.title },
            });
          }
        }
        // 1.3. overdue (in_progress/blocked, deadline passed; skip escalated + already-blocked) -> high
        for (const t of db.tasks) {
          const isBlocked = t.state === 'blocked';
          if (
            (t.state === 'in_progress' || t.state === 'blocked') &&
            t.closure_required_at &&
            new Date(t.closure_required_at).getTime() < now &&
            !escalated.has(t.id) &&
            !(isBlocked && !escalated.has(t.id))
          ) {
            const overMs = now - new Date(t.closure_required_at).getTime();
            items.push({
              kind: 'task_overdue', id: String(t.id), severity: 'high',
              title: t.title,
              detail: `Команда ${t.team_name} · просрочено на ${humanDur(overMs)}`,
              created_at: t.closure_required_at,
              suggested_action: 'Открыть задачу',
              link: { path: `/tasks/${t.id}`, label: t.title },
            });
          }
        }
        // 3. unread alerts -> severity by event type
        for (const a of db.alerts) {
          if (a.is_read) continue;
          const severity = a.event_type === 'blocked' ? 'critical' : a.event_type === 'stale' ? 'high' : 'normal';
          const action =
            a.event_type === 'blocked' ? 'Разобрать блокировку' :
            a.event_type === 'stale' ? 'Проверить сессию' : 'Посмотреть';
          const since = new Date(a.created_at).getTime();
          const link =
            a.session_id != null ? { path: `/sessions/${a.session_id}`, label: `Сессия ${a.session_id}` }
            : a.queue_task_id != null ? { path: `/tasks/${a.queue_task_id}`, label: `Задача ${a.queue_task_id}` }
            : { path: '/', label: 'Дашборд' };
          items.push({
            kind: 'alert_unread', id: String(a.id), severity,
            title: `Watchdog: ${a.event_type}`,
            detail: `${a.team_name} · ${a.description} · ${humanDur(now - since)}`,
            created_at: a.created_at,
            suggested_action: action,
            link,
          });
        }
        const rank: Record<string, number> = { critical: 0, high: 1, normal: 2 };
        items.sort((x, y) => {
          if (rank[x.severity] !== rank[y.severity]) return rank[x.severity] - rank[y.severity];
          return new Date(x.created_at).getTime() - new Date(y.created_at).getTime();
        });
        const total = items.length;
        const offset = params?.offset ?? 0;
        const limit = params?.limit ?? 50;
        const page = items.slice(offset, offset + limit);
        return { total, items: clone(page) };
      },
    },

    sessions: {
      async list(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let list = db.sessions;
        if (params?.team_id != null) list = list.filter((s) => s.team_id === params.team_id);
        if (params?.role_id != null) list = list.filter((s) => s.role_id === params.role_id);
        if (params?.state) list = list.filter((s) => s.state === params.state);
        return { sessions: clone(list.map(toSessionDetail)), total: list.length };
      },

      async create(teamId, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const team = db.teams.find((t) => t.id === teamId);
        if (!team) throw new ApiClientError(404, 'not_found', `Team ${teamId} not found`);
        const role = db.roles.find((r) => r.id === req.role_id && r.team_id === teamId);
        if (!role) throw new ApiClientError(404, 'not_found', `Role ${req.role_id} not found in team ${teamId}`);
        const id = nextId();
        const at = now();
        const task = req.queue_task_id != null ? db.tasks.find((t) => t.id === req.queue_task_id) : undefined;
        db.sessions.unshift({
          id,
          team_id: teamId,
          team_name: team.name,
          role_id: role.id,
          role_name: role.name,
          runtime_type: 'pi',
          state: 'running',
          queue_task_id: task?.id,
          queue_task_title: task?.title,
          started_at: at,
          // slice 7: live-метрики для мок-сессии (pi-рантайм отдаёт context/tokens/model)
          model: 'anthropic/claude-sonnet',
          context_used_percentage: 42,
          context_total_input_tokens: 84_000,
          context_total_output_tokens: 1_200,
          log_path: `mock/logs/session-${id}.log`,
        });
        db.audit.unshift({ id: nextId(), timestamp: at, action: 'session.create', resource: `session:${id}` });
        return { id, state: 'running', status: 'started' };
      },

      async get(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const s = db.sessions.find((x) => x.id === id);
        if (!s) throw new ApiClientError(404, 'not_found', `Session ${id} not found`);
        return clone(toSessionDetail(s));
      },

      async stop(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const s = db.sessions.find((x) => x.id === id);
        if (!s) throw new ApiClientError(404, 'not_found', `Session ${id} not found`);
        if (s.state !== 'running' && s.state !== 'starting' && s.state !== 'idle' && s.state !== 'stopped') {
          throw new ApiClientError(409, 'conflict', `Session ${id} is ${s.state}, cannot stop`);
        }
        s.state = 'stopped';
        db.audit.unshift({ id: nextId(), timestamp: now(), action: 'session.stop', resource: `session:${id}` });
        return { id, state: 'stopped', status: 'stopped' };
      },

      // P1.5: compact-plan — живые сессии с context_used_percentage >= 80 (desc)
      async getCompactPlan() {
        maybeFail();
        await latency();
        const db = getDb();
        const wall = 80;
        const active = db.sessions.filter((s) => ['running', 'starting', 'idle', 'stopping'].includes(s.state));
        const sessions = active
          .filter((s) => typeof s.context_used_percentage === 'number' && s.context_used_percentage >= wall)
          .map((s) => ({
            session_id: s.id, team_id: s.team_id, team_name: s.team_name,
            role_id: s.role_id, role_name: s.role_name, runtime_type: s.runtime_type,
            state: s.state, model: s.model, working_dir: s.working_dir,
            queue_task_id: s.queue_task_id,
            context_used_percentage: s.context_used_percentage,
            suggested_action: (s.state === 'running' ? 'wait_for_turn' : 'handover') as 'handover' | 'wait_for_turn',
          }))
          .sort((a, b) => (b.context_used_percentage ?? 0) - (a.context_used_percentage ?? 0));
        return { sessions: clone(sessions), total: sessions.length, wall_pct: wall };
      },

      // P1.5: handover (dry-run: план; full: per-step отчёт + мутации)
      async handover(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const s = db.sessions.find((x) => x.id === id);
        if (!s) throw new ApiClientError(404, 'not_found', `Session ${id} not found`);
        if (!req.reason || req.reason.trim() === '') throw new ApiClientError(400, 'validation_failed', 'reason is required');
        if (!['running', 'starting', 'idle', 'stopping'].includes(s.state)) {
          throw new ApiClientError(409, 'conflict', `Session ${id} is ${s.state}, cannot handover`);
        }
        const comment = `handover from session ${id} — ${req.reason.trim()}`;
        const task = s.queue_task_id != null ? db.tasks.find((t) => t.id === s.queue_task_id) : undefined;
        if (req.dry_run) {
          return {
            status: 'planned' as const, dry_run: true, old_session_id: id,
            context_prepared: { status: 'would' as const, detail: `${s.context_used_percentage ?? 0}% контекста` },
            successor_started: { status: 'would' as const, detail: `role=${s.role_name} team=${s.team_name} runtime=${s.runtime_type}` },
            task_reassigned: task ? { status: 'would' as const, detail: `task ${task.id}` } : { status: 'would' as const, reason: 'no task attached' },
            old_stopped: { status: 'would' as const },
            context_package: {
              context_file: `mock/handovers/handover-${id}.md`, transcript_path: s.log_path, lines: 200,
              task: task ? { id: task.id, title: task.title, state: task.state } : undefined,
            },
            successor_preview: {
              role_id: s.role_id, role_name: s.role_name, team_id: s.team_id, team_name: s.team_name,
              runtime_type: s.runtime_type, working_dir: s.working_dir ?? '', handoff_comment: comment,
            },
          };
        }
        // полный прогон: stop → task-handoff → successor
        s.state = 'stopped';
        db.audit.unshift({ id: nextId(), timestamp: now(), action: 'session.handover', resource: `session:${id}` });
        // test-only backdoor: reason «[fail]» → failed, «[partial]» → partial
        if (comment.includes('[fail]')) {
          s.state = 'stopped';
          return {
            status: 'failed' as const, dry_run: false, old_session_id: id,
            context_prepared: { status: 'ok' as const, detail: `mock/handovers/handover-${id}.md` },
            successor_started: { status: 'failed' as const, reason: 'runtime error (mock backdoor)' },
            task_reassigned: { status: 'skipped' as const, reason: 'successor не создан' },
            old_stopped: { status: 'ok' as const },
          };
        }
        let newTaskId: number | undefined;
        let taskStep: HandoverStep = { status: 'skipped', reason: 'no task attached' };
        if (task && task.state !== 'done' && task.state !== 'canceled') {
          const nid = nextId();
          db.tasks.unshift({ ...task, id: nid, state: 'pending' } as typeof task);
          task.state = 'done';
          newTaskId = nid;
          taskStep = { status: 'ok', detail: `task ${task.id} → ${nid}` };
        } else if (task) {
          taskStep = { status: 'skipped', reason: `task ${task.id} is ${task.state}` };
        }
        const nid = nextId();
        db.sessions.unshift({ ...s, id: nid, state: 'running', queue_task_id: newTaskId ?? s.queue_task_id, started_at: now() });
        const partial = comment.includes('[partial]');
        return {
          status: (partial ? 'partial' : 'succeeded') as 'succeeded' | 'partial', dry_run: false, old_session_id: id, new_session_id: nid,
          context_prepared: { status: 'ok' as const, detail: `mock/handovers/handover-${id}.md` },
          successor_started: { status: 'ok' as const, detail: `session ${nid} started` },
          task_reassigned: taskStep,
          old_stopped: partial ? { status: 'failed' as const, reason: 'процесс не ответил (mock backdoor)' } : { status: 'ok' as const },
          context_package: {
            context_file: `mock/handovers/handover-${id}.md`, transcript_path: s.log_path, lines: 200,
            task: task ? { id: task.id, title: task.title, state: 'in_progress', new_id: newTaskId } : undefined,
          },
        };
      },
    },

    tasks: {
      async list(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let list = db.tasks;
        if (params?.team_id != null) list = list.filter((t) => t.team_id === params.team_id);
        if (params?.state) list = list.filter((t) => t.state === params.state);
        if (params?.destination_role_id != null) list = list.filter((t) => t.destination_role_id === params.destination_role_id);
        const limit = params?.limit ?? 100;
        const offset = params?.offset ?? 0;
        const page = list.slice(offset, offset + limit);
        return { tasks: clone(page).map(decorateOverdue), total: list.length };
      },

      async overdue(params) {
        maybeFail();
        await latency();
        const db = getDb();
        const nowMs = Date.now();
        const list = db.tasks
          .filter(
            (t) =>
              (t.state === 'in_progress' || t.state === 'blocked') &&
              t.closure_required_at &&
              new Date(t.closure_required_at).getTime() < nowMs,
          )
          .sort((a, b) => new Date(a.closure_required_at!).getTime() - new Date(b.closure_required_at!).getTime());
        const limit = params?.limit ?? 50;
        const offset = params?.offset ?? 0;
        const page = list.slice(offset, offset + limit).map(decorateOverdue);
        return { tasks: clone(page), total: list.length };
      },

      async create(req) {
        maybeFail();
        await latency();
        const db = getDb();
        if (!req.title?.trim()) throw new ApiClientError(400, 'validation_failed', "Field 'title' is required");
        const team = db.teams.find((t) => t.id === req.team_id);
        if (!team) throw new ApiClientError(404, 'not_found', `Team ${req.team_id} not found`);
        const role = db.roles.find((r) => r.id === req.destination_role_id && r.team_id === team.id);
        if (!role) throw new ApiClientError(404, 'not_found', `Role ${req.destination_role_id} not found in team ${team.id}`);
        const id = nextId();
        const at = now();
        db.tasks.unshift({
          id,
          team_id: team.id,
          team_name: team.name,
          parent_task_id: req.parent_task_id,
          title: req.title.trim(),
          body: req.body,
          body_context: req.body_context,
          state: 'pending',
          priority: req.priority ?? 3,
          destination_role_id: role.id,
          destination_role_name: role.name,
          source_role_id: req.source_role_id,
          created_at: at,
          updated_at: at,
        });
        db.audit.unshift({ id: nextId(), timestamp: at, action: 'task.create', resource: `task:${id}` });
        return { id, state: 'pending', status: 'created' };
      },

      async get(id) {
        maybeFail();
        await latency();
        const db = getDb();
        fireDueTimerWakes(db);
        const t = db.tasks.find((x) => x.id === id);
        if (!t) throw new ApiClientError(404, 'not_found', `Task ${id} not found`);
        const wake = latestWakeFor(db, id);
        return {
          task: decorateOverdue(clone(t)),
          subtasks: clone(db.tasks.filter((x) => x.parent_task_id === id)).map(decorateOverdue),
          wake: wake ? clone(wakeView(wake)) : undefined,
        };
      },

      async updateState(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const t = db.tasks.find((x) => x.id === id);
        if (!t) throw new ApiClientError(404, 'not_found', `Task ${id} not found`);
        if (!canTransition(t.state, req.state)) {
          throw new ApiClientError(409, 'conflict', `Cannot transition task ${id} from ${t.state} to ${req.state}`);
        }
        if (req.state === 'done' && !req.closure_reason) {
          throw new ApiClientError(400, 'validation_failed', 'closure_reason is required for state=done');
        }
        t.state = req.state;
        t.updated_at = now();
        if (req.state === 'in_progress' && !t.started_at) t.started_at = now();
        // P0.2: дедлайн закрытия в in_progress (мок: now + 2h, зеркалит backend)
        if (req.state === 'in_progress') t.closure_required_at = new Date(Date.now() + 2 * 3600_000).toISOString();
        if (isTerminalTaskState(req.state)) {
          t.closure_reason = req.closure_reason;
          t.closure_target_id = req.closure_target_id;
        }
        // P0.3: wake при блокировке (мок, зеркалит backend)
        if (req.state === 'blocked' && req.wake) {
          db.taskWakes.forEach((w) => {
            if (w.task_id === id && !w.fired) {
              w.fired = true;
              w.fired_at = now();
            }
          });
          const ref =
            req.wake.kind === 'timer'
              ? new Date(Date.now() + parseDurMs(req.wake.after || '')).toISOString()
              : String(req.wake.task_id ?? '');
          db.taskWakes.push({
            id: nextId(), task_id: id, kind: req.wake.kind, ref,
            continuation: req.wake.continuation ?? '', fired: false, created_at: now(),
          });
        }
        // P0.3: referenced-задача закрыта → её task-wakes срабатывают
        if (isTerminalTaskState(req.state)) fireTaskWakes(db, id);
        db.audit.unshift({ id: nextId(), timestamp: now(), action: `task.${req.state}`, resource: `task:${id}` });
        return clone(t);
      },

      async handoff(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const t = db.tasks.find((x) => x.id === id);
        if (!t) throw new ApiClientError(404, 'not_found', `Task ${id} not found`);
        if (isTerminalTaskState(t.state)) throw new ApiClientError(409, 'conflict', `Task ${id} is ${t.state}, cannot hand off`);
        const role = db.roles.find((r) => r.id === req.to_role_id && r.team_id === t.team_id);
        if (!role) throw new ApiClientError(404, 'not_found', `Role ${req.to_role_id} not found in team ${t.team_id}`);
        const newId = nextId();
        const at = now();
        t.state = 'done';
        t.closure_reason = 'handed_off_to';
        t.closure_target_id = newId;
        t.updated_at = at;
        fireTaskWakes(db, id); // P0.3: handoff закрывает задачу → её task-wakes
        const nt: Task = {
          ...clone(t),
          id: newId,
          state: 'pending',
          destination_role_id: role.id,
          destination_role_name: role.name,
          source_role_id: t.destination_role_id,
          started_at: undefined,
          closure_reason: undefined,
          closure_target_id: undefined,
          created_at: at,
          updated_at: at,
        };
        db.tasks.unshift(nt);
        db.audit.unshift({ id: nextId(), timestamp: at, action: 'task.handoff', resource: `task:${id}` });
        return { new_task_id: newId, closed_task_id: id, task: clone(nt), status: 'handed_off' };
      },

      async resolve(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const t = db.tasks.find((x) => x.id === id);
        if (!t) throw new ApiClientError(404, 'not_found', `Task ${id} not found`);
        if (!req?.decision?.trim()) throw new ApiClientError(400, 'validation_failed', 'decision is required to resolve a blocked task');
        if (t.state !== 'blocked') throw new ApiClientError(409, 'conflict', `Task ${id} is ${t.state}, only blocked tasks can be resolved`);
        const at = now();
        // закрыть арм. wake (идемпотентность: не сработает повторно)
        db.taskWakes.forEach((w) => {
          if (w.task_id === id && !w.fired) {
            w.fired = true;
            w.fired_at = at;
          }
        });
        t.state = 'pending';
        t.updated_at = at;
        addMockHist(db, {
          queue_task_id: id, from_state: 'blocked', to_state: 'pending', actor_type: 'human',
          comment: `resolve: ${req.decision}`, icon: 'check', color: 'green',
          metadata: { type: 'resolve', decision: req.decision },
        });
        db.audit.unshift({ id: nextId(), timestamp: at, action: 'task.resolve', resource: `task:${id}` });
        return { task: clone(t), status: 'resolved' };
      },
    },

    messages: {
      async getMessages(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let list: Message[] = db.messages;
        if (params?.team_id) list = list.filter((m) => m.team_id === params.team_id);
        if (params?.type) list = list.filter((m) => m.type === params.type);
        if (params?.from_role_id) list = list.filter((m) => m.from_role_id === params.from_role_id);
        const limit = params?.limit ?? 50;
        const offset = params?.offset ?? 0;
        const page = list.slice(offset, offset + limit);
        return { messages: clone(page), total: list.length, has_more: offset + page.length < list.length };
      },

      async sendMessage(req) {
        maybeFail();
        await latency();
        if (!req.body?.trim()) throw new ApiClientError(422, 'validation', "Field 'body' is required");
        const db = getDb();
        const id = nextId();
        db.messages.unshift({
          id,
          team_id: req.team_id,
          queue_task_id: req.queue_task_id,
          to_role_id: req.to_role_id,
          to_role_name: db.roles.find((r) => r.id === req.to_role_id)?.name,
          from_role_id: 999,
          from_role_name: 'You',
          type: req.type,
          body: req.body,
          is_read: true,
          created_at: now(),
          is_mine: true,
        });
        return { id, status: 'sent', delivered_to: req.to_role_id ? [req.to_role_id] : db.roles.map((r) => r.id).slice(0, 4) };
      },

      async getChatrooms() {
        maybeFail();
        await latency();
        const db = getDb();
        return { chatrooms: clone(db.chatrooms) };
      },

      async getChatroomMessages(chatroomId) {
        maybeFail();
        await latency();
        const db = getDb();
        if (!db.chatrooms.some((c) => c.id === chatroomId)) throw new ApiClientError(404, 'not_found', 'Chatroom not found');
        const msgs = db.chatMessages.filter((m) => m.chatroom_id === chatroomId);
        return { messages: clone(msgs), has_more: false };
      },

      async sendChatroomMessage(chatroomId, req) {
        maybeFail();
        await latency();
        if (!req.body?.trim()) throw new ApiClientError(422, 'validation', "Field 'body' is required");
        const db = getDb();
        const room = db.chatrooms.find((c) => c.id === chatroomId);
        if (!room) throw new ApiClientError(404, 'not_found', 'Chatroom not found');
        const id = nextId();
        db.chatMessages.push({
          id,
          chatroom_id: chatroomId,
          from_role_name: 'You',
          body: req.body,
          created_at: now(),
          is_mine: true,
        });
        room.last_message = { body: req.body, from_role_name: 'You', created_at: now() };
        return { id, status: 'sent' };
      },
    },

    library: {
      async getLibrary(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let list = db.library;
        if (params?.type) list = list.filter((i) => i.type === params.type);
        if (params?.group) list = list.filter((i) => i.group === params.group);
        if (params?.search) {
          const q = params.search.toLowerCase();
          list = list.filter((i) => i.name.toLowerCase().includes(q) || (i.description ?? '').toLowerCase().includes(q));
        }
        const groups = new Map<string, number>();
        for (const i of db.library) {
          const g = i.group ?? 'Ungrouped';
          groups.set(g, (groups.get(g) ?? 0) + 1);
        }
        const limit = params?.limit ?? 50;
        const offset = params?.offset ?? 0;
        return {
          items: clone(list.slice(offset, offset + limit)),
          total: list.length,
          groups: [...groups.entries()].map(([name, items_count]) => ({ name, items_count })),
        };
      },

      async getLibraryItem(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const item = db.library.find((i) => i.id === id);
        if (!item) throw new ApiClientError(404, 'not_found', `Library item ${id} not found`);
        return {
          item: clone(item),
          spec: { name: item.name, type: item.type, version: item.version },
          versions: [
            { version: item.version, created_at: item.updated_at, author: item.author, changes: 'latest' },
            { version: '0.1.0', created_at: item.created_at, author: item.author, changes: 'initial' },
          ],
        };
      },

      async saveToLibrary(req) {
        maybeFail();
        await latency();
        const db = getDb();
        const id = nextId();
        db.library.unshift({
          id,
          type: req.type,
          name: req.name,
          description: req.description,
          group: req.group,
          version: '1.0.0',
          author: 'you',
          is_public: req.is_public ?? false,
          downloads_count: 0,
          created_at: now(),
          updated_at: now(),
          tags: req.tags,
        });
        return { id, status: 'saved', library_item_id: id };
      },

      async updateLibraryItem(id, req: RenameRequest) {
        maybeFail();
        await latency();
        const db = getDb();
        const item = db.library.find((it) => it.id === id);
        if (!item) throw new ApiClientError(404, 'not_found', `Library item ${id} not found`);
        if (!req.name || req.name.trim() === '') throw new ApiClientError(400, 'validation_failed', 'name is required');
        item.name = req.name;
        if (req.description !== undefined) item.description = req.description;
        return { id, name: item.name, status: 'updated' };
      },

      async applyLibrary(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const item = db.library.find((i) => i.id === id);
        if (!item) throw new ApiClientError(404, 'not_found', `Library item ${id} not found`);
        item.downloads_count += 1;
        if (item.type === 'workflow') {
          if (!req?.target_team_id) {
            throw new ApiClientError(400, 'validation_failed', 'target_team_id is required for workflow apply');
          }
          if (!db.teams.some((t) => t.id === req.target_team_id)) {
            throw new ApiClientError(404, 'not_found', `Team ${req.target_team_id} not found`);
          }
          return { status: 'applied', updated_resources: { teams: [req.target_team_id] } };
        }
        if (item.type === 'segment') {
          // merge в команду (target обязателен); идемпотентно по имени
          if (!req?.target_team_id) {
            throw new ApiClientError(400, 'validation_failed', 'target_team_id is required for segment apply');
          }
          const team = db.teams.find((t) => t.id === req.target_team_id);
          if (!team) throw new ApiClientError(404, 'not_found', `Team ${req.target_team_id} not found`);
          const existing = db.segments.find((s) => s.team_id === team.id && s.name === item.name);
          if (existing) return { status: 'merged', created_resources: { segments: [], roles: [] } };
          const segId = nextId();
          db.segments.push({
            id: segId,
            team_id: team.id,
            name: item.name,
            config: {},
            roles_count: 0,
            created_at: now(),
            updated_at: now(),
          });
          recalcTeamCounts();
          return { status: 'merged', created_resources: { segments: [segId], roles: [] } };
        }
        if (item.type === 'role') {
          // apply в команду (target обязателен); сегмент: overrides.segment_id →
          // overrides.segment (создаётся, если нет) → первый сегмент → создаётся 'general'
          if (!req?.target_team_id) {
            throw new ApiClientError(400, 'validation_failed', 'target_team_id is required for role apply');
          }
          const team = db.teams.find((t) => t.id === req.target_team_id);
          if (!team) throw new ApiClientError(404, 'not_found', `Team ${req.target_team_id} not found`);
          const teamSegments = db.segments.filter((s) => s.team_id === team.id);
          let segment: Segment | undefined = teamSegments[0];
          let createdSegmentId: number | undefined;
          if (req?.overrides?.segment_id !== undefined) {
            const sid = Number(req.overrides.segment_id);
            segment = teamSegments.find((s) => s.id === sid);
            if (!segment) throw new ApiClientError(404, 'not_found', `Segment ${sid} not found in team ${team.id}`);
          } else if (typeof req?.overrides?.segment === 'string') {
            segment = teamSegments.find((s) => s.name === req.overrides!.segment);
            if (!segment) {
              const segId = nextId();
              segment = {
                id: segId,
                team_id: team.id,
                name: req.overrides!.segment,
                config: {},
                roles_count: 0,
                created_at: now(),
                updated_at: now(),
              };
              db.segments.push(segment);
              createdSegmentId = segId;
            }
          } else if (!segment) {
            const segId = nextId();
            segment = { id: segId, team_id: team.id, name: 'general', config: {}, roles_count: 0, created_at: now(), updated_at: now() };
            db.segments.push(segment);
            createdSegmentId = segId;
          }
          if (db.roles.some((r) => r.segment_id === segment!.id && r.name === item.name)) {
            throw new ApiClientError(409, 'conflict', `Role "${item.name}" already exists in segment "${segment!.name}"`);
          }
          const roleId = nextId();
          db.roles.push({
            id: roleId,
            team_id: team.id,
            segment_id: segment!.id,
            segment_name: segment!.name,
            name: item.name,
            address: `${segment!.name}.${item.name}`,
            agent_spec: 'mock-spec',
            state: 'active',
            created_at: now(),
            updated_at: now(),
          });
          recalcTeamCounts();
          const created: { segments?: number[]; roles: number[] } = { roles: [roleId] };
          if (createdSegmentId) created.segments = [createdSegmentId];
          return { status: 'applied', created_resources: created, updated_resources: { teams: [team.id] } };
        }
        // type === 'team'
        if (req?.target_team_id) {
          if (!db.teams.some((t) => t.id === req.target_team_id)) {
            throw new ApiClientError(404, 'not_found', `Team ${req.target_team_id} not found`);
          }
          return { status: 'merged', updated_resources: { teams: [req.target_team_id] } };
        }
        const teamId = nextId();
        db.teams.push({
          id: teamId,
          name: (req?.overrides?.name as string) ?? `${item.name} (applied)`,
          state: 'active',
          segments_count: 0,
          roles_count: 0,
          created_at: now(),
          updated_at: now(),
        });
        recalcTeamCounts();
        return { status: 'applied', created_resources: { teams: [teamId] } };
      },
    },

    history: {
      async getTaskHistory(taskId) {
        maybeFail();
        await latency();
        const db = getDb();
        const list = db.taskHistory.filter((h) => h.queue_task_id === taskId);
        if (list.length === 0 && !db.tasks.some((t) => t.id === taskId)) {
          throw new ApiClientError(404, 'not_found', `Task ${taskId} not found`);
        }
        return { history: clone(list), total: list.length };
      },

      async getSessionHistory(sessionId) {
        maybeFail();
        await latency();
        const db = getDb();
        const s = db.sessions.find((x) => x.id === sessionId);
        if (!s) throw new ApiClientError(404, 'not_found', `Session ${sessionId} not found`);
        return {
          history: [
            { id: nextId(), session_id: sessionId, to_state: 'starting', actor_type: 'daemon', created_at: s.started_at ?? now() },
            { id: nextId(), session_id: sessionId, from_state: 'starting', to_state: s.state, actor_type: 'daemon', created_at: now() },
          ],
          total: 2,
        };
      },

      async getAuditLog(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let list = db.audit;
        if (params?.action) list = list.filter((e) => e.action === params.action);
        if (params?.resource) list = list.filter((e) => e.resource?.includes(params.resource!));
        const limit = params?.limit ?? 50;
        const offset = params?.offset ?? 0;
        return { entries: clone(list.slice(offset, offset + limit)), total: list.length };
      },

      async getTranscript(sessionId) {
        maybeFail();
        await latency();
        const db = getDb();
        const s = db.sessions.find((x) => x.id === sessionId);
        if (!s) throw new ApiClientError(404, 'not_found', `Session ${sessionId} not found`);
        return {
          transcript: [
            {
              id: 1,
              session_id: sessionId,
              message_type: 'prompt',
              content: 'Implement auth middleware per guidance/role.md',
              role: 'user',
              created_at: s.started_at ?? now(),
            },
            {
              id: 2,
              session_id: sessionId,
              message_type: 'response',
              content: 'Starting with the middleware skeleton…',
              metadata: { model: 'claude-3-7-sonnet', tokens: 812, latency_ms: 920 },
              role: 'assistant',
              created_at: now(),
            },
            {
              id: 3,
              session_id: sessionId,
              message_type: 'tool_call',
              content: 'read docs/auth-middleware.md',
              metadata: { tool_name: 'read', tool_arguments: { path: 'docs/auth-middleware.md' } },
              role: 'assistant',
              created_at: now(),
            },
            {
              id: 4,
              session_id: sessionId,
              message_type: 'tool_result',
              content: '<file contents>',
              metadata: { tool_name: 'read' },
              role: 'assistant',
              created_at: now(),
            },
          ],
          total: 4,
          has_more: false,
        };
      },
    },
    config: {
      async getConfig() {
        maybeFail();
        await latency();
        const db = getDb();
        return resolveConfig(db.configOverrides);
      },      async setConfig(key, value) {
        maybeFail();
        await latency();
        const db = getDb();
        const k = CONFIG_REGISTRY.find((x) => x.key === key);
        if (!k) throw new ApiClientError(404, 'not_found', `Unknown config key ${key}`);
        if (!k.editable) throw new ApiClientError(403, 'forbidden', `Config key ${key} is read-only`);
        const v = normalizeSetValue(k.type, value);
        const before = resolveConfig(db.configOverrides);
        const beforeKey = before.sections.find((s) => s.name === k.section)!.keys.find((x) => x.key === key)!;
        db.configOverrides[key] = v;
        return { key, value: v, source: 'override', requires_restart: k.requires_restart, needs_restart: k.requires_restart && beforeKey.current_value !== v };
      },
      async resetConfig(key) {
        maybeFail();
        await latency();
        const db = getDb();
        const k = CONFIG_REGISTRY.find((x) => x.key === key);
        if (!k) throw new ApiClientError(404, 'not_found', `Unknown config key ${key}`);
        delete db.configOverrides[key];
        const after = resolveConfig(db.configOverrides);
        const kv = after.sections.find((s) => s.name === k.section)!.keys.find((x) => x.key === key)!;
        return { key, source: kv.source, value: kv.current_value };
      },
    },
    // P1.0: Projects (mirror of backend ProjectService)
    projects: {
      async list(params) {
        maybeFail();
        await latency();
        const db = getDb();
        let items = db.projects.slice();
        if (params?.archived) items = items.filter((p) => p.archived);
        else if (!params?.include_archived) items = items.filter((p) => !p.archived);
        if (params?.name) items = items.filter((p) => p.name.toLowerCase().includes(params!.name!.toLowerCase()));
        const total = items.length;
        const offset = params?.offset ?? 0;
        const limit = params?.limit ?? 50;
        items = items.slice(offset, offset + limit);
        return { projects: items, total };
      },
      async create(req) {
        maybeFail();
        await latency();
        const db = getDb();
        if (!req.name) throw new ApiClientError(400, 'validation_failed', 'name is required');
        if (req.kind !== 'existing' && req.kind !== 'new') throw new ApiClientError(400, 'validation_failed', "kind must be 'existing' or 'new'");
        const path = req.kind === 'existing' ? req.path : (req.parent_path ? `${req.parent_path}/${req.name}` : req.name);
        if (!path) throw new ApiClientError(400, 'validation_failed', req.kind === 'existing' ? 'path required for existing' : 'parent_path required for new');
        if (db.projects.some((p) => p.path === path)) throw new ApiClientError(409, 'conflict', `project path already exists: ${path}`);
        if (db.projects.some((p) => p.name === req.name)) throw new ApiClientError(409, 'conflict', `project name already exists: ${req.name}`);
        const git = true; // mock: git доступен (README + git init для new)
        const warning: string | undefined = undefined;
        const project: Project = {
          id: nextId(), name: req.name, path, kind: req.kind as ProjectKind, git,
          description: req.description, archived: false,
          created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
        };
        db.projects.push(project);
        return { project, warning };
      },
      async get(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const p = db.projects.find((x) => x.id === id);
        if (!p) throw new ApiClientError(404, 'not_found', `project ${id} not found`);
        const activeSessionStates = ['starting', 'running', 'idle', 'stopping'];
        const counts = {
          teams: db.teams.filter((t) => t.project_id === id).length,
          tasks: db.tasks.filter((t) => t.project_id === id).length,
          active_sessions: db.sessions.filter((s) => s.project_id === id && activeSessionStates.includes(s.state)).length,
        };
        return { project: p, counts };
      },
      async update(id, req) {
        maybeFail();
        await latency();
        const db = getDb();
        const p = db.projects.find((x) => x.id === id);
        if (!p) throw new ApiClientError(404, 'not_found', `project ${id} not found`);
        if (p.archived) throw new ApiClientError(409, 'conflict', 'project is archived');
        if (req.path !== undefined) throw new ApiClientError(409, 'conflict', 'project path is immutable after creation');
        if (req.name !== undefined) {
          if (!req.name) throw new ApiClientError(400, 'validation_failed', 'name must not be empty');
          if (db.projects.some((x) => x.id !== id && x.name === req.name)) throw new ApiClientError(409, 'conflict', 'project name already exists');
          p.name = req.name;
        }
        if (req.description !== undefined) p.description = req.description;
        p.updated_at = new Date().toISOString();
        return { project: p };
      },
      async archive(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const p = db.projects.find((x) => x.id === id);
        if (!p) throw new ApiClientError(404, 'not_found', `project ${id} not found`);
        p.archived = true; // idempotent
        p.updated_at = new Date().toISOString();
        return { project: p };
      },
    },
    // P1.6: watchdog-джобы (mock-симуляция)
    watchdogJobs: {
      async list() {
        maybeFail();
        await latency();
        const db = getDb();
        return { jobs: db.watchdogJobs.slice(), total: db.watchdogJobs.length };
      },
      async get(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const j = db.watchdogJobs.find((x) => x.id === id);
        if (!j) throw new ApiClientError(404, 'not_found', `watchdog job ${id} not found`);
        return { job: j, recent_evaluations: (db.watchdogJobEvents ?? []).filter((e) => e.job_id === id).slice(0, 20) };
      },
      async register(req) {
        maybeFail();
        await latency();
        const db = getDb();
        if (!req.name?.trim()) throw new ApiClientError(400, 'validation_failed', 'name is required');
        if (typeof req.threshold_pct !== 'number' || req.threshold_pct < 1 || req.threshold_pct > 100) {
          throw new ApiClientError(400, 'validation_failed', 'threshold_pct must be 1..100');
        }
        if (!db.sessions.some((s) => s.id === req.target_session_id)) throw new ApiClientError(404, 'not_found', `session ${req.target_session_id} not found`);
        const now = new Date().toISOString();
        const job: WatchdogJob = {
          id: nextId(), name: req.name.trim(), policy: 'context-usage-threshold',
          target_session_id: req.target_session_id, threshold_pct: req.threshold_pct,
          message: req.message, enabled: true, created_by: 'mock-user', created_at: now,
          fired_count: 0,
        };
        db.watchdogJobs.push(job);
        return job;
      },
      async stop(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const j = db.watchdogJobs.find((x) => x.id === id);
        if (!j) throw new ApiClientError(404, 'not_found', `watchdog job ${id} not found`);
        j.enabled = false;
        db.watchdogJobEvents = db.watchdogJobEvents ?? [];
        db.watchdogJobEvents.push({ id: nextId(), job_id: id, outcome: 'stopped', detail: 'остановлен пользователем', created_at: new Date().toISOString() });
        return j;
      },
      async remove(id) {
        maybeFail();
        await latency();
        const db = getDb();
        const j = db.watchdogJobs.find((x) => x.id === id);
        if (!j) throw new ApiClientError(404, 'not_found', `watchdog job ${id} not found`);
        if (j.fired_count > 0) throw new ApiClientError(409, 'conflict', 'watchdog job has fired history — stop it instead');
        db.watchdogJobs = db.watchdogJobs.filter((x) => x.id !== id);
        db.watchdogJobEvents = (db.watchdogJobEvents ?? []).filter((e) => e.job_id !== id);
      },
    },
  };
}
