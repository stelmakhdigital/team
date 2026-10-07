import { useCallback, useRef } from 'react';import { useQuery } from '../hooks/useQuery';
import { api } from '../api';
import { ErrorState, Spinner, Unavailable, isNotFoundError } from '../components/ui/States';
import { useWebSocket } from '../hooks/useWebSocket';
import SummaryCards from '../components/Dashboard/SummaryCards';
import TaskList from '../components/Dashboard/TaskList';
import SessionGrid from '../components/Dashboard/SessionGrid';
import AlertsPanel from '../components/Dashboard/AlertsPanel';
import MetricsChart from '../components/Dashboard/MetricsChart';
import { useToast } from '../components/ui/Toast';

export default function DashboardPage() {
  const summary = useQuery('dashboard.summary', () => api.dashboard.getSummary());
  const tasks = useQuery('dashboard.tasks', () => api.dashboard.getTasks());
  const sessions = useQuery('dashboard.sessions', () => api.dashboard.getSessions());
  const alerts = useQuery('dashboard.alerts', () => api.dashboard.getAlerts());
  const metrics = useQuery('dashboard.metrics', () => api.dashboard.getMetrics());
  const { status, onMessage } = useWebSocket(['dashboard']);
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

  if (summary.loading) return <div className="page"><Spinner label="Loading dashboard…" /></div>;
  if (summary.error) return <div className="page"><ErrorState error={summary.error} onRetry={summary.refetch} /></div>;

  return (
    <div className="page">
      <div className="page-head">
        <h1>Dashboard</h1>
        <span className={`ws-badge ws-${status}`}>real-time: {status}</span>
      </div>

      {summary.data && <SummaryCards summary={summary.data} />}
      <div className="dash-grid">
        {tasks.loading ? (
          <Spinner label="Loading tasks…" />
        ) : tasks.error ? (
          <ErrorState error={tasks.error} onRetry={tasks.refetch} />
        ) : (
          <TaskList tasks={tasks.data?.tasks ?? []} />
        )}
        {alerts.loading ? (
          <Spinner label="Loading alerts…" />
        ) : alerts.error ? (
          <ErrorState error={alerts.error} onRetry={alerts.refetch} />
        ) : (
          <AlertsPanel alerts={alerts.data?.alerts ?? []} />
        )}
        {sessions.loading ? (
          <Spinner label="Loading sessions…" />
        ) : sessions.error ? (
          <ErrorState error={sessions.error} onRetry={sessions.refetch} />
        ) : (
          <SessionGrid sessions={sessions.data?.sessions ?? []} />
        )}
        {metrics.loading ? (
          <Spinner label="Loading metrics…" />
        ) : metrics.error ? (
          isNotFoundError(metrics.error) ? (
            <Unavailable feature="Metrics" />
          ) : (
            <ErrorState error={metrics.error} onRetry={metrics.refetch} />
          )
        ) : (
          <MetricsChart metrics={metrics.data!} />
        )}
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
