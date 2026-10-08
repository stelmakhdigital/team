import { memo } from 'react';
import type { Task } from '../../types/api';
import { formatRelative } from '../../lib/format';
import { Badge, EmptyState } from '../ui/States';

function TaskListInner({ tasks }: { tasks: Task[] }) {
  if (tasks.length === 0) {
    return <EmptyState title="No active tasks" hint="Tasks created by leads will appear here." />;
  }
  return (
    <div className="card">
      <h2>Active tasks</h2>
      <table className="table">
        <thead>
          <tr>
            <th>Task</th>
            <th>Team</th>
            <th>Assigned to</th>
            <th>State</th>
            <th>Progress</th>
            <th>Updated</th>
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <tr key={t.id} className={t.is_stale ? 'row-stale' : ''}>
              <td>
                <div className="task-title">
                  {t.title}
                  {t.is_stale && <Badge kind="warn" title="Not updated for more than 2h">stale</Badge>}
                </div>
                <div className="muted small">#{t.id} · priority {t.priority}</div>
              </td>
              <td>{t.team_name}</td>
              <td>{t.destination_role_name}</td>
              <td>
                <Badge kind="task" value={t.state} />
              </td>
              <td>
                {t.progress !== undefined ? (
                  <div className="progress" role="progressbar" aria-valuenow={t.progress} aria-valuemin={0} aria-valuemax={100}>
                    <div className="progress-fill" style={{ width: `${t.progress}%` }} />
                  </div>
                ) : (
                  <span className="muted">—</span>
                )}
              </td>
              <td className="muted">{formatRelative(t.updated_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default memo(TaskListInner);
