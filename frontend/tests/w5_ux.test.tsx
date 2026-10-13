import { describe, expect, it, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter, BrowserRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// W5 (UX Wave 5): Sessions live-output + URL-фильтры + tail-транскрипт.

function renderAt(path: string, router: 'memory' | 'browser' = 'memory') {
  return router === 'browser'
    ? render(
        <BrowserRouter>
          <App />
        </BrowserRouter>,
      )
    : render(
        <MemoryRouter initialEntries={[path]}>
          <App />
        </MemoryRouter>,
      );
}

describe('W5.6: HistoryPage селектор задачи/сессии', () => {
  beforeEach(() => resetMockDb());

  it('W5.6: выбор другой задачи → загружается её история + URL-sync ?task=', async () => {
    window.history.pushState({}, '', '/history');
    renderAt('/history', 'browser');
    await screen.findByRole('tab', { name: /Audit log/ }, { timeout: 10_000 });
    // default: первая задача (#1) → её timeline (ждём загрузки списка задач)
    await screen.findByRole('tab', { name: 'Task #1 history' }, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('tab', { name: 'Task #1 history' }));
    await screen.findByText('Task #1 timeline', {}, { timeout: 10_000 });
    // выбираем задачу #4 → загружается её история (ждём загрузки списка задач)
    await screen.findByRole('option', { name: '#4 Fix flaky CI job' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('История: задача'), { target: { value: '4' } });
    await waitFor(() => expect(window.location.search).toBe('?task=4&session=1'), { timeout: 10_000 });
    await screen.findByText('Task #4 timeline', {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('tab', { name: 'Task #4 history' }, { timeout: 10_000 }));
    await screen.findByText('Task #4 timeline', {}, { timeout: 10_000 });
    await waitFor(() => expect(screen.getByText(/No progress for 2h/)).toBeInTheDocument(), { timeout: 10_000 }); // история #4, не #1
  }, 25_000);

  it('W5.6: ?task= из URL → история выбранной задачи', async () => {
    window.history.pushState({}, '', '/history?task=4');
    renderAt('/history', 'browser');
    await screen.findByRole('tab', { name: /Audit log/ }, { timeout: 10_000 });
    await screen.findByRole('option', { name: '#4 Fix flaky CI job' }, { timeout: 10_000 }); // список задач загрузился
    await screen.findByRole('tab', { name: 'Task #4 history' }, { timeout: 10_000 });
    expect((screen.getByLabelText('История: задача') as HTMLSelectElement).value).toBe('4');
    fireEvent.click(screen.getByRole('tab', { name: 'Task #4 history' }));
    await screen.findByText('Task #4 timeline', {}, { timeout: 10_000 });
  }, 25_000);
});

describe('W5.5: Sessions live-output + URL-фильтры + tail-транскрипт', () => {
  beforeEach(() => resetMockDb());

  // W5.5: WS session.output → строки в live-output (prepend, последние 20)
  it('W5.5: WS-событие → строка в live-output', async () => {
    vi.useFakeTimers();
    try {
      renderAt('/sessions/1');
      // initial load (mock latency)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
      expect(screen.getByText('Live output')).toBeInTheDocument();
      expect(screen.getByText(/Пока нет вывода/)).toBeInTheDocument();
      // mock WS pool: session.output = pool[3] → 4-й тик (8s × 4)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(32_000);
      });
      const pre = document.querySelector('.live-output') as HTMLElement;
      // mock-пул печает session.output с зафиксированным tick=0 (шаблон строк
      // строится один раз при маунте) → ожидаем именно эти строки
      expect(pre.textContent).toContain('mock: processing step 0…');
      expect(pre.textContent).toContain('mock: step 0 done');
    } finally {
      vi.useRealTimers();
    }
  }, 20_000);

  // W5.5: URL-sync фильтров (state/team/role)
  it('W5.5: URL-sync фильтров SessionsPage', async () => {
    window.history.pushState({}, '', '/sessions');
    renderAt('/sessions', 'browser');
    await screen.findByLabelText('State (filter)', {}, { timeout: 10_000 });
    await screen.findByRole('option', { name: 'Dev Team' }, { timeout: 10_000 }); // команды загрузились
    fireEvent.change(screen.getByLabelText('Team (filter)'), { target: { value: '1' } });
    await waitFor(() => expect(window.location.search).toBe('?team=1'), { timeout: 10_000 });
    // роль появилась после выбора команды
    await screen.findByRole('option', { name: 'Lead' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('Role (filter)'), { target: { value: '1' } });
    await waitFor(() => expect(window.location.search).toBe('?team=1&role=1'), { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('State (filter)'), { target: { value: 'failed' } });
    await waitFor(() => expect(window.location.search).toBe('?team=1&state=failed&role=1'), { timeout: 10_000 });
  }, 25_000);

  // W5.5: транскрипт — последние 50 + «подгрузить ещё»
  it('W5.5: транскрипт tail — последние 50 + подгрузить ещё', async () => {
    const db = getDb();
    db.transcripts[5] = Array.from({ length: 60 }, (_, i) => `line-${i + 1}`);
    renderAt('/sessions/5');
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    await waitFor(() => expect(document.querySelectorAll('.transcript-list li').length).toBe(50), { timeout: 10_000 });
    expect(screen.getByRole('button', { name: 'Подгрузить ещё' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Подгрузить ещё' }));
    await waitFor(() => expect(document.querySelectorAll('.transcript-list li').length).toBe(60), { timeout: 10_000 });
    expect(screen.queryByRole('button', { name: 'Подгрузить ещё' })).not.toBeInTheDocument();
  }, 25_000);

  // W5.5: ссылка «Искать в транскриптах» с role_id → prefill на TranscriptSearchPage
  it('W5.5: ссылка на транскрипты с role_id и prefill формы', async () => {
    window.history.pushState({}, '', '/sessions');
    renderAt('/sessions', 'browser');
    await screen.findByLabelText('State (filter)', {}, { timeout: 10_000 });
    await screen.findByRole('option', { name: 'Dev Team' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('Team (filter)'), { target: { value: '1' } });
    await screen.findByRole('option', { name: 'Lead' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('Role (filter)'), { target: { value: '1' } });
    const link = await screen.findByText('Искать в транскриптах →', {}, { timeout: 10_000 });
    expect((link.closest('a') as HTMLAnchorElement).getAttribute('href')).toBe('/transcript-search?team_id=1&role_id=1');
    fireEvent.click(link);
    // prefill: команда и роль из URL; ждём загрузки обоих списков (гонка latency):
    // select показывает value только при наличии <option value>
    await screen.findByRole('heading', { name: 'Поиск по транскриптам' }, { timeout: 10_000 });
    await screen.findByRole('option', { name: 'Lead' }, { timeout: 10_000 });
    await screen.findByRole('option', { name: 'Dev Team' }, { timeout: 10_000 });
    expect((screen.getByLabelText('Команда') as HTMLSelectElement).value).toBe('1');
    expect((screen.getByLabelText('Роль') as HTMLSelectElement).value).toBe('1');
  }, 25_000);
});
