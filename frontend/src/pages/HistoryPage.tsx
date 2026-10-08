import { useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, Spinner, Unavailable, isNotFoundError , Badge } from '../components/ui/States';
import { formatDateTime, formatRelative } from '../lib/format';

export default function HistoryPage() {
  const audit = useQuery('history.audit', () => api.history.getAuditLog({ limit: 50 }));
  const tasks = useQuery('history.tasks', () => api.dashboard.getTasks());
  const sessions = useQuery('history.sessions', () => api.sessions.list());
  const taskId = tasks.data?.tasks[0]?.id;
  const sessionId = sessions.data?.sessions[0]?.id;
  const taskHistory = useQuery(
    taskId != null ? `history.task.${taskId}` : 'history.task.none',
    taskId != null ? () => api.history.getTaskHistory(taskId) : async () => ({ history: [], total: 0 }),
    [taskId],
  );
  const transcript = useQuery(
    sessionId != null ? `history.transcript.${sessionId}` : 'history.transcript.none',
    sessionId != null ? () => api.history.getTranscript(sessionId) : async () => ({ transcript: [], total: 0, has_more: false }),
    [sessionId],
  );

  const [tab, setTab] = useState<'audit' | 'task' | 'transcript'>('audit');

  return (
    <div className="page">
      <div className="page-head">
        <h1>History</h1>
      </div>
      <div className="tabs" role="tablist">
        {(
          [
            ['audit', 'Audit log'],
            ['task', taskId != null ? `Task #${taskId} history` : 'Task history'],
            ['transcript', sessionId != null ? `Session #${sessionId} transcript` : 'Session transcript'],
          ] as const
        ).map(([key, label]) => (
          <button key={key} role="tab" aria-selected={tab === key} className={`tab${tab === key ? ' active' : ''}`} onClick={() => setTab(key)}>
            {label}
          </button>
        ))}
      </div>

      {tab === 'audit' && (
        <>
          {audit.loading && <Spinner label="Loading audit log…" />}
          {audit.error &&
          (isNotFoundError(audit.error) ? (
            <Unavailable feature="Audit log" hint="The audit endpoint is not implemented in the backend yet (planned)." />
          ) : (
            <ErrorState error={audit.error} onRetry={audit.refetch} />
          ))}
          {audit.data && audit.data.entries.length === 0 && <EmptyState title="No audit entries" />}
          {audit.data && audit.data.entries.length > 0 && (
            <div className="card">
              <div className="table-wrap"><table className="table">
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>User</th>
                    <th>Action</th>
                    <th>Resource</th>
                    <th>Severity</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.data.entries.map((e) => (
                    <tr key={e.id}>
                      <td className="muted">{formatDateTime(e.timestamp)}</td>
                      <td>{e.user_name ?? '—'}</td>
                      <td>
                        <code>{e.action}</code>
                      </td>
                      <td className="muted">{e.resource ?? '—'}</td>
                      <td>
                        <Badge kind="sev" value={e.severity ?? 'info'} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table></div>
            </div>
          )}
        </>
      )}

      {tab === 'task' && (
        <>
          {taskHistory.loading && <Spinner label="Loading task history…" />}
          {taskHistory.error && <ErrorState error={taskHistory.error} onRetry={taskHistory.refetch} />}
          {taskHistory.data && taskHistory.data.history.length === 0 && <EmptyState title="No task history available" />}
          {taskHistory.data && taskHistory.data.history.length > 0 && (
            <div className="card">
              <h2>Task #{taskId} timeline</h2>
              <ol className="timeline">
                {taskHistory.data.history.map((h) => (
                  <li key={h.id} className={`timeline-item tl-${h.color ?? 'blue'}`}>
                    <span className="timeline-icon" aria-hidden="true">
                      {h.icon ?? '•'}
                    </span>
                    <div>
                      <strong>
                        {h.from_state ? `${h.from_state} → ${h.to_state}` : h.to_state}
                      </strong>{' '}
                      <span className="muted small">
                        by {h.actor_type}
                        {h.actor_role_name ? ` (${h.actor_role_name})` : ''} · {formatRelative(h.created_at)}
                      </span>
                      {h.comment && <div className="muted">{h.comment}</div>}
                    </div>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </>
      )}

      {tab === 'transcript' && (
        <>
          {transcript.loading && <Spinner label="Loading transcript…" />}
          {transcript.error && <ErrorState error={transcript.error} onRetry={transcript.refetch} />}
          {transcript.data && transcript.data.transcript.length === 0 && <EmptyState title="No transcript available" />}
          {transcript.data && transcript.data.transcript.length > 0 && (
            <div className="card">
              <h2>Session #{sessionId} transcript</h2>
              {transcript.data.transcript.map((t) => (
                <div key={t.id} className={`transcript-entry ts-${t.message_type}`}>
                  <div className="muted small">
                    {t.message_type}
                    {t.metadata?.model ? ` · ${t.metadata.model}` : ''}
                    {t.metadata?.tokens ? ` · ${t.metadata.tokens} tokens` : ''}
                    {t.metadata?.latency_ms ? ` · ${t.metadata.latency_ms}ms` : ''}
                  </div>
                  <div>{t.content}</div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
