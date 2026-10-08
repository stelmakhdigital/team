import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { Role } from '../../../types/api';

export interface RoleNodeData extends Record<string, unknown> {
  role: Role;
  selected: boolean;
  editMode: boolean;
}

function activityState(role: Role): 'running' | 'failed' | 'stopped' | 'idle' {
  if (role.session?.state === 'running') return 'running';
  if (role.session?.state === 'failed') return 'failed';
  if (role.session?.state === 'stopped') return 'stopped';
  return 'idle';
}

function uptimeSince(startedAt?: string): string | null {
  if (!startedAt) return null;
  const ms = Date.now() - new Date(startedAt).getTime();
  if (Number.isNaN(ms) || ms < 0) return null;
  const m = Math.floor(ms / 60_000);
  if (m < 1) return '<1m';
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return `${h}h${String(m % 60).padStart(2, '0')}m`;
}

function RoleNodeInner({ data }: NodeProps) {
  const { role, selected, editMode } = data as RoleNodeData;
  const act = activityState(role);
  const up = uptimeSince(role.session?.started_at);

  return (
    <div
      className={`topo-node role-node topo-act-${act} ${selected ? 'topo-selected' : ''}`}
      data-role-id={role.id}
      aria-label={`role ${role.name} activity ${act}`}
    >
      <Handle type="target" position={Position.Left} className={`topo-handle ${editMode ? 'topo-handle-edit' : ''}`} />
      <Handle type="source" position={Position.Right} className={`topo-handle ${editMode ? 'topo-handle-edit' : ''}`} />
      <div className="role-node-head">
        <span className={`activity-dot act-${act}`} data-activity-state={act} aria-hidden="true" />
        <span className="role-node-name">{role.name}</span>
        {act === 'running' && up && <span className="role-node-uptime">{up}</span>}
      </div>
      <div className="role-node-body">
        <div className="role-node-line">
          <span className="mono dim">{role.agent_spec}</span>
          {role.profile && <span className="mono badge">{role.profile}</span>}
        </div>
        <div className="role-node-line">
          <span className="mono state-label">{role.session?.state ?? 'no session'}</span>
        </div>
        {/* R4: live-метрики — слоты (пока «--», backend-срез добавит данные) */}
        <div className="role-node-metrics" aria-label="context and tokens">
          <span className="metric" data-slot="context">ctx --%</span>
          <span className="metric" data-slot="tokens">tok --</span>
          <span className="metric dim" data-slot="model">{role.profile ?? role.agent_spec}</span>
        </div>
      </div>
    </div>
  );
}

export const RoleNode = memo(RoleNodeInner, (a, b) => {
  const da = a.data as RoleNodeData;
  const db = b.data as RoleNodeData;
  return (
    da.role.id === db.role.id &&
    da.role.state === db.role.state &&
    da.role.session?.state === db.role.session?.state &&
    da.role.session?.started_at === db.role.session?.started_at &&
    da.role.profile === db.role.profile &&
    da.role.agent_spec === db.role.agent_spec &&
    da.selected === db.selected &&
    da.editMode === db.editMode
  );
});
