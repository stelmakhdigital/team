import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P0.2: Overdue / дедлайны задач — вкладка Overdue, дедлайн-колонка, OVERDUE-бейдж,
// деталка (Deadline), Attention (task_overdue).
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P0.2: Overdue / дедлайны задач', () => {
  beforeEach(() => resetMockDb());

  it('TasksPage shows an "Overdue" tab', async () => {
    renderAt('/tasks');
    expect(await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByRole('tab', { name: /Overdue/ }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('TasksPage "All" view marks an overdue task with an OVERDUE badge in the Due column', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 });
    // mock has overdue in_progress/blocked tasks (#1 in_progress, #4 blocked) → OVERDUE badges
    expect((await screen.findAllByText('OVERDUE', {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
  }, 20_000);

  it('TasksPage "Overdue" tab lists only overdue tasks (excludes within-deadline ones)', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 });
    const tab = await screen.findByRole('tab', { name: /Overdue/ }, { timeout: 10_000 });
    fireEvent.click(tab);
    // an overdue task (#1, in_progress, deadline passed) is listed
    expect(await screen.findByText('Implement auth middleware', {}, { timeout: 10_000 })).toBeInTheDocument();
    // a within-deadline task (#3, in_progress, deadline in the future) is NOT listed
    expect(screen.queryByText('Review PR #142')).not.toBeInTheDocument();
  }, 20_000);

  it('TaskDetailPage shows the closure deadline (+ OVERDUE when passed)', async () => {
    renderAt('/tasks/1');
    expect(await screen.findByRole('heading', { name: /Implement auth middleware/ }, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getAllByText('Deadline').length).toBeGreaterThanOrEqual(1);
    // task #1 is overdue → OVERDUE shown next to the deadline
    expect((await screen.findAllByText('OVERDUE', {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
  }, 20_000);

  it('AttentionPage renders a task_overdue item with the "Открыть задачу" action', async () => {
    renderAt('/attention');
    expect(await screen.findByRole('heading', { name: 'Внимание' }, { timeout: 10_000 })).toBeInTheDocument();
    // task #1 (in_progress, deadline passed) is emitted as task_overdue
    expect(await screen.findByText('Открыть задачу', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
