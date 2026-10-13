import { describe, expect, it, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter, BrowserRouter } from 'react-router-dom';
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

  // W5.1: burst 3 событий в debounce-окне буфера → все 3 в ленте, новейший сверху
  // (раньше discard-throttle молча выбрасывал всё после первого события).
  it('W5.1: burst 3 WS-событий → все 3 в ленте, новейший сверху', async () => {
    vi.useFakeTimers();
    try {
      renderPulse();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
      expect(screen.getByRole('heading', { name: 'Pulse' })).toBeInTheDocument();
      const emit = (globalThis as unknown as { __wsMockEmit?: (m: unknown) => void }).__wsMockEmit;
      expect(emit).toBeDefined();
      act(() => {
        // burst: 3 события мгновенно (внутри debounce-окна 300мс)
        emit!({ type: 'task.created', data: { task_id: 77, title: 'Burst task' }, timestamp: new Date().toISOString() });
        emit!({ type: 'message.sent', data: { message_id: 55, from_role_name: 'Scout' }, timestamp: new Date().toISOString() });
        emit!({ type: 'alert.created', data: { alert_id: 9, severity: 'low', event_type: 'idle' }, timestamp: new Date().toISOString() });
      });
      // debounce 300мс → flush всего буфера одним prepend
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });
      const list = document.querySelector('.pulse-list') as HTMLElement;
      const idx = (needle: string) =>
        Array.from(list.querySelectorAll('.pulse-summary')).findIndex((el) =>
          (el.textContent ?? '').includes(needle));
      // порядок прихода: task → message → alert; новейший (alert) сверху
      expect(idx('Task #77 created')).toBeGreaterThan(idx('Message from Scout'));
      expect(idx('Message from Scout')).toBeGreaterThan(idx('Alert [low] idle'));
      expect(idx('Alert [low] idle')).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  }, 20_000);

  // W5.1: нет событий → буфер не растёт, лента рендерится без live-записей
  it('W5.1: без событий буфер пуст — лента не получает live-записей', async () => {
    renderPulse();
    expect(await screen.findByRole('heading', { name: 'Pulse' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.queryByText('Task #77 created: Burst task')).not.toBeInTheDocument();
    expect(document.querySelector('.pulse-list')).not.toBeNull();
  }, 20_000);

  // W5.2: поиск по событиям — ввод → запрос с q (substring, case-insensitive)
  it('W5.2: ввод в поиск фильтрует ленту по q (case-insensitive)', async () => {
    renderPulse();
    await screen.findByRole('heading', { name: 'Pulse' }, { timeout: 10_000 });
    const input = screen.getByLabelText('Поиск по событиям');
    fireEvent.change(input, { target: { value: 'AUTH' } });
    // debounce 300мс + mock-latency: ждём одновременно матч (substring по summary,
    // case-insensitive) и исчезновение не-матча (до фильтра матч был в полной ленте)
    await waitFor(() => {
      expect(screen.queryByText('Task #2 created: Implement auth middleware')).toBeInTheDocument();
      expect(screen.queryByText('Session #2 started (Worker, pi)')).not.toBeInTheDocument();
    }, { timeout: 10_000 });
  }, 20_000);

  // W5.2: URL-sync ?q= + сброс очистки
  it('W5.2: URL-sync ?q= и сброс очистки', async () => {
    window.history.pushState({}, '', '/pulse');
    render(
      <BrowserRouter>
        <App />
      </BrowserRouter>,
    );
    await screen.findByRole('heading', { name: 'Pulse' }, { timeout: 10_000 });
    const input = screen.getByLabelText('Поиск по событиям');
    fireEvent.change(input, { target: { value: 'auth' } });
    expect(await screen.findByText('Task #2 created: Implement auth middleware', {}, { timeout: 10_000 })).toBeInTheDocument();
    // URL-sync: ?q=auth
    await waitFor(() => expect(window.location.search).toBe('?q=auth'), { timeout: 10_000 });
    // во время loading ленты фильтры заменяются скелетоном — пере-находим инпут
    const input2 = await screen.findByLabelText('Поиск по событиям', {}, { timeout: 10_000 });
    // сброс → q снят, лента полная
    fireEvent.change(input2, { target: { value: '' } });
    await waitFor(() => expect(window.location.search).toBe(''), { timeout: 10_000 });
    expect(await screen.findByText('Session #2 started (Worker, pi)', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
