import { describe, expect, it, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';
import { api } from '../src/api';
import SpecModal from '../src/components/SpecModal';
import { RoleNode } from '../src/components/Topology/nodes/RoleNode';
import { ReactFlowProvider } from '@xyflow/react';
import ConfigPanel from '../src/components/TeamBuilder/ConfigPanel';
import { useEscapeStack } from '../src/hooks/useEscapeStack';
import { hasExcludedOverlay } from '../src/lib/hotkeys';
import type { Role } from '../src/types/api';

// W3 — UX wave 3: hotkeys (K/ Esc / ?), Specs surface (SpecModal), роли в ContextTree.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

const PH = /type a command or search/;

describe('W3.1: Hotkeys', () => {
  beforeEach(() => {
    resetMockDb();
    localStorage.clear();
  });

  it('K (вне input, нет оверлеев) открывает палитру', async () => {
    renderAt('/');
    await screen.findByRole('navigation', { name: 'Main' }, { timeout: 10_000 });
    fireEvent.keyDown(document.body, { key: 'k' });
    expect(await screen.findByPlaceholderText(PH, {}, { timeout: 5000 })).toBeInTheDocument();
  }, 20_000);

  it('Shift+K открывает палитру', async () => {
    renderAt('/');
    await screen.findByRole('navigation', { name: 'Main' }, { timeout: 10_000 });
    fireEvent.keyDown(document.body, { key: 'K', shiftKey: true });
    expect(await screen.findByPlaceholderText(PH, {}, { timeout: 5000 })).toBeInTheDocument();
  }, 20_000);

  it('K в фокусе input НЕ открывает палитру', async () => {
    renderAt('/teams?new=1');
    // форма создания команды — есть input name
    const input = await screen.findByPlaceholderText('e.g. Dev Team', {}, { timeout: 10_000 });
    fireEvent.keyDown(input, { key: 'k' });
    await waitFor(() => {
      expect(screen.queryByPlaceholderText(PH)).not.toBeInTheDocument();
    }, { timeout: 1500 });
  }, 20_000);

  it('палитра открыта → оверлей-исключение на месте (data-hotkey-exclude)', async () => {
    renderAt('/');
    await screen.findByRole('navigation', { name: 'Main' }, { timeout: 10_000 });
    fireEvent.keyDown(document.body, { key: 'k' });
    const input = await screen.findByPlaceholderText(PH, {}, { timeout: 5000 });
    const overlay = input.closest('.palette-overlay') as HTMLElement;
    expect(overlay).toHaveAttribute('data-hotkey-exclude');
    // hasExcludedOverlay() видит его
    expect(hasExcludedOverlay()).toBe(true);
  }, 20_000);

  it('hasExcludedOverlay: без оверлеев — false', () => {
    document.body.innerHTML = '';
    expect(hasExcludedOverlay()).toBe(false);
    const el = document.createElement('div');
    el.setAttribute('data-hotkey-exclude', '');
    document.body.appendChild(el);
    expect(hasExcludedOverlay()).toBe(true);
    el.remove();
  });

  it('Esc закрывает верхний оверлей (стек: нижний не закрывается)', () => {
    // юнит-тест useEscapeStack: A под B; Esc → закрывается только B
    const hits: string[] = [];
    function A() {
      useEscapeStack(() => hits.push('a'), true);
      return null;
    }
    function B() {
      useEscapeStack(() => hits.push('b'), true);
      return null;
    }
    const { unmount } = render(
      <div>
        <A />
        <B />
      </div>,
    );
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(hits).toEqual(['b']);
    unmount();
    // после unmount B — Esc доходит до A
    const { unmount: unmount2 } = render(<A />);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(hits).toEqual(['b', 'a']);
    unmount2();
  });

  it('Esc закрывает палитру (глобально, не только из input)', async () => {
    renderAt('/');
    await screen.findByRole('navigation', { name: 'Main' }, { timeout: 10_000 });
    fireEvent.keyDown(document.body, { key: 'k' });
    await screen.findByPlaceholderText(PH, {}, { timeout: 5000 });
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByPlaceholderText(PH)).not.toBeInTheDocument(), { timeout: 5000 });
  }, 20_000);

  it('«?» на Dashboard — подсказка hotkeys; dismiss по клику + localStorage', async () => {
    renderAt('/');
    await screen.findByRole('heading', { name: 'Dashboard' }, { timeout: 15_000 });
    // не открыта
    expect(screen.queryByTestId('hotkeys-hint')).toBeNull();
    // «?» открывает
    fireEvent.keyDown(document.body, { key: '?' });
    const hint = await screen.findByTestId('hotkeys-hint', {}, { timeout: 5000 });
    expect(hint).toBeInTheDocument();
    expect(screen.getByText('K', { selector: 'kbd' })).toBeInTheDocument();
    // dismiss по кнопке
    fireEvent.click(screen.getByTestId('hotkeys-hint-dismiss'));
    await waitFor(() => expect(screen.queryByTestId('hotkeys-hint')).not.toBeInTheDocument(), { timeout: 5000 });
    expect(localStorage.getItem('hotkeys-hint.dismissed')).toBe('1');
  }, 20_000);

  it('dismissed-подсказка больше не показывается (localStorage)', async () => {
    localStorage.setItem('hotkeys-hint.dismissed', '1');
    renderAt('/');
    await screen.findByRole('heading', { name: 'Dashboard' }, { timeout: 15_000 });
    fireEvent.keyDown(document.body, { key: '?' });
    await waitFor(() => {
      expect(screen.queryByTestId('hotkeys-hint')).toBeNull();
    }, { timeout: 1500 });
  }, 20_000);

  it('placeholder палитры — «type a command or search… (K)»', async () => {
    renderAt('/');
    await screen.findByRole('navigation', { name: 'Main' }, { timeout: 10_000 });
    fireEvent.keyDown(document.body, { key: 'k' });
    const input = await screen.findByPlaceholderText(PH, {}, { timeout: 5000 });
    const ph = input.getAttribute('placeholder') ?? '';
    expect(ph).toMatch(/type a command or search/);
    expect(ph).toMatch(/\(K/);
  }, 20_000);
});

describe('W3.2: Specs surface (read-only)', () => {
  beforeEach(() => resetMockDb());

  it('SpecModal: yaml в <pre>, CTA Download + Copy (мока p-i-lead)', async () => {
    render(<SpecModal name="pi-lead" onClose={() => {}} />);
    const pre = await screen.findByTestId('spec-yaml', {}, { timeout: 5000 });
    expect(pre.textContent).toContain('name: pi-lead');
    expect(pre.textContent).toContain('model: claude-3-7-sonnet');
    expect(screen.getByTestId('spec-download')).toBeInTheDocument();
    expect(screen.getByTestId('spec-copy')).toBeInTheDocument();
  }, 15_000);

  it('SpecModal: неизвестный spec → 404-состояние «не найден»', async () => {
    render(<SpecModal name="no-such-spec" onClose={() => {}} />);
    const err = await screen.findByTestId('spec-error', {}, { timeout: 5000 });
    expect(err.textContent).toContain('не найден');
    expect(screen.queryByTestId('spec-yaml')).toBeNull();
  }, 15_000);

  it('RoleNode: иконка spec (role.agent_spec задан) → клик вызывает onOpenSpec(name)', () => {
    const onOpenSpec = vi.fn();
    const role: Role = {
      id: 7, team_id: 1, segment_id: 1, segment_name: 'Backend',
      name: 'Lead', address: 't1:Backend.Lead', agent_spec: 'pi-go-backend',
      state: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    render(
      <ReactFlowProvider>
        <RoleNode
          {...{ id: 'role:7', type: 'role', data: { role, selected: false, editMode: false, onOpenSpec } } as unknown as Parameters<typeof RoleNode>[0]}
        />
      </ReactFlowProvider>,
    );
    const btn = screen.getByTestId('role-node-spec');
    fireEvent.click(btn);
    expect(onOpenSpec).toHaveBeenCalledWith('pi-go-backend');
    cleanup();
  });

  it('RoleNode: без onOpenSpec иконки нет', () => {
    const role: Role = {
      id: 8, team_id: 1, segment_id: 1, segment_name: 'Backend',
      name: 'Worker', address: 't1:Backend.Worker', agent_spec: 'pi-go-worker',
      state: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    render(
      <ReactFlowProvider>
        <RoleNode
          {...{ id: 'role:8', type: 'role', data: { role, selected: false, editMode: false } } as unknown as Parameters<typeof RoleNode>[0]}
        />
      </ReactFlowProvider>,
    );
    expect(screen.queryByTestId('role-node-spec')).toBeNull();
    cleanup();
  });

  it('ConfigPanel: кнопка «Open spec» → onOpenSpec с текущим значением input', async () => {
    const onOpenSpec = vi.fn();
    const role: Role = {
      id: 1, team_id: 1, segment_id: 1, segment_name: 'Backend',
      name: 'Lead', address: 't1:Backend.Lead', agent_spec: 'pi-go-backend',
      state: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    };
    render(
      <ConfigPanel
        selection={{ type: 'role', id: 1, name: 'Lead' }}
        role={role}
        onClose={() => {}}
        onChanged={() => {}}
        onOpenSpec={onOpenSpec}
      />,
    );
    // input заполняется из role config (pi-go-backend)
    const input = (await screen.findByDisplayValue('pi-go-backend', {}, { timeout: 10_000 })) as HTMLInputElement;
    expect(input).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('cfg-open-spec'));
    expect(onOpenSpec).toHaveBeenCalledWith('pi-go-backend');
  }, 20_000);

  it('FleetPanel (Dashboard): иконка spec у роли с agent_spec', async () => {
    renderAt('/');
    // строки fleet появляются после загрузки; иконка — у ролей с agent_spec (Lead: pi-go-backend)
    const btns = await screen.findAllByTestId('fleet-spec-btn', {}, { timeout: 15_000 });
    expect(btns.length).toBeGreaterThanOrEqual(1);
    const titles = btns.map((b) => b.getAttribute('title') ?? '');
    expect(titles.some((t) => /pi-go-backend/.test(t))).toBe(true);
  }, 30_000);
});

describe('W3.3: Роли в ContextTree (3-й уровень)', () => {
  beforeEach(() => {
    resetMockDb();
    localStorage.clear();
    // по умолчанию ContextTree развёрнуто (широкий экран — jsdom width 1024? narrow = max-width 1200 → свёрнуто!)
    localStorage.setItem('context-tree.collapsed', '0');
  });

  async function expandTeam(projectName: string, teamName: string) {
    const projLinks = await screen.findAllByText(projectName, {}, { timeout: 10_000 });
    const projRow = projLinks[0].closest('.ctx-tree-proj') as HTMLElement;
    fireEvent.click(projRow.querySelector('.ctx-tree-toggle') as HTMLButtonElement);
    // lazy-команды
    await waitFor(() => {
      expect(projRow.querySelector('.ctx-tree-link--team')).toBeTruthy();
    }, { timeout: 10_000 });
    const teamLink = Array.from(projRow.querySelectorAll('.ctx-tree-link--team')).find(
      (el) => el.textContent === teamName,
    ) as HTMLElement;
    const teamRow = teamLink.closest('.ctx-tree-row--team') as HTMLElement;
    fireEvent.click(teamRow.querySelector('.ctx-tree-toggle') as HTMLButtonElement);
    return projRow;
  }

  it('раскрытие команды → роли (Lead/Worker/Worker2/Reviewer) со ссылками на /teams/1', async () => {
    renderAt('/');
    const projRow = await expandTeam('team-console', 'Dev Team');
    const rolesUl = projRow.querySelector('.ctx-tree-roles') as HTMLElement;
    expect(rolesUl).toBeTruthy();
    await waitFor(() => {
      expect(projRow.querySelector('[data-testid="ctx-role-1"]')).toBeTruthy();
    }, { timeout: 10_000 });
    const names = Array.from(projRow.querySelectorAll('.ctx-tree-rolename')).map((el) => el.textContent);
    expect(names).toContain('Lead');
    expect(names).toContain('Worker');
    expect(names).toContain('Worker2');
    expect(names).toContain('Reviewer');
    // ссылка роли → /teams/{id}
    const leadLink = projRow.querySelector('[data-testid="ctx-role-1"]') as HTMLAnchorElement;
    expect(leadLink.getAttribute('href')).toBe('/teams/1');
  }, 30_000);

  it('live-точки ролей: Lead/Worker2 running, Reviewer (idle) — active', async () => {
    renderAt('/');
    const projRow = await expandTeam('team-console', 'Dev Team');
    await waitFor(() => {
      expect(projRow.querySelector('[data-testid="ctx-role-1"] .ctx-dot--running')).toBeTruthy();
    }, { timeout: 10_000 });
    // мока: role3 (Worker2) — running-сессия; role4 (Reviewer) — idle-сессия → 'active'
    expect(projRow.querySelector('[data-testid="ctx-role-3"] .ctx-dot--running')).toBeTruthy();
    expect(projRow.querySelector('[data-testid="ctx-role-4"] .ctx-dot--active')).toBeTruthy();
  }, 30_000);

  it('bounded: >20 ролей → показывает 20 + «+N ещё»', async () => {
    const orig = api.teams.getTeam;
    const many: Role[] = Array.from({ length: 25 }, (_, i) => ({
      id: i + 1, team_id: 1, segment_id: 1, segment_name: 'S',
      name: `Role${i + 1}`, address: `t1:S.Role${i + 1}`, agent_spec: 'pi-worker',
      state: 'active', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }));
    vi.spyOn(api.teams, 'getTeam').mockImplementation(async (id: number) => {
      if (id === 1) return { team: { id: 1 } as never, segments: [], roles: many, relatives: [] };
      return orig(id);
    });
    try {
      renderAt('/');
      const projRow = await expandTeam('team-console', 'Dev Team');
      await waitFor(() => {
        expect(projRow.querySelector('.ctx-tree-more')).toBeTruthy();
      }, { timeout: 10_000 });
      const roleLinks = projRow.querySelectorAll('.ctx-tree-link--role').length;
      expect(roleLinks).toBe(20);
      expect(projRow.querySelector('.ctx-tree-more-link')?.textContent).toBe('+5 ещё');
    } finally {
      vi.restoreAllMocks();
    }
  }, 30_000);

  it('команда без раскрытия → роли не загружаются (lazy)', async () => {
    renderAt('/');
    const projLinks = await screen.findAllByText('team-console', {}, { timeout: 10_000 });
    const projRow = projLinks[0].closest('.ctx-tree-proj') as HTMLElement;
    fireEvent.click(projRow.querySelector('.ctx-tree-toggle') as HTMLButtonElement);
    await waitFor(() => {
      expect(projRow.querySelector('.ctx-tree-link--team')).toBeTruthy();
    }, { timeout: 10_000 });
    // команды раскрыты, но роли НЕ запрашивались
    expect(projRow.querySelector('.ctx-tree-roles')).toBeNull();
  }, 30_000);
});
