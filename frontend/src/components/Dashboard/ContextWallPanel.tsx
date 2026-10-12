import { useState } from 'react';
import { useQuery } from '../../hooks/useQuery';
import { api } from '../../api';
import { ErrorState, PageSkeleton } from '../ui/States';
import HandoverModal from '../HandoverModal';

// P1.5 — Dashboard-панель "Context wall": сессии у контекст-стены (>= порога).
export default function ContextWallPanel() {
  const plan = useQuery('dashboard.compactPlan', () => api.sessions.getCompactPlan());
  const [handoverId, setHandoverId] = useState<number | null>(null);

  if (plan.loading) return <div className="card"><PageSkeleton label="Context wall…" /></div>;
  if (plan.error) return <div className="card"><ErrorState error={plan.error} onRetry={plan.refetch} /></div>;

  const entries = plan.data?.sessions ?? [];
  const wall = plan.data?.wall_pct ?? 80;
  return (
    <div className="card">
      <h2>Context wall</h2>
      {entries.length === 0 ? (
        <div className="empty-line">Нет сессий выше порога {wall}% · Сессии появятся, когда контекст вырастёт.</div>
      ) : (
        <ul className="context-wall-list">
          {entries.map((e) => (
            <li key={e.session_id} className="context-wall-item">
              <div className="context-wall-head">
                <strong>{e.role_name}</strong>
                <span className="muted small">/ {e.team_name}</span>
                <span className="context-wall-pct">{e.context_used_percentage ?? '–'}%</span>
              </div>
              <div className="muted small">
                {e.model ? `${e.model} · ` : ''}сессия #{e.session_id} ·{' '}
                {e.suggested_action === 'wait_for_turn' ? 'ожидает завершения хода' : 'готов к handover'}
              </div>
              <button type="button" className="btn btn-sm" onClick={() => setHandoverId(e.session_id)}>
                Продолжить в новой сессии
              </button>
            </li>
          ))}
        </ul>
      )}
      {handoverId != null && <HandoverModal sessionId={handoverId} onClose={() => setHandoverId(null)} />}
    </div>
  );
}
