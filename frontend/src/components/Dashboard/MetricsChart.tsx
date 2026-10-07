import { useId } from 'react';
import type { GetMetricsResponse, TimeSeriesPoint } from '../../types/api';
import { EmptyState } from '../ui/States';

const SERIES: { key: keyof GetMetricsResponse['metrics']; label: string }[] = [
  { key: 'tasks_created', label: 'Tasks created' },
  { key: 'tasks_completed', label: 'Tasks completed' },
  { key: 'sessions_active', label: 'Active sessions' },
  { key: 'queue_size', label: 'Queue size' },
  { key: 'llm_tokens', label: 'LLM tokens' },
];

function Sparkline({ points, color }: { points: TimeSeriesPoint[]; color: string }) {
  const id = useId();
  if (points.length < 2) return <div className="muted small">no data</div>;
  const w = 280;
  const h = 64;
  const max = Math.max(...points.map((p) => p.value), 1);
  const min = Math.min(...points.map((p) => p.value), 0);
  const range = max - min || 1;
  const coords = points.map((p, i) => {
    const x = (i / (points.length - 1)) * w;
    const y = h - ((p.value - min) / range) * (h - 6) - 3;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  return (
    <svg
      className="sparkline"
      viewBox={`0 0 ${w} ${h}`}
      width="100%"
      height={h}
      role="img"
      aria-label="time series chart"
    >
      <polyline points={coords.join(' ')} fill="none" stroke={color} strokeWidth="2" />
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.25" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <polygon points={`0,${h} ${coords.join(' ')} ${w},${h}`} fill={`url(#${id})`} />
    </svg>
  );
}

export default function MetricsChart({ metrics }: { metrics: GetMetricsResponse }) {
  if (!metrics.metrics || Object.keys(metrics.metrics).length === 0) {
    return <EmptyState title="No metrics" />;
  }
  const colors = ['#4f8ef7', '#3fb950', '#d29922', '#f778ba', '#a371f7'];
  return (
    <div className="card">
      <h2>
        Metrics{' '}
        <span className="muted small">
          ({new Date(metrics.time_range.start).toLocaleDateString()} → {new Date(metrics.time_range.end).toLocaleTimeString()})
        </span>
      </h2>
      <div className="metrics-grid">
        {SERIES.map((s, i) => (
          <div key={s.key} className="metric">
            <div className="metric-label">{s.label}</div>
            <Sparkline points={metrics.metrics[s.key]} color={colors[i % colors.length]} />
          </div>
        ))}
      </div>
    </div>
  );
}
