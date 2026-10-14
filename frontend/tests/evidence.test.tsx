import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P3.13: Proof/evidence (closure ≠ acceptance) — Task Detail панель evidence.

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P3.13: proof/evidence on tasks', () => {
  beforeEach(() => {
    resetMockDb();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('Task detail shows evidence panel + readiness/acceptance (task 1 = ready/accepted)', async () => {
    renderAt('/tasks/1');
    const panel = await screen.findByTestId('evidence-panel', {}, { timeout: 10_000 });
    expect(within(panel).getByText('Evidence / proof')).toBeInTheDocument();
    // readiness/acceptance (task 1: есть accepted → ready/accepted)
    const badges = await screen.findByTestId('evidence-readiness', {}, { timeout: 10_000 });
    expect(within(badges).getByText(/ready/)).toBeInTheDocument();
    expect(screen.getByText('acceptance: accepted')).toBeInTheDocument();
  }, 25_000);

  it('evidence list shows artifacts (type, name, status)', async () => {
    renderAt('/tasks/1');
    await screen.findByTestId('evidence-panel', {}, { timeout: 10_000 });
    // 2 артефакта (mock): auth_test.go (accepted) + build.log (submitted)
    await screen.findByText('auth_test.go', {}, { timeout: 10_000 });
    expect(screen.getByText('build.log')).toBeInTheDocument();
    // type-чипы + status-бейджи
    expect(screen.getByText('test')).toBeInTheDocument();
    expect(screen.getByText('accepted')).toBeInTheDocument();
    expect(screen.getByText('submitted')).toBeInTheDocument();
  }, 25_000);

  it('closure ≠ acceptance: done task with acceptance=pending (task 5)', async () => {
    renderAt('/tasks/5');
    await screen.findByTestId('evidence-panel', {}, { timeout: 10_000 });
    // задача done, но acceptance=pending (evidence submitted, без verdict)
    expect(screen.getByText('done')).toBeInTheDocument();
    expect(screen.getByText('acceptance: pending')).toBeInTheDocument();
  }, 25_000);

  it('Add evidence: form appears, submit creates artifact', async () => {
    renderAt('/tasks/1');
    await screen.findByTestId('evidence-panel', {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByTestId('evidence-add-btn', {}, { timeout: 10_000 }));
    const nameInput = await screen.findByLabelText('Evidence name', {}, { timeout: 10_000 });
    const contentInput = screen.getByLabelText('Evidence content');
    fireEvent.change(nameInput, { target: { value: 'coverage.png' } });
    fireEvent.change(contentInput, { target: { value: '87% coverage' } });
    fireEvent.click(screen.getByRole('button', { name: /Submit evidence/ }));
    // новый артефакт появляется в списке
    await screen.findByText('coverage.png', {}, { timeout: 10_000 });
  }, 25_000);

  it('Judge: accept on a submitted artifact → status accepted', async () => {
    renderAt('/tasks/1');
    const panel = await screen.findByTestId('evidence-panel', {}, { timeout: 10_000 });
    // build.log (submitted) → Accept
    const buildItem = (await screen.findByText('build.log', {}, { timeout: 10_000 })).closest('.ev-item') as HTMLElement;
    const acceptBtn = within(buildItem).getByRole('button', { name: /Accept/ });
    fireEvent.click(acceptBtn);
    // P3: вердикт — через мини-модалку с reason (было window.prompt)
    await screen.findByRole('dialog', { name: /Verdict accept/ }, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Подтвердить accept/ }));
    // статус build.log → accepted (item обновился)
    await waitFor(() => {
      const items = screen.getAllByTestId('evidence-item');
      expect(items.length).toBeGreaterThan(0);
    }, { timeout: 10_000 });
    // теперь два accepted (auth_test.go + build.log)
    await waitFor(() => expect(within(panel).getAllByText('accepted').length).toBe(2), { timeout: 10_000 });
  }, 25_000);
});
