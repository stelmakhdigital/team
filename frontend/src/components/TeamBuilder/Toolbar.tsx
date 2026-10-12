import { useState } from 'react';
import type { DragEvent } from 'react';
import { DRAG_TYPES, DEFAULT_PALETTE } from './palette';

interface ToolbarSegment {
  id: number;
  name: string;
}

interface ToolbarProps {
  connectType: string;
  onConnectType: (t: string) => void;
  // T4: явное создание роли через форму (name + agent_spec + target segment).
  segments: ToolbarSegment[];
  onAddRole: (name: string, agentSpec: string, segmentId: number) => void;
  addError?: string | null;
  adding?: boolean;
}

const AGENT_SPEC_SUGGESTIONS = ['pi-lead', 'pi-worker', 'pi-reviewer', 'pi-watchdog', 'pi-go-backend', 'pi-go-worker'];

/** Left palette: draggable segments/roles + relation type picker + «Add role» form. */
export default function Toolbar({ connectType, onConnectType, segments, onAddRole, addError, adding }: ToolbarProps) {
  const palette = DEFAULT_PALETTE;
  const [roleName, setRoleName] = useState('');
  const [roleSpec, setRoleSpec] = useState('');
  const [roleSeg, setRoleSeg] = useState('');

  const dragPayload = (kind: 'segment' | 'role', name: string, agentSpec?: string) => (e: DragEvent) => {
    e.dataTransfer.setData(DRAG_TYPES[kind], JSON.stringify({ name, agent_spec: agentSpec }));
    e.dataTransfer.effectAllowed = 'copy';
  };

  const submitRole = (e: React.FormEvent) => {
    e.preventDefault();
    if (!roleName.trim() || !roleSeg) return;
    onAddRole(roleName.trim(), roleSpec.trim() || 'pi-worker', Number(roleSeg));
    setRoleName('');
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
        <h3>➕ Add role</h3>
        <form onSubmit={submitRole} className="add-role-form">
          <label>
            Name
            <input value={roleName} onChange={(e) => setRoleName(e.target.value)} placeholder="qa-bot" aria-label="New role name" />
          </label>
          <label>
            Agent spec
            <input value={roleSpec} onChange={(e) => setRoleSpec(e.target.value)} placeholder="pi-worker" list="add-role-specs" aria-label="New role agent spec" />
          </label>
          <datalist id="add-role-specs">
            {AGENT_SPEC_SUGGESTIONS.map((s) => (
              <option key={s} value={s} />
            ))}
          </datalist>
          <label>
            Segment
            <select value={roleSeg} onChange={(e) => setRoleSeg(e.target.value)} aria-label="New role target segment">
              <option value="">— select —</option>
              {segments.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </label>
          <button type="submit" className="btn btn-primary" disabled={!roleName.trim() || !roleSeg || !!adding} aria-label="Add role">
            {adding ? 'Adding…' : '+ Add role'}
          </button>
          {addError && <div className="form-error small">{addError}</div>}
        </form>
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
