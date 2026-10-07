import { beforeEach, describe, expect, it } from 'vitest';
import { createMockAdapter, setMockError } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';
import { ApiClientError } from '../src/api/errors';

describe('MockAdapter contract compatibility', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });

  it('getTopology returns team/segments/roles/relatives with layout', async () => {
    const api = createMockAdapter();
    const res = await api.teams.getTopology(1);
    expect(res.team.id).toBe(1);
    expect(res.segments.length).toBeGreaterThan(0);
    expect(res.roles.length).toBeGreaterThan(0);
    expect(res.relatives.length).toBeGreaterThan(0);
    expect(res.layout).toBeDefined();
    expect(res.layout!.segments[0].position).toHaveProperty('x');
  });

  it('createTeam + createSegment + createRole + createRelative roundtrip', async () => {
    const api = createMockAdapter();
    const team = await api.teams.createTeam({ name: 'T' });
    expect(team.status).toBe('created');
    const seg = await api.teams.createSegment(team.id, { name: 'S', layout: { x: 10, y: 20 } });
    expect(seg.status).toBe('created');
    expect(seg.layout).toEqual({ x: 10, y: 20, width: 420, height: 260 });
    const role1 = await api.teams.createRole(seg.id, { name: 'A', agent_spec: 'pi-a' });
    const role2 = await api.teams.createRole(seg.id, { name: 'B', agent_spec: 'pi-b' });
    expect(role1.address).toContain('S.A');
    const rel = await api.teams.createRelative(team.id, { from_role_id: role1.id, to_role_id: role2.id, type: 'delegates_to' });
    expect(rel.status).toBe('created');

    const top = await api.teams.getTopology(team.id);
    expect(top.segments.length).toBe(1);
    expect(top.roles.length).toBe(2);
    expect(top.relatives.length).toBe(1);
  });

  it('duplicate relative → conflict (409)', async () => {
    const api = createMockAdapter();
    const top = await api.teams.getTopology(1);
    const r = top.relatives[0];
    await expect(
      api.teams.createRelative(1, { from_role_id: r.from_role_id, to_role_id: r.to_role_id, type: r.type }),
    ).rejects.toMatchObject({ status: 409, code: 'conflict' });
  });

  it('validateTopology returns contract shape', async () => {
    const api = createMockAdapter();
    const res = await api.teams.validateTopology(1);
    expect(res).toHaveProperty('is_valid');
    expect(Array.isArray(res.errors)).toBe(true);
    expect(Array.isArray(res.warnings)).toBe(true);
  });

  it('dashboard endpoints return contract shapes', async () => {
    const api = createMockAdapter();
    const s = await api.dashboard.getSummary();
    expect(s.teams).toHaveProperty('total');
    expect(s.tasks).toHaveProperty('in_progress');
    const t = await api.dashboard.getTasks();
    expect(t.tasks[0]).toHaveProperty('state');
    const m = await api.dashboard.getMetrics();
    expect(m.metrics.tasks_created.length).toBe(24);
  });

  it('error simulator throws ApiClientError with proper codes', async () => {
    const api = createMockAdapter();
    for (const [kind, code] of [
      ['unauthorized', 'unauthorized'],
      ['forbidden', 'forbidden'],
      ['not_found', 'not_found'],
      ['conflict', 'conflict'],
      ['server', 'server'],
      ['network', 'network'],
    ] as const) {
      setMockError(kind);
      await expect(api.teams.getTeams()).rejects.toBeInstanceOf(ApiClientError);
      try {
        await api.teams.getTeams();
      } catch (e) {
        expect((e as ApiClientError).code).toBe(code);
      }
      setMockError(null);
    }
  });

  it('404 for unknown team', async () => {
    const api = createMockAdapter();
    await expect(api.teams.getTopology(999)).rejects.toMatchObject({ status: 404, code: 'not_found' });
  });

  it('tasks lifecycle: create → in_progress → done (closure required) + handoff + terminal 409', async () => {
    const api = createMockAdapter();
    const top = await api.teams.getTopology(1);
    const roleA = top.roles[0];
    const roleB = top.roles[1];

    // create (missing title → 400 validation_failed)
    await expect(
      api.tasks.create({ team_id: 1, destination_role_id: roleA.id, title: '   ' }),
    ).rejects.toMatchObject({ status: 400, code: 'validation_failed' });

    const created = await api.tasks.create({ team_id: 1, destination_role_id: roleA.id, title: 'Do the thing', priority: 5 });
    expect(created.status).toBe('created');
    expect(created.state).toBe('pending');

    // list/get roundtrip
    const list = await api.tasks.list({ team_id: 1 });
    expect(list.tasks.some((t) => t.id === created.id && t.state === 'pending')).toBe(true);
    const detail = await api.tasks.get(created.id);
    expect(detail.task.title).toBe('Do the thing');
    expect(detail.task.destination_role_name).toBe(roleA.name);
    expect(Array.isArray(detail.subtasks)).toBe(true);

    // pending → in_progress
    const started = await api.tasks.updateState(created.id, { state: 'in_progress' });
    expect(started.state).toBe('in_progress');
    expect(started.started_at).toBeDefined();

    // done requires closure_reason → 400
    await expect(api.tasks.updateState(created.id, { state: 'done' })).rejects.toMatchObject({
      status: 400,
      code: 'validation_failed',
    });
    const done = await api.tasks.updateState(created.id, { state: 'done', closure_reason: 'no_follow_on' });
    expect(done.state).toBe('done');
    expect(done.closure_reason).toBe('no_follow_on');

    // terminal → any transition → 409
    await expect(api.tasks.updateState(created.id, { state: 'pending' })).rejects.toMatchObject({ status: 409, code: 'conflict' });

    // handoff: new pending task in role B, original closed handed_off_to
    const created2 = await api.tasks.create({ team_id: 1, destination_role_id: roleA.id, title: 'Handoff target' });
    const res = await api.tasks.handoff(created2.id, { to_role_id: roleB.id });
    expect(res.status).toBe('handed_off');
    expect(res.closed_task_id).toBe(created2.id);
    expect(res.task.id).toBe(res.new_task_id);
    expect(res.task.state).toBe('pending');
    expect(res.task.destination_role_id).toBe(roleB.id);
    const closed = await api.tasks.get(created2.id);
    expect(closed.task.state).toBe('done');
    expect(closed.task.closure_reason).toBe('handed_off_to');
    expect(closed.task.closure_target_id).toBe(res.new_task_id);
  }, 30_000);
});
