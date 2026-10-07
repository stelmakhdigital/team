import type { DragEvent } from 'react';
import { DRAG_TYPES, DEFAULT_PALETTE } from './palette';

interface ToolbarProps {
  connectType: string;
  onConnectType: (t: string) => void;
}

/** Left palette: draggable segments/roles + relation type picker. */
export default function Toolbar({ connectType, onConnectType }: ToolbarProps) {
  const palette = DEFAULT_PALETTE;

  const dragPayload = (kind: 'segment' | 'role', name: string, agentSpec?: string) => (e: DragEvent) => {
    e.dataTransfer.setData(DRAG_TYPES[kind], JSON.stringify({ name, agent_spec: agentSpec }));
    e.dataTransfer.effectAllowed = 'copy';
  };

  return (
    <aside className="toolbar" aria-label="Builder palette">
      <section>
        <h3>📦 Segments</h3>
        <ul className="palette-list">
          {palette.segments.map((s) => (
            <li key={s.name} draggable onDragStart={dragPayload('segment', s.name)} className="palette-item">
              <span className="palette-dot dot-segment" aria-hidden="true" />
              {s.name}
            </li>
          ))}
          <li className="palette-item palette-custom">
            <span className="palette-dot dot-segment" aria-hidden="true" />
            Custom…
          </li>
        </ul>
      </section>
      <section>
        <h3>👤 Roles</h3>
        <ul className="palette-list">
          {palette.roles.map((r) => (
            <li key={r.name} draggable onDragStart={dragPayload('role', r.name, r.agent_spec)} className="palette-item">
              <span className="palette-dot dot-role" aria-hidden="true" />
              {r.name}
            </li>
          ))}
        </ul>
      </section>
      <section>
        <h3>🔗 Relations</h3>
        <p className="muted small">Connection type for new links</p>
        <select value={connectType} onChange={(e) => onConnectType(e.target.value)} aria-label="Connection type">
          {palette.relations.map((t) => (
            <option key={t} value={t}>
              {t.replace(/_/g, ' ')}
            </option>
          ))}
        </select>
      </section>
    </aside>
  );
}
