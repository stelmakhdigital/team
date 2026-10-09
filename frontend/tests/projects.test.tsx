import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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

describe('P1.0: Projects page', () => {
  beforeEach(() => resetMockDb());

  it('lists active projects (archived hidden by default)', async () => {
    renderAt('/projects');
    expect(await screen.findByRole('heading', { name: 'Projects' }, { timeout: 10_000 })).toBeInTheDocument();
    // активные проекты из seed
    expect(await screen.findByText('team-console', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('data-pipeline')).toBeInTheDocument();
    // архивный проект скрыт по умолчанию
    expect(screen.queryByText('scratch-experiment')).not.toBeInTheDocument();
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
    await screen.findByText('team-console', {}, { timeout: 10_000 });
    fireEvent.click(screen.getAllByRole('button', { name: 'Archive' })[0]);
    // проект скрыт из активного списка (archived)
    await waitFor(() => expect(screen.queryByText('team-console')).not.toBeInTheDocument(), { timeout: 10_000 });
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
