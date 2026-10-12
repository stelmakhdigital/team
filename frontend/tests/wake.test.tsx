import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { api } from '../src/api';
import { resetMockDb } from '../src/api/mock/data';

// P0.3: Block with wake — форма block, wake-панель (armed), resolve-flow, M2 (due-left).
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P0.3: Block with wake', () => {
  beforeEach(() => resetMockDb());

  it('TasksPage "block" opens a block-with-wake form (wake type + continuation)', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 });
    // in_progress/pending задачи имеют действие "block" (несколько строк — берём первую)
    const blockBtns = await screen.findAllByRole('button', { name: /⊘ block/ }, { timeout: 10_000 });
    fireEvent.click(blockBtns[0]);
    expect(await screen.findByRole('heading', { name: /Block task/ }, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText(/timer — after a duration/)).toBeInTheDocument();
    expect(screen.getByText(/how to continue/i)).toBeInTheDocument();
  }, 20_000);

  it('TaskDetailPage shows the armed wake panel + resolve flow (blocked → pending + history)', async () => {
    // заблокировать in_progress задачу #3 с timer-wake
    await api.tasks.updateState(3, { state: 'blocked', wake: { kind: 'timer', after: '1h', continuation: 'wait for CI' } });
    renderAt('/tasks/3');
    const panel = await screen.findByTestId('wake-panel', {}, { timeout: 10_000 });
    expect(panel).toBeInTheDocument();
    expect(screen.getByText('armed')).toBeInTheDocument();
    expect(screen.getByText(/wait for CI/)).toBeInTheDocument();
    expect(screen.getByText(/fires in/)).toBeInTheDocument();
    // resolve-flow
    fireEvent.change(screen.getByLabelText('Resolve decision'), { target: { value: 'dependency cleared' } });
    fireEvent.click(screen.getByTestId('wake-resolve-btn'));
    // запись resolve в историю
    expect(await screen.findByText(/resolve: dependency cleared/, {}, { timeout: 10_000 })).toBeInTheDocument();
    // wake закрыт (fired), панель осталась
    expect((await screen.findAllByText('fired', {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
  }, 30_000);

  it('M2: TasksPage Due column shows "left" for a within-deadline task', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 });
    // in_progress задача с будущим дедлайном (#3) → "N left"
    expect((await screen.findAllByText(/left/, {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
  }, 20_000);
});

describe('P0.3: resolve semantics (mock)', () => {
  beforeEach(() => resetMockDb());

  it('resolve of a non-blocked task → 409 conflict', async () => {
    // задача #2 pending (не blocked)
    await expect(api.tasks.resolve(2, { decision: 'x' })).rejects.toMatchObject({ status: 409 });
  });

  it('resolve without a decision → 400 validation', async () => {
    await api.tasks.updateState(3, { state: 'blocked' });
    await expect(api.tasks.resolve(3, { decision: '' })).rejects.toMatchObject({ status: 400 });
  });

  it('block with a task-wake: closing the referenced task unblocks (mock fires)', async () => {
    // #3 (in_progress) блокируется task-wake на #1
    await api.tasks.updateState(3, { state: 'blocked', wake: { kind: 'task', task_id: 1, continuation: 'resume after #1' } });
    let d = await api.tasks.get(3);
    expect(d.wake?.kind).toBe('task');
    expect(d.wake?.ref).toBe('1');
    expect(d.wake?.fired).toBe(false);
    // закрыть referenced #1 (in_progress → done) → wake срабатывает
    await api.tasks.updateState(1, { state: 'done', closure_reason: 'no_follow_on' });
    d = await api.tasks.get(3);
    expect(d.task.state).toBe('pending');
    expect(d.wake?.fired).toBe(true);
  });
});
