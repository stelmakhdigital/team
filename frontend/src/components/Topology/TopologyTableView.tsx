// Таблица топологии (R3, референс openRIG Table view): альтернатива графику
// и fallback на узких экранах.
import type { Role, Segment } from '../../types/api';
import { Badge } from '../ui/States';

interface Props {
  segments: Segment[];
  roles: Role[];
  onSelect?: (id: number) => void;
}

function uptimeSince(startedAt?: string): string | null {
  if (!startedAt) return null;
  const ms = Date.now() - new Date(startedAt).getTime();
  if (Number.isNaN(ms) || ms < 0) return null;
  const m = Math.floor(ms / 60_000);
  if (m < 1) return '<1m';
  if (m < 60) return `${m}m`;
  return `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}m`;
}

function actState(role: Role): 'running' | 'failed' | 'stopped' | 'idle' {
  if (role.session?.state === 'running') return 'running';
  if (role.session?.state === 'failed') return 'failed';
  if (role.session?.state === 'stopped') return 'stopped';
  return 'idle';
}

export default function TopologyTableView({ segments, roles, onSelect }: Props) {
  const segName = new Map(segments.map((s) => [s.id, s.name]));
  const sorted = [...roles].sort((a, b) => {
    const sa = segName.get(a.segment_id) ?? '';
    const sb = segName.get(b.segment_id) ?? '';
    return sa.localeCompare(sb) || a.name.localeCompare(b.name);
  });

  return (
    <div className="topo-table-wrap">
      <table className="table topo-table" aria-label="Topology table">
        <thead>
          <tr>
            <th>Role</th>
            <th>Segment</th>
            <th>State</th>
            <th>Session</th>
            <th>Runtime</th>
            <th>Uptime</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => {
            const act = actState(r);
            const up = uptimeSince(r.session?.started_at);
            return (
              <tr key={r.id} onClick={() => onSelect?.(r.id)} className="topo-table-row">
                <td>
                  <span className={`activity-dot act-${act}`} data-activity-state={act} aria-hidden="true" />
                  <span className="topo-table-role">{r.name}</span>
                  {r.profile && <span className="mono badge">{r.profile}</span>}
                </td>
                <td className="mono dim">{segName.get(r.segment_id) ?? `#${r.segment_id}`}</td>
                <td><Badge kind="entity" value={r.state} /></td>
                <td className="mono dim">{r.session ? r.session.state : 'no session'}</td>
                <td className="mono dim">{r.agent_spec}</td>
                <td className="mono dim">{act === 'running' && up ? up : '—'}</td>
              </tr>
            );
          })}
          {sorted.length === 0 && (
            <tr>
              <td colSpan={6} className="muted">No roles yet</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
