import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import App from '../src/App';
import CaptureModal from '../src/components/CaptureModal';
import TaskDetailPage from '../src/pages/TaskDetailPage';
import { ToastProvider } from '../src/components/ui/Toast';
import { resetMockDb } from '../src/api/mock/data';
import { setMockError } from '../src/api/mock/adapter';

// P2.8: capture (стл-кадр экрана сессии) — кнопка → модалка со снимком, Copy, error-state.
function renderAt(path: string) {
  return render(
    <ToastProvider>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </ToastProvider>,
  );
}

describe('P2.8: capture (session screen still-frame)', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });
  afterEach(() => {
    setMockError(null);
  });

  it('Capture button → modal with snapshot (tmux-pane) + source + saved_path + Copy', async () => {
    renderAt('/sessions/1'); // pi running, has transcript
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    // кнопка Capture в карточке Transcript
    fireEvent.click(await screen.findByRole('button', { name: /Capture/ }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    // содержимое снимка (tail транскрипта)
    await within(modal).findByText(/use JWT with rotation/, {}, { timeout: 10_000 });
    // source-бейдж (tmux-pane для pi) + captured_at
    expect(within(modal).getByText('tmux-pane')).toBeInTheDocument();
    // saved_path
    expect(within(modal).getByText(/session-1-.*\.txt/)).toBeInTheDocument();
    // кнопка Copy
    expect(within(modal).getByRole('button', { name: /Copy/ })).toBeInTheDocument();
  }, 25_000);

  it('modal Copy button copies the snapshot to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderAt('/sessions/1');
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: /Capture/ }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    await within(modal).findByText(/use JWT with rotation/, {}, { timeout: 10_000 });
    fireEvent.click(within(modal).getByRole('button', { name: /Copy/ }));
    expect(writeText).toHaveBeenCalled();
    await waitFor(() => expect(within(modal).getByText(/Copied/)).toBeInTheDocument(), { timeout: 10_000 });
  }, 25_000);

  it('error-state on 409 (dead tmux) with clear reason', async () => {
    setMockError('conflict'); // симулируем 409 (tmux мертва)
    const { unmount } = render(
      <MemoryRouter initialEntries={['/sessions/1']}>
        <CaptureModal sessionId={1} onClose={() => {}} />
      </MemoryRouter>,
    );
    // capture-on-open падает с 409 → error-state (Conflict) + Retry
    await screen.findByText('Conflict', {}, { timeout: 10_000 });
    expect(screen.getByText(/Retry/)).toBeInTheDocument();
    // 409-причина (message) показана
    expect(await screen.findByText(/Simulated conflict error/i, {}, { timeout: 10_000 })).toBeInTheDocument();
    unmount();
  }, 25_000);

  // W5.3: кнопка ⎙ в строке списка сессий открывает CaptureModal
  it('W5.3: кнопка ⎙ в строке Sessions открывает CaptureModal', async () => {
    renderAt('/sessions');
    await screen.findByRole('button', { name: 'Capture session 1' }, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: 'Capture session 1' }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    await within(modal).findByText(/use JWT with rotation/, {}, { timeout: 10_000 });
  }, 25_000);

  // W5.3: «→ Evidence» — navigate в EvidencePanel задачи + предзаполнение content
  it('W5.3: → Evidence навигация и предзаполнение content в EvidencePanel', async () => {
    render(
      <ToastProvider>
        <MemoryRouter initialEntries={['/sessions/1']}>
          <Routes>
            <Route path="/sessions/1" element={<CaptureModal sessionId={1} taskId={1} onClose={() => {}} />} />
            <Route path="/tasks/:taskId" element={<TaskDetailPage />} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>,
    );
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    await within(modal).findByText(/use JWT with rotation/, {}, { timeout: 10_000 });
    fireEvent.click(within(modal).getByRole('button', { name: '→ Evidence' }));
    // navigate → TaskDetailPage → EvidencePanel, content предзаполнен capture
    await screen.findByText('Evidence / proof', {}, { timeout: 10_000 });
    const ta = screen.getByLabelText('Evidence content') as HTMLTextAreaElement;
    expect(ta.value).toContain('use JWT with rotation');
  }, 25_000);

  // W5.3: «Вставить последний capture сессии» в EvidencePanel заполняет textarea
  it('W5.3: вставка capture сессии в EvidencePanel заполняет content', async () => {
    renderAt('/tasks/1');
    await screen.findByText('Evidence / proof', {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByTestId('evidence-add-btn', {}, { timeout: 10_000 }));
    const sel = screen.getByLabelText('Вставить capture сессии');
    // ждём загрузки сессий команды (опции селекта)
    await screen.findByRole('option', { name: /#1 Lead/ }, { timeout: 10_000 });
    fireEvent.change(sel, { target: { value: '1' } });
    fireEvent.click(screen.getByRole('button', { name: 'Вставить последний capture сессии' }));
    await waitFor(() => {
      const ta = screen.getByLabelText('Evidence content') as HTMLTextAreaElement;
      expect(ta.value).toContain('use JWT with rotation');
    }, { timeout: 10_000 });
    // тост об успехе вставки
    expect(await screen.findByText(/Capture сессии #1 вставлен/, {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 25_000);

  // W5.3: провал clipboard Copy → тост (не молча)
  it('W5.3: ошибка clipboard Copy показывает тост', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    Object.assign(navigator, { clipboard: { writeText } });
    renderAt('/sessions/1');
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: /Capture/ }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    await within(modal).findByText(/use JWT with rotation/, {}, { timeout: 10_000 });
    fireEvent.click(within(modal).getByRole('button', { name: /Copy/ }));
    expect(
      await screen.findByText(/Clipboard недоступен/, {}, { timeout: 10_000 }),
    ).toBeInTheDocument();
  }, 25_000);
});
