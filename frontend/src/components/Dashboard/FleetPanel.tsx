import { useEffect, useRef } from 'react';
import { api } from '../../api';
import { useQuery } from '../../hooks/useQuery';
import { composeFleetRows, FLEET_MAX_ROWS, fleetCtxTone } from '../../lib/fleet';
import { formatUptime } from '../../lib/format';
import { ErrorState, Spinner } from '../ui/States';
import EntityLink from '../ui/EntityLink';

// UX Wave 1 — W1.1: блок «Fleet» на Dashboard.
// Live-роли по всем командам: activity-dot, команда → роль, текущая in_progress-задача,
// ctx%, uptime, CTA на топологию команды и на сессию.
// Источники — api.sessions.list() + api.teams.getTeams() + api.tasks.list()
// (composing на клиенте, по одной загрузке на тип; refetch 5s + WS — как в DashboardPage).
export default function FleetPanel() {
  const sessionsQ = useQuery('fleet.sessions', () => api.sessions.list());
  const teamsQ = useQuery('fleet.teams', () => api.teams.getTeams());
  const tasksQ = useQuery('fleet.tasks', () => api.tasks.list());

  // 5s-поллинг (паттерн TeamBuilderPage): контекст/состояния меняются со временем
  const refetchRef = useRef({
    sessions: sessionsQ.refetch,
    tasks: tasksQ.refetch,
  });
  refetchRef.current = { sessions: sessionsQ.refetch, tasks: tasksQ.refetch };
  useEffect(() => {
    const t = setInterval(() => {
      refetchRef.current.sessions();
      refetchRef.current.tasks();
    }, 5000);
    return () => clearInterval(t);
  }, []);

  const loading = sessionsQ.loading || teamsQ.loading || tasksQ.loading;
  const error = sessionsQ.error ?? tasksQ.error ?? teamsQ.error;
  if (loading) return <div className="card fleet-card"><Spinner label="Loading fleet…" /></div>;
  if (error) return <div className="card fleet-card"><ErrorState error={error} onRetry={() => { sessionsQ.refetch(); teamsQ.refetch(); tasksQ.refetch(); }} /></div>;

  const rows = composeFleetRows(sessionsQ.data?.sessions ?? [], teamsQ.data?.teams ?? [], tasksQ.data?.tasks ?? []);
  const visible = rows.slice(0, FLEET_MAX_ROWS);
  const hidden = rows.length - visible.length;

  return (
    <div className="card fleet-card">
      <h2>Fleet</h2>
      {rows.length === 0 ? (
        <div className="fleet-empty">
          <span className="muted">Нет активных ролей</span>{' '}
          <EntityLink to="/teams" title="Открыть команды">Открыть команды</EntityLink>
        </div>
      ) : (
        <div className="fleet-rows">
          {visible.map((r) => (
            <div key={r.sessionId} className={`fleet-row fleet-row-${r.state}`}>
              <span className={`fleet-dot fleet-dot-${r.state}`} data-testid="fleet-dot" data-activity-state={r.state} aria-hidden="true" />
              <span className="fleet-who" title={`${r.teamName} — ${r.role}`}>
                <EntityLink to={`/teams/${r.teamId}`} title="Открыть топологию команды">
                  {r.teamName}
                </EntityLink>
                <span className="muted"> → </span>
                <strong>{r.role}</strong>
              </span>
              <span className="fleet-task" title="Текущая in_progress-задача роли">
                {r.taskId != null ? (
                  <EntityLink to={`/tasks/${r.taskId}`} title="Открыть задачу">{r.taskTitle}</EntityLink>
                ) : (
                  <span className="muted">—</span>
                )}
              </span>
              <span
                className={`fleet-ctx${fleetCtxTone(r.ctxPct) ? ` fleet-ctx-${fleetCtxTone(r.ctxPct)}` : ''}`}
                title="Context used"
              >
                {r.ctxPct !== null ? `ctx ${Math.round(r.ctxPct)}%` : 'ctx —'}
              </span>
              <span className="fleet-up muted small" title="Uptime сессии">
                {r.uptimeSeconds !== undefined ? `up ${formatUptime(r.uptimeSeconds)}` : 'up —'}
              </span>
              <span className="fleet-cta">
                <EntityLink to={`/teams/${r.teamId}`} title="Топология команды">
                  ⬡
                </EntityLink>
                <EntityLink to={`/sessions/${r.sessionId}`} title="Открыть сессию">
                  ◉
                </EntityLink>
              </span>
            </div>
          ))}
        </div>
      )}
      {hidden > 0 && (
        <div className="fleet-more">
          <EntityLink to="/sessions" title="Все сессии">+{hidden} ещё</EntityLink>
        </div>
      )}
    </div>
  );
}
