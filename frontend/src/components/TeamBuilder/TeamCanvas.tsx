import { useRef, useState, type DragEvent, type PointerEvent as ReactPointerEvent } from 'react';
import type { Relative, Role, Segment, TopologyLayout } from '../../types/api';
import { RELATIVE_TYPE_LABELS } from '../../lib/topology';
import { DRAG_TYPES, ROLE_H, ROLE_W } from './palette';

export const CANVAS_W = 2000;
export const CANVAS_H = 1200;

export type Selection = { type: 'segment' | 'role' | 'relative'; id: number } | null;

interface TeamCanvasProps {
  segments: Segment[];
  roles: Role[];
  relatives: Relative[];
  layout: TopologyLayout | undefined;
  zoom: number;
  grid: boolean;
  snap: boolean;
  selection: Selection;
  connectFrom: number | null; // role id waiting for a target
  onSelect: (sel: Selection) => void;
  onRoleMoved: (roleId: number, pos: { x: number; y: number }) => void;
  onSegmentMoved: (segmentId: number, pos: { x: number; y: number }) => void;
  onDropSegment: (payload: { name: string; agent_spec?: string }, pos: { x: number; y: number }) => void;
  onDropRole: (payload: { name: string; agent_spec?: string }, segmentId: number | null, pos: { x: number; y: number }) => void;
  onRoleConnectClick: (roleId: number) => void;
  onRelativeSelected: (id: number) => void;
}

interface DragState {
  kind: 'role' | 'segment';
  id: number;
  offsetX: number;
  offsetYX: number;
  offsetY: number;
  moved: boolean;
}

function snapTo(v: number, snap: boolean): number {
  return snap ? Math.round(v / 10) * 10 : Math.round(v);
}

export default function TeamCanvas(props: TeamCanvasProps) {
  const {
    segments,
    roles,
    relatives,
    layout,
    zoom,
    grid,
    snap,
    selection,
    connectFrom,
    onSelect,
    onRoleMoved,
    onSegmentMoved,
    onDropSegment,
    onDropRole,
    onRoleConnectClick,
    onRelativeSelected,
  } = props;

  const canvasRef = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<DragState | null>(null);
  const [dragPos, setDragPos] = useState<{ x: number; y: number } | null>(null);
  const [dropTarget, setDropTarget] = useState<number | null>(null); // segment being drag-overed

  const segPos = (id: number) => layout?.segments.find((s) => s.segment_id === id)?.position;
  const rolePos = (id: number): { x: number; y: number } => {
    if (drag?.kind === 'role' && drag.id === id && dragPos) return dragPos;
    return layout?.roles.find((r) => r.role_id === id)?.position ?? { x: 40, y: 40 };
  };

  const toCanvas = (clientX: number, clientY: number) => {
    const rect = canvasRef.current?.getBoundingClientRect();
    if (!rect) return { x: 0, y: 0 };
    return { x: (clientX - rect.left) / zoom, y: (clientY - rect.top) / zoom };
  };

  const startDrag = (e: ReactPointerEvent, kind: 'role' | 'segment', id: number) => {
    if (connectFrom !== null) return; // in connect mode clicks route to roles
    const p = toCanvas(e.clientX, e.clientY);
    const cur = kind === 'role' ? rolePos(id) : segPos(id) ?? { x: 0, y: 0 };
    setDrag({ kind, id, offsetX: p.x - cur.x, offsetYX: p.x, offsetY: p.y - cur.y, moved: false });
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  };

  const onPointerMove = (e: ReactPointerEvent) => {
    if (!drag) return;
    const p = toCanvas(e.clientX, e.clientY);
    setDrag({ ...drag, moved: true });
    setDragPos({
      x: snapTo(p.x - drag.offsetX, snap),
      y: snapTo(p.y - drag.offsetY, snap),
    });
  };

  const onPointerUp = () => {
    if (!drag) return;
    if (drag.moved && dragPos) {
      const pos = { x: dragPos.x, y: dragPos.y };
      if (drag.kind === 'role') onRoleMoved(drag.id, pos);
      else onSegmentMoved(drag.id, pos);
    } else {
      onSelect(drag.kind === 'role' ? { type: 'role', id: drag.id } : { type: 'segment', id: drag.id });
    }
    setDrag(null);
    setDragPos(null);
  };

  const handleDrop = (e: DragEvent) => {
    e.preventDefault();
    setDropTarget(null);
    const p = toCanvas(e.clientX, e.clientY);
    const segRaw = e.dataTransfer.getData(DRAG_TYPES.segment);
    const roleRaw = e.dataTransfer.getData(DRAG_TYPES.role);
    if (segRaw) {
      onDropSegment(JSON.parse(segRaw), { x: snapTo(p.x, snap), y: snapTo(p.y, snap) });
    } else if (roleRaw) {
      const payload = JSON.parse(roleRaw) as { name: string; agent_spec?: string };
      // find segment under the drop point
      let target: number | null = null;
      for (const s of segments) {
        const sp = segPos(s.id);
        if (sp && p.x >= sp.x && p.x <= sp.x + sp.width && p.y >= sp.y && p.y <= sp.y + sp.height) {
          target = s.id;
          break;
        }
      }
      onDropRole(payload, target, { x: snapTo(p.x, snap), y: snapTo(p.y, snap) });
    }
  };

  const handleDragOver = (e: DragEvent) => {
    e.preventDefault();
    const p = toCanvas(e.clientX, e.clientY);
    const over = segments.find((s) => {
      const sp = segPos(s.id);
      return sp && p.x >= sp.x && p.x <= sp.x + sp.width && p.y >= sp.y && p.y <= sp.y + sp.height;
    });
    setDropTarget(over?.id ?? null);
  };

  const handleRoleClick = (role: Role) => {
    if (connectFrom !== null) {
      if (connectFrom !== role.id) onRoleConnectClick(role.id);
      return;
    }
    onSelect({ type: 'role', id: role.id });
  };

  return (
    <div className="canvas-scroll">
      <div
        ref={canvasRef}
        className={`canvas${grid ? ' canvas-grid' : ''}`}
        style={{ width: CANVAS_W, height: CANVAS_H, transform: `scale(${zoom})`, transformOrigin: '0 0' }}
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragLeave={() => setDropTarget(null)}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onClick={(e) => {
          if (e.target === canvasRef.current) onSelect(null);
        }}
        role="application"
        aria-label="Team topology canvas"
      >
        {/* SVG connectors under blocks */}
        <svg className="canvas-svg" width={CANVAS_W} height={CANVAS_H} aria-hidden="true">
          {relatives.map((rel) => {
            const from = roles.find((r) => r.id === rel.from_role_id);
            const to = roles.find((r) => r.id === rel.to_role_id);
            if (!from || !to) return null;
            const fp = { x: rolePos(from.id).x + ROLE_W / 2, y: rolePos(from.id).y + ROLE_H / 2 };
            const tp = { x: rolePos(to.id).x + ROLE_W / 2, y: rolePos(to.id).y + ROLE_H / 2 };
            const dx = Math.max(40, Math.abs(tp.x - fp.x) / 2);
            const d = `M ${fp.x} ${fp.y} C ${fp.x + dx} ${fp.y}, ${tp.x - dx} ${tp.y}, ${tp.x} ${tp.y}`;
            const selected = selection?.type === 'relative' && selection.id === rel.id;
            const labelX = (fp.x + tp.x) / 2;
            const labelY = (fp.y + tp.y) / 2 - 8;
            return (
              <g key={rel.id}>
                <path
                  d={d}
                  className={`connector${selected ? ' connector-selected' : ''}`}
                  strokeDasharray={rel.type === 'can_observe' ? '6 4' : undefined}
                  onClick={(e) => {
                    e.stopPropagation();
                    onRelativeSelected(rel.id);
                  }}
                />
                <path d={d} className="connector-hit" onClick={(e) => { e.stopPropagation(); onRelativeSelected(rel.id); }} />
                <text x={labelX} y={labelY} className="connector-label" textAnchor="middle">
                  {RELATIVE_TYPE_LABELS[rel.type] ?? rel.type}
                </text>
              </g>
            );
          })}
        </svg>

        {/* Segments */}
        {segments.map((s) => {
          const sp = segPos(s.id);
          if (!sp) return null;
          const selected = selection?.type === 'segment' && selection.id === s.id;
          const isDropTarget = dropTarget === s.id;
          return (
            <div
              key={s.id}
              className={`segment-block${selected ? ' selected' : ''}${isDropTarget ? ' drop-target' : ''}`}
              style={{ left: sp.x, top: sp.y, width: sp.width, height: sp.height }}
              onPointerDown={(e) => {
                e.stopPropagation();
                startDrag(e, 'segment', s.id);
              }}
            >
              <div className="segment-title">{s.name}</div>
            </div>
          );
        })}

        {/* Roles */}
        {roles.map((r) => {
          const rp = rolePos(r.id);
          const selected = selection?.type === 'role' && selection.id === r.id;
          const isConnectSource = connectFrom === r.id;
          return (
            <div
              key={r.id}
              className={`role-block role-${r.state}${selected ? ' selected' : ''}${isConnectSource ? ' connect-source' : ''}`}
              style={{ left: rp.x, top: rp.y, width: ROLE_W, height: ROLE_H }}
              onPointerDown={(e) => {
                if (connectFrom !== null) return;
                e.stopPropagation();
                startDrag(e, 'role', r.id);
              }}
              onClick={(e) => {
                e.stopPropagation();
                handleRoleClick(r);
              }}
              title={r.address}
            >
              <span className="role-name">{r.name}</span>
              <span className="role-spec muted">{r.agent_spec}</span>
              {r.session && <span className={`dot dot-${r.session.state}`} aria-label={`session ${r.session.state}`} />}
            </div>
          );
        })}

        {connectFrom !== null && <div className="connect-banner">Select the target role…</div>}
      </div>
    </div>
  );
}
