import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// P1.5: session handover + compact-plan — context-wall banner (session detail),
// dashboard panel, handover modal (dry-run план → full прогон + new session link).
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P1.5: session handover + compact-plan', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('shows the context-wall banner + Handover button on a session at the wall', async () => {
    renderAt('/sessions/5'); // mock session 5: 88% context, running
    expect(await screen.findByText(/у контекст-стены \(88%\)/, {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Продолжить в новой сессии' }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('no context-wall banner for a session below the threshold', async () => {
    renderAt('/sessions/1'); // mock session 1: 42% context
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    expect(screen.queryByText(/контекст-стены/)).not.toBeInTheDocument();
  }, 20_000);

  it('dashboard shows the Context wall panel with the session at the wall', async () => {
    renderAt('/');
    const wallHeading = await screen.findByRole('heading', { name: 'Context wall' }, { timeout: 10_000 });
    expect(wallHeading).toBeInTheDocument();
    // mock session 5 (Worker2, 88%) is at the wall — scope to the Context wall card
    const wallCard = wallHeading.closest<HTMLElement>('.card')!;
    expect(await within(wallCard).findByText('Worker2', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(within(wallCard).getByText('88%')).toBeInTheDocument();
  }, 20_000);

  it('handover modal: dry-run план → выполнить → new session link', async () => {
    renderAt('/sessions/5');
    await screen.findByText(/у контекст-стены \(88%\)/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: 'Продолжить в новой сессии' }, { timeout: 10_000 }));

    // modal opens
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    // reason is required → buttons disabled until filled
    const textarea = within(modal).getByPlaceholderText(/context wall/);
    fireEvent.change(textarea, { target: { value: 'context wall 88%' } });

    // dry-run → plan
    fireEvent.click(within(modal).getByRole('button', { name: 'Показать план' }));
    expect(await within(modal).findByText('План (dry-run)', undefined, { timeout: 10_000 })).toBeInTheDocument();

    // full run → result + new session link
    fireEvent.click(within(modal).getByRole('button', { name: 'Выполнить' }));
    expect(await within(modal).findByText('Handover выполнен', undefined, { timeout: 10_000 })).toBeInTheDocument();
    expect(await within(modal).findByRole('link', { name: /Сессия #/ }, { timeout: 10_000 })).toBeInTheDocument();
  }, 30_000);

  it('dashboard Context wall panel shows a positive empty line when nothing is at the wall', async () => {
    resetMockDb();
    const db = getDb();
    // all sessions below the 80% wall
    db.sessions = db.sessions.map((s) => ({ ...s, context_used_percentage: 20 }));
    renderAt('/');
    const wallHeading = await screen.findByRole('heading', { name: 'Context wall' }, { timeout: 10_000 });
    const wallCard = wallHeading.closest<HTMLElement>('.card')!;
    expect(await within(wallCard).findByText(/Нет сессий выше порога/)).toBeInTheDocument();
    expect(within(wallCard).queryByRole('button', { name: 'Продолжить в новой сессии' })).not.toBeInTheDocument();
  }, 20_000);

  it('handover form disables actions until a reason is provided', async () => {
    renderAt('/sessions/5');
    await screen.findByText(/у контекст-стены \(88%\)/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: 'Продолжить в новой сессии' }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    // no reason yet → both actions disabled
    expect(within(modal).getByRole('button', { name: 'Показать план' })).toBeDisabled();
    expect(within(modal).getByRole('button', { name: 'Выполнить' })).toBeDisabled();
    // fill reason → enabled
    fireEvent.change(within(modal).getByPlaceholderText(/context wall/), { target: { value: 'context wall 88%' } });
    expect(within(modal).getByRole('button', { name: 'Показать план' })).toBeEnabled();
    expect(within(modal).getByRole('button', { name: 'Выполнить' })).toBeEnabled();
  }, 20_000);

  it('handover modal renders a partial per-step result (mock backdoor)', async () => {
    renderAt('/sessions/5');
    await screen.findByText(/у контекст-стены \(88%\)/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: 'Продолжить в новой сессии' }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    fireEvent.change(within(modal).getByPlaceholderText(/context wall/), { target: { value: 'context wall [partial]' } });
    fireEvent.click(within(modal).getByRole('button', { name: 'Выполнить' }));
    expect(await within(modal).findByText('Handover с ошибками', undefined, { timeout: 10_000 })).toBeInTheDocument();
    // per-step: old_stopped shown as failed (ошибка)
    expect(within(modal).getByText('ошибка')).toBeInTheDocument();
  }, 30_000);

  it('handover modal renders a failed result (mock backdoor)', async () => {
    renderAt('/sessions/5');
    await screen.findByText(/у контекст-стены \(88%\)/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: 'Продолжить в новой сессии' }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    fireEvent.change(within(modal).getByPlaceholderText(/context wall/), { target: { value: 'context wall [fail]' } });
    fireEvent.click(within(modal).getByRole('button', { name: 'Выполнить' }));
    expect(await within(modal).findByText('Handover не выполнен', undefined, { timeout: 10_000 })).toBeInTheDocument();
    // no new session link on a failed handover
    expect(within(modal).queryByRole('link', { name: /Сессия #/ })).not.toBeInTheDocument();
  }, 30_000);
});
