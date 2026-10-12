import { memo } from 'react';
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import type { DashboardSummaryResponse } from '../../types/api';
import { formatRelative } from '../../lib/format';

function SummaryCardsInner({ summary }: { summary: DashboardSummaryResponse }) {
  const cards: { label: string; value: ReactNode; sub: ReactNode }[] = [
    { label: 'Teams', value: `${summary.teams.active}/${summary.teams.total}`, sub: 'active / total' },
    {
      label: 'Tasks',
      value: summary.tasks.in_progress,
      sub: `${summary.tasks.pending} pending · ${summary.tasks.blocked} blocked · ${summary.tasks.done_today} done today`,
    },
    {
      label: 'Sessions',
      value: summary.sessions.running,
      // B-2: «N failed» — кликабельно → /sessions?state=failed (быстрый доступ к упавшим).
      sub: (
        <>
          {summary.sessions.failed > 0 ? (
            <Link to="/sessions?state=failed" title="View failed sessions">
              {summary.sessions.failed} failed
            </Link>
          ) : (
            '0 failed'
          )}{' '}
          of {summary.sessions.total}
        </>
      ),
    },
    {
      label: 'Alerts',
      value: summary.alerts.total,
      sub: `${summary.alerts.critical} critical · ${summary.alerts.warning} warning`,
    },
  ];
  return (
    <section aria-label="Summary">
      <div className="cards-grid">
        {cards.map((c) => (
          <div key={c.label} className="card stat-card">
            <div className="stat-label">{c.label}</div>
            <div className="stat-value">{c.value}</div>
            <div className="stat-sub">{c.sub}</div>
          </div>
        ))}
      </div>
      <div className="updated-at">Updated {formatRelative(summary.updated_at)}</div>
    </section>
  );
}

export default memo(SummaryCardsInner);
