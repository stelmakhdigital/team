import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { render } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { RoleNode, activityState } from '../src/components/Topology/nodes/RoleNode';
import type { Role } from '../src/types/api';

const cssPath = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src/topology.css');
const topologyCss = readFileSync(cssPath, 'utf8');

const iso = '2026-01-01T00:00:00Z';
const role = (over: Partial<Role> = {}): Role => ({
  id: 1,
  team_id: 1,
  segment_id: 1,
  segment_name: 'Backend',
  name: 'Lead',
  address: 't1:Backend.Lead',
  agent_spec: 'pi-go-backend',
  state: 'active',
  created_at: iso,
  updated_at: iso,
  ...over,
});
let n = 0;
function renderNode(state: 'running' | 'failed' | 'stopped' | undefined) {
  n += 1;
  const r = role(state ? { session: { id: 10, state, started_at: iso } } : {});
  const props = { id: `rn-${n}`, type: 'role', data: { role: r, selected: false, editMode: false } } as unknown as Parameters<typeof RoleNode>[0];
  return render(
    <ReactFlowProvider>
      <RoleNode {...props} />
    </ReactFlowProvider>,
  );
}

const css = topologyCss;

describe('W1.2 RoleNode activity ring (обводка по session-state)', () => {
  it('running → role-ring-running', () => {
    const { container } = renderNode('running');
    expect(container.querySelector('.role-ring-running')).not.toBeNull();
  });
  it('failed → role-ring-failed', () => {
    const { container } = renderNode('failed');
    expect(container.querySelector('.role-ring-failed')).not.toBeNull();
  });
  it('no session (idle) → role-ring-idle', () => {
    const { container } = renderNode(undefined);
    expect(container.querySelector('.role-ring-idle')).not.toBeNull();
  });
  it('stopped → role-ring-stopped', () => {
    const { container } = renderNode('stopped');
    expect(container.querySelector('.role-ring-stopped')).not.toBeNull();
  });
  it('activityState maps session.state → ring state', () => {
    expect(activityState(role({ session: { id: 1, state: 'running' } }))).toBe('running');
    expect(activityState(role({ session: { id: 1, state: 'failed' } }))).toBe('failed');
    expect(activityState(role({ session: { id: 1, state: 'stopped' } }))).toBe('stopped');
    expect(activityState(role())).toBe('idle');
  });
  it('topology.css: running ring uses CSS pulse keyframes (без JS-тик)', () => {
    expect(css).toMatch(/role-ring-pulse/);
    expect(css).toMatch(/role-ring-running\s*{[^}]*animation:\s*role-ring-pulse/s);
  });
  it('topology.css: prefers-reduced-motion отключает pulse (статичная обводка)', () => {
    // внутри @media (prefers-reduced-motion: reduce) — animation: none для role-ring-running
    const media = css.match(/@media\s*\(\s*prefers-reduced-motion:\s*reduce\s*\)\s*{([\s\S]*?)\n}/g) ?? [];
    const inReduce = media.join('\n');
    expect(inReduce).toMatch(/role-ring-running\s*{[^}]*animation:\s*none/s);
  });
});
