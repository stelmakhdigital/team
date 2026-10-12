import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { render } from '@testing-library/react';
import { Position, ReactFlowProvider } from '@xyflow/react';
import { TaskEdge } from '../src/components/Topology/edges/TaskEdge';

const cssPath = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src/topology.css');
const topologyCss = readFileSync(cssPath, 'utf8');

const baseProps = {
  id: 'rel:1',
  sourceX: 0,
  sourceY: 0,
  sourcePosition: Position.Right,
  targetX: 120,
  targetY: 40,
  targetPosition: Position.Left,
  markerEnd: undefined,
};

function renderEdge(data: Record<string, unknown>) {
  const props = { ...baseProps, data } as unknown as Parameters<typeof TaskEdge>[0];
  return render(
    <ReactFlowProvider>
      <TaskEdge {...props} />
    </ReactFlowProvider>,
  );
}

describe('W1.3 TaskEdge «пакет» (active флаг)', () => {
  it('active=true → circle + animateMotion по bezier-пути', () => {
    const { container } = renderEdge({ kind: 'delegates_to', selected: false, active: true });
    const circle = container.querySelector('circle');
    expect(circle).not.toBeNull();
    const motion = circle!.querySelector('animateMotion');
    expect(motion).not.toBeNull();
    // dur ~2s, repeat indefinite
    expect(motion!.getAttribute('dur')).toBe('2s');
    expect(motion!.getAttribute('repeatCount')).toBe('indefinite');
    // path = тот же bezier-путь ребра
    const pathD = container.querySelector('path.react-flow__edge-path')?.getAttribute('d');
    expect(motion!.getAttribute('path')).toBe(pathD);
  });

  it('active=true → бегущий пунктир (class task-edge-active)', () => {
    const { container } = renderEdge({ kind: 'delegates_to', selected: false, active: true });
    expect(container.querySelector('.task-edge-active')).not.toBeNull();
  });

  it('active=false/отсутствует → статичное ребро (без animateMotion, без пунктира)', () => {
    const { container } = renderEdge({ kind: 'delegates_to', selected: false, active: false });
    expect(container.querySelector('animateMotion')).toBeNull();
    expect(container.querySelector('.task-edge-active')).toBeNull();
    const { container: c2 } = render(
      <ReactFlowProvider>
        <TaskEdge {...({ ...baseProps, data: { kind: 'delegates_to', selected: false } } as unknown as Parameters<typeof TaskEdge>[0])} />
      </ReactFlowProvider>,
    );
    expect(c2.querySelector('animateMotion')).toBeNull();
  });

  it('memo/areEqual учитывает active: переключение false→true добирает animateMotion', () => {
    const { container, rerender } = renderEdge({ kind: 'delegates_to', selected: false, active: false });
    expect(container.querySelector('animateMotion')).toBeNull();
    rerender(
      <ReactFlowProvider>
        <TaskEdge {...({ ...baseProps, data: { kind: 'delegates_to', selected: false, active: true } } as unknown as Parameters<typeof TaskEdge>[0])} />
      </ReactFlowProvider>,
    );
    expect(container.querySelector('animateMotion')).not.toBeNull();
  });

  it('topology.css: prefers-reduced-motion отключает бегущий пунктир', () => {
    const media = topologyCss.match(/@media\s*\(\s*prefers-reduced-motion:\s*reduce\s*\)\s*{([\s\S]*?)\n}/g) ?? [];
    const inReduce = media.join('\n');
    expect(inReduce).toMatch(/task-edge-active\s*{[^}]*animation:\s*none/s);
  });
});
