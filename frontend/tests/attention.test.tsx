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
    // T3: «Suggested action» — действие (ссылка); текст — из suggested_action
    // (blocked-task → «Разблокировать»), ведёт на /tasks/4 («Fix flaky CI job»).
    const link = await screen.findByRole('link', { name: 'Разблокировать' }, { timeout: 10_000 });
    fireEvent.click(link);
    // TaskDetailPage h1 = "Tasks / <title>"
    expect(await screen.findByRole('heading', { name: /Fix flaky CI job/ }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('shows a meaningful empty state when nothing needs attention', async () => {
    resetMockDb();
    const db = getDb();
    db.tasks = [];
    db.alerts = [];
    db.humanRequests = []; // P3.16: иначе pending human_request не даст empty-state
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

  it('renders a watchdog_job alert (P1.6) as an unread attention item', async () => {
    resetMockDb();
    const db = getDb();
    db.alerts.push({
      id: 99,
      team_id: 1,
      team_name: 'Dev Team',
      event_type: 'watchdog_job',
      severity: 'high',
      description: 'Context threshold 85% hit',
      session_id: 3,
      created_at: new Date().toISOString(),
      is_read: false,
      requires_action: true,
    } as any);
    renderAt('/attention');
    await screen.findByRole('heading', { name: 'Внимание' }, { timeout: 10_000 });
    // alert renders under its 'Watchdog: <event_type>' title + link on the session
    expect(await screen.findByText('Watchdog: watchdog_job', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
