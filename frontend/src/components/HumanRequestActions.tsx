import { useState } from 'react';
import { useMutation } from '../hooks/useMutation';
import { api } from '../api';

// P3.16 — Approve/Deny для pending human_request (в Attention view).
// id — числовой id human_request (из attention item: "hreq-<id>").
export default function HumanRequestActions({
  id,
  operation,
  onDecided,
}: {
  id: number;
  operation: string;
  onDecided: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const m = useMutation(async (decision: 'approve' | 'deny', reason: string) =>
    api.posture.decideHumanRequest(id, decision, reason || undefined),
  );

  const doDecide = async (decision: 'approve' | 'deny') => {
    setError(null);
    const reason = window.prompt(
      decision === 'approve' ? 'Причина (опционально):' : 'Причина отказа (опционально):',
      '',
    );
    if (reason === null) return; // cancelled
    try {
      await m.mutate(decision, reason);
      onDecided();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unknown error');
    }
  };

  return (
    <div className="hreq-actions">
      <button
        type="button"
        className="btn btn-sm btn-primary"
        disabled={m.pending}
        onClick={() => doDecide('approve')}
        title={`Подтвердить: ${operation}`}
      >
        {m.pending && m.error === null ? '…' : 'Approve'}
      </button>
      <button type="button" className="btn btn-sm" disabled={m.pending} onClick={() => doDecide('deny')}>
        Deny
      </button>
      {error ? (
        <span className="form-error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}
