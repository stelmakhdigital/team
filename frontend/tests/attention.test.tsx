import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// P0.1: Attention view «Что требует меня» — рендер, CTA-навигация, пустое
// состояние, бейдж-счётчик в навигации.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P0.1: Attention view «Что требует меня»', () => {
  beforeEach(() => resetMockDb());

  it('renders the aggregated list (blocked task + unread alerts) with severity badges', async () => {
    renderAt('/attention');
    expect(await screen.findByRole('heading', { name: 'Внимание' }, { timeout: 10_000 })).toBeInTheDocument();
    // blocked task (high) — «Fix flaky CI job» is the seeded blocked task
    // (appears in both the title cell and its CTA link → findAllByText)
    const titles = await screen.findAllByText(/Fix flaky CI job/, {}, { timeout: 10_000 });
    expect(titles.length).toBeGreaterThanOrEqual(1);
    // an unread watchdog alert (critical via event_type=blocked)
    expect(await screen.findByText('Watchdog: blocked', {}, { timeout: 10_000 })).toBeInTheDocument();
    // severity badges are rendered
    expect(screen.getAllByText('high').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('critical').length).toBeGreaterThanOrEqual(1);
    // suggested action is shown
    expect(await screen.findByText('Разблокировать', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('CTA EntityLink navigates to the target entity (task detail)', async () => {
    renderAt('/attention');
    await screen.findByRole('heading', { name: 'Внимание' }, { timeout: 10_000 });
    // the blocked-task item's CTA links to /tasks/4 (labelled with the title)
    const link = await screen.findByRole('link', { name: 'Fix flaky CI job' }, { timeout: 10_000 });
    fireEvent.click(link);
    // TaskDetailPage h1 = "Tasks / <title>"
    expect(await screen.findByRole('heading', { name: /Fix flaky CI job/ }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('shows a meaningful empty state when nothing needs attention', async () => {
    resetMockDb();
    const db = getDb();
    db.tasks = [];
    db.alerts = [];
    renderAt('/attention');
    expect(await screen.findByRole('heading', { name: 'Внимание' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Всё спокойно', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('sidebar nav badge shows the attention total (>0 in seeded mock)', async () => {
    renderAt('/');
    const badge = await screen.findByTestId('attention-count', {}, { timeout: 10_000 });
    const n = parseInt(badge.textContent ?? '0', 10);
    expect(Number.isFinite(n)).toBe(true);
    expect(n).toBeGreaterThanOrEqual(1);
  }, 20_000);
});
