import { useCallback, useRef } from 'react';import { useQuery } from '../hooks/useQuery';
import { Link } from 'react-router-dom';
import { api } from '../api';
import { ErrorState, PageSkeleton, Spinner, Unavailable, isNotFoundError } from '../components/ui/States';
import { useWebSocket } from '../hooks/useWebSocket';
import SummaryCards from '../components/Dashboard/SummaryCards';
import TaskList from '../components/Dashboard/TaskList';
import SessionGrid from '../components/Dashboard/SessionGrid';
import FleetPanel from '../components/Dashboard/FleetPanel';
import AlertsPanel from '../components/Dashboard/AlertsPanel';
import ContextWallPanel from '../components/Dashboard/ContextWallPanel';
import MetricsChart from '../components/Dashboard/MetricsChart';
import { useToast } from '../components/ui/Toast';

export default function DashboardPage() {
  const summary = useQuery('dashboard.summary', () => api.dashboard.getSummary());
  const tasks = useQuery('dashboard.tasks', () => api.dashboard.getTasks());
  const sessions = useQuery('dashboard.sessions', () => api.dashboard.getSessions());
  const alerts = useQuery('dashboard.alerts', () => api.dashboard.getAlerts());
  const metrics = useQuery('dashboard.metrics', () => api.dashboard.getMetrics());
  // W2.4: first-run — «команд нет вообще» → онбординг вместо пустых панелей
  const teamsQ = useQuery('dashboard.first-run.teams', () => api.teams.getTeams());
  const { onMessage } = useWebSocket(['dashboard']);
  const { toast } = useToast();

  // live updates: refetch on relevant events (WS events are synthetic in mock mode)
  const { onMessage: setWs } = useLiveRefetch({
    onEvent: () => {
      summary.refetch();
      tasks.refetch();
      sessions.refetch();
      alerts.refetch();
      toast('info', 'Live update received');
    },
  });
  onMessage(setWs);

  if (summary.loading) return <div className="page"><PageSkeleton label="Loading dashboard…" /></div>;
  if (summary.error) return <div className="page"><ErrorState error={summary.error} onRetry={summary.refetch} /></div>;

  // W2.4: first-run (команд нет вообще) — онбординг-карточка вместо пустого дашборда
  if (!teamsQ.loading && teamsQ.data && teamsQ.data.teams.length === 0) {
    return <FirstRun />
  }

  return (
    <div className="page">
      <div className="page-head">
        <h1>Dashboard</h1>
      </div>

      {summary.data && <SummaryCards summary={summary.data} />}
      <FleetPanel />
      <div className="dash-grid">
        <div className="dash-row">
          <div>{tasks.loading ? (
            <Spinner label="Loading tasks…" />
          ) : tasks.error ? (
            <ErrorState error={tasks.error} onRetry={tasks.refetch} />
          ) : (
            <TaskList tasks={tasks.data?.tasks ?? []} />
          )}</div>
          <div>{alerts.loading ? (
            <Spinner label="Loading alerts…" />
          ) : alerts.error ? (
            <ErrorState error={alerts.error} onRetry={alerts.refetch} />
          ) : (
            <AlertsPanel alerts={alerts.data?.alerts ?? []} />
          )}</div>
        </div>
        <div className="dash-row">
          <div>{sessions.loading ? (
            <Spinner label="Loading sessions…" />
          ) : sessions.error ? (
            <ErrorState error={sessions.error} onRetry={sessions.refetch} />
          ) : (
            <SessionGrid sessions={sessions.data?.sessions ?? []} />
          )}</div>
          <div>{metrics.loading ? (
            <Spinner label="Loading metrics…" />
          ) : metrics.error ? (
            isNotFoundError(metrics.error) ? (
              <Unavailable feature="Metrics" />
            ) : (
              <ErrorState error={metrics.error} onRetry={metrics.refetch} />
            )
          ) : (
            <MetricsChart metrics={metrics.data!} />
          )}</div>
        </div>
        <div className="dash-row">
          <div className="full-width"><ContextWallPanel /></div>
        </div>
      </div>
    </div>
  );
}

// small helper: debounce refetches triggered by WS events
function useLiveRefetch({ onEvent }: { onEvent: () => void }) {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const last = useRef(0);
  const onMessage = useCallback(
    (_msg: { type: string }) => {
      const now = Date.now();
      if (now - last.current < 3000) return;
      last.current = now;
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(onEvent, 300);
    },
    [onEvent],
  );
  return { onMessage };
}

// W2.4: first-run — онбординг-карточка (команд нет вообще).
function FirstRun() {
  return (
    <div className="page">
      <div className="page-head">
        <h1>Dashboard</h1>
      </div>
      <div className="card firstrun-card" data-testid="dashboard-first-run">
        <h2 className="firstrun-title">Добро пожаловать</h2>
        <p className="muted firstrun-sub">
          У вас пока нет команд. Начните с готового стартера или создайте команду с нуля —
          топология, роли и сессии появятся здесь.
        </p>
        <div className="firstrun-cta">
          <Link to="/library" className="btn btn-primary" data-testid="firstrun-apply-starter">
            Быстрый старт: Apply starter «dev-team»
          </Link>
          <Link to="/teams?new=1" className="btn" data-testid="firstrun-create-team">
            Создать команду с нуля
          </Link>
        </div>
        <ol className="firstrun-steps muted small">
          <li>Примените стартер — он создаёт команду с ролями и связями.</li>
          <li>Откройте команду в Builder — настройте роли и запуск сессий.</li>
          <li>Следите за флотом и задачами с этого дашборда.</li>
        </ol>
      </div>
    </div>
  );
}
