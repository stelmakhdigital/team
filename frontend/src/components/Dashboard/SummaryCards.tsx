import type { DashboardSummaryResponse } from '../../types/api';
import { formatRelative } from '../../lib/format';

export default function SummaryCards({ summary }: { summary: DashboardSummaryResponse }) {
  const cards = [
    { label: 'Teams', value: `${summary.teams.active}/${summary.teams.total}`, sub: 'active / total' },
    {
      label: 'Tasks',
      value: summary.tasks.in_progress,
      sub: `${summary.tasks.pending} pending · ${summary.tasks.blocked} blocked · ${summary.tasks.done_today} done today`,
    },
    {
      label: 'Sessions',
      value: summary.sessions.running,
      sub: `${summary.sessions.failed} failed of ${summary.sessions.total}`,
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
