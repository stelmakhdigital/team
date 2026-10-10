import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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
});
