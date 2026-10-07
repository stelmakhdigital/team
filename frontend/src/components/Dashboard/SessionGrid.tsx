import type { Session } from '../../types/api';
import { formatBytes, formatUptime } from '../../lib/format';
import { EmptyState } from '../ui/States';

export default function SessionGrid({ sessions }: { sessions: Session[] }) {
  if (sessions.length === 0) {
    return <EmptyState title="No sessions" hint="Agent sessions will appear here when teams start." />;
  }
  return (
    <div className="card">
      <h2>Agent sessions</h2>
      <div className="session-grid">
        {sessions.map((s) => (
          <div key={s.id} className={`session-card session-${s.state}`}>
            <div className="session-head">
              <span className={`dot dot-${s.state}`} aria-hidden="true" />
              <strong>{s.role_name}</strong>
              <span className="muted small">{s.team_name}</span>
            </div>
            <div className="session-meta">
              <span className="badge">{s.runtime_type}</span>
              <span className="muted small">{s.state}</span>
            </div>
            {s.queue_task_title && <div className="session-task">{s.queue_task_title}</div>}
            <div className="session-stats muted small">
              <span>up {formatUptime(s.uptime_seconds)}</span>
              {s.cpu_percent !== undefined && <span>{s.cpu_percent.toFixed(0)}% CPU</span>}
              {s.memory_bytes !== undefined && <span>{formatBytes(s.memory_bytes)}</span>}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
