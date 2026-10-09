import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { ErrorState, Spinner, Badge } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';
import { formatDateTime, formatRelative } from '../lib/format';

// U4b: task drill-down — full task detail + history timeline + subtasks (fetch-by-id).
export default function TaskDetailPage() {
  const { taskId } = useParams();
  const id = Number(taskId);
  const task = useQuery(`task.${id}`, () => api.tasks.get(id));
  const hist = useQuery(`task.history.${id}`, () => api.history.getTaskHistory(id));

  if (task.loading) return <div className="page"><Spinner label="Loading task…" /></div>;
  if (task.error) return <div className="page"><ErrorState error={task.error} onRetry={task.refetch} /></div>;

  const t = task.data!.task;
  const subtasks = task.data!.subtasks ?? [];
  const history = hist.data?.history ?? [];

  const rows: Array<[string, React.ReactNode]> = [
    ['Team', t.team_name],
    ['Assigned to', t.destination_role_name],
    ...(t.source_role_name ? ([['Source', t.source_role_name]] as Array<[string, React.ReactNode]>) : []),
    ['Priority', String(t.priority)],
    ['Progress', t.progress !== undefined ? `${t.progress}%` : '—'],
    ['Closure', t.closure_reason ? `${t.closure_reason}` : '—'],
    ['Created', formatDateTime(t.created_at)],
    ['Updated', `${formatRelative(t.updated_at)} (${formatDateTime(t.updated_at)})`],
  ];

  return (
    <div className="page">
      <div className="page-head">
        <h1>
          <Link to="/tasks" className="crumb">Tasks</Link> / {t.title}
        </h1>
        <Badge kind="task" value={t.state} />
      </div>

      <div className="detail-layout">
        <div className="detail-main">
          <div className="card">
            <h2>Task #{t.id}</h2>
            <dl className="cfg-meta-grid">
              {rows.map(([k, v]) => (
                <div key={k} className="cfg-meta-item"><dt>{k}</dt><dd>{v}</dd></div>
              ))}
            </dl>
            {t.body && (
              <>
                <h4>Body</h4>
                <pre className="spec-pre">{t.body}</pre>
              </>
            )}
          </div>

          {subtasks.length > 0 && (
            <div className="card">
              <h2>Subtasks ({subtasks.length})</h2>
              <ul className="subtask-list">
                {subtasks.map((st) => (
                  <li key={st.id}>
                    <EntityLink to={`/tasks/${st.id}`} title="Open subtask">
                      <code>#{st.id}</code> {st.title}
                    </EntityLink>
                    <Badge kind="task" value={st.state} />
                    <span className="muted small">{st.destination_role_name}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>

        <aside className="card detail-pane">
          <h2>History</h2>
          {hist.loading ? (
            <Spinner />
          ) : history.length === 0 ? (
            <p className="muted">No history events.</p>
          ) : (
            <ul className="hist-list">
              {history.map((h) => (
                <li key={h.id} className="hist-entry">
                  <div className="muted small">
                    {formatRelative(h.created_at)} · {h.actor_type}
                    {h.actor_role_name ? ` · ${h.actor_role_name}` : ''}
                  </div>
                  <div className="hist-transition">
                    {h.from_state && <span className="muted">{h.from_state} →</span>} <strong>{h.to_state}</strong>
                  </div>
                  {h.closure_reason && <div className="muted small">closure: {h.closure_reason}{h.closure_target_title ? ` → ${h.closure_target_title}` : ''}</div>}
                  {h.comment && <div className="small">{h.comment}</div>}
                </li>
              ))}
            </ul>
          )}
        </aside>
      </div>
    </div>
  );
}
