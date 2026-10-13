import { memo } from 'react';
import type { WatchdogAlert } from '../../types/api';
import { formatRelative } from '../../lib/format';
import { Badge, EmptyState } from '../ui/States';
import EntityLink from '../ui/EntityLink';

function AlertsPanelInner({ alerts }: { alerts: WatchdogAlert[] }) {
  return (
    <div className="card">
      <h2>Watchdog alerts</h2>
      {alerts.length === 0 ? (
        <EmptyState title="Алертов нет" hint="Все агенты в норме — watchdog предупреждений не поднимал." />
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
              {(a.queue_task_id || a.session_id) && (
                <div className="alert-source small">
                  source:
                  {a.queue_task_id && <EntityLink to={`/tasks/${a.queue_task_id}`} title="Open source task"> task #{a.queue_task_id}</EntityLink>}
                  {a.session_id && <EntityLink to={`/sessions/${a.session_id}`} title="Open source session"> session #{a.session_id}</EntityLink>}
                </div>
              )}
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
