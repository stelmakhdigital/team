// Integration tests: frontend real API client (fetch) против живого backend.
// Пропускаются, если backend не запущен (http://localhost:8080/healthz).
// Запуск: backend `go run ./cmd/daemon` + `npm test`.
import { describe, expect, it } from 'vitest';
import { createRealAdapter } from '../src/api/real';

const backendAvailable = await (async () => {
  try {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), 1500);
    const res = await fetch('http://localhost:8080/healthz', { signal: ctrl.signal });
    clearTimeout(t);
    return res.ok;
  } catch {
    return false;
  }
})();

describe.runIf(backendAvailable)('integration: frontend real client vs backend', () => {
  const api = createRealAdapter();

  it('Team Builder vertical: create team+spec → topology (snake_case, layout) → validate → save', async () => {
    const stamp = Date.now();
    const team = await api.teams.createTeam({
      name: `IT-${stamp}`,
      spec: {
        segments: [
          { name: 'Backend', layout: { x: 60, y: 60, width: 420, height: 260 } },
          { name: 'Review', layout: { x: 560, y: 60, width: 320, height: 220 } },
        ],
        roles: [
          { name: 'Lead', agent_spec: 'pi-lead', segment: 'Backend', layout: { x: 90, y: 120 } },
          { name: 'Worker', agent_spec: 'pi-worker', segment: 'Backend', layout: { x: 90, y: 240 } },
          { name: 'Reviewer', agent_spec: 'pi-reviewer', segment: 'Review', layout: { x: 600, y: 140 } },
        ],
        // backend принимает адресный формат from/to (blockers #8); контракт 20 описывает from_role/to_role
        relatives: [
          { from: 'Backend.Lead', to: 'Backend.Worker', type: 'delegates_to' } as never,
          { from: 'Backend.Worker', to: 'Review.Reviewer', type: 'collaborates_with' } as never,
        ] as never,
      } as never,
    });
    expect(team.status).toBe('created');

    const topo = await api.teams.getTopology(team.id);
    // контрактные snake_case поля
    expect(topo.team).toHaveProperty('segments_count', 2);
    expect(topo.team).toHaveProperty('roles_count', 3);
    expect(topo.segments[0]).toHaveProperty('roles_count');
    expect(topo.roles[0]).toHaveProperty('segment_name');
    expect(topo.relatives[0]).toHaveProperty('from_role_name');
    expect(topo.relatives[0]).toHaveProperty('to_role_name');
    // layout: сегмент с width/height
    const segPos = topo.layout?.segments[0]?.position;
    expect(segPos).toMatchObject({ x: 60, y: 60, width: 420, height: 260 });
    expect(topo.layout?.roles[0]?.position).toMatchObject({ x: 90, y: 120 });

    const val = await api.teams.validateTopology(team.id);
    expect(typeof val.is_valid).toBe('boolean');
    expect(Array.isArray(val.errors)).toBe(true);
    expect(Array.isArray(val.warnings)).toBe(true);

    const saved = await api.teams.saveTopology(team.id, {});
    expect(saved.status).toBe('saved');
  }, 20_000);

  it('role config endpoint', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    const role = topo.roles[0];
    const cfg = await api.teams.getRoleConfig(role.id);
    expect(cfg.role.id).toBe(role.id);
    expect(cfg.agent_spec).toHaveProperty('pi_config');
    expect(Array.isArray(cfg.available_profiles)).toBe(true);
    expect(Array.isArray(cfg.available_plugins)).toBe(true);
  }, 20_000);

  it('error envelope: not_found + conflict → ApiClientError с кодом', async () => {
    await expect(api.teams.getTopology(999_999)).rejects.toMatchObject({ status: 404, code: 'not_found' });
    const teams = await api.teams.getTeams();
    const team = teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    if (topo.relatives.length === 0) return;
    const r = topo.relatives[0];
    await expect(
      api.teams.createRelative(team.id, { from_role_id: r.from_role_id, to_role_id: r.to_role_id, type: r.type }),
    ).rejects.toMatchObject({ status: 409, code: 'conflict' });
  }, 20_000);

  it('slice 2: dashboard summary/tasks + task history (реальные endpoint UI)', async () => {
    // summary: контрактные формы (включая alerts total/critical/warning)
    const summary = await api.dashboard.getSummary();
    expect(summary.teams).toHaveProperty('total');
    expect(summary.teams).toHaveProperty('active');
    expect(summary.tasks).toMatchObject({
      total: expect.any(Number),
      pending: expect.any(Number),
      in_progress: expect.any(Number),
      blocked: expect.any(Number),
      done_today: expect.any(Number),
    });
    expect(summary.sessions).toMatchObject({
      total: expect.any(Number),
      running: expect.any(Number),
      failed: expect.any(Number),
    });
    expect(summary.alerts).toMatchObject({
      total: expect.any(Number),
      critical: expect.any(Number),
      warning: expect.any(Number),
    });
    expect(summary).toHaveProperty('updated_at');

    // dashboard/tasks: Task-форма с именами team/role и UI-флагами
    const dt = await api.dashboard.getTasks();
    expect(typeof dt.total).toBe('number');
    if (dt.tasks.length > 0) {
      const t = dt.tasks[0];
      expect(t).toMatchObject({
        id: expect.any(Number),
        team_id: expect.any(Number),
        title: expect.any(String),
        state: expect.any(String),
        destination_role_name: expect.any(String),
        is_stale: expect.any(Boolean),
        is_blocked: expect.any(Boolean),
      });
    }

    // task history: GET /tasks/{id}/history (контракт TaskHistoryEntry)
    if (dt.tasks.length > 0) {
      const h = await api.history.getTaskHistory(dt.tasks[0].id);
      expect(Array.isArray(h.history)).toBe(true);
      if (h.history.length > 0) {
        expect(h.history[0]).toHaveProperty('queue_task_id');
        expect(h.history[0]).toHaveProperty('to_state');
        expect(h.history[0]).toHaveProperty('actor_type');
        expect(h.history[0]).toHaveProperty('created_at');
      }
    }
  }, 30_000);
});
