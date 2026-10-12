import { useEffect, useState } from 'react';
import { useEscapeStack } from '../../hooks/useEscapeStack';
import { isEditableTarget } from '../../lib/hotkeys';

// W3.1: мини-подсказка hotkeys на Dashboard по «?» (вне input).
// Dismiss — по Esc/клику; состояние в localStorage (показывать, пока не скрыли).
const HINT_KEY = 'hotkeys-hint.dismissed';

export default function HotkeysHint() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    let dismissed = false;
    try {
      dismissed = localStorage.getItem(HINT_KEY) === '1';
    } catch {
      /* no-op */
    }
    if (dismissed) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '?') return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isEditableTarget(e.target)) return;
      e.preventDefault();
      setOpen(true);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const dismiss = () => {
    setOpen(false);
    try {
      localStorage.setItem(HINT_KEY, '1');
    } catch {
      /* no-op */
    }
  };
  useEscapeStack(dismiss, open);

  if (!open) return null;
  return (
    <div className="hotkeys-hint" role="dialog" aria-label="Hotkeys" onClick={dismiss} data-testid="hotkeys-hint" data-hotkey-exclude>
      <div className="hotkeys-hint-card" onClick={(e) => e.stopPropagation()}>
        <h3>Hotkeys</h3>
        <ul>
          <li>
            <kbd>K</kbd> — палитра команд (поиск/переход)
          </li>
          <li>
            <kbd>Esc</kbd> — закрыть верхний оверлей
          </li>
          <li>
            <kbd>?</kbd> — эта подсказка
          </li>
        </ul>
        <button type="button" className="btn btn-small" onClick={dismiss} data-testid="hotkeys-hint-dismiss">
          Понятно
        </button>
      </div>
    </div>
  );
}
