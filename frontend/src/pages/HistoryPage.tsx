import { useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { EmptyState, ErrorState, Spinner } from '../components/ui/States';
import { formatDateTime, formatRelative } from '../lib/format';

export default function HistoryPage() {
  const audit = useQuery('history.audit', () => api.history.getAuditLog({ limit: 50 }));
  const taskHistory = useQuery('history.task.1', () => api.history.getTaskHistory(1));
  const transcript = useQuery('history.transcript.1', () => api.history.getTranscript(1));

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
            ['task', 'Task #1 history'],
            ['transcript', 'Session #1 transcript'],
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
          {audit.error && <ErrorState error={audit.error} onRetry={audit.refetch} />}
          {audit.data && audit.data.entries.length === 0 && <EmptyState title="No audit entries" />}
          {audit.data && audit.data.entries.length > 0 && (
            <div className="card">
              <table className="table">
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
                        <span className={`badge badge-sev-${e.severity ?? 'info'}`}>{e.severity ?? 'info'}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {tab === 'task' && (
        <>
          {taskHistory.loading && <Spinner label="Loading task history…" />}
          {taskHistory.error && <ErrorState error={taskHistory.error} onRetry={taskHistory.refetch} />}
          {taskHistory.data && taskHistory.data.history.length === 0 && <EmptyState title="No history for this task" />}
          {taskHistory.data && taskHistory.data.history.length > 0 && (
            <div className="card">
              <h2>Task #1 timeline</h2>
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
          {transcript.data && (
            <div className="card">
              <h2>Session #1 transcript</h2>
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
