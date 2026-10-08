import { describe, expect, it } from 'vitest';
import { hasCycle, isolatedBlocks, layoutWorkflow, workflowLevels } from '../src/components/Workflow/layout';
import type { WorkflowBlock, WorkflowConnection } from '../src/types/api';

const iso = '2026-01-01T00:00:00Z';
const blk = (id: number, type: WorkflowBlock['type'] = 'task'): WorkflowBlock => ({
  id,
  workflow_id: 1,
  type,
  position: { x: 0, y: 0 },
  config: {},
  created_at: iso,
});
const conn = (id: number, from: number, to: number, condition?: string): WorkflowConnection => ({
  id,
  workflow_id: 1,
  from_block_id: from,
  to_block_id: to,
  condition,
  created_at: iso,
});

describe('workflow auto-layout (R6.4)', () => {
  it('линейная цепочка → уровни 0,1,2', () => {
    const b = [blk(1), blk(2), blk(3)];
    const c = [conn(1, 1, 2), conn(2, 2, 3)];
    const lv = workflowLevels(b, c);
    expect(lv.get(1)).toBe(0);
    expect(lv.get(2)).toBe(1);
    expect(lv.get(3)).toBe(2);
  });

  it('ветвление: слияние получает max(веток)', () => {
    const b = [blk(1), blk(2), blk(3), blk(4)];
    const c = [conn(1, 1, 2), conn(2, 1, 3), conn(3, 2, 4), conn(4, 3, 4)];
    const lv = workflowLevels(b, c);
    expect(lv.get(1)).toBe(0);
    expect(lv.get(2)).toBe(1);
    expect(lv.get(3)).toBe(1);
    expect(lv.get(4)).toBe(2);
  });

  it('цикл не роняет layout; hasCycle=true', () => {
    const b = [blk(1), blk(2), blk(3)];
    const c = [conn(1, 1, 2), conn(2, 2, 3), conn(3, 3, 1)];
    expect(() => layoutWorkflow(b, c)).not.toThrow();
    expect(hasCycle(b, c)).toBe(true);
    const res = layoutWorkflow(b, c);
    expect(res.positions.size).toBe(3);
  });

  it('без циклов — hasCycle=false', () => {
    const b = [blk(1), blk(2), blk(3)];
    const c = [conn(1, 1, 2), conn(2, 2, 3)];
    expect(hasCycle(b, c)).toBe(false);
  });

  it('изолированные блоки', () => {
    const b = [blk(1), blk(2), blk(3)];
    const c = [conn(1, 1, 2)];
    const iso = isolatedBlocks(b, c);
    expect(iso.has(3)).toBe(true);
    expect(iso.has(1)).toBe(false);
    expect(iso.has(2)).toBe(false);
  });

  it('layoutWorkflow: колонки по уровням, строки внутри', () => {
    const b = [blk(1), blk(2), blk(3)];
    const c = [conn(1, 1, 2), conn(2, 1, 3)];
    const res = layoutWorkflow(b, c);
    expect(res.positions.get(1)?.x).toBe(0);
    expect(res.positions.get(2)?.x).toBe(res.positions.get(3)?.x);
    expect(res.positions.get(2)!.x).toBeGreaterThan(res.positions.get(1)!.x);
    expect(res.positions.get(2)?.y).not.toBe(res.positions.get(3)?.y);
  });

  it('пустой workflow — не падает', () => {
    const res = layoutWorkflow([], []);
    expect(res.positions.size).toBe(0);
    expect(res.width).toBeGreaterThan(0);
  });
});
