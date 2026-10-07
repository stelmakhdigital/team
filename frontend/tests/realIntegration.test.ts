// Integration tests: frontend real API client (fetch) против живого backend.
// Пропускаются, если backend не запущен (http://localhost:8080/healthz).
// Запуск: backend `go run ./cmd/daemon` + `npm test`.
import { describe, expect, it } from 'vitest';
import { createRealAdapter } from '../src/api/real';

// Интеграция идёт напрямую на живой daemon (не через Vite-прокси).
// getApiConfig() читает import.meta.env в момент запроса.
if (typeof import.meta.env !== 'undefined') {
  import.meta.env.VITE_API_BASE_URL = 'http://localhost:8080';
}

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

  // poll until predicate true or timeout (reaper/watchdog работают асинхронно)
  async function waitUntil(pred: () => Promise<boolean>, ms = 8_000) {
    const deadline = Date.now() + ms;
    for (;;) {
      if (await pred()) return;
      if (Date.now() > deadline) throw new Error('waitUntil: timeout');
      await new Promise((r) => setTimeout(r, 400));
    }
  }

  it('slice 3: session lifecycle (create → history/transcript/dashboard → stop)', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    const role = topo.roles[0];

    const created = await api.sessions.create(team.id, { role_id: role.id, command: 'sleep', args: ['60'] });
    expect(created.status).toBe('started');
    const sid = created.id;

    // GET /sessions/:id — contract view (snake_case, role_name, state)
    const detail = await api.sessions.get(sid);
    expect(detail.state).toBe('running');
    expect(detail.role_name).toBe(role.name);
    expect(detail.team_id).toBe(team.id);

    // dashboard/sessions: контрактная Session-форма, сессия видна
    await waitUntil(async () => {
      const ds = await api.dashboard.getSessions();
      const s = ds.sessions.find((x) => x.id === sid);
      return !!s && s.state === 'running' && !!s.role_name && !!s.runtime_type;
    });

    // session history: минимум starting → running
    const h = await api.history.getSessionHistory(sid);
    expect(h.total).toBeGreaterThanOrEqual(2);
    expect(h.history[h.history.length - 1].to_state).toBe('running');

    // transcript: контрактная форма (transcript[] + has_more)
    const tr = await api.history.getTranscript(sid);
    expect(Array.isArray(tr.transcript)).toBe(true);
    expect(typeof tr.has_more).toBe('boolean');

    // stop (DELETE) → stopped; повторный stop идемпотентен (200 stopped)
    const stopped = await api.sessions.stop(sid);
    expect(stopped.state).toBe('stopped');
    expect((await api.sessions.get(sid)).state).toBe('stopped');
    const again = await api.sessions.stop(sid);
    expect(again.state).toBe('stopped');
  }, 30_000);

  it('slice 3: crashed process → failed (exit_code) + watchdog alert', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    const role = topo.roles[0];

    const created = await api.sessions.create(team.id, {
      role_id: role.id,
      command: '/bin/sh',
      args: ['-c', 'exit 3'],
    });
    const sid = created.id;

    // reaper: running → failed, exit_code=3
    await waitUntil(async () => (await api.sessions.get(sid)).state === 'failed');
    const failed = await api.sessions.get(sid);
    expect(failed.exit_code).toBe(3);

    // история фиксирует переход → failed
    const h = await api.history.getSessionHistory(sid);
    expect(h.history.some((e) => e.to_state === 'failed')).toBe(true);

    // watchdog: alert в dashboard/alerts (severity по контракту)
    await waitUntil(async () => {
      const a = await api.dashboard.getAlerts();
      return a.alerts.some((x) => x.session_id === sid && x.requires_action);
    });
  }, 30_000);
});
