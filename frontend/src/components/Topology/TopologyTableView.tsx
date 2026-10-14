// W4.1: Table view в TeamBuilder — секции по segment + строки ролей с live-данными
// (state dot, model, ctx% с порогами, текущая in_progress-задача, CTA open-role / send).
// Live-данные — из liveSessions (Map<roleId, SessionDetail>) и tasks (то же, что в
// TeamBuilderPage: 5s-рефетч). Dot/ctx-классы — существующие токены (как Fleet).
import type { Role, Segment, SessionDetail, Task } from '../../types/api';
import { fleetCtxTone, fleetDotState, type FleetDotState } from '../../lib/fleet';
import EntityLink from '../ui/EntityLink';

interface Props {
  segments: Segment[];
  roles: Role[];
  /** live-сессии по roleId (Map, стабильный ref-обновление из TeamBuilderPage) */
  liveSessions: Map<number, SessionDetail>;
  /** задачи команды (для текущей in_progress-задачи роли) */
  tasks: Task[];
  /** open role — переключение на Graph + фокус роли */
  onOpenRole?: (roleId: number) => void;
  /** send — если есть сессия (передача в сессию) */
  onSend?: (sessionId: number) => void;
}

const DOT_LABEL: Record<FleetDotState, string> = {
  running: 'running',
  idle: 'idle',
  failed: 'failed',
  stopped: 'stopped',
};

export default function TopologyTableView({ segments, roles, liveSessions, tasks, onOpenRole, onSend }: Props) {
  const segName = new Map(segments.map((s) => [s.id, s.name]));

  // текущая in_progress-задача по roleId (первая)
  const taskByRole = new Map<number, Task>();
  for (const t of tasks) {
    if (t.state === 'in_progress' && !taskByRole.has(t.destination_role_id)) taskByRole.set(t.destination_role_id, t);
  }

  // группировка ролей по segment (порядок сегментов как в segments, роли — по имени)
  const groups: { segmentId: number; name: string; roles: Role[] }[] = [];
  for (const s of segments) {
    const inSeg = roles
      .filter((r) => r.segment_id === s.id)
      .sort((a, b) => a.name.localeCompare(b.name));
    if (inSeg.length > 0) groups.push({ segmentId: s.id, name: s.name, roles: inSeg });
  }
  // роли в несуществующем segment (на всякий случай)
  const orphanSegIds = new Set(
    roles.filter((r) => !segments.some((s) => s.id === r.segment_id)).map((r) => r.segment_id),
  );
  for (const sid of orphanSegIds) {
    const inSeg = roles.filter((r) => r.segment_id === sid).sort((a, b) => a.name.localeCompare(b.name));
    groups.push({ segmentId: sid, name: segName.get(sid) ?? `#${sid}`, roles: inSeg });
  }

  if (roles.length === 0) {
    return (
      <div className="topo-table-wrap">
        <div className="muted">Ролей пока нет — добавьте роли в топологию команды.</div>
      </div>
    );
  }

  return (
    <div className="topo-table-wrap">
      <table className="table topo-table" aria-label="Topology table">
        <thead>
          <tr>
            <th scope="col">Role</th>
            <th scope="col">Spec</th>
            <th scope="col">Model</th>
            <th scope="col">State</th>
            <th scope="col">Ctx</th>
            <th scope="col">Task</th>
            <th scope="col"></th>
          </tr>
        </thead>
        {groups.map((g) => (
          <tbody key={g.segmentId}>
            <tr className="topo-table-seg">
              <td colSpan={7}>
                <span className="topo-table-seg-name">{g.name}</span>
                <span className="topo-table-seg-count muted">{g.roles.length}</span>
              </td>
            </tr>
            {g.roles.map((r) => {
              const sess = liveSessions.get(r.id) ?? null;
              const hasSession = sess != null;
              const state: FleetDotState = hasSession ? fleetDotState(sess.state) : 'stopped';
              const ctx = hasSession && typeof sess!.context_used_percentage === 'number' ? sess!.context_used_percentage : null;
              const tone = fleetCtxTone(ctx);
              const task = taskByRole.get(r.id) ?? null;
              return (
                <tr key={r.id} className="topo-table-row">
                  <td>
                    <span className={`fleet-dot fleet-dot-${state}`} data-activity-state={state} aria-hidden="true" />
                    <EntityLink to={`/teams/${r.team_id}`} title="Открыть топологию команды" className="topo-table-role">
                      {r.name}
                    </EntityLink>
                  </td>
                  <td className="mono dim">{r.agent_spec}</td>
                  <td className="mono dim">{hasSession && sess!.model ? sess!.model : '—'}</td>
                  <td>
                    <span className={`topo-table-state topo-table-state-${state}`} title={hasSession ? DOT_LABEL[state] : 'no session'}>
                      <span className={`fleet-dot fleet-dot-${state}`} aria-hidden="true" />
                      {hasSession ? DOT_LABEL[state] : '—'}
                    </span>
                  </td>
                  <td>
                    <span className={`fleet-ctx${tone ? ` fleet-ctx-${tone}` : ''}`} title="Context used">
                      {ctx !== null ? `ctx ${Math.round(ctx)}%` : '—'}
                    </span>
                  </td>
                  <td className="topo-table-task">
                    {task ? (
                      <EntityLink to={`/tasks/${task.id}`} title="Открыть задачу">
                        {task.title}
                      </EntityLink>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                  <td className="topo-table-cta">
                    <button type="button" className="btn btn-small" onClick={() => onOpenRole?.(r.id)} title="Фокус в графе" aria-label={`Открыть роль ${r.name} в графе`}>
                      ⬡ open
                    </button>
                    {hasSession ? (
                      <button type="button" className="btn btn-small" onClick={() => onSend?.(sess!.id)} title="Send в сессию" aria-label={`Отправить в сессию ${r.name}`}>
                        send
                      </button>
                    ) : (
                      <span className="muted" aria-hidden="true">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        ))}
      </table>
    </div>
  );
}
