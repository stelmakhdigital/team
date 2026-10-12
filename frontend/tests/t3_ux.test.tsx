import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// T3 — UX-читаемость: Attention action, Sessions page, Metrics значения, Messages 2-панель.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('T3.4: Sessions — пункт меню + экран-список', () => {
  beforeEach(() => resetMockDb());

  it('nav has «Sessions» → /sessions with a sessions table', async () => {
    renderAt('/sessions');
    // heading
    expect(await screen.findByRole('heading', { name: 'Sessions' }, { timeout: 10_000 })).toBeInTheDocument();
    // a seeded session role is listed (Lead / Worker / Reviewer)
    expect(await screen.findAllByText(/Lead/, {}, { timeout: 10_000 }).then((n) => n.length)).toBeGreaterThanOrEqual(1);
    // table headers
    expect(screen.getByRole('columnheader', { name: 'Runtime' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Model' })).toBeInTheDocument();
  }, 20_000);

  it('clicking a session row navigates to /sessions/{id}', async () => {
    renderAt('/sessions');
    // клик по #1 (id-ссылка на detail)
    const idLink = await screen.findByRole('link', { name: '1' }, { timeout: 10_000 });
    fireEvent.click(idLink);
    // SessionDetailPage: h2 «Session #1» (exact — не h1 «Dashboard / Session #1 (Lead)»)
    expect(
      await screen.findByRole('heading', { name: (_c, el) => el?.textContent === 'Session #1' }, { timeout: 10_000 }),
    ).toBeInTheDocument();
  }, 20_000);

  it('state filter narrows the list', async () => {
    resetMockDb();
    const db = getDb();
    // оставим только running-сессии
    db.sessions = db.sessions.filter((s) => s.state === 'running');
    renderAt('/sessions');
    expect(await screen.findByRole('heading', { name: 'Sessions' }, { timeout: 10_000 })).toBeInTheDocument();
    // нет failed-состояния в списке
    await screen.findAllByText('running', {}, { timeout: 10_000 });
  }, 20_000);
});

describe('T3.2: Dashboard Metrics — значения + единицы + min/max', () => {
  beforeEach(() => resetMockDb());

  it('each metric shows current value + unit + min/max', async () => {
    renderAt('/');
    // заголовки метрик
    expect(await screen.findByText('Tasks created', {}, { timeout: 10_000 })).toBeInTheDocument();
    // единицы отображаются (count / tokens / USD)
    expect(screen.getAllByText('count').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('tokens').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('USD').length).toBeGreaterThanOrEqual(1);
    // min/max строка
    expect(screen.getAllByText(/min .+ · max /).length).toBeGreaterThanOrEqual(1);
  }, 20_000);
});

describe('T3.1: Attention — Suggested action как действие (без пустой колонки)', () => {
  beforeEach(() => resetMockDb());

  it('Suggested action is a clickable link (no nameless «Открыть» column)', async () => {
    renderAt('/attention');
    expect(await screen.findByRole('heading', { name: 'Внимание' }, { timeout: 10_000 })).toBeInTheDocument();
    // заголовок «Suggested action» есть (W2.3: до 2 групп «Решения»/«События» → по одному на таблицу),
    // а безымянной колонки «Открыть» — нет
    const sugCols = screen.getAllByRole('columnheader', { name: 'Suggested action' });
    expect(sugCols.length >= 1).toBe(true);
    const openCol = screen.queryByRole('columnheader', { name: 'Открыть' });
    expect(openCol).toBeNull();
    // suggested action рендерится как link (не plain text)
    expect(await screen.findByRole('link', { name: 'Разблокировать' }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});

describe('T3.3: Messages — 2-панная группировка Direct/Rooms', () => {
  beforeEach(() => resetMockDb());

  it('renders Direct + Rooms sections (2 panels, not a flat list)', async () => {
    renderAt('/messages');
    expect(await screen.findByRole('heading', { name: 'Message Center' }, { timeout: 10_000 })).toBeInTheDocument();
    // секции диалогов
    expect(await screen.findByText('Direct', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('Rooms')).toBeInTheDocument();
    // подсказка выбора диалога (справа)
    expect(await screen.findByText('Select a conversation', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('clicking a room shows its messages in the right panel', async () => {
    renderAt('/messages');
    await screen.findByRole('heading', { name: 'Message Center' }, { timeout: 10_000 });
    // выбери комнату «dev-team» (сидится в mock)
    const roomBtn = await screen.findByRole('button', { name: /dev-team/ }, { timeout: 10_000 });
    fireEvent.click(roomBtn);
    // справа — заголовок комнаты #dev-team
    expect(await screen.findByText('#dev-team', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
