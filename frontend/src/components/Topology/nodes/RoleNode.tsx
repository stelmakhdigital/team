import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { Role } from '../../../types/api';

export interface RoleNodeData extends Record<string, unknown> {
  role: Role;
  selected: boolean;
  editMode: boolean;
  // R4 (slice 7): live-метрики сессии (omit/«--» если рантайм не знает)
  ctxPct?: number | null;
  tokensIn?: number | null;
  tokensOut?: number | null;
  model?: string | null;
  // live-терминал: последние строки session.output (подрезано канвасом)
  output?: string[];
}

export function activityState(role: Role): 'running' | 'failed' | 'stopped' | 'idle' {
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

function compactTokens(n?: number | null): string {
  if (n === null || n === undefined || Number.isNaN(n)) return '--';
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

function ctxClass(pct?: number | null): string {
  if (pct === null || pct === undefined) return '';
  if (pct >= 80) return 'ctx-crit';
  if (pct >= 60) return 'ctx-warn';
  return 'ctx-ok';
}

const MAX_TERM_LINES = 6;

function RoleNodeInner({ data }: NodeProps) {
  const { role, selected, editMode, ctxPct, tokensIn, tokensOut, model, output } = data as RoleNodeData;
  const act = activityState(role);
  const up = uptimeSince(role.session?.started_at);
  const ctxKnown = ctxPct !== null && ctxPct !== undefined;
  const tokKnown = (tokensIn ?? 0) > 0 || (tokensOut ?? 0) > 0;
  const modelLabel = model || role.profile || role.agent_spec;
  const lines = (output ?? []).slice(-MAX_TERM_LINES);
  const hasSession = !!role.session || act === 'running';

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
        {/* R4: live-метрики (контракт 20 §3.6): ctx% / tokens / model */}
        <div className="role-node-metrics" aria-label="context and tokens">
          <span className={`metric ctx ${ctxClass(ctxPct)}`} data-slot="context">
            ctx {ctxKnown ? `${Math.round(ctxPct!)}%` : '--%'}
          </span>
          <span className="metric" data-slot="tokens">
            tok {tokKnown ? `${compactTokens((tokensIn ?? 0) + (tokensOut ?? 0))}` : '--'}
          </span>
          <span className="metric dim" data-slot="model" title={modelLabel}>
            {modelLabel}
          </span>
        </div>
      </div>
      {/* R4: live-терминал (session.output) — popover при hover */}
      {hasSession && (
        <div className="role-term" data-term-open={lines.length > 0 ? 'true' : 'false'} aria-label="session output">
          <div className="role-term-head">⌘ output {lines.length > 0 && <span className="mono dim">live</span>}</div>
          <div className="role-term-body">
            {lines.length > 0
              ? lines.map((l, i) => (
                  <div key={i} className="role-term-line">{l}</div>
                ))
              : <div className="role-term-line dim">waiting for output…</div>}
          </div>
        </div>
      )}
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
    da.editMode === db.editMode &&
    da.ctxPct === db.ctxPct &&
    da.tokensIn === db.tokensIn &&
    da.tokensOut === db.tokensOut &&
    da.model === db.model &&
    (da.output?.length ?? 0) === (db.output?.length ?? 0) &&
    da.output?.[da.output.length - 1] === db.output?.[db.output.length - 1]
  );
});
