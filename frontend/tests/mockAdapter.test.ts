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
});
