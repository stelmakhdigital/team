import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import type { ReactElement } from 'react';
import { BrowserRouter } from 'react-router-dom';
import App from '../src/App';
import { ErrorBoundary } from '../src/ErrorBoundary';
import { resetMockDb } from '../src/api/mock/data';

// U-X (UX polish): ErrorBoundary + URL-sync фильтров (Tasks/TranscriptSearch).

function renderAt(path: string) {
  // BrowserRouter: URL-ассерты через window.location (паттерн w5_ux)
  window.history.pushState({}, '', path);
  return render(
    <BrowserRouter>
      <App />
    </BrowserRouter>,
  );
}

describe('U-X1: ErrorBoundary — честный экран краха рендера', () => {
  const origError = console.error;
  beforeEach(() => {
    resetMockDb();
    console.error = () => {}; // React dev шумит о пойманном крэше
  });
  afterEach(() => {
    console.error = origError;
  });

  function Boom(): ReactElement {
    throw new Error('boom-render'); // unreachable — крэш до возврата
  }

  it('крэш child → alert-экран с описанием и кнопками (вместо белого экрана)', () => {
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('Интерфейс сломался');
    expect(alert.textContent).toContain('boom-render');
    expect(screen.getByRole('button', { name: /Перезагрузить/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Повторить рендер/ })).toBeTruthy();
  });

  it('healthy child → рендерится как есть (граница прозрачна)', () => {
    render(
      <ErrorBoundary>
        <div data-testid="ok">alive</div>
      </ErrorBoundary>,
    );
    expect(screen.getByTestId('ok').textContent).toBe('alive');
  });
});

describe('U-X3: URL-sync фильтров (TasksPage)', () => {
  beforeEach(() => resetMockDb());

  it('выбор фильтров пишет URL ?team=&state=&project=', async () => {
    renderAt('/tasks');
    await screen.findByRole('option', { name: 'Dev Team' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('Team (filter)'), { target: { value: '1' } });
    await screen.findByRole('option', { name: 'team-console' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText('Project (filter)'), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText('State (filter)'), { target: { value: 'in_progress' } });
    await waitFor(() => {
      const p = new URLSearchParams(window.location.search);
      expect(p.get('team')).toBe('1');
      expect(p.get('state')).toBe('in_progress');
      expect(p.get('project')).toBe('1');
    }, { timeout: 10_000 });
  }, 20_000);

  it('URL ?state=in_progress&team=1 → селекты преднастроены (shareable-ссылка)', async () => {
    renderAt('/tasks?state=in_progress&team=1');
    await screen.findByRole('option', { name: 'Dev Team' }, { timeout: 10_000 });
    await waitFor(() => {
      const teamSel = screen.getByLabelText('Team (filter)') as HTMLSelectElement;
      const stateSel = screen.getByLabelText('State (filter)') as HTMLSelectElement;
      expect(teamSel.value).toBe('1');
      expect(stateSel.value).toBe('in_progress');
    }, { timeout: 10_000 });
  }, 20_000);
});

describe('U-X3: URL-sync TranscriptSearchPage', () => {
  beforeEach(() => resetMockDb());

  it('выбор команды → URL ?team_id=1; поиск → +q=', async () => {
    renderAt('/transcript-search');
    await screen.findByRole('option', { name: 'Dev Team' }, { timeout: 10_000 });
    fireEvent.change(screen.getByLabelText(/Команда/), { target: { value: '1' } });
    await waitFor(() => {
      expect(new URLSearchParams(window.location.search).get('team_id')).toBe('1');
    }, { timeout: 10_000 });
    await screen.findByRole('option', { name: 'Lead' }, { timeout: 10_000 }); // роли команды загрузились
    fireEvent.change(screen.getByLabelText(/Роль/), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText(/Поиск/), { target: { value: 'auth' } });
    fireEvent.click(screen.getByRole('button', { name: /Найти/ }));
    await waitFor(() => {
      const p = new URLSearchParams(window.location.search);
      expect(p.get('team_id')).toBe('1');
      expect(p.get('role_id')).toBe('1');
      expect(p.get('q')).toBe('auth');
    }, { timeout: 10_000 });
  }, 20_000);
});

describe('U-X4: focus-trap модалок (CaptureModal)', () => {
  beforeEach(() => resetMockDb());

  it('Tab циклится внутри модалки; Esc закрывает; фокус возвращается на триггер', async () => {
    renderAt('/sessions');
    const trigger = await screen.findByRole('button', { name: 'Capture session 1' }, { timeout: 10_000 });
    trigger.focus(); // jsdom не фокусирует по клику — явно (в браузере клик даёт focus)
    fireEvent.click(trigger);
    // dialog без aria-label → имя из контента не матчится; ищем по роли и тексту
    const dialog = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    await screen.findByText(/Capture — Session #1/, {}, { timeout: 10_000 });
    expect(dialog.contains(document.activeElement)).toBe(true);
    for (let i = 0; i < 6; i++) {
      fireEvent.keyDown(document.activeElement!, { key: 'Tab' });
      expect(dialog.contains(document.activeElement)).toBe(true);
    }
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull(), { timeout: 10_000 });
    expect(document.activeElement).toBe(trigger);
  }, 30_000);
});

describe('U-X6: Pulse роли фильтра пополняются live-событиями', () => {
  beforeEach(() => resetMockDb());

  it('WS session.started с новой ролью → опция появляется в селекте Роль', async () => {
    vi.useFakeTimers();
    try {
      renderAt('/pulse');
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
      });
      const emit = (globalThis as unknown as { __wsMockEmit?: (m: unknown) => void }).__wsMockEmit;
      expect(emit).toBeDefined();
      act(() => {
        emit!({ type: 'session.started', data: { session_id: 999, role_id: 424, role_name: 'LiveRole', team_id: 1 }, timestamp: new Date().toISOString() });
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });
      // waitFor под fake timers виснет — прямой запрос после flush
      expect(screen.getByRole('option', { name: 'LiveRole' })).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  }, 30_000);
});
