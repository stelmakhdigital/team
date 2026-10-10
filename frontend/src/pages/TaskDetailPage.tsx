import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import { ErrorState, PageSkeleton, Spinner, Badge } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import EntityLink from '../components/ui/EntityLink';
import { formatDateTime, formatRelative, formatRelativeFromSecs } from '../lib/format';
import type { Task, TaskWake } from '../types/api';

// U4b: task drill-down — full task detail + history timeline + subtasks (fetch-by-id).
export default function TaskDetailPage() {
  const { taskId } = useParams();
  const id = Number(taskId);
  const task = useQuery(`task.${id}`, () => api.tasks.get(id));
  const hist = useQuery(`task.history.${id}`, () => api.history.getTaskHistory(id));

  if (task.loading) return <div className="page"><PageSkeleton label="Loading task…" /></div>;
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
    [
      'Deadline',
      t.closure_required_at ? (
        <span>
          {formatDateTime(t.closure_required_at)}
          {t.is_overdue ? (
            <span className="due-overdue"> OVERDUE</span>
          ) : (
            t.remaining_secs != null ? (
              <span className="muted small"> · {formatRelativeFromSecs(t.remaining_secs)} left</span>
            ) : null
          )}
        </span>
      ) : (
        '—'
      ),
    ],
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

          {(t.state === 'blocked' || task.data!.wake) && (
            <WakePanel
              task={t}
              wake={task.data!.wake}
              onRefetch={() => {
                task.refetch();
                hist.refetch();
              }}
            />
          )}

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

// P0.3: панель wake (block with wake) + ручной resolve.
function WakePanel({ task, wake, onRefetch }: { task: Task; wake?: TaskWake; onRefetch: () => void }) {
  const { toast } = useToast();
  const [decision, setDecision] = useState('');
  const resolve = useMutation((d: string) => api.tasks.resolve(task.id, { decision: d }));
  const isBlocked = task.state === 'blocked';
  const armed = !!wake && !wake.fired;

  const doResolve = async () => {
    if (!decision.trim()) return;
    try {
      await resolve.mutate(decision.trim());
      toast('success', `Task #${task.id} resolved — back to work`);
      setDecision('');
      onRefetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="card wake-panel" data-testid="wake-panel">
      <h2>
        Wake
        {armed ? <span className="wake-status wake-armed">armed</span> : wake?.fired ? <span className="wake-status wake-fired">fired</span> : null}
      </h2>
      {!wake ? (
        <p className="muted">No wake set — the task will wait until it is resolved manually.</p>
      ) : (
        <>
          <div className="wake-kind">
            <Badge kind="task" value={wake.kind}>{wake.kind}</Badge>
            {wake.kind === 'timer' ? (
              <span className="muted">
                {wake.fired ? 'fired' : `fires in ${formatRelativeFromSecs(wake.remaining_secs ?? 0)}`}
              </span>
            ) : wake.kind === 'watchdog' ? (
              <EntityLink to="/watchdog/jobs" title="Open watchdog jobs">
                watchdog job #{wake.job_id ?? wake.ref}
              </EntityLink>
            ) : (
              <EntityLink to={`/tasks/${wake.ref}`} title="Open referenced task">
                task #{wake.ref}
              </EntityLink>
            )}
          </div>
          {wake.continuation && (
            <div className="wake-continuation">
              <strong>Continue:</strong> {wake.continuation}
            </div>
          )}
          {isBlocked && (
            <div className="wake-resolve">
              <input
                value={decision}
                onChange={(e) => setDecision(e.target.value)}
                placeholder="Decision (required): why the task is back in work"
                aria-label="Resolve decision"
              />
              <button
                className="btn btn-primary"
                onClick={doResolve}
                disabled={resolve.pending || !decision.trim()}
                data-testid="wake-resolve-btn"
              >
                {resolve.pending ? 'Resolving…' : 'Resolve'}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
