import { useState } from 'react';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { errorMessage } from './ui/States';
import type { SendToSessionResult } from '../types/api';

// P2.11: Send to session + verify — ввод текста в терминал живой сессии (tmux/pi)
// с честным статусом доставки (delivered/staged/retained). Dangerous: влияет на
// работающий процесс → warning + RBAC operator+.

const STATUS_LABEL: Record<SendToSessionResult['status'], string> = {
  delivered: 'delivered · подтверждено capture-м',
  staged: 'staged · введено, не подтверждено',
  retained: 'retained · не отправлено (guard)',
};

export default function SendToSession({ sessionId }: { sessionId: number }) {
  const [text, setText] = useState('');
  const [submit, setSubmit] = useState(true);
  const [tailOpen, setTailOpen] = useState(false);
  const { mutate, pending, error } = useMutation(() => api.sessions.send(sessionId, text, submit));
  const [result, setResult] = useState<SendToSessionResult | null>(null);

  const canSend = !pending && text.trim().length > 0;

  const onSend = async () => {
    if (!canSend) return;
    setTailOpen(false);
    try {
      const res = await mutate();
      setResult(res);
    } catch {
      /* error — в useMutation */
    }
  };

  return (
    <div className="card send-card">
      <div className="card-head">
        <h2>Send to session</h2>
        {result ? (
          <span className={`send-status send-status-${result.status}`}>{result.status}</span>
        ) : null}
      </div>
      <p className="send-warning">
        {'⚠\uFE0E'} Текст будет введён в терминал живой сессии (действие на работающий процесс).
      </p>
      <textarea
        className="send-input"
        rows={3}
        placeholder="Текст для ввода в сессию… (≤ 2000 символов)"
        value={text}
        onChange={(e) => setText(e.target.value)}
        disabled={pending}
        aria-label="Текст для отправки в сессию"
      />
      <div className="send-controls">
        <label className="send-submit">
          <input
            type="checkbox"
            checked={submit}
            onChange={(e) => setSubmit(e.target.checked)}
            disabled={pending}
          />
          Submit (Enter после текста)
        </label>
        <button type="button" className="btn btn-primary btn-sm" onClick={onSend} disabled={!canSend}>
          {pending ? '…' : 'Send'}
        </button>
      </div>
      {pending ? <p className="muted small">Отправляем…</p> : null}
      {error ? (
        // P3: честный текст через errorMessage (дружественные коды вместо сырого HTTP)
        <p className="send-error" role="alert">{errorMessage(error)}</p>
      ) : null}
      {result ? (
        <div className="send-result">
          <p className="send-result-line">
            <strong>{STATUS_LABEL[result.status]}</strong>
            {result.guard ? <span className="muted small"> · guard: {result.guard}</span> : null}
          </p>
          {result.verified && result.tail && result.tail.length > 0 ? (
            <>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => setTailOpen((v) => !v)}
                aria-expanded={tailOpen}
              >
                {tailOpen ? '▾' : '▸'} Capture tail ({result.tail.length})
              </button>
              {tailOpen ? (
                <pre className="send-tail">{result.tail.join('\n')}</pre>
              ) : null}
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
