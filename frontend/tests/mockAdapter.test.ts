import { beforeEach, describe, expect, it } from 'vitest';
import { createMockAdapter, setMockError } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';
import { ApiClientError } from '../src/api/errors';

describe('MockAdapter contract compatibility', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });

  it('getTopology returns team/segments/roles/relatives; layout optional (auto-layout default)', async () => {
    const api = createMockAdapter();
    const res = await api.teams.getTopology(1);
    expect(res.team.id).toBe(1);
    expect(res.segments.length).toBeGreaterThan(0);
    expect(res.roles.length).toBeGreaterThan(0);
    expect(res.relatives.length).toBeGreaterThan(0);
    // R6.2: seeded user-layout удалён (перекрывал роли) — по умолчанию
    // layout: undefined = чистый auto-layout; после PATCH — сохраняется
    expect(res.layout).toBeUndefined();
    await api.teams.updateRoleLayout(1, { position: { x: 10, y: 20 } });
    const res2 = await api.teams.getTopology(1);
    expect(res2.layout).toBeDefined();
    const entry = res2.layout!.roles.find((r) => r.role_id === 1);
    expect(entry?.position).toEqual({ x: 10, y: 20 });
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

  it('workflow R6.4: delete block removes its connections; delete connection/workflow', async () => {
    const api = createMockAdapter();
    const wf = await api.workflows.createWorkflow({ team_id: 1, name: 'del-test' });
    const b1 = await api.workflows.createBlock(wf.id, { type: 'task', position: { x: 0, y: 0 }, config: {} });
    const b2 = await api.workflows.createBlock(wf.id, { type: 'agent', position: { x: 1, y: 0 }, config: {} });
    const c1 = await api.workflows.createConnection(wf.id, { from_block_id: b1.id, to_block_id: b2.id });
    // delete connection
    const d1 = await api.workflows.deleteConnection(wf.id, c1.id);
    expect(d1.status).toBe('deleted');
    // recreate + delete block (каскад по связям)
    await api.workflows.createConnection(wf.id, { from_block_id: b1.id, to_block_id: b2.id });
    const d2 = await api.workflows.deleteBlock(wf.id, b1.id);
    expect(d2.status).toBe('deleted');
    expect(d2.removed_connections).toBe(1);
    const got = await api.workflows.getWorkflow(wf.id);
    expect(got.blocks.map((b) => b.id)).toEqual([b2.id]);
    expect(got.connections.length).toBe(0);
    // delete workflow
    const d3 = await api.workflows.deleteWorkflow(wf.id);
    expect(d3.status).toBe('deleted');
    await expect(api.workflows.getWorkflow(wf.id)).rejects.toMatchObject({ status: 404 });
  });

  it('library apply: 404 / team new / team merge / workflow target / segment merge / role apply', async () => {
    const api = createMockAdapter();

    // 404: неизвестный item
    await expect(api.library.applyLibrary(999, {})).rejects.toMatchObject({ status: 404, code: 'not_found' });

    // team без target → новая команда (overrides.name), downloads_count++
    const before = (await api.teams.getTeams()).teams.length;
    const teamItem = await api.library.saveToLibrary({ type: 'team', source_id: 1, name: 'L-team' });
    const applied = await api.library.applyLibrary(teamItem.id, { overrides: { name: 'Applied team' } });
    expect(applied.status).toBe('applied');
    expect(applied.created_resources?.teams).toHaveLength(1);
    const newId = applied.created_resources!.teams![0];
    const teams = await api.teams.getTeams();
    expect(teams.teams).toHaveLength(before + 1);
    expect(teams.teams.find((t) => t.id === newId)?.name).toBe('Applied team');
    expect((await api.library.getLibraryItem(teamItem.id)).item.downloads_count).toBe(1);

    // team c target → merged
    const merged = await api.library.applyLibrary(teamItem.id, { target_team_id: 1 });
    expect(merged.status).toBe('merged');
    expect(merged.updated_resources?.teams).toEqual([1]);

    // workflow: без target → 400; с target → applied
    const wf = await api.workflows.createWorkflow({ team_id: 1, name: 'wf-lib', blocks: [] });
    const wfItem = await api.library.saveToLibrary({ type: 'workflow', source_id: wf.id, name: 'L-wf' });
    await expect(api.library.applyLibrary(wfItem.id, {})).rejects.toMatchObject({ status: 400 });
    const wfApplied = await api.library.applyLibrary(wfItem.id, { target_team_id: 1 });
    expect(wfApplied.status).toBe('applied');

    // segment: без target → 400; с target → merged (создаёт сегмент), повтор идемпотентен
    const segItem = await api.library.saveToLibrary({ type: 'segment', source_id: 1, name: 'Ops' });
    await expect(api.library.applyLibrary(segItem.id, {})).rejects.toMatchObject({ status: 400 });
    const segApplied = await api.library.applyLibrary(segItem.id, { target_team_id: 1 });
    expect(segApplied.status).toBe('merged');
    expect(segApplied.created_resources?.segments).toHaveLength(1);
    const segAgain = await api.library.applyLibrary(segItem.id, { target_team_id: 1 });
    expect(segAgain.created_resources?.segments).toEqual([]);
    const topo1 = await api.teams.getTopology(1);
    expect(topo1.segments.some((s) => s.name === 'Ops')).toBe(true);

    // role: без target → 400; с target → applied (+создание сегмента по overrides.segment);
    // дубль имени в сегменте → 409
    const roleItem = await api.library.saveToLibrary({ type: 'role', source_id: 1, name: 'Ops' });
    await expect(api.library.applyLibrary(roleItem.id, {})).rejects.toMatchObject({ status: 400 });
    const roleApplied = await api.library.applyLibrary(roleItem.id, {
      target_team_id: 1,
      overrides: { segment: 'NewSeg' },
    });
    expect(roleApplied.status).toBe('applied');
    expect(roleApplied.created_resources?.roles).toHaveLength(1);
    expect(roleApplied.created_resources?.segments).toHaveLength(1);
    const topo2 = await api.teams.getTopology(1);
    expect(topo2.roles.some((r) => r.name === 'Ops' && r.segment_name === 'NewSeg')).toBe(true);
    const dupRole = await api.library.saveToLibrary({ type: 'role', source_id: 1, name: 'Ops' });
    await expect(
      api.library.applyLibrary(dupRole.id, { target_team_id: 1, overrides: { segment: 'NewSeg' } }),
    ).rejects.toMatchObject({ status: 409, code: 'conflict' });
  }, 30_000);
});
