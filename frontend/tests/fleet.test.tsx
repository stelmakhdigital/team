import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';
import { composeFleetRows, computeActiveRoleIds, fleetCtxTone, fleetDotState, FLEET_MAX_ROWS } from '../src/lib/fleet';
import type { SessionDetail, Task, Team } from '../src/types/api';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

const iso = (minAgo: number) => new Date(Date.now() - minAgo * 60_000).toISOString();
const sess = (over: Partial<SessionDetail>): SessionDetail => ({
  id: 1,
  team_id: 1,
  team_name: 'Dev Team',
  role_id: 1,
  role_name: 'Lead',
  runtime_type: 'pi',
  state: 'running',
  created_at: iso(30),
  updated_at: iso(1),
  ...over,
});
const task = (over: Partial<Task>): Task => ({
  id: 1,
  team_id: 1,
  team_name: 'Dev Team',
  title: 'Do thing',
  state: 'in_progress',
  priority: 1,
  destination_role_id: 1,
  destination_role_name: 'Lead',
  created_at: iso(60),
  updated_at: iso(5),
  ...over,
});
const team: Team[] = [{ id: 1, name: 'Dev Team', state: 'active', segments_count: 1, roles_count: 2, created_at: iso(1), updated_at: iso(1) }];

describe('W1.1 Fleet — pure composers', () => {
  it('composeFleetRows: dot-state mapping (running/idle/failed/stopped→stopped)', () => {
    expect(fleetDotState('running')).toBe('running');
    expect(fleetDotState('idle')).toBe('idle');
    expect(fleetDotState('failed')).toBe('failed');
    expect(fleetDotState('stopped')).toBe('stopped');
    expect(fleetDotState('starting')).toBe('stopped');
  });

  it('composeFleetRows: one row per live session, joins current in_progress task by role', () => {
    const rows = composeFleetRows(
      [
        sess({ id: 1, role_id: 1, role_name: 'Lead', state: 'running', context_used_percentage: 42 }),
        sess({ id: 2, role_id: 2, role_name: 'Worker', state: 'idle', context_used_percentage: 88 }),
      ],
      team,
      [task({ id: 1, destination_role_id: 2, destination_role_name: 'Worker', title: 'Build' })],
    );
    expect(rows).toHaveLength(2);
    const lead = rows.find((r) => r.roleId === 1)!;
    const worker = rows.find((r) => r.roleId === 2)!;
    expect(lead.state).toBe('running');
    expect(lead.ctxPct).toBe(42);
    expect(lead.taskId).toBeNull();
    expect(lead.taskTitle).toBeNull();
    expect(worker.state).toBe('idle');
    expect(worker.taskId).toBe(1);
    expect(worker.taskTitle).toBe('Build');
    expect(worker.uptimeSeconds).toBeTypeOf('number');
  });

  it('composeFleetRows: only in_progress tasks attach (done/pending ignored)', () => {
    const rows = composeFleetRows(
      [sess({ role_id: 1 })],
      team,
      [task({ id: 9, state: 'done', destination_role_id: 1 })],
    );
    expect(rows[0].taskId).toBeNull();
  });

  it('composeFleetRows: ctx% null when runtime omits it (no data → «—»)', () => {
    const rows = composeFleetRows([sess({ context_used_percentage: undefined })], team, []);
    expect(rows[0].ctxPct).toBeNull();
  });

  it('composeFleetRows: running first, then team/role order', () => {
    const rows = composeFleetRows(
      [
        sess({ id: 1, role_id: 1, role_name: 'A', state: 'idle' }),
        sess({ id: 2, role_id: 2, role_name: 'B', state: 'running' }),
        sess({ id: 3, role_id: 3, role_name: 'C', state: 'failed' }),
      ],
      team,
      [],
    );
    expect(rows.map((r) => r.roleId)).toEqual([2, 3, 1]);
  });

  it('computeActiveRoleIds: only in_progress destination roles', () => {
    const ids = computeActiveRoleIds([
      task({ id: 1, destination_role_id: 2 }),
      task({ id: 2, state: 'done', destination_role_id: 3 }),
      task({ id: 3, state: 'pending', destination_role_id: 4 }),
    ]);
    expect(ids.has(2)).toBe(true);
    expect(ids.has(3)).toBe(false);
    expect(ids.has(4)).toBe(false);
  });

  it('fleetCtxTone: thresholds <60 ok / 60-80 warn / >=80 crit / null', () => {
    expect(fleetCtxTone(10)).toBe('ok');
    expect(fleetCtxTone(59)).toBe('ok');
    expect(fleetCtxTone(60)).toBe('warn');
    expect(fleetCtxTone(79)).toBe('warn');
    expect(fleetCtxTone(80)).toBe('crit');
    expect(fleetCtxTone(null)).toBe(null);
  });

  it('bounded: FLEET_MAX_ROWS = 12', () => {
    expect(FLEET_MAX_ROWS).toBe(12);
  });
});

describe('W1.1 Fleet — Dashboard UI', () => {
  beforeEach(() => resetMockDb());

  it('renders Fleet block with live roles (dot / team→role / task / ctx% / CTA)', async () => {
    renderAt('/');
    const h = await screen.findByRole('heading', { name: 'Fleet' }, { timeout: 15_000 });
    expect(h).toBeInTheDocument();
    const card = h.closest('.fleet-card') as unknown as HTMLElement;
    // seeded running roles are listed (Lead/Worker/Reviewer on team 1)
    expect(within(card).getAllByText('Lead').length).toBeGreaterThanOrEqual(1);
    // activity dots present, running ones carry the running class
    const dots = within(card).getAllByTestId('fleet-dot');
    expect(dots.length).toBeGreaterThanOrEqual(3);
    expect(dots.some((d) => d.classList.contains('fleet-dot-running'))).toBe(true);
    // ctx% thresholds → tone class (seed: 42 → ok, 67 → warn)
    expect(within(card).getAllByText(/ctx 42%/)[0].className).toContain('fleet-ctx-ok');
    expect(within(card).getAllByText(/ctx 67%/)[0].className).toContain('fleet-ctx-warn');
    // CTA to session detail exists
    expect(within(card).getAllByTitle('Открыть сессию').length).toBeGreaterThanOrEqual(1);
  }, 25_000);

  it('bounded: >12 roles → shows 12 + «+N ещё»', async () => {
    const db = getDb();
    // pad to 15 live sessions
    const base = db.sessions[0];
    for (let i = 0; i < 15; i++) {
      db.sessions.push({
        ...base,
        id: 900 + i,
        role_id: 900 + i,
        role_name: `Role${i}`,
        state: 'idle',
      });
    }
    renderAt('/');
    const h = await screen.findByRole('heading', { name: 'Fleet' }, { timeout: 15_000 });
    const card = h.closest('.fleet-card') as unknown as HTMLElement;
    expect(within(card).getAllByTestId('fleet-dot')).toHaveLength(12);
    expect(within(card).getByText(/\+\d+ ещё/)).toBeInTheDocument();
  }, 25_000);

  it('empty state: no sessions → «Нет активных ролей» + CTA «Открыть команды»', async () => {
    const db = getDb();
    db.sessions = [];
    renderAt('/');
    const h = await screen.findByRole('heading', { name: 'Fleet' }, { timeout: 15_000 });
    const card = h.closest('.fleet-card') as unknown as HTMLElement;
    expect(within(card).getByText('Нет активных ролей')).toBeInTheDocument();
    expect(within(card).getByText('Открыть команды')).toBeInTheDocument();
  }, 25_000);
});
