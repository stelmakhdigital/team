import type { WatchdogAlert } from '../../types/api';
import { formatRelative } from '../../lib/format';
import { EmptyState } from '../ui/States';

export default function AlertsPanel({ alerts }: { alerts: WatchdogAlert[] }) {
  return (
    <div className="card">
      <h2>Watchdog alerts</h2>
      {alerts.length === 0 ? (
        <EmptyState title="No alerts" hint="All agents are on track." />
      ) : (
        <ul className="alert-list">
          {alerts.map((a) => (
            <li key={a.id} className={`alert alert-${a.severity}${a.is_read ? ' alert-read' : ''}`}>
              <div className="alert-head">
                <span className="badge badge-sev-{a.severity}">{a.severity}</span>
                <span className="alert-type">{a.event_type}</span>
                <span className="muted small">{a.team_name}</span>
                {a.requires_action && !a.is_read && <span className="badge badge-warn">action required</span>}
              </div>
              <div className="alert-desc">{a.description}</div>
              <div className="muted small">
                {formatRelative(a.created_at)}
                {a.action_taken ? ` · action: ${a.action_taken}` : ''}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
