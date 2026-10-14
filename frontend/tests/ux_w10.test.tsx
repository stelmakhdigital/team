import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { BackToTop } from '../src/components/ui/BackToTop';

// W10: кнопка «наверх» — появление после прокрутки, smooth-scroll,
// уважение prefers-reduced-motion.
function stubMatchMedia(matches: boolean) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    configurable: true,
    value: (q: string) => ({ matches: q.includes('reduce') ? matches : false, media: q }),
  });
}

describe('W10: BackToTop', () => {
  beforeEach(() => {
    stubMatchMedia(false);
    window.scrollY = 0;
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('не видна в начале страницы', () => {
    render(<BackToTop />);
    expect(screen.queryByRole('button', { name: 'Подняться в начало страницы' })).not.toBeInTheDocument();
  });

  it('появляется после прокрутки >600px и скроллит наверх (smooth)', () => {
    render(<BackToTop />);
    act(() => {
      window.scrollY = 700;
      window.dispatchEvent(new Event('scroll'));
    });
    const btn = screen.getByRole('button', { name: 'Подняться в начало страницы' });
    fireEvent.click(btn);
    expect(window.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' });
  });

  it('prefers-reduced-motion → scroll без smooth', () => {
    stubMatchMedia(true);
    render(<BackToTop />);
    act(() => {
      window.scrollY = 700;
      window.dispatchEvent(new Event('scroll'));
    });
    fireEvent.click(screen.getByRole('button', { name: 'Подняться в начало страницы' }));
    expect(window.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'auto' });
  });

  it('скрывается при возврате наверх', () => {
    render(<BackToTop />);
    act(() => {
      window.scrollY = 700;
      window.dispatchEvent(new Event('scroll'));
    });
    expect(screen.getByRole('button', { name: 'Подняться в начало страницы' })).toBeInTheDocument();
    act(() => {
      window.scrollY = 100;
      window.dispatchEvent(new Event('scroll'));
    });
    expect(screen.queryByRole('button', { name: 'Подняться в начало страницы' })).not.toBeInTheDocument();
  });
});
