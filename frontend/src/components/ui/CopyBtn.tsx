import { useRef, useState } from 'react';
import { useToast } from './Toast';

// U-X10: копирование в буфер обмена с визуальным фидбеком («✓ Скопировано»)
// и честной ошибкой через toast, если браузер не дал доступ к clipboard.
export function CopyBtn({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number>(0);
  const { toast } = useToast();

  const onClick = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1600);
    } catch {
      toast('error', 'Браузер не дал доступ к буферу обмена');
    }
  };

  return (
    <button
      type="button"
      className={`btn btn-sm copy-btn${copied ? ' copy-btn--ok' : ''}`}
      title={label ? `Скопировать: ${label}` : 'Скопировать'}
      aria-label={label ? `Скопировать ${label}` : 'Скопировать'}
      onClick={onClick}
    >
      {copied ? '✓ Скопировано' : '⧉'}
    </button>
  );
}
