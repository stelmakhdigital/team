import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { EmptyState, ErrorState, PageSkeleton, errorMessage } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';
import type { WatchdogJob } from '../types/api';

export default function WatchdogJobsPage() {
  const { data, loading, error, refetch } = useQuery('watchdogJobs.list', () => api.watchdogJobs.list(), []);
  const { data: sessionsData } = useQuery('sessions.list', () => api.sessions.list(), []);
  const { toast } = useToast();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [sessionId, setSessionId] = useState<number | ''>('');
  const [threshold, setThreshold] = useState<number>(80);
  const [message, setMessage] = useState('');

  const register = useMutation((req: { name: string; target_session_id: number; threshold_pct: number; message?: string }) =>
    api.watchdogJobs.register(req),
  );
  const stop = useMutation((id: number) => api.watchdogJobs.stop(id));
  const remove = useMutation((id: number) => api.watchdogJobs.remove(id));

  const sessions = (sessionsData?.sessions ?? []).filter((s) =>
    ['starting', 'running', 'idle', 'stopping'].includes(s.state),
  );

  const submit = async () => {
    if (!name.trim() || sessionId === '') return;
    try {
      await register.mutate({
        name: name.trim(),
        target_session_id: Number(sessionId),
        threshold_pct: threshold,
        message: message.trim() || undefined,
      });
      toast('success', `Watchdog job '${name.trim()}' registered`);
      setName(''); setSessionId(''); setThreshold(80); setMessage(''); setCreating(false);
      refetch();
    } catch {
      toast('error', errorMessage(register.error ?? new Error('Failed to register job')));
    }
  };

  const doStop = async (j: WatchdogJob) => {
    try {
      await stop.mutate(j.id);
      toast('success', `Job '${j.name}' stopped`);
      refetch();
    } catch {
      toast('error', errorMessage(stop.error ?? new Error('Failed to stop job')));
    }
  };

  const doDelete = async (j: WatchdogJob) => {
    if (j.fired_count > 0) {
      toast('error', 'Job has fired history — stop it instead of deleting');
      return;
    }
    if (!window.confirm(`Delete watchdog job '${j.name}'?`)) return;
    try {
      await remove.mutate(j.id);
      toast('success', `Job '${j.name}' deleted`);
      refetch();
    } catch {
      toast('error', errorMessage(remove.error ?? new Error('Failed to delete job')));
    }
  };

  if (loading && !data) return <PageSkeleton rows={5} />;

  return (
    <div className="page">
      <div className="page-head">
        <h1>Watchdog jobs</h1>
        <div className="page-head-actions">
          <button className="btn btn-primary" onClick={() => setCreating((v) => !v)}>
            {creating ? '× Cancel' : '+ Новый джоб'}
          </button>
        </div>
      </div>

      {creating && (
        <div className="panel watchdog-form">
          <h3>Новый watchdog-джоб</h3>
          <div className="watchdog-form-grid">
            <label>
              Name
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="worker ctx" />
            </label>
            <label>
              Target-сессия
              <select value={sessionId} onChange={(e) => setSessionId(e.target.value ? Number(e.target.value) : '')}>
                <option value="">— выбрать сессию —</option>
                {sessions.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.id} · {s.role_name ?? 'role'} ({s.state}{s.context_used_percentage != null ? `, ${Math.round(s.context_used_percentage)}%` : ''})
                  </option>
                ))}
              </select>
            </label>
            <label>
              Порог, %
              <input
                type="number" min={1} max={100} value={threshold}
                onChange={(e) => setThreshold(Number(e.target.value))}
              />
            </label>
            <label>
              Message
              <input value={message} onChange={(e) => setMessage(e.target.value)} placeholder="context high" />
            </label>
          </div>
          <div className="watchdog-form-actions">
            <button
              className="btn btn-primary"
              disabled={register.pending || !name.trim() || sessionId === ''}
              onClick={submit}
            >
              {register.pending ? 'Registering…' : 'Register'}
            </button>
            {register.error && <span className="form-error">{errorMessage(register.error)}</span>}
          </div>
          <p className="hint">
            Политика <code>context-usage-threshold</code>: когда live-контекст сессии пересекает
            порог — алерт в Attention (повтор — при росте &gt;5 п.п.).
          </p>
        </div>
      )}

      {error ? (
        <ErrorState error={error} onRetry={refetch} />
      ) : !data || data.jobs.length === 0 ? (
        <EmptyState title="Нет watchdog-джобов" hint="Создайте первый: выберите сессию и порог контекста." action={<button className="btn btn-primary" onClick={() => setCreating(true)}>+ Новый джоб</button>} />
      ) : (
        <div className="panel">
          <table className="data-table watchdog-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Policy</th>
                <th>Сессия</th>
                <th>Порог</th>
                <th>Статус</th>
                <th>Fired</th>
                <th>Последний fire</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {data.jobs.map((j) => (
                <tr key={j.id}>
                  <td><strong>{j.name}</strong>{j.message ? <div className="hint">{j.message}</div> : null}</td>
                  <td><code>{j.policy}</code></td>
                  <td><Link to={`/sessions/${j.target_session_id}`}>#{j.target_session_id}</Link></td>
                  <td>{j.threshold_pct}%</td>
                  <td>
                    <span className={`watchdog-status ${j.enabled ? 'watchdog-status--active' : 'watchdog-status--stopped'}`}>
                      {j.enabled ? 'active' : 'stopped'}
                    </span>
                  </td>
                  <td>{j.fired_count}</td>
                  <td>{j.last_fired_at ? <>{Math.round(j.last_fired_pct ?? 0)}% · {formatRelative(j.last_fired_at)}</> : '—'}</td>
                  <td className="row-actions">
                    {j.enabled ? (
                      <button className="btn btn-ghost" disabled={stop.pending} onClick={() => doStop(j)}>Stop</button>
                    ) : (
                      <span className="muted-text">—</span>
                    )}
                    <button className="btn btn-ghost" disabled={remove.pending} onClick={() => doDelete(j)}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
