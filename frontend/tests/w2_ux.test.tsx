import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// W2 — UX wave 2: ContextTree, Artifacts attribution, Attention groups, first-run.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

// helper: развернуть ветку проекта в ContextTree (клик по toggle строки проекта)
async function expandProject(name: string): Promise<HTMLElement> {
  const projLinks = await screen.findAllByText(name, {}, { timeout: 10_000 });
  const projRow = projLinks[0].closest('.ctx-tree-proj') as HTMLElement;
  const toggle = projRow.querySelector('.ctx-tree-toggle') as HTMLButtonElement;
  fireEvent.click(toggle);
  return projRow;
}

describe('W2.1: ContextTree — Projects → Teams в сайдбаре', () => {
  beforeEach(() => {
    resetMockDb();
    localStorage.clear();
  });

  it('shows active projects (team-console, data-pipeline), archived hidden', async () => {
    renderAt('/');
    await screen.findByTestId('context-tree', {}, { timeout: 10_000 });
    // оба active-проекта есть
    expect(await screen.findByText('team-console', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('data-pipeline')).toBeInTheDocument();
    // archived-проект скрыт
    expect(screen.queryByText('scratch-experiment')).toBeNull();
  }, 20_000);

  it('expands project → Dev Team с running-точкой (green)', async () => {
    renderAt('/');
    const projRow = await expandProject('team-console');
    // после lazy-загрузки команд — строка Dev Team с running-точкой
    await waitFor(() => {
      expect(projRow.querySelector('.ctx-tree-link--team')).toBeTruthy();
      expect(projRow.querySelector('.ctx-dot--running')).toBeTruthy();
    }, { timeout: 10_000 });
  }, 20_000);

  it('empty project shows «—»', async () => {
    renderAt('/');
    // data-pipeline — проект без команд
    const projRow = await expandProject('data-pipeline');
    await waitFor(() => {
      expect(projRow.querySelector('.ctx-tree-empty')).toBeTruthy();
    }, { timeout: 10_000 });
  }, 20_000);

  it('failed session → red dot', async () => {
    renderAt('/');
    // Dev Team (project 1): все его сессии failed
    const db = getDb();
    db.sessions.forEach((s) => {
      if (s.team_id === 1) s.state = 'failed';
    });
    const projRow = await expandProject('team-console');
    await waitFor(() => {
      expect(projRow.querySelector('.ctx-dot--failed')).toBeTruthy();
    }, { timeout: 10_000 });
  }, 20_000);

  it('bounded: >10 projects → «+N ещё»', async () => {
    const db = getDb();
    // добавим ещё 10 active-проектов (итого 12)
    for (let i = 0; i < 10; i++) {
      db.projects.push({
        id: 100 + i,
        name: `extra-proj-${i}`,
        path: `/tmp/extra-proj-${i}`,
        kind: 'existing',
        git: false,
        archived: false,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      });
    }
    renderAt('/');
    await screen.findByText('+2 ещё', {}, { timeout: 10_000 });
  }, 20_000);

  it('collapse toggles + persists to localStorage', async () => {
    renderAt('/');
    await screen.findByText('team-console', {}, { timeout: 10_000 });
    const toggle = screen.getByTestId('context-tree-toggle');
    // свернуть
    fireEvent.click(toggle);
    // после сворачивания проекты исчезают
    await waitFor(() => expect(screen.queryByText('team-console')).toBeNull());
    expect(localStorage.getItem('context-tree.collapsed')).toBe('1');
    // развернуть обратно
    fireEvent.click(screen.getByTestId('context-tree-toggle'));
    await screen.findByText('team-console', {}, { timeout: 10_000 });
    expect(localStorage.getItem('context-tree.collapsed')).toBe('0');
  }, 20_000);

  it('current location highlight (/projects/1 → team-console active)', async () => {
    renderAt('/projects/1');
    const sidebar = document.querySelector('.sidebar') as HTMLElement;
    // ждём ссылку проекта в сайдбаре (ContextTree)
    const [link] = await within(sidebar).findAllByRole('link', { name: 'team-console' }, { timeout: 10_000 });
    expect(link.classList.contains('active')).toBe(true);
  }, 20_000);

  it('empty (no projects) → «Нет проектов» + CTA', async () => {
    const db = getDb();
    db.projects = [];
    renderAt('/');
    await screen.findByText('Нет проектов', {}, { timeout: 10_000 });
    expect(screen.getByTestId('context-tree-create')).toBeInTheDocument();
  }, 20_000);
});

describe('W2.2: Artifacts — git-attribution', () => {
  beforeEach(() => resetMockDb());

  it('shows attribution badge (git) + author line under file name', async () => {
    const { container } = renderAt('/projects/1');
    // заголовок attribution: git (mock project git=true, attribute=true)
    expect(await screen.findByText('attribution: git', {}, { timeout: 10_000 })).toBeInTheDocument();
    // у файла README.md — строка атрибуции с автором (dev@team-console)
    const attr = await screen.findByText(/dev@team-console/, {}, { timeout: 10_000 });
    expect(attr).toBeInTheDocument();
    expect(attr.closest('.artifacts-attr')).toBeTruthy();
    void container;
  }, 20_000);

  it('non-git project → «attribution: нет git-данных» и без author-строк', async () => {
    const db = getDb();
    // project 1 — не-git
    const p = db.projects.find((x) => x.id === 1)!;
    p.git = false;
    const { container } = renderAt('/projects/1');
    expect(await screen.findByText('attribution: нет git-данных', {}, { timeout: 10_000 })).toBeInTheDocument();
    // author-строк нет
    expect(container.querySelector('.artifacts-attr')).toBeNull();
  }, 20_000);
});

describe('W2.3: Attention — группы «Решения» / «События»', () => {
  beforeEach(() => resetMockDb());

  it('renders both groups (1 human_request + events) with counts', async () => {
    renderAt('/attention');
    // группа Решения (human_request → Approve/Deny)
    const dec = await screen.findByTestId('attention-group-decisions', {}, { timeout: 10_000 });
    expect(dec).toBeInTheDocument();
    const decTitle = dec.querySelector('.attention-group-title')!;
    expect(decTitle.textContent).toContain('Решения');
    // CTA approve/deny в группе решений
    expect(dec.textContent).toMatch(/Approve|Approve/);
    // группа События (task_blocked/overdue)
    const ev = await screen.findByTestId('attention-group-events', {}, { timeout: 10_000 });
    expect(ev).toBeInTheDocument();
    expect(ev.querySelector('.attention-group-title')!.textContent).toContain('События');
  }, 20_000);

  it('empty group is hidden (no human_requests → no «Решения»)', async () => {
    const db = getDb();
    db.humanRequests = [];
    renderAt('/attention');
    // события есть
    await screen.findByTestId('attention-group-events', {}, { timeout: 10_000 });
    // группа решений скрыта
    expect(screen.queryByTestId('attention-group-decisions')).toBeNull();
  }, 20_000);
});

describe('W2.4: Dashboard first-run', () => {
  beforeEach(() => resetMockDb());

  it('no teams → first-run card with CTAs', async () => {
    const db = getDb();
    db.teams = [];
    renderAt('/');
    const card = await screen.findByTestId('dashboard-first-run', {}, { timeout: 10_000 });
    expect(card).toBeInTheDocument();
    expect(screen.getByTestId('firstrun-apply-starter')).toBeInTheDocument();
    expect(screen.getByTestId('firstrun-create-team')).toBeInTheDocument();
  }, 20_000);

  it('has teams → normal dashboard (no first-run card) [regression]', async () => {
    renderAt('/');
    await screen.findByText('Dashboard', {}, { timeout: 10_000 });
    // ждём, чтобы first-run не появился
    await new Promise((r) => setTimeout(r, 800));
    expect(screen.queryByTestId('dashboard-first-run')).toBeNull();
  }, 20_000);
});
