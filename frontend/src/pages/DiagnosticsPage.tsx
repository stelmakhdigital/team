import { useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import type { DiagnosticsResult } from '../types/api';
import { EmptyState, ErrorState, PageSkeleton, errorMessage } from '../components/ui/States';

const STATUS_LABEL: Record<string, string> = {
  ok: 'ok',
  warn: 'warn',
  fail: 'fail',
};

const OVERALL_LABEL: Record<string, string> = {
  ok: '✓ Всё в порядке',
  warn: '⚠ Есть предупреждения',
  fail: '✖ Требуют внимания (расхождения)',
};

// P1.8 — Health/Doctor-панель: «the surface that shows the truth».
export default function DiagnosticsPage() {
  const { data, loading, error, refetch } = useQuery('diagnostics', () => api.diagnostics.get(), []);
  const [override, setOverride] = useState<DiagnosticsResult | null>(null);
  const run = useMutation(() => api.diagnostics.run());
  const result = override ?? data;

  const doRun = () => {
    setOverride(null);
    run.mutate().then((r) => {
      setOverride(r);
      refetch();
    }).catch(() => {});
  };

  if (loading && !result) return <PageSkeleton rows={7} label="Проверяю здоровье…" />;
  if (error && !result) return <div className="page"><ErrorState error={error} onRetry={refetch} /></div>;
  if (!result) return <div className="page"><EmptyState title="Нет данных" hint="Попробуйте перезапустить" /></div>;

  const counts = { ok: 0, warn: 0, fail: 0 } as Record<string, number>;
  for (const c of result.checks) counts[c.status] = (counts[c.status] ?? 0) + 1;

  return (
    <div className="page">
      <div className="page-head">
        <h1>Health / Doctor</h1>
        <div className="page-head-actions">
          <button className="btn btn-primary" onClick={doRun} disabled={run.pending}>
            {run.pending ? 'Проверяю…' : '⟳ Run full diagnostics'}
          </button>
        </div>
      </div>

      <div className={`doctor-overall doctor-overall--${result.overall}`}>
        <strong>{OVERALL_LABEL[result.overall]}</strong>
        <span className="muted small">
          {' '}{result.checks.length} checks · {counts.ok} ok · {counts.warn ?? 0} warn · {counts.fail ?? 0} fail
        </span>
        <span className="muted small"> · generated {new Date(result.generated_at).toLocaleTimeString()}</span>
      </div>

      {run.error ? <div className="error-banner">{errorMessage(run.error)}</div> : null}

      <div className="doctor-list">
        {result.checks.map((c) => (
          <div key={c.name} className={`doctor-item doctor-item--${c.status}`}>
            <div className="doctor-item-head">
              <span className={`doctor-status doctor-status--${c.status}`}>{STATUS_LABEL[c.status]}</span>
              <code className="doctor-name">{c.name}</code>
              <span className="doctor-detail">{c.detail}</span>
            </div>
            {c.evidence && Object.keys(c.evidence).length > 0 ? (
              <pre className="doctor-evidence">{JSON.stringify(c.evidence, null, 2)}</pre>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}
