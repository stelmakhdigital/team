import type { SessionDetail, Task, Team } from '../types/api';

// UX Wave 1 — W1.1: Fleet на Dashboard.
// Composing на клиенте (без backend-эндпоинта): одна загрузка на тип
// (sessions.list + teams.getTeams + tasks.list) → строки live-ролей.

export type FleetDotState = 'running' | 'idle' | 'failed' | 'stopped';

export interface FleetRow {
  sessionId: number;
  state: FleetDotState;
  teamId: number;
  teamName: string;
  roleId: number;
  role: string;
  /** текущая in_progress-задача роли (или null — «—») */
  taskId: number | null;
  taskTitle: string | null;
  /** context% (null = нет данных — «—») */
  ctxPct: number | null;
  uptimeSeconds?: number;
}

export const FLEET_MAX_ROWS = 12;

function uptimeSinceCreated(createdAt: string): number | undefined {
  const t = new Date(createdAt).getTime();
  if (Number.isNaN(t)) return undefined;
  const secs = Math.floor((Date.now() - t) / 1000);
  return secs >= 0 ? secs : undefined;
}

const DOT_ORDER: Record<FleetDotState, number> = {
  running: 0,
  failed: 1,
  idle: 2,
  stopped: 3,
};

export function fleetDotState(raw: SessionDetail['state']): FleetDotState {
  if (raw === 'running') return 'running';
  if (raw === 'idle') return 'idle';
  if (raw === 'failed') return 'failed';
  return 'stopped'; // stopped/starting/stopping → нейтральный
}

export type FleetCtxTone = 'ok' | 'warn' | 'crit' | null;

/** пороги: <60 green / 60-80 amber / >=80 red (как ctx-классы топологии) */
export function fleetCtxTone(pct: number | null): FleetCtxTone {
  if (pct === null) return null;
  if (pct >= 80) return 'crit';
  if (pct >= 60) return 'warn';
  return 'ok';
}

/**
 * Строки = роли с live-сессией по всем командам (по одной строке на сессию).
 * Сортировка: running → failed → idle → stopped, затем команда/роль (stable).
 */
export function composeFleetRows(
  sessions: SessionDetail[],
  _teams: Team[],
  tasks: Task[],
): FleetRow[] {
  const activeTaskByRole = new Map<number, Task>();
  for (const t of tasks) {
    if (t.state !== 'in_progress') continue;
    if (!activeTaskByRole.has(t.destination_role_id)) activeTaskByRole.set(t.destination_role_id, t);
  }
  const rows: FleetRow[] = sessions.map((s) => {
    const task = activeTaskByRole.get(s.role_id) ?? null;
    return {
      sessionId: s.id,
      state: fleetDotState(s.state),
      teamId: s.team_id,
      teamName: s.team_name,
      roleId: s.role_id,
      role: s.role_name,
      taskId: task ? task.id : null,
      taskTitle: task ? task.title : null,
      ctxPct: typeof s.context_used_percentage === 'number' ? s.context_used_percentage : null,
      uptimeSeconds: uptimeSinceCreated(s.created_at),
    };
  });
  return rows.sort((a, b) => {
    const d = DOT_ORDER[a.state] - DOT_ORDER[b.state];
    if (d !== 0) return d;
    const byTeam = a.teamName.localeCompare(b.teamName);
    if (byTeam !== 0) return byTeam;
    return a.role.localeCompare(b.role);
  });
}

/** W1.3: id ролей, у которых есть in_progress-задача (активные получатели работы). */
export function computeActiveRoleIds(tasks: Task[]): Set<number> {
  const ids = new Set<number>();
  for (const t of tasks) {
    if (t.state === 'in_progress') ids.add(t.destination_role_id);
  }
  return ids;
}
