import { useEffect, useRef, useState } from 'react';
import { api } from '../../api';
import { useQuery } from '../../hooks/useQuery';
import { composeFleetRows, FLEET_MAX_ROWS, fleetCtxTone } from '../../lib/fleet';
import { formatUptime } from '../../lib/format';
import { ErrorState, Spinner } from '../ui/States';
import EntityLink from '../ui/EntityLink';
import SpecModal from '../SpecModal';

// UX Wave 1 — W1.1: блок «Fleet» на Dashboard.
// Live-роли по всем командам: activity-dot, команда → роль, текущая in_progress-задача,
// ctx%, uptime, CTA на топологию команды и на сессию.
// Источники — api.sessions.list() + api.teams.getTeams() + api.tasks.list()
// (composing на клиенте, по одной загрузке на тип; refetch 5s + WS — как в DashboardPage).
// W3.2: иконка «spec» (монохромная, currentColor — W1-ограничение на иконки)
const ICON_SPEC = (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
    <path d="M4 1.5h7.5L14 5v9a1.5 1.5 0 0 1-1.5 1.5h-8A1.5 1.5 0 0 1 3 14V3a1.5 1.5 0 0 1 1-1.5z" />
    <path d="M11 1.5V5h3.5" />
    <path d="M5.5 9.5h5M5.5 12h3.5" />
  </svg>
);

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

  // W3.2: agent_spec по roleId — lazy через getTeam для команд из live-сессий
  // (кэш в памяти; данные редко меняются, повторных запросов на команду нет).
  const [roleSpecs, setRoleSpecs] = useState<Map<number, string>>(() => new Map());
  const [specName, setSpecName] = useState<string | null>(null);
  const specsRequested = useRef<Set<number>>(new Set());
  useEffect(() => {
    const sessions = sessionsQ.data?.sessions ?? [];
    const teamIds = new Set(sessions.map((s) => s.team_id));
    let cancelled = false;
    for (const tid of teamIds) {
      if (specsRequested.current.has(tid)) continue;
      specsRequested.current.add(tid);
      api.teams
        .getTeam(tid)
        .then((res) => {
          if (cancelled) return;
          setRoleSpecs((prev) => {
            const next = new Map(prev);
            for (const r of res.roles ?? []) {
              if (r.agent_spec) next.set(r.id, r.agent_spec);
            }
            return next;
          });
        })
        .catch(() => {
          /* нет данных — иконка просто не появится */
        });
    }
    return () => {
      cancelled = true;
    };
  }, [sessionsQ.data]);

  const loading = sessionsQ.loading || teamsQ.loading || tasksQ.loading;
  const error = sessionsQ.error ?? tasksQ.error ?? teamsQ.error;
  if (loading) return <div className="card fleet-card" data-tour="fleet"><Spinner label="Loading fleet…" /></div>;
  if (error) return <div className="card fleet-card" data-tour="fleet"><ErrorState error={error} onRetry={() => { sessionsQ.refetch(); teamsQ.refetch(); tasksQ.refetch(); }} /></div>;

  const rows = composeFleetRows(sessionsQ.data?.sessions ?? [], teamsQ.data?.teams ?? [], tasksQ.data?.tasks ?? [], roleSpecs);
  const visible = rows.slice(0, FLEET_MAX_ROWS);
  const hidden = rows.length - visible.length;

  const modal = <SpecModal name={specName} onClose={() => setSpecName(null)} />;

  return (
    <>
    <div className="card fleet-card" data-tour="fleet">
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
                {r.agentSpec && (
                  <button
                    type="button"
                    className="fleet-spec-btn"
                    title={`Открыть spec «${r.agentSpec}» (read-only)`}
                    aria-label={`Открыть spec ${r.agentSpec}`}
                    data-testid="fleet-spec-btn"
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      setSpecName(r.agentSpec);
                    }}
                  >
                    {ICON_SPEC}
                  </button>
                )}
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
    {modal}
    </>
  );
}
