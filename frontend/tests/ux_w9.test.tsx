import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { ToastProvider } from '../src/components/ui/Toast';
import { CopyBtn } from '../src/components/ui/CopyBtn';
import { Time } from '../src/components/ui/Time';

// W9: копирование с фидбеком (U-X10) и тайм-тултипы (U-X9).
function renderCopy() {
  return render(
    <ToastProvider>
      <CopyBtn text="session:42" label="ID сессии" />
    </ToastProvider>,
  );
}

describe('W9: CopyBtn', () => {
  const writeText = vi.fn().mockResolvedValue(undefined);

  beforeEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });
    writeText.mockClear();
    vi.useFakeTimers();
  });

  afterEach(() => vi.useRealTimers());

  it('копирует текст и показывает «✓ Скопировано», затем возвращается к ⧉', async () => {
    renderCopy();
    const btn = screen.getByRole('button', { name: 'Скопировать ID сессии' });
    await act(async () => {
      fireEvent.click(btn);
    });
    expect(writeText).toHaveBeenCalledWith('session:42');
    expect(screen.getByText('✓ Скопировано')).toBeInTheDocument();
    // через 1600мс — назад к ⧉
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1700);
    });
    expect(screen.getByText('⧉')).toBeInTheDocument();
  });

  it('ошибка буфера → честный toast, без «Скопировано»', async () => {
    writeText.mockRejectedValueOnce(new Error('denied'));
    renderCopy();
    const btn = screen.getByRole('button', { name: 'Скопировать ID сессии' });
    await act(async () => {
      fireEvent.click(btn);
    });
    expect(screen.queryByText('✓ Скопировано')).not.toBeInTheDocument();
    expect(screen.getByText(/Браузер не дал доступ к буферу/)).toBeInTheDocument();
  });
});

describe('W9: Time (relative + absolute title)', () => {
  it('рендерит относительное время и абсолютное в title', () => {
    // фиксированное «сейчас» не задаётся: formatRelative от 2025-01-01 → «давно»,
    // важен title (абсолютный формат), а не текст.
    render(<Time iso="2025-01-01T00:00:00Z" />);
    // title — абсолютное время (formatDateTime: «1 янв, 00:00»-стиль, без года),
    // текст — относительное («Nd ago»/«just now»)
    const el = screen.getByTitle(/\d{1,2}:\d{2}/);
    expect(el).toBeInTheDocument();
    expect(el.textContent).toMatch(/(\dm|\dh|\dd|just now) ago|just now/);
  });
});
