import { useEffect, useRef } from 'react';

// U-X4 (P3): focus-trap для модалок + возврат фокуса на триггер.
// - на open: запоминаем document.activeElement (кнопка-триггер), фокусируем
//   первый фокусируемый элемент контейнера;
// - Tab/Shift+Tab циклятся внутри контейнера (не убегают на фон);
// - на close (unmount эффекта): возврат фокуса на триггер.
// Контейнер — ref на backdrop модалки (role="dialog").
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function useFocusTrap(containerRef: { current: HTMLElement | null }, open: boolean): void {
  const triggerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const el = containerRef.current;
    if (el === null) return;
    const first = el.querySelector<HTMLElement>(FOCUSABLE);
    if (first) first.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return;
      // видимые фокусируемые внутри контейнера (display:none → offsetParent null)
      const items = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (n) => n === document.activeElement || n.offsetParent !== null,
      );
      if (items.length === 0) return;
      const idx = items.indexOf(document.activeElement as HTMLElement);
      e.preventDefault();
      if (e.shiftKey) {
        const next = idx <= 0 ? items[items.length - 1] : items[idx - 1];
        next.focus();
      } else {
        const next = idx === items.length - 1 || idx === -1 ? items[0] : items[idx + 1];
        next.focus();
      }
    };
    el.addEventListener('keydown', onKey);
    return () => {
      el.removeEventListener('keydown', onKey);
      const t = triggerRef.current;
      if (t && document.contains(t)) t.focus();
    };
  }, [open, containerRef]);
}
