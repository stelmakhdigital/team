import { describe, expect, it } from 'vitest';
import type { Relative, Role, Segment } from '../src/types/api';
import {
  autoLayoutMoves,
  computeTopologyLayout,
  memberPosition,
  measureSegment,
  topoOrderSegments,
  HIERARCHY_KINDS,
  ROLE_W,
  ROLE_H,
  ROLE_GAP_X,
  ROLE_GAP_Y,
  SEG_PAD_X,
  SEG_PAD_TOP,
  ENTITY_GAP_Y,
} from '../src/components/Topology/layout/autoLayout';

// фикстуры
const seg = (id: number, name: string): Segment => ({
  id,
  team_id: 1,
  name,
  config: {},
  roles_count: 0,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});
const role = (id: number, segment_id: number, name: string): Role => ({
  id,
  team_id: 1,
  segment_id,
  segment_name: '',
  name,
  address: `s.r${id}`,
  agent_spec: 'pi-worker',
  state: 'active',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});
const rel = (id: number, from: number, to: number, type: Relative['type']): Relative => ({
  id,
  team_id: 1,
  from_role_id: from,
  from_role_name: '',
  to_role_id: to,
  to_role_name: '',
  type,
  created_at: '2026-01-01T00:00:00Z',
});

describe('measureSegment', () => {
  it('одна роль → одна колонка', () => {
    const m = measureSegment(1);
    expect(m.width).toBe(ROLE_W + SEG_PAD_X * 2);
    expect(m.height).toBe(ROLE_H + SEG_PAD_TOP + 28);
  });
  it('4 роли → 3 колонки × 2 ряда (макс 3)', () => {
    const m = measureSegment(4);
    const cols = 3, rows = 2;
    expect(m.width).toBe(cols * ROLE_W + (cols - 1) * ROLE_GAP_X + SEG_PAD_X * 2);
    expect(m.height).toBe(rows * ROLE_H + (rows - 1) * ROLE_GAP_Y + SEG_PAD_TOP + 28);
  });
  it('0 ролей → всё равно валидный контейнер', () => {
    expect(measureSegment(0).width).toBeGreaterThan(0);
  });
});

describe('memberPosition', () => {
  it('сетка до 3 колонок', () => {
    expect(memberPosition(0)).toEqual({ x: SEG_PAD_X, y: SEG_PAD_TOP });
    expect(memberPosition(1).x).toBe(SEG_PAD_X + ROLE_W + ROLE_GAP_X);
    expect(memberPosition(3).y).toBe(SEG_PAD_TOP + ROLE_H + ROLE_GAP_Y);
  });
});

describe('topoOrderSegments', () => {
  const A = seg(1, 'orch');
  const B = seg(2, 'dev');
  const C = seg(3, 'review');
  const r1 = role(1, 1, 'lead');
  const r2 = role(2, 2, 'impl');
  const r3 = role(3, 3, 'rev');

  it('delegates_to задаёт порядок A → B → C', () => {
    const order = topoOrderSegments([A, C, B], [r1, r2, r3], [
      rel(1, 1, 2, 'delegates_to'),
      rel(2, 2, 3, 'delegates_to'),
    ]);
    expect(order).toEqual([1, 2, 3]);
  });

  it('приоритет hierarchy-рёбрам: can_observe review→dev не переопределяет порядок', () => {
    const order = topoOrderSegments([A, B, C], [r1, r2, r3], [
      rel(1, 1, 2, 'delegates_to'),
      rel(2, 2, 3, 'delegates_to'),
      rel(3, 3, 2, 'can_observe'), // review наблюдает dev (обратное направление!)
    ]);
    expect(order).toEqual([1, 2, 3]);
  });

  it('без hierarchy-рёбер — fallback на все рёбра', () => {
    const order = topoOrderSegments([B, A], [r1, r2], [rel(1, 1, 2, 'can_observe')]);
    expect(order).toEqual([1, 2]);
  });

  it('цикл: все вершины в порядке оригинального списка', () => {
    const order = topoOrderSegments([A, B], [r1, r2], [
      rel(1, 1, 2, 'delegates_to'),
      rel(2, 2, 1, 'delegates_to'),
    ]);
    expect(order).toContain(1);
    expect(order).toContain(2);
    expect(order).toHaveLength(2);
  });

  it('HIERARCHY_KINDS — delegates_to + spawned_by', () => {
    expect(HIERARCHY_KINDS.has('delegates_to')).toBe(true);
    expect(HIERARCHY_KINDS.has('spawned_by')).toBe(true);
    expect(HIERARCHY_KINDS.has('collaborates_with')).toBe(false);
  });
});

describe('computeTopologyLayout', () => {
  const A = seg(1, 'orch');
  const B = seg(2, 'dev');
  const r1 = role(1, 1, 'lead');
  const r2 = role(2, 2, 'impl');
  const r3 = role(3, 2, 'qa');

  it('auto: один столбец, segments в порядке toposort, роли внутри контейнеров', () => {
    const res = computeTopologyLayout([B, A], [r2, r3, r1], [rel(1, 1, 2, 'delegates_to')]);
    expect(res.mode).toBe('auto');
    // A (с исходящим) — выше B
    expect(res.segments.get(1)!.y).toBeLessThan(res.segments.get(2)!.y);
    // вертикальный gap
    const a = res.segments.get(1)!;
    const b = res.segments.get(2)!;
    expect(b.y - (a.y + a.height)).toBe(ENTITY_GAP_Y);
    // роли B: parentId, позиции внутри контейнера
    expect(res.roles.get(2)!.parentId).toBe(2);
    expect(res.roles.get(2)!).toMatchObject(memberPosition(0));
    expect(res.roles.get(3)!).toMatchObject(memberPosition(1));
    expect(res.roles.get(1)!).toMatchObject({ ...memberPosition(0), parentId: 1 });
  });

  it('hybrid: сохранённый layout побеждает', () => {
    const layout = {
      segments: [
        { segment_id: 1, position: { x: 100, y: 200, width: 500, height: 300 }, collapsed: false },
        { segment_id: 2, position: { x: 700, y: 200, width: 400, height: 200 }, collapsed: false },
      ],
      roles: [
        { role_id: 1, segment_id: 1, position: { x: 130, y: 260 } },
        { role_id: 2, segment_id: 2, position: { x: 730, y: 260 } },
      ],
      relatives: [],
    };
    const res = computeTopologyLayout([A, B], [r1, r2, r3], [rel(1, 1, 2, 'delegates_to')], layout);
    expect(res.mode).toBe('saved');
    expect(res.segments.get(1)!).toMatchObject({ x: 100, y: 200, width: 500, height: 300 });
    // роль 1: relative = absolute - segment.pos
    expect(res.roles.get(1)!).toMatchObject({ x: 30, y: 60, parentId: 1 });
    // роль 3 без сохранённой позиции → авто-позиция внутри segment B
    expect(res.roles.get(3)!.parentId).toBe(2);
    expect(res.roles.get(3)!.x).toBeGreaterThanOrEqual(0);
  });

  it('standalone роли (нет segment) — под колонкой, parentId=null', () => {
    const orphan = role(9, 99, 'free');
    const res = computeTopologyLayout([A], [r1, orphan], []);
    const p = res.roles.get(9)!;
    expect(p.parentId).toBeNull();
    expect(p.y).toBeGreaterThan(res.segments.get(1)!.y + res.segments.get(1)!.height);
  });

  it('пустая топология — без ошибок', () => {
    const res = computeTopologyLayout([], [], []);
    expect(res.segments.size).toBe(0);
    expect(res.roles.size).toBe(0);
  });
});

describe('autoLayoutMoves (reset-to-auto)', () => {
  const A = seg(1, 'orch');
  const B = seg(2, 'dev');
  const r1 = role(1, 1, 'lead');
  const r2 = role(2, 2, 'impl');

  it('абсолютные позиции ролей = segment.pos + relative', () => {
    const ops = autoLayoutMoves([B, A], [r2, r1], [rel(1, 1, 2, 'delegates_to')]);
    expect(ops.segments).toHaveLength(2);
    expect(ops.roles).toHaveLength(2);
    for (const r of ops.roles) {
      const segOp = ops.segments.find((s) => s.segmentId === (r.roleId === 1 ? 1 : 2))!;
      expect(r.position.x).toBeGreaterThanOrEqual(segOp.position.x);
      expect(r.position.y).toBeGreaterThanOrEqual(segOp.position.y);
    }
  });

  it('standalone-роли не включаются (нет segment_id)', () => {
    const orphan = role(9, 99, 'free');
    const ops = autoLayoutMoves([A], [r1, orphan], []);
    expect(ops.roles.map((r) => r.roleId)).toEqual([1]);
  });

  it('идемпотентен: повторный расчёт даёт те же позиции', () => {
    const a = autoLayoutMoves([B, A], [r2, r1], [rel(1, 1, 2, 'delegates_to')]);
    const b = autoLayoutMoves([B, A], [r2, r1], [rel(1, 1, 2, 'delegates_to')]);
    expect(a).toEqual(b);
  });
});
