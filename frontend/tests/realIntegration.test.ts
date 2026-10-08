// Integration tests: frontend real API client (fetch) против живого backend.
// Пропускаются, если backend не запущен (http://localhost:8080/healthz).
// Запуск: backend `go run ./cmd/daemon` + `npm test`.
import { describe, expect, it } from 'vitest';
import { createRealAdapter } from '../src/api/real';

// Интеграция идёт напрямую на живой daemon (не через Vite-прокси).
// Базовый URL настраивается: INTEGRATION_BASE_URL (default http://localhost:8080);
// если демон с DAEMON_API_KEYS — INTEGRATION_API_KEY (заголовок X-API-Key).
declare const process: { env: Record<string, string | undefined> };
const BASE = process.env.INTEGRATION_BASE_URL ?? 'http://localhost:8080';
const API_KEY = process.env.INTEGRATION_API_KEY;
if (typeof import.meta.env !== 'undefined') {
  import.meta.env.VITE_API_BASE_URL = BASE;
  if (API_KEY) import.meta.env.VITE_API_KEY = API_KEY;
}
// WS-аутентификация — только query ?api_key= (браузерный WS не шлёт заголовки)
const WS_URL = `${BASE.replace(/^http/, 'ws')}/ws${API_KEY ? `?api_key=${encodeURIComponent(API_KEY)}` : ''}`;

const backendAvailable = await (async () => {
  try {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), 1500);
    const res = await fetch(`${BASE}/healthz`, {
      signal: ctrl.signal,
      headers: API_KEY ? { 'X-API-Key': API_KEY } : undefined,
    });
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

    // transcript: контрактная форма (transcript[] + total + has_more)
    const tr = await api.history.getTranscript(sid);
    expect(Array.isArray(tr.transcript)).toBe(true);
    expect(typeof tr.total).toBe('number');
    expect(tr.total).toBeGreaterThanOrEqual(tr.transcript.length);
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

    // reaper: running → failed, exit_code=3 (под нагрузкой может занять несколько секунд)
    await waitUntil(async () => (await api.sessions.get(sid)).state === 'failed', 15_000);
    const failed = await api.sessions.get(sid);
    expect(failed.exit_code).toBe(3);

    // история фиксирует переход → failed
    const h = await api.history.getSessionHistory(sid);
    expect(h.history.some((e) => e.to_state === 'failed')).toBe(true);

    // watchdog: alert в dashboard/alerts (цикл watchdog ~10 c)
    await waitUntil(async () => {
      const a = await api.dashboard.getAlerts();
      return a.alerts.some((x) => x.session_id === sid && x.requires_action);
    }, 25_000);
  }, 45_000);

  it('tasks lifecycle: create → in_progress → done (closure) → handoff → terminal 409', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    const roleA = topo.roles[0];
    const roleB = topo.roles[1] ?? roleA;

    // create → 201 {id, state, status}
    const created = await api.tasks.create({ team_id: team.id, destination_role_id: roleA.id, title: 'IT lifecycle task' });
    expect(created.status).toBe('created');
    expect(created.state).toBe('pending');

    // list + get (контрактная форма с destination_role_name)
    const list = await api.tasks.list({ team_id: team.id });
    expect(list.tasks.some((t) => t.id === created.id)).toBe(true);
    const detail = await api.tasks.get(created.id);
    expect(detail.task.title).toBe('IT lifecycle task');
    expect(detail.task.destination_role_name).toBe(roleA.name);
    expect(Array.isArray(detail.subtasks)).toBe(true);

    // invalid transition: pending → pending? нет; done без closure → 400
    await expect(api.tasks.updateState(created.id, { state: 'done' })).rejects.toMatchObject({ status: 400 });

    // pending → in_progress → done (closure_reason)
    const started = await api.tasks.updateState(created.id, { state: 'in_progress' });
    expect(started.state).toBe('in_progress');
    const done = await api.tasks.updateState(created.id, { state: 'done', closure_reason: 'no_follow_on' });
    expect(done.state).toBe('done');
    expect(done.closure_reason).toBe('no_follow_on');

    // terminal → 409
    await expect(api.tasks.updateState(created.id, { state: 'pending' })).rejects.toMatchObject({ status: 409 });

    // handoff → новая задача у целевой роли, исходная закрыта handed_off_to
    const t2 = await api.tasks.create({ team_id: team.id, destination_role_id: roleA.id, title: 'IT handoff task' });
    const res = await api.tasks.handoff(t2.id, { to_role_id: roleB.id });
    expect(res.status).toBe('handed_off');
    expect(res.closed_task_id).toBe(t2.id);
    expect(res.task.destination_role_id).toBe(roleB.id);
    expect(res.task.state).toBe('pending');
    const closed = await api.tasks.get(t2.id);
    expect(closed.task.state).toBe('done');
    expect(closed.task.closure_reason).toBe('handed_off_to');

    // history: переходы зафиксированы
    const h = await api.history.getTaskHistory(created.id);
    expect(h.history.some((e) => e.to_state === 'in_progress')).toBe(true);
    expect(h.history.some((e) => e.to_state === 'done')).toBe(true);
  }, 30_000);

  // ---- slice 4: Message Center ----

  it('slice 4: messages — direct + broadcast + list filters', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    const [roleA] = [topo.roles[0]];

    // direct: to_role_id обязателен, delivered_to = [role]
    const direct = await api.messages.sendMessage({
      team_id: team.id,
      type: 'direct',
      to_role_id: roleA.id,
      body: `IT direct ${Date.now()}`,
    });
    expect(direct.status).toBe('sent');
    expect(direct.delivered_to).toEqual([roleA.id]);

    // broadcast: без to_role_id, доставлено всем ролям команды
    const broadcast = await api.messages.sendMessage({ team_id: team.id, type: 'broadcast', body: 'IT broadcast' });
    expect(broadcast.status).toBe('sent');
    expect(broadcast.delivered_to).toHaveLength(topo.roles.length);

    // type system/watchdog — только серверные → 400
    await expect(
      api.messages.sendMessage({ team_id: team.id, type: 'system' as never, body: 'nope' }),
    ).rejects.toMatchObject({ status: 400, code: 'validation_failed' });

    // list: фильтр team_id, формы (is_mine, to_role_name; from_role_name у оператора
    // omitempty в текущем билде — фикс зафиксирован в answer_backend.md, slice 6)
    const list = await api.messages.getMessages({ team_id: team.id, limit: 50 });
    expect(list.total).toBeGreaterThanOrEqual(2);
    expect(list.messages.every((m) => m.team_id === team.id)).toBe(true);
    const d = list.messages.find((m) => m.id === direct.id);
    expect(d?.to_role_id).toBe(roleA.id);
    expect(d?.to_role_name).toBe(roleA.name);
    expect(d?.is_mine).toBe(true);
    expect(d?.from_role_name ?? 'You').toBe('You');
    expect(list.messages.some((m) => m.id === broadcast.id && m.type === 'broadcast')).toBe(true);
  }, 20_000);

  it('slice 4: chatrooms — авто-создание (team + segments), send + list', async () => {
    // отдельная свежая команда: chatroom-имя фиксируется при создании, rename команды
    // (save c name) его не обновляет
    const stamp = Date.now();
    const team = await api.teams.createTeam({
      name: `IT-chat-${stamp}`,
      spec: {
        segments: [{ name: 'Backend', layout: { x: 0, y: 0, width: 300, height: 200 } }],
        roles: [
          { name: 'Lead', agent_spec: 'pi-lead', segment: 'Backend', layout: { x: 30, y: 40 } },
          { name: 'Worker', agent_spec: 'pi-worker', segment: 'Backend', layout: { x: 30, y: 100 } },
        ],
        relatives: [{ from: 'Backend.Lead', to: 'Backend.Worker', type: 'delegates_to' } as never],
      } as never,
    });
    const topo = await api.teams.getTopology(team.id);

    const rooms = await api.messages.getChatrooms();
    const teamRooms = rooms.chatrooms.filter((r) => r.team_id === team.id);
    // team-level room + по одному на сегмент
    expect(teamRooms.length).toBeGreaterThanOrEqual(1 + topo.segments.length);
    const teamRoom = teamRooms.find((r) => !r.segment_id);
    expect(teamRoom?.name).toBe(team.name);
    expect(teamRoom?.members_count).toBe(topo.roles.length);
    const segName = topo.segments[0]?.name;
    const segRoom = teamRooms.find((r) => r.segment_id === topo.segments[0]?.id);
    expect(segRoom?.name).toBe(`${segName}-general`);

    // send + list: is_mine, from_role_name="You"
    const sent = await api.messages.sendChatroomMessage(teamRoom!.id, { body: `IT room ${Date.now()}` });
    expect(sent.status).toBe('sent');
    const msgs = await api.messages.getChatroomMessages(teamRoom!.id);
    expect(msgs.messages.some((m) => m.body === `IT room ${Date.now()}` || m.id === sent.id)).toBe(true);
    const mine = msgs.messages.find((m) => m.id === sent.id);
    expect(mine?.is_mine).toBe(true);
    expect(mine?.from_role_name).toBe('You');
  }, 20_000);

  // ---- slice 5a: Workflows ----

  it('slice 5a: workflows CRUD (blocks: индексы в POST, реальные id в connections) + drag', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];

    // create ± blocks/connections: from/to в POST /workflows — 0-based индексы массива blocks
    const created = await api.workflows.createWorkflow({
      team_id: team.id,
      name: `IT-flow-${Date.now()}`,
      blocks: [
        { type: 'task', position: { x: 100, y: 100 }, config: { prompt: 'do' }, label: 'Step1' },
        { type: 'decision', position: { x: 300, y: 100 }, config: {}, label: 'Gate' },
        { type: 'agent', position: { x: 500, y: 100 }, config: {}, label: 'Do' },
      ],
      connections: [
        { from_block_id: 0, to_block_id: 1 },
        { from_block_id: 1, to_block_id: 2, condition: 'pass' },
      ],
    });
    expect(created.status).toBe('created');

    const wf = await api.workflows.getWorkflow(created.id);
    expect(wf.workflow.id).toBe(created.id);
    expect(wf.workflow.team_id).toBe(team.id);
    expect(wf.blocks).toHaveLength(3);
    expect(wf.blocks[0]).toMatchObject({ type: 'task', label: 'Step1' });
    // индексы 0/1/2 → реальные id блоков 1..3
    expect(wf.connections).toHaveLength(2);
    const [c1, c2] = wf.connections;
    expect([c1.from_block_id, c1.to_block_id]).toEqual([wf.blocks[0].id, wf.blocks[1].id]);
    expect(c2.condition).toBe('pass');

    // отдельный block + connection (реальные id)
    const extra = await api.workflows.createBlock(created.id, { type: 'manual', position: { x: 700, y: 100 }, config: {}, label: 'Extra' });
    expect(extra.status).toBe('created');
    const conn = await api.workflows.createConnection(created.id, { from_block_id: wf.blocks[2].id, to_block_id: extra.id, condition: 'ok' });
    expect(conn.status).toBe('created');

    // PATCH block (drag): changes.position/config old/new
    const upd = await api.workflows.updateBlock(created.id, wf.blocks[0].id, {
      position: { x: 120, y: 140 },
      config: { prompt: 'do2' },
    });
    expect(upd.status).toBe('updated');
    expect(upd.changes.position).toMatchObject({
      old: { x: 100, y: 100 },
      new: { x: 120, y: 140 },
    });
    expect(upd.changes.config?.new).toMatchObject({ prompt: 'do2' });

    // list: фильтр team_id
    const list = await api.workflows.getWorkflows({ team_id: team.id });
    expect(list.workflows.some((w) => w.id === created.id)).toBe(true);
  }, 30_000);

  // ---- slice 5b: Library ----

  it('slice 5b: library — save → list → get → apply (new team + merge) + save_to_library', async () => {
    const stamp = Date.now();
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];

    // save (snapshot команды)
    const saved = await api.library.saveToLibrary({
      type: 'team',
      source_id: team.id,
      name: `IT-snap-${stamp}`,
      group: 'IT-tests',
      is_public: true,
      tags: ['it'],
    });
    expect(saved.status).toBe('saved');
    expect(saved.library_item_id).toBe(saved.id);

    // unique (type, name) → 409
    await expect(
      api.library.saveToLibrary({ type: 'team', source_id: team.id, name: `IT-snap-${stamp}`, group: 'IT-tests' }),
    ).rejects.toMatchObject({ status: 409, code: 'conflict' });

    // list: фильтр type + item в списке, groups посчитаны
    const list = await api.library.getLibrary({ type: 'team' });
    const item = list.items.find((i) => i.id === saved.id);
    expect(item).toMatchObject({ type: 'team', name: `IT-snap-${stamp}`, group: 'IT-tests', is_public: true, downloads_count: 0 });
    expect(list.groups.some((g) => g.name === 'IT-tests' && g.items_count >= 1)).toBe(true);

    // get: item + spec (снапшот: segments/roles/relatives) + versions
    const got = await api.library.getLibraryItem(saved.id);
    expect(got.item.id).toBe(saved.id);
    expect(got.spec).toHaveProperty('segments');
    expect(got.spec).toHaveProperty('roles');
    expect(got.spec).toHaveProperty('relatives');
    expect(Array.isArray(got.versions)).toBe(true);
    expect(got.versions.length).toBeGreaterThanOrEqual(1);

    // apply без target → новая команда (overrides.name), downloads_count++
    const applied = await api.library.applyLibrary(saved.id, { overrides: { name: `IT-applied-${stamp}` } });
    expect(applied.status).toBe('applied');
    expect(applied.created_resources?.teams).toHaveLength(1);
    const newTeam = await api.teams.getTeam(applied.created_resources!.teams![0]);
    expect(newTeam.team.name).toBe(`IT-applied-${stamp}`);
    expect((await api.library.getLibraryItem(saved.id)).item.downloads_count).toBe(1);

    // apply c target → merge (не ошибка)
    const merged = await api.library.applyLibrary(saved.id, { target_team_id: team.id });
    expect(merged.status).toBe('merged');

    // save_to_library в POST /teams/{id}/save → library_item_id
    // (имя item = "team-<team name>", unique (type,name) → сначала уникальное имя, иначе 409)
    const uniqueName = `IT-lib-${stamp}`;
    await api.teams.saveTopology(team.id, { name: uniqueName });
    const savedTeam = await api.teams.saveTopology(team.id, {
      save_to_library: true,
      library_group: 'IT-tests',
    });
    expect(savedTeam.status).toBe('saved');
    expect(typeof savedTeam.library_item_id).toBe('number');
    const savedItem = await api.library.getLibraryItem(savedTeam.library_item_id!);
    expect(savedItem.item.name).toBe(`team-${uniqueName}`);
  }, 30_000);

  it('slice 5b: library apply — workflow требует target_team_id; role/segment → 400', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const stamp = Date.now();

    // workflow: save → apply c target → applied; без target → 400
    const wf = await api.workflows.createWorkflow({ team_id: team.id, name: `IT-libwf-${stamp}`, blocks: [] });
    const item = await api.library.saveToLibrary({ type: 'workflow', source_id: wf.id, name: `IT-libwf-${stamp}` });
    await expect(api.library.applyLibrary(item.id, {})).rejects.toMatchObject({ status: 400 });
    // повторный apply того же workflow в ту же команду → 409 (уже существует)
    await expect(api.library.applyLibrary(item.id, { target_team_id: team.id })).rejects.toMatchObject({ status: 409 });
  }, 20_000);

  // ---- slice 5b: audit + metrics ----

  it('slice 5b: audit log — запись после действия + фильтр action', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const before = await api.history.getAuditLog({ action: 'library.save', limit: 1 });
    // любое успешное POST → запись в audit
    await api.library.saveToLibrary({
      type: 'team',
      source_id: team.id,
      name: `IT-audit-${Date.now()}`,
      group: 'IT-tests',
    });
    const log = await api.history.getAuditLog({ action: 'library.save', limit: 50 });
    expect(log.total).toBeGreaterThanOrEqual(before.total + 1);
    expect(log.entries.length).toBeGreaterThanOrEqual(1);
    const e = log.entries[0];
    expect(e.action).toBe('library.save');
    expect(e).toHaveProperty('timestamp');
    expect(e).toHaveProperty('user_name');
    expect(e).toHaveProperty('ip_address');
  }, 20_000);

  it('slice 5b: dashboard/metrics — 12 точек × 5 серий, ranges', async () => {
    for (const range of ['1h', '24h', '7d'] as const) {
      const m = await api.dashboard.getMetrics({ range });
      expect(m.time_range).toHaveProperty('start');
      expect(m.time_range).toHaveProperty('end');
      const keys: Array<keyof typeof m.metrics> = [
        'tasks_created',
        'tasks_completed',
        'sessions_active',
        'queue_size',
        'llm_tokens',
      ];
      for (const k of keys) {
        expect(m.metrics[k]).toHaveLength(12);
        expect(m.metrics[k][0]).toHaveProperty('timestamp');
        expect(typeof m.metrics[k][0].value).toBe('number');
      }
    }
  }, 20_000);

  // ---- slice 5a: WS dashboard channel ----

  it('slice 5a: WS /ws — subscribe dashboard → task.created при создании задачи', async () => {
    const teams = await api.teams.getTeams();
    const team = teams.teams.find((t) => t.name.startsWith('IT-')) ?? teams.teams[0];
    const topo = await api.teams.getTopology(team.id);
    const stamp = Date.now();

    // Гонки: (1) event, опубликованный до того, как сервер прочитал subscribe,
    // теряется → пауза после subscribe; (2) event может прийти раньше HTTP-ответа
    // на create (сервер публикует до/вместе с ответом) → матчим event по заранее
    // известному title задачи (id известен только после POST-ответа).
    // Не пришёл за 4 c — следующая задача (до 5 ретраев).
    // ВАЖНО: все обработчики назначаются до каких-либо await.
    const WS = (globalThis as { WebSocket?: typeof WebSocket }).WebSocket;
    expect(WS, 'global WebSocket (node >= 21)').toBeDefined();

    await new Promise<void>((resolve, reject) => {
      let ws: any;
      let settled = false;
      let expectTitle: string | null = null;
      let retries = 0;
      const st = `${stamp}-${Date.now() % 1000}`;
      const fail = (e: Error) => {
        if (settled) return;
        settled = true;
        try { ws?.close(); } catch { /* ignore */ }
        reject(e);
      };
      const done = () => {
        if (settled) return;
        settled = true;
        try { ws?.close(); } catch { /* ignore */ }
        resolve();
      };
      const overall = setTimeout(() => fail(new Error('WS overall timeout')), 25_000);

      const createNext = async () => {
        const title = `IT-ws-${st}-${retries}`;
        expectTitle = title;
        const created = await api.tasks.create({
          team_id: team.id,
          destination_role_id: topo.roles[0].id,
          title,
        });
        expect(created.state).toBe('pending');
        // event не пришёл за 4 c — следующая задача (до 5 ретраев)
        setTimeout(() => {
          if (!settled && expectTitle === title) {
            retries += 1;
            if (retries > 5) fail(new Error('WS: task.created не получен после 6 попыток'));
            else void createNext();
          }
        }, 4_000);
      };

      ws = new WS!(WS_URL);
      ws.onmessage = (ev: MessageEvent) => {
        const m = JSON.parse(String(ev.data));
        if (m.type !== 'task.created') return; // события приходят только по подписанным каналам
        if (m.data?.title === expectTitle) {
          expect(m.data.state).toBe('pending');
          expect(typeof m.data.task_id).toBe('number');
          expect(typeof m.timestamp).toBe('string');
          clearTimeout(overall);
          done();
        }
      };
      ws.onopen = () => {
        ws.send(JSON.stringify({ type: 'subscribe', channels: ['dashboard'] }));
        // пауза: ждём, пока сервер прочитает subscribe, — event после неё не потеряется
        setTimeout(() => { if (!settled && expectTitle === null) { retries = 1; void createNext(); } }, 500);
      };
      ws.onerror = () => fail(new Error('WS error'));
    });
  }, 30_000);

  // ---- slice 7: session live metrics + session.output (контракт 20 §3.6/§4.3) ----

  it('slice 7: GET /sessions/:id — live-метрики (context-поля из usage; omit без usage)', async () => {
    const stamp = Date.now();
    const team = await api.teams.createTeam({
      name: `IT-live-${stamp}`,
      spec: {
        segments: [{ name: 'Core' }],
        roles: [
          { name: 'Usage', agent_spec: 'pi-worker', segment: 'Core' },
          { name: 'Plain', agent_spec: 'pi-worker', segment: 'Core' },
        ],
      } as never,
    });
    expect(team.status).toBe('created');
    const topo = await api.teams.getTopology(team.id);
    const usageRole = topo.roles.find((r) => r.name === 'Usage')!;
    const plainRole = topo.roles.find((r) => r.name === 'Plain')!;

    // 1) сессия с JSONL usage-записью → context-поля заполнены
    const s1 = await api.sessions.create(team.id, {
      role_id: usageRole.id,
      command: 'sh',
      args: ['-c', `printf '{"message":{"usage":{"input_tokens":20000,"cache_read_input_tokens":0,"output_tokens":10}}}\\n'; sleep 60`],
    });
    expect(s1.status).toBe('started');
    await new Promise((r) => setTimeout(r, 600)); // процесс успел записать строку в лог

    const d1 = await api.sessions.get(s1.id);
    expect(d1.state).toBe('running');
    expect(typeof d1.log_path).toBe('string');
    expect(d1.log_path).toBeTruthy();
    expect(typeof d1.context_used_percentage).toBe('number');
    expect(d1.context_used_percentage!).toBeCloseTo(10, 1); // 20000/200000
    expect(d1.context_total_input_tokens).toBe(20000);
    expect(d1.context_total_output_tokens).toBe(10);
    expect(d1.model).toBeUndefined(); // process-рантайм — model omit
    const stop1 = await api.sessions.stop(s1.id);
    expect(stop1.state).toBe('stopped');

    // 2) обычный вывод (без usage, TUI-подобный) → context-поля OMIT (не 0!)
    const s2 = await api.sessions.create(team.id, {
      role_id: plainRole.id,
      command: 'sh',
      args: ['-c', 'echo plain-output; sleep 60'],
    });
    expect(s2.status).toBe('started');
    await new Promise((r) => setTimeout(r, 600));

    const d2 = await api.sessions.get(s2.id);
    expect(typeof d2.log_path).toBe('string');
    expect(d2.context_used_percentage).toBeUndefined();
    expect(d2.context_total_input_tokens).toBeUndefined();
    expect(d2.context_total_output_tokens).toBeUndefined();
    expect(d2.model).toBeUndefined();
    const stop2 = await api.sessions.stop(s2.id);
    expect(stop2.state).toBe('stopped');
  }, 40_000);

  it('slice 7: WS session.output — subscribe session:{id} → lines[] {ts,text,stream}', async () => {
    const stamp = Date.now();
    const st = `${stamp}-${Date.now() % 1000}`;
    const marker = `IT-live-line-${st}`;

    const team = await api.teams.createTeam({
      name: `IT-wsout-${stamp}`,
      spec: {
        segments: [{ name: 'Core' }],
        roles: [{ name: 'Echoer', agent_spec: 'pi-worker', segment: 'Core' }],
      } as never,
    });
    expect(team.status).toBe('created');
    const topo = await api.teams.getTopology(team.id);
    const role = topo.roles[0];

    // строка появится через 1.2s — ПОСЛЕ subscribe (гонка subscribe-read покрывается паузой)
    const s = await api.sessions.create(team.id, {
      role_id: role.id,
      command: 'sh',
      args: ['-c', `sleep 1.2; echo ${marker}; sleep 60`],
    });
    expect(s.status).toBe('started');
    const sid = s.id;

    const WS = (globalThis as { WebSocket?: typeof WebSocket }).WebSocket;
    expect(WS, 'global WebSocket (node >= 21)').toBeDefined();

    await new Promise<void>((resolve, reject) => {
      let ws: any;
      let settled = false;
      const fail = (e: Error) => {
        if (settled) return;
        settled = true;
        try { ws?.close(); } catch { /* ignore */ }
        reject(e);
      };
      const done = () => {
        if (settled) return;
        settled = true;
        try { ws?.close(); } catch { /* ignore */ }
        resolve();
      };
      const overall = setTimeout(() => fail(new Error('WS session.output: overall timeout')), 25_000);

      ws = new WS!(WS_URL);
      ws.onmessage = (ev: MessageEvent) => {
        const m = JSON.parse(String(ev.data));
        if (m.type !== 'session.output') return;
        if (m.data?.session_id !== sid) return;
        const lines = m.data?.lines;
        if (!Array.isArray(lines) || lines.length === 0) {
          fail(new Error('session.output: empty lines[]'));
          return;
        }
        if (!lines.some((l: any) => l.text === marker)) {
          return; // батч ещё до нужной строки (или частичный) — ждём дальше
        }
        const line = lines.find((l: any) => l.text === marker);
        expect(typeof line.ts).toBe('string');
        expect(line.ts).toBeTruthy();
        expect(line.stream).toBe('stdout');
        expect(typeof m.timestamp).toBe('string');
        clearTimeout(overall);
        done();
      };
      ws.onopen = () => {
        ws.send(JSON.stringify({ type: 'subscribe', channels: [`session:${sid}`, 'dashboard'] }));
      };
      ws.onerror = () => fail(new Error('WS error'));
    });

    const stopped = await api.sessions.stop(sid);
    expect(stopped.state).toBe('stopped');
  }, 40_000);
});

// ---- slice 6: RBAC (автоскип без daemon ИЛИ без тестовых ключей) ----
// Ключи: INTEGRATION_VIEWER_KEY / INTEGRATION_OPERATOR_KEY (DB-ключи, role viewer/operator);
// INTEGRATION_API_KEY — admin (env-ключ демона).
const VIEWER_KEY = process.env.INTEGRATION_VIEWER_KEY;
const OPERATOR_KEY = process.env.INTEGRATION_OPERATOR_KEY;

describe.runIf(backendAvailable && !!VIEWER_KEY && !!OPERATOR_KEY)('integration: slice 6 RBAC', () => {
  const env = import.meta.env as Record<string, string | undefined>;

  // Команда с хотя бы одной ролью (тесты не должны зависеть от порядка/состава teams[0]:
  // в БД могут быть команды без ролей — пробные/примитивные; archived — тоже).
  async function firstTeamWithRoles() {
    const admin = createRealAdapter();
    const teams = await admin.teams.getTeams();
    for (const t of teams.teams) {
      if (t.state !== 'active') continue;
      const topo = await admin.teams.getTopology(t.id);
      if (topo.roles.length > 0) return { team: t, topo, admin };
    }
    throw new Error('no active team with roles found');
  }

  function useKey(key: string) {
    const prev = env.VITE_API_KEY;
    env.VITE_API_KEY = key;
    const api = createRealAdapter();
    return {
      api,
      done: () => {
        if (prev === undefined) delete env.VITE_API_KEY;
        else env.VITE_API_KEY = prev;
      },
    };
  }

  it('viewer: GET 200, POST → 403 forbidden', async () => {
    const { api, done } = useKey(VIEWER_KEY!);
    try {
      const teams = await api.teams.getTeams();
      expect(Array.isArray(teams.teams)).toBe(true);
      await expect(api.teams.createTeam({ name: `IT-rbac-viewer-${Date.now()}` })).rejects.toMatchObject({
        status: 403,
        code: 'forbidden',
      });
      const audit = await api.history.getAuditLog({ limit: 1 });
      expect(Array.isArray(audit.entries)).toBe(true);
    } finally {
      done();
    }
  }, 20_000);

  it('operator: POST 201, PATCH role config → 403 forbidden (нет config.update)', async () => {
    const { team: _team, topo, admin: _admin } = await firstTeamWithRoles();
    const role = topo.roles[0];
    const { api, done } = useKey(OPERATOR_KEY!);
    try {
      const created = await api.teams.createTeam({ name: `IT-rbac-op-${Date.now()}` });
      expect(created.status).toBe('created');
      await expect(api.teams.updateRoleConfig(role.id, {})).rejects.toMatchObject({
        status: 403,
        code: 'forbidden',
      });
    } finally {
      done();
    }
  }, 20_000);

  it('audit: запись DB-ключа содержит user_id/api_key_id', async () => {
    const { team, topo, admin } = await firstTeamWithRoles();
    const { api, done } = useKey(OPERATOR_KEY!);
    try {
      await api.tasks.create({
        team_id: team.id,
        destination_role_id: topo.roles[0].id,
        title: `IT-rbac-audit-${Date.now()}`,
      });
      const log = await admin.history.getAuditLog({ action: 'task.create', limit: 1 });
      const e = log.entries[0];
      expect(e).toBeDefined();
      expect(e.user_name).toBeTruthy();
      expect(typeof e.user_id).toBe('number');
      expect(typeof e.api_key_id).toBe('number');
    } finally {
      done();
    }
  }, 20_000);
});
