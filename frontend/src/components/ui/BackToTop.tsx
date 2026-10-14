import { useEffect, useState } from 'react';

// W10: кнопка «наверх» для длинных лент (Pulse). Появляется после 300px
// прокрутки, уважает prefers-reduced-motion (scroll без smooth).
export function BackToTop() {
  const [show, setShow] = useState(false);

  useEffect(() => {
    const onScroll = () => setShow(window.scrollY > 300);
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  if (!show) return null;

  return (
    <button
      type="button"
      className="back-to-top"
      aria-label="Подняться в начало страницы"
      title="Наверх"
      onClick={() => {
        const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        window.scrollTo({ top: 0, behavior: reduced ? 'auto' : 'smooth' });
      }}
    >
      ↑ Наверх
    </button>
  );
}
