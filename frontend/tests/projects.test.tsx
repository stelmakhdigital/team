import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P1.0: Projects (мультипроектность) — список, создание existing/new, archive,
// деталка, селект в team-форме, фильтр задач по проекту.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

// W2.1: ContextTree в сайдбаре дублирует имена проектов → проверки списка
// проектов скупаются в .content (основной контент, без сайдбара).
function content() {
  const el = document.querySelector('.content') as HTMLElement | null;
  if (!el) throw new Error('.content not found');
  return el;
}

describe('P1.0: Projects page', () => {
  beforeEach(() => resetMockDb());

  it('lists active projects (archived hidden by default)', async () => {
    renderAt('/projects');
    expect(await screen.findByRole('heading', { name: 'Projects' }, { timeout: 10_000 })).toBeInTheDocument();
    const c = content();
    await waitFor(() => expect(within(c).getByText('team-console')).toBeInTheDocument(), { timeout: 10_000 });
    expect(within(c).getByText('data-pipeline')).toBeInTheDocument();
    // архивный проект скрыт по умолчанию (в основном контенте)
    expect(within(c).queryByText('scratch-experiment')).not.toBeInTheDocument();
  }, 20_000);

  it('show-archived toggle reveals archived projects', async () => {
    renderAt('/projects');
    await screen.findByRole('heading', { name: 'Projects' }, { timeout: 10_000 });
    const toggle = await screen.findByLabelText('show archived');
    fireEvent.click(toggle);
    expect(await screen.findByText('scratch-experiment', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('creates an existing project via the modal', async () => {
    renderAt('/projects');
    await screen.findByRole('heading', { name: 'Projects' }, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: '+ New project' }));
    // kind=existing (default)
    fireEvent.change(screen.getByPlaceholderText('my-project'), { target: { value: 'new-proj' } });
    fireEvent.change(screen.getByPlaceholderText('/abs/path/to/dir'), { target: { value: '/tmp/new-proj' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('new-proj', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('creates a new project (parent_path)', async () => {
    renderAt('/projects');
    await screen.findByRole('heading', { name: 'Projects' }, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: '+ New project' }));
    // переключить на kind=new (на странице есть и dev-select «Simulate error» — берём тот, что 'existing')
    const kindSelect = screen
      .getAllByRole('combobox')
      .find((s) => (s as HTMLSelectElement).value === 'existing') as HTMLSelectElement;
    fireEvent.change(kindSelect, { target: { value: 'new' } });
    fireEvent.change(screen.getByPlaceholderText('my-project'), { target: { value: 'fresh-proj' } });
    fireEvent.change(screen.getByPlaceholderText('/abs/parent/dir'), { target: { value: '/tmp' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('fresh-proj', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('archives a project', async () => {
    renderAt('/projects');
    await screen.findByRole('heading', { name: 'Projects' }, { timeout: 10_000 });
    await waitFor(() => expect(within(content()).getByText('team-console')).toBeInTheDocument(), { timeout: 10_000 });
    fireEvent.click(screen.getAllByRole('button', { name: 'Archive' })[0]);
    // проект скрыт из активного списка (архивный) — в основном контенте
    await waitFor(() => expect(within(content()).queryByText('team-console')).not.toBeInTheDocument(), { timeout: 10_000 });
  }, 20_000);
});

describe('P1.0: Project detail + bindings', () => {
  beforeEach(() => resetMockDb());

  it('shows project detail with counts + related team', async () => {
    renderAt('/projects/1');
    expect((await screen.findAllByText('team-console', {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
    // related team (Dev Team → project 1)
    expect(await screen.findByText('Dev Team', {}, { timeout: 10_000 })).toBeInTheDocument();
    // counts строка
    expect(screen.getAllByText(/teams · /).length).toBeGreaterThanOrEqual(1);
  }, 20_000);

  it('team create form has a project selector', async () => {
    renderAt('/teams');
    await screen.findByRole('heading', { name: 'Teams' }, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: '+ New team' }));
    expect(await screen.findByText('Project (optional)', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('tasks page has a project filter', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 });
    expect(await screen.findByText('Project', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('All projects')).toBeInTheDocument();
  }, 20_000);
});

describe('P4: Project artifacts + agent specs', () => {
  beforeEach(() => resetMockDb());

  it('project detail shows an artifacts tree (top level, lazy expand)', async () => {
    renderAt('/projects/1');
    expect(await screen.findByText('Artifacts (файлы проекта)', {}, { timeout: 10_000 })).toBeInTheDocument();
    // верхний уровень: README.md + каталоги docs/artifacts (без вложенных файлов)
    expect(await screen.findByText('README.md', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('docs')).toBeInTheDocument();
    expect(screen.getByText('artifacts')).toBeInTheDocument();
    // вложенные файлы НЕ показаны до раскрытия
    expect(screen.queryByText('report-2026-10-12.md')).not.toBeInTheDocument();
    // раскрыть каталог artifacts (последний на верхнем уровне) → lazy-список внутри
    const expandBtns = screen.getAllByRole('button', { name: 'Expand' });
    expect(expandBtns.length).toBe(2); // docs + artifacts
    fireEvent.click(expandBtns[expandBtns.length - 1]);
    expect(await screen.findByText('report-2026-10-12.md', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('schema.sql')).toBeInTheDocument();
  }, 20_000);

  it('artifact preview opens content in a modal', async () => {
    renderAt('/projects/1');
    await screen.findByText('Artifacts (файлы проекта)', {}, { timeout: 10_000 });
    // на верхнем уровне один файл (README.md) → одна кнопка Preview
    const previewBtns = await screen.findAllByRole('button', { name: 'Preview' }, { timeout: 10_000 });
    expect(previewBtns.length).toBe(1);
    fireEvent.click(previewBtns[0]);
    // превью показывает содержимое mock-файла
    expect(await screen.findByText(/Mock artifact content/, {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('agent spec field is backed by GET /agentspecs (mock)', async () => {
    const { createMockAdapter } = await import('../src/api/mock/adapter');
    const api = createMockAdapter();
    const res = await api.teams.listAgentSpecs();
    expect(res.specs).toContain('pi-lead');
    expect(res.specs).toContain('pi-worker');
  }, 20_000);
});
