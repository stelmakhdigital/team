
import { useEscapeStack } from '../hooks/useEscapeStack';
import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import type { CaptureResult } from '../types/api';
import { useMutation } from '../hooks/useMutation';
import { ErrorState, Spinner } from './ui/States';
import { useToast } from './ui/Toast';
import { formatRelative } from '../lib/format';

/**
 * P2.8 — CaptureModal: стл-кадр экрана сессии (последние N строк) + сохранение.
 * tmux/pi → tmux-pane (мёртвый tmux → 409 с причиной); process → log-tail.
 * W5.3: опциональный taskId (когда контекст задачи известен, напр. с detail
 * сессии по роли) → кнопка «→ Evidence»: navigate в EvidencePanel задачи с
 * предзаполнением content текстом capture (navigate state).
 */
export default function CaptureModal({ sessionId, taskId, onClose }: { sessionId: number; taskId?: number; onClose: () => void }) {
  const [lines, setLines] = useState(20);
  const [res, setRes] = useState<CaptureResult | null>(null);
  const [copied, setCopied] = useState(false);
  const captureMut = useMutation((n: number) => api.sessions.capture(sessionId, n));
  const navigate = useNavigate();
  const { toast } = useToast();

  const doCapture = useCallback(
    (n: number) => {
      setRes(null);
      setCopied(false);
      captureMut.mutate(n).then((r) => setRes(r)).catch(() => { /* error в captureMut.error */ });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  // capture на открытие
  useEffect(() => {
    doCapture(lines);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const copy = async () => {
    if (!res) return;
    try {
      await navigator.clipboard.writeText(res.lines.join('\n'));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // W5.3: clipboard может быть заблокирован — сообщаем тостом, не молча
      toast('error', 'Clipboard недоступен — скопируйте текст вручную из снапшота');
    }
  };

  // W5.3: «→ Evidence» — navigate в EvidencePanel задачи + предзаполнение content
  const toEvidence = () => {
    if (taskId == null || !res) return;
    navigate(`/tasks/${taskId}`, {
      state: { evidencePrefill: { content: res.lines.join('\n') } },
    });
    onClose();
  };

  const err = captureMut.error;
    // W3.1: Esc закрывает модалку (если она верхний оверлей)
  useEscapeStack(onClose, true);

return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" data-hotkey-exclude onClick={onClose}>
      <div className="modal capture-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>⎙ Capture — Session #{sessionId}</h2>
          <button type="button" className="modal-close" aria-label="Close" onClick={onClose}>×</button>
        </div>
        <div className="modal-body">
          <div className="capture-controls">
            <label className="capture-lines-label">
              Lines
              <select value={lines} onChange={(e) => setLines(Number(e.target.value))}>
                <option value={10}>10</option>
                <option value={20}>20</option>
                <option value={50}>50</option>
                <option value={100}>100</option>
                <option value={200}>200</option>
              </select>
            </label>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => doCapture(lines)}
              disabled={captureMut.pending}
            >
              {captureMut.pending ? 'Снимаю…' : '⎙ Capture'}
            </button>
          </div>

          {captureMut.pending ? <Spinner /> : null}

          {err ? (
            <div className="capture-error">
              <ErrorState error={err} onRetry={() => doCapture(lines)} />
              {err.status === 409 && err.message ? (
                <p className="muted small capture-error-reason">Причина: {err.message}</p>
              ) : null}
            </div>
          ) : null}

          {res && !err ? (
            <div className="capture-result">
              <div className="capture-meta">
                <span className="badge badge--entity">{res.source}</span>
                <span className="muted small"> · captured {formatRelative(res.captured_at)}</span>
              </div>
              <pre className="capture-snapshot">{res.lines.join('\n') || '(empty)'}</pre>
              <div className="capture-saved">
                <span className="muted small">Saved:</span>{' '}
                <code className="capture-path">{res.saved_path}</code>
              </div>
              <div className="capture-actions">
                <button type="button" className="btn btn-sm" onClick={copy}>
                  {copied ? '✓ Copied' : '⧉ Copy'}
                </button>
                {taskId != null && (
                  <button
                    type="button"
                    className="btn btn-sm btn-primary"
                    onClick={toEvidence}
                    title="Открыть Evidence задачи и вставить capture как артефакт"
                  >
                    → Evidence
                  </button>
                )}
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
