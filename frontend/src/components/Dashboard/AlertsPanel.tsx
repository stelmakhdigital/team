import { memo } from 'react';
import type { WatchdogAlert } from '../../types/api';
import { formatRelative } from '../../lib/format';
import { Badge, EmptyState } from '../ui/States';

function AlertsPanelInner({ alerts }: { alerts: WatchdogAlert[] }) {
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
                <Badge kind="sev" value={a.severity} />
                <span className="alert-type">{a.event_type}</span>
                <span className="muted small">{a.team_name}</span>
                {a.requires_action && !a.is_read && <Badge kind="warn">action required</Badge>}
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

export default memo(AlertsPanelInner);
