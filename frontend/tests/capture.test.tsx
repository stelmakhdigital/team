import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import CaptureModal from '../src/components/CaptureModal';
import { resetMockDb } from '../src/api/mock/data';
import { setMockError } from '../src/api/mock/adapter';

// P2.8: capture (стл-кадр экрана сессии) — кнопка → модалка со снимком, Copy, error-state.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
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
    const { unmount } = render(<CaptureModal sessionId={1} onClose={() => {}} />);
    // capture-on-open падает с 409 → error-state (Conflict) + Retry
    await screen.findByText('Conflict', {}, { timeout: 10_000 });
    expect(screen.getByText(/Retry/)).toBeInTheDocument();
    // 409-причина (message) показана
    expect(await screen.findByText(/Simulated conflict error/i, {}, { timeout: 10_000 })).toBeInTheDocument();
    unmount();
  }, 25_000);
});
