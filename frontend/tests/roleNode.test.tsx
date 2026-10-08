import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { RoleNode } from '../src/components/Topology/nodes/RoleNode';
import type { Role } from '../src/types/api';

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
function renderNode(data: Record<string, unknown>) {
  n += 1;
  // RoleNode читает только data; остальные поля NodeProps не нужны для рендера
  const props = { id: `rn-${n}`, type: 'role', data } as unknown as Parameters<typeof RoleNode>[0];
  return render(
    <ReactFlowProvider>
      <RoleNode {...props} />
    </ReactFlowProvider>,
  );
}

describe('RoleNode R4 (slice 7): live-метрики + live-терминал', () => {
  it('реальные ctx%/tokens/model вместо слотов «--»', () => {
    renderNode({
      role: role({ session: { id: 10, state: 'running', started_at: iso } }),
      selected: false,
      editMode: false,
      ctxPct: 42,
      tokensIn: 84_000,
      tokensOut: 1_200,
      model: 'anthropic/claude-sonnet',
    });
    expect(screen.getByText('ctx 42%')).toBeInTheDocument();
    // 84000 + 1200 = 85200 → 85k
    expect(screen.getByText('tok 85k')).toBeInTheDocument();
    expect(screen.getByText('anthropic/claude-sonnet')).toBeInTheDocument();
  });

  it('пороги цвета: <60 ok, 60-80 warn, >=80 crit', () => {
    const { unmount } = renderNode({ role: role(), selected: false, editMode: false, ctxPct: 88 });
    expect(screen.getByText('ctx 88%').className).toContain('ctx-crit');
    unmount();
    renderNode({ role: role(), selected: false, editMode: false, ctxPct: 67 });
    expect(screen.getByText('ctx 67%').className).toContain('ctx-warn');
    unmount();
    renderNode({ role: role(), selected: false, editMode: false, ctxPct: 12 });
    expect(screen.getByText('ctx 12%').className).toContain('ctx-ok');
  });

  it('без live-данных — слоты «--» (omit у рантайма)', () => {
    renderNode({
      role: role({ session: { id: 10, state: 'running' } }),
      selected: false,
      editMode: false,
      ctxPct: null,
      tokensIn: null,
      tokensOut: null,
      model: null,
    });
    expect(screen.getByText('ctx --%')).toBeInTheDocument();
    expect(screen.getByText('tok --')).toBeInTheDocument();
  });

  it('live-терминал: строки session.output в popover (последние)', () => {
    renderNode({
      role: role({ session: { id: 10, state: 'running' } }),
      selected: false,
      editMode: false,
      output: ['line one', 'line two', 'line three'],
    });
    expect(screen.getByText('line one')).toBeInTheDocument();
    expect(screen.getByText('line three')).toBeInTheDocument();
    expect(screen.getByLabelText('session output')).toBeInTheDocument();
  });

  it('live-терминал: без строк — waiting for output', () => {
    renderNode({
      role: role({ session: { id: 10, state: 'running' } }),
      selected: false,
      editMode: false,
      output: [],
    });
    expect(screen.getByText('waiting for output…')).toBeInTheDocument();
  });

  it('нет сессии — терминал не рендерится', () => {
    renderNode({ role: role(), selected: false, editMode: false, output: ['x'] });
    expect(screen.queryByLabelText('session output')).toBeNull();
  });
});

