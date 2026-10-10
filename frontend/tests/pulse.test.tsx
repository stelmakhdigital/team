import { describe, expect, it, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// P2.9: Pulse feed (единая лента событий) — рендер ленты, фильтр по типу, пустое состояние.
function renderPulse() {
  return render(
    <MemoryRouter initialEntries={['/pulse']}>
      <App />
    </MemoryRouter>,
  );
}

describe('P2.9: Pulse feed (единая лента событий)', () => {
  beforeEach(() => resetMockDb());

  it('renders the event feed with type badges + entity links', async () => {
    renderPulse();
    expect(await screen.findByRole('heading', { name: 'Pulse' }, { timeout: 10_000 })).toBeInTheDocument();
    // a seeded mock event renders with its summary + type badge (badge + filter option)
    expect(await screen.findByText('Session #2 started (Worker, pi)', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect((await screen.findAllByText('task.state_changed', {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
    // a task event with a title → EntityLink renders the title (two task events share it)
    expect((await screen.findAllByText('Implement auth middleware', {}, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
  }, 20_000);

  it('filters the feed by event type (select)', async () => {
    renderPulse();
    await screen.findByRole('heading', { name: 'Pulse' }, { timeout: 10_000 });
    // type filter — target by its label (AppShell has a global <select> too)
    fireEvent.change(screen.getByLabelText('Тип'), { target: { value: 'session.started' } });
    // after filter: the session.started event is present, a task event is gone
    expect(await screen.findByText('Session #2 started (Worker, pi)', {}, { timeout: 10_000 })).toBeInTheDocument();
    await waitFor(
      () => expect(screen.queryByText('Task #2: in_progress → in_progress')).not.toBeInTheDocument(),
      { timeout: 10_000 },
    );
  }, 20_000);

  it('shows an empty state when there are no events', async () => {
    resetMockDb();
    getDb().events = [];
    renderPulse();
    expect(await screen.findByRole('heading', { name: 'Pulse' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Событий нет', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  // K3 (P2.9): live-prepend — WS-событие → новая запись в ленте.
  it('K3: WS event → new entry prepended to the feed (live-prepend)', async () => {
    vi.useFakeTimers();
    try {
      renderPulse();
      // initial load (mock API latency) + mount (WS interval registered). Fake timers →
      // только синхронные screen.* (async findBy под fake-timers зависает).
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
      expect(screen.getByRole('heading', { name: 'Pulse' })).toBeInTheDocument();
      // live WS-событие (task #1) ещё не появилось
      expect(screen.queryByText('Task #1 → in_progress')).not.toBeInTheDocument();
      // mock WS emits pool[0] = task.state_changed (task_id 1) each 8s → onEvent
      await act(async () => {
        await vi.advanceTimersByTimeAsync(8000);
      });
      // X3 debounce (300ms) → prepend
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });
      expect(screen.getByText('Task #1 → in_progress')).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  }, 20_000);
});
