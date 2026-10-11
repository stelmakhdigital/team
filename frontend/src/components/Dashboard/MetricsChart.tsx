import { useId } from 'react';
import type { GetMetricsResponse, TimeSeriesPoint } from '../../types/api';
import { EmptyState } from '../ui/States';

// T3: каждая метрика — текущее значение + единицы (count/tokens/USD) + min/max +
// hover-tooltip по точкам sparkline. Без chart-библиотеки (кастомный SVG).
const SERIES: { key: keyof GetMetricsResponse['metrics']; label: string; unit: string }[] = [
  { key: 'tasks_created', label: 'Tasks created', unit: 'count' },
  { key: 'tasks_completed', label: 'Tasks completed', unit: 'count' },
  { key: 'sessions_active', label: 'Active sessions', unit: 'count' },
  { key: 'queue_size', label: 'Queue size', unit: 'count' },
  { key: 'llm_tokens', label: 'LLM tokens', unit: 'tokens' },
  { key: 'llm_cost_usd', label: 'LLM cost', unit: 'USD' }, // P2.14
];

// Формат значения по единицам: USD → $X.XX, tokens → Xk, count → целое.
function fmtValue(v: number, unit: string): string {
  if (unit === 'USD') return `$${v.toFixed(2)}`;
  if (unit === 'tokens') return v >= 1000 ? `${(v / 1000).toFixed(1)}k` : String(Math.round(v));
  return String(Math.round(v));
}

function Sparkline({ points, color, unit }: { points: TimeSeriesPoint[]; color: string; unit: string }) {
  const id = useId();
  if (points.length < 2) return <div className="muted small">no data</div>;
  const w = 280;
  const h = 64;
  const max = Math.max(...points.map((p) => p.value), 1);
  const min = Math.min(...points.map((p) => p.value), 0);
  const range = max - min || 1;
  const pt = (p: TimeSeriesPoint, i: number) => {
    const x = (i / (points.length - 1)) * w;
    const y = h - ((p.value - min) / range) * (h - 6) - 3;
    return { x, y };
  };
  const coords = points.map((p, i) => pt(p, i)).map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`);
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
      {/* T3: hover-tooltip по точкам (нативный <title>: время + значение) */}
      {points.map((p, i) => {
        const c = pt(p, i);
        return (
          <circle key={i} cx={c.x} cy={c.y} r={6} fill="transparent" className="spark-point">
            <title>{`${new Date(p.timestamp).toLocaleString()} — ${fmtValue(p.value, unit)} ${unit}`}</title>
          </circle>
        );
      })}
    </svg>
  );
}

export default function MetricsChart({ metrics }: { metrics: GetMetricsResponse }) {
  if (!metrics.metrics || Object.keys(metrics.metrics).length === 0) {
    return <EmptyState title="No metrics" />;
  }
  const colors = ['#4f8ef7', '#3fb950', '#d29922', '#f778ba', '#a371f7', '#2dd4bf'];
  return (
    <div className="card">
      <h2>
        Metrics{' '}
        <span className="muted small">
          ({new Date(metrics.time_range.start).toLocaleDateString()} → {new Date(metrics.time_range.end).toLocaleTimeString()})
        </span>
      </h2>
      <div className="metrics-grid">
        {SERIES.map((s, i) => {
          const points = metrics.metrics[s.key] ?? [];
          const last = points.length > 0 ? points[points.length - 1] : null;
          const min = points.length > 0 ? Math.min(...points.map((p) => p.value)) : 0;
          const max = points.length > 0 ? Math.max(...points.map((p) => p.value)) : 0;
          return (
            <div key={s.key} className="metric">
              <div className="metric-label">{s.label}</div>
              <div className="metric-value">
                {last ? fmtValue(last.value, s.unit) : '—'}
                <span className="metric-unit"> {s.unit}</span>
              </div>
              <Sparkline points={points} color={colors[i % colors.length]} unit={s.unit} />
              <div className="metric-range muted small">
                min {fmtValue(min, s.unit)} · max {fmtValue(max, s.unit)}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
