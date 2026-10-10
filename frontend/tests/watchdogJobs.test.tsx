import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P1.6: Watchdog-джобы — список, создание, stop, валидация, watchdog-wake kind.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P1.6: Watchdog jobs page', () => {
  beforeEach(() => resetMockDb());

  it('lists seeded jobs with policy, threshold, status', async () => {
    renderAt('/watchdog/jobs');
    expect(await screen.findByRole('heading', { name: 'Watchdog jobs' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('worker ctx', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('lead ctx')).toBeInTheDocument();
    expect(screen.getAllByText('context-usage-threshold').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('80%')).toBeInTheDocument();
    // fired_count
    expect(screen.getByText('2')).toBeInTheDocument();
  }, 20_000);

  it('registers a new job via the form', async () => {
    renderAt('/watchdog/jobs');
    await screen.findByRole('heading', { name: 'Watchdog jobs' }, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: '+ New job' }));
    // заполняем форму
    fireEvent.change(await screen.findByLabelText(/Name/), { target: { value: 'new job' } });
    const select = await screen.findByLabelText(/Target-сессия/);
    fireEvent.change(select, { target: { value: '1' } });
    fireEvent.click(await screen.findByRole('button', { name: 'Register' }));
    await waitFor(() => expect(screen.getByText('new job')).toBeInTheDocument(), { timeout: 10_000 });
  }, 20_000);

  it('does not register without a name', async () => {
    renderAt('/watchdog/jobs');
    await screen.findByRole('heading', { name: 'Watchdog jobs' }, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: '+ New job' }));
    // без имени → кнопка disabled (Register)
    const register = await screen.findByRole('button', { name: 'Register' });
    expect(register).toBeDisabled();
  }, 20_000);

  it('stops an active job (status → stopped)', async () => {
    renderAt('/watchdog/jobs');
    await screen.findByRole('heading', { name: 'Watchdog jobs' }, { timeout: 10_000 });
    // seed: 2 активных (worker ctx + lead ctx) → 2 кнопки Stop
    const stopButtons = await screen.findAllByRole('button', { name: 'Stop' });
    expect(stopButtons.length).toBeGreaterThanOrEqual(1);
    fireEvent.click(stopButtons[0]);
    await waitFor(() => {
      // после stop — статус "stopped" появляется
      expect(screen.getAllByText('stopped').length).toBeGreaterThanOrEqual(1);
    }, { timeout: 10_000 });
  }, 20_000);

  it('block-with-wake form offers the watchdog kind + job selector', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: /Tasks/ }, { timeout: 30_000 });
    // открываем block-модалку для первой задачи (кнопка «⛔ block»)
    const blockBtns = await screen.findAllByRole('button', { name: /block/i }, { timeout: 30_000 });
    fireEvent.click(blockBtns[0]);
    const typeSelect = await screen.findByLabelText(/Wake type/, {}, { timeout: 30_000 });
    fireEvent.change(typeSelect, { target: { value: 'watchdog' } });
    // появляется селектор дjоба
    expect(await screen.findByLabelText(/Watchdog job/, {}, { timeout: 30_000 })).toBeInTheDocument();
  }, 45_000);
});
