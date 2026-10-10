import { useEffect, useState } from 'react';
import type { BriefingPreviewResponse, Role } from '../types/api';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { errorMessage } from './ui/States';

/**
 * P1.7 — BriefingPreviewModal: «Предпросмотр брифинга» (dry-run, без запуска).
 * Показывает, какой briefing получит агент при запуске роли + как он будет передан.
 */
export default function BriefingPreviewModal({
  teamId,
  role,
  onClose,
}: {
  teamId: number;
  role: Role;
  onClose: () => void;
}) {
  const [preview, setPreview] = useState<BriefingPreviewResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [override, setOverride] = useState('');
  const [append, setAppend] = useState('');

  const mutate = useMutation(() =>
    api.sessions.previewBriefing(teamId, {
      role_id: role.id,
      override: override.trim() || undefined,
      append: append.trim() || undefined,
    }),
  );

  useEffect(() => {
    mutate.mutate().then(setPreview).catch((e) => setError(errorMessage(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [teamId, role.id]);

  const refresh = () => {
    setError(null);
    setPreview(null);
    mutate.mutate().then(setPreview).catch((e) => setError(errorMessage(e)));
  };

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal briefing-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>Предпросмотр брифинга</h2>
          <button className="btn btn-icon" onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </div>

        <p className="muted small">
          Роль <strong>{role.name}</strong> — вот какой briefing агент получит
          при запуске (dry-run, сессия не стартует).
        </p>

        <div className="briefing-opts">
          <label className="field">
            <span>Override (вместо builtin, optional)</span>
            <textarea
              rows={2}
              value={override}
              onChange={(e) => setOverride(e.target.value)}
              placeholder="Свой briefing (перекрывает стандартный)"
            />
          </label>
          <label className="field">
            <span>Append (добавка, optional)</span>
            <input value={append} onChange={(e) => setAppend(e.target.value)} placeholder="Доп. контекст в конец" />
          </label>
          <button className="btn" onClick={refresh} disabled={mutate.pending}>
            {mutate.pending ? 'Собираю…' : 'Обновить'}
          </button>
        </div>

        {error ? <div className="error-banner">{error}</div> : null}

        {preview ? (
          <>
            <div className="briefing-delivery">
              Передача: <strong>{preview.delivery.mode === 'pi-system-prompt' ? 'pi → system_prompt' : 'файл + env'}</strong>
              <span className="muted small"> — {preview.delivery.detail}</span>
              <span className="badge badge--entity" style={{ marginLeft: 8 }}>
                {preview.briefing_applied}
              </span>
            </div>
            <pre className="briefing-text">{preview.briefing_text}</pre>
          </>
        ) : !mutate.pending ? null : (
          <div className="briefing-loading muted">Собираю briefing…</div>
        )}
      </div>
    </div>
  );
}
