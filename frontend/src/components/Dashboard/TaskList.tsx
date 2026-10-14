import { memo } from 'react';
import type { Task } from '../../types/api';
import { formatRelative, formatDateTime, stateTitle } from '../../lib/format';
import { Badge, EmptyState } from '../ui/States';
import EntityLink from '../ui/EntityLink';

function TaskListInner({ tasks }: { tasks: Task[] }) {
  if (tasks.length === 0) {
    return <EmptyState title="No active tasks" hint="Tasks created by leads will appear here." />;
  }
  return (
    <div className="card">
      <h2>Active tasks</h2>
      <div className="table-wrap"><table className="table">
        <thead>
          <tr>
            <th scope="col">Task</th>
            <th scope="col">Team</th>
            <th scope="col">Assigned to</th>
            <th scope="col">State</th>
            <th scope="col">Progress</th>
            <th scope="col">Updated</th>
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <tr key={t.id} className={t.is_stale ? 'row-stale' : ''}>
              <td>
                <div className="task-title">
                  <EntityLink to={`/tasks/${t.id}`} title="Open task details">{t.title}</EntityLink>
                  {t.is_stale && <Badge kind="warn" title="Not updated for more than 2h">stale</Badge>}
                </div>
                <div className="muted small">#{t.id} · priority {t.priority}</div>
              </td>
              <td><EntityLink to={`/teams/${t.team_id}`} title="Open team">{t.team_name}</EntityLink></td>
              <td>{t.destination_role_name}</td>
              <td>
                <Badge kind="task" value={t.state} title={stateTitle(t.state)} />
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
              <td className="muted" title={formatDateTime(t.updated_at)}>{formatRelative(t.updated_at)}</td>
            </tr>
          ))}
        </tbody>
      </table></div>
    </div>
  );
}

export default memo(TaskListInner);
