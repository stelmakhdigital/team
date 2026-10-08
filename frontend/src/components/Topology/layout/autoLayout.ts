// Auto-layout для топологии (R1, редизайн UI — референс openRIG applyTreeLayout).
// Чистая функция: топология + сохранённый layout → позиции узлов.
//
// Правила (docs: _workspace/openrig-ui-research.md §2.1):
//  - один вертикальный столбец сущностей; порядок — topological sort по edges
//    delegates_to/spawned_by (fallback: все edges);
//  - segment — контейнер (до 3 колонок ролей), роль — 240×150;
//  - «hybrid»: если сохранён пользовательский layout (есть segment/role позиции) —
//    он побеждает; новые сущности без сохранённой позиции — авто-позиции.
//  - роли без segment — standalone-сущности (абсолютные координаты).

import type { Relative, Role, Segment, TopologyLayout } from '../../../types/api';

export const ROLE_W = 240;
export const ROLE_H = 150;
export const MAX_COLS = 3;
export const ROLE_GAP_X = 36;
export const ROLE_GAP_Y = 32;
export const SEG_PAD_X = 28;
export const SEG_PAD_TOP = 44;
export const SEG_PAD_BOTTOM = 28;
export const ENTITY_GAP_Y = 120;

export const HIERARCHY_KINDS = new Set(['delegates_to', 'spawned_by']);

export interface SegmentPos {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface RolePos {
  x: number;
  y: number;
  /** id родительского segment (null — standalone) */
  parentId: number | null;
}

export interface TopologyLayoutResult {
  mode: 'auto' | 'saved';
  segments: Map<number, SegmentPos>;
  roles: Map<number, RolePos>;
}

/** Размер segment-контейнера под `count` ролей. */
export function measureSegment(count: number): { width: number; height: number } {
  const n = Math.max(1, count);
  const cols = Math.min(n, MAX_COLS);
  const rows = Math.ceil(n / MAX_COLS);
  return {
    width: cols * ROLE_W + (cols - 1) * ROLE_GAP_X + SEG_PAD_X * 2,
    height: rows * ROLE_H + (rows - 1) * ROLE_GAP_Y + SEG_PAD_TOP + SEG_PAD_BOTTOM,
  };
}

/** Авто-позиция роли внутри segment (относительно контейнера). */
export function memberPosition(index: number): { x: number; y: number } {
  const col = index % MAX_COLS;
  const row = Math.floor(index / MAX_COLS);
  return {
    x: SEG_PAD_X + col * (ROLE_W + ROLE_GAP_X),
    y: SEG_PAD_TOP + row * (ROLE_H + ROLE_GAP_Y),
  };
}

interface SegEdge {
  from: number;
  to: number;
  kind: string;
}

function buildSegmentEdges(roles: Role[], relatives: Relative[]): SegEdge[] {
  const roleSeg = new Map<number, number>();
  for (const r of roles) roleSeg.set(r.id, r.segment_id);
  const seen = new Set<string>();
  const out: SegEdge[] = [];
  for (const rel of relatives) {
    const a = roleSeg.get(rel.from_role_id);
    const b = roleSeg.get(rel.to_role_id);
    if (a === undefined || b === undefined || a === b) continue;
    const key = `${a}→${b}:${rel.type}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ from: a, to: b, kind: rel.type });
  }
  return out;
}

/**
 * Topological sort сегментов в один столбец.
 * Приоритет — hierarchy-рёбра (delegates_to/spawned_by); если их нет — все рёбра.
 * Циклы: недостижимые вершины докладываются в порядке оригинального списка.
 */
export function topoOrderSegments(
  segments: Segment[],
  roles: Role[],
  relatives: Relative[],
): number[] {
  const ids = segments.map((s) => s.id);
  const idSet = new Set(ids);
  const edges = buildSegmentEdges(roles, relatives);
  let layoutEdges = edges.filter((e) => HIERARCHY_KINDS.has(e.kind));
  if (layoutEdges.length === 0) layoutEdges = edges;

  const outgoing = new Map<number, number[]>();
  const indegree = new Map<number, number>();
  for (const id of ids) {
    outgoing.set(id, []);
    indegree.set(id, 0);
  }
  for (const e of layoutEdges) {
    if (!idSet.has(e.from) || !idSet.has(e.to)) continue;
    outgoing.get(e.from)!.push(e.to);
    indegree.set(e.to, (indegree.get(e.to) ?? 0) + 1);
  }

  const outCount = (id: number) => outgoing.get(id)!.length;
  const idxOf = new Map(ids.map((id, i) => [id, i]));

  const connected: number[] = [];
  const disconnected: number[] = [];
  for (const id of ids) {
    if ((indegree.get(id) ?? 0) === 0) {
      (outCount(id) > 0 ? connected : disconnected).push(id);
    }
  }

  const ordered: number[] = [];
  const marked = new Set<number>();
  const ready = [...connected].sort(
    (a, b) => outCount(b) - outCount(a) || (idxOf.get(a)! - idxOf.get(b)!),
  );
  while (ready.length > 0) {
    ready.sort((a, b) => outCount(b) - outCount(a) || (idxOf.get(a)! - idxOf.get(b)!));
    const cur = ready.shift()!;
    if (marked.has(cur)) continue;
    marked.add(cur);
    ordered.push(cur);
    for (const next of outgoing.get(cur)!) {
      indegree.set(next, (indegree.get(next) ?? 0) - 1);
      if (indegree.get(next) === 0 && !marked.has(next)) ready.push(next);
    }
  }
  // недостижимые (циклы) + изолированные — в исходном порядке
  for (const id of [...ids, ...disconnected]) {
    if (!marked.has(id)) {
      marked.add(id);
      ordered.push(id);
    }
  }
  return ordered;
}

export function hasSavedLayout(layout?: TopologyLayout): boolean {
  return !!layout && (layout.segments.length > 0 || layout.roles.length > 0);
}

/**
 * Основной вход: расчёт позиций.
 * `layout` — сохранённый пользовательский layout (hybrid: побеждает, если есть).
 */
export function computeTopologyLayout(
  segments: Segment[],
  roles: Role[],
  relatives: Relative[],
  layout?: TopologyLayout,
): TopologyLayoutResult {
  const segCount = new Map<number, number>();
  const rolesBySegment = new Map<number, Role[]>();
  const standalone: Role[] = [];
  for (const r of roles) {
    if (segments.some((s) => s.id === r.segment_id)) {
      segCount.set(r.segment_id, (segCount.get(r.segment_id) ?? 0) + 1);
      const list = rolesBySegment.get(r.segment_id) ?? [];
      list.push(r);
      rolesBySegment.set(r.segment_id, list);
    } else {
      standalone.push(r);
    }
  }

  if (hasSavedLayout(layout)) {
    // hybrid: сохранённые позиции побеждают
    const segPos = new Map<number, SegmentPos>();
    const rolePos = new Map<number, RolePos>();
    const segById = new Map(segments.map((s) => [s.id, s]));

    for (const s of segments) {
      const saved = layout!.segments.find((e) => e.segment_id === s.id);
      const measured = measureSegment(segCount.get(s.id) ?? 0);
      segPos.set(s.id, {
        x: saved?.position.x ?? 0,
        y: saved?.position.y ?? 0,
        width: saved?.position.width || measured.width,
        height: saved?.position.height || measured.height,
      });
    }
    for (const r of roles) {
      const saved = layout!.roles.find((e) => e.role_id === r.id);
      const parent = segById.has(r.segment_id) ? r.segment_id : null;
      const parentPos = parent !== null ? segPos.get(parent)! : null;
      if (saved && parentPos) {
        rolePos.set(r.id, {
          x: saved.position.x - parentPos.x,
          y: saved.position.y - parentPos.y,
          parentId: parent,
        });
      } else if (parentPos) {
        const idx = (rolesBySegment.get(r.segment_id) ?? []).findIndex((r2) => r2.id === r.id);
        const p = memberPosition(Math.max(0, idx));
        rolePos.set(r.id, { ...p, parentId: parent });
      } else {
        // standalone со сдвигом, чтобы не пересекаться: колонка справа
        const idx = standalone.indexOf(r);
        rolePos.set(r.id, { x: 900 + idx * (ROLE_W + ROLE_GAP_X), y: 0, parentId: null });
      }
    }
    return { mode: 'saved', segments: segPos, roles: rolePos };
  }

  // auto: один столбец по toposort
  const order = topoOrderSegments(segments, roles, relatives);
  const segPos = new Map<number, SegmentPos>();
  const rolePos = new Map<number, RolePos>();
  let nextY = 0;
  for (const segId of order) {
    const count = segCount.get(segId) ?? 0;
    const size = measureSegment(count);
    segPos.set(segId, { x: 0, y: nextY, width: size.width, height: size.height });
    const members = rolesBySegment.get(segId) ?? [];
    members.forEach((r, i) => {
      rolePos.set(r.id, { ...memberPosition(i), parentId: segId });
    });
    nextY += size.height + ENTITY_GAP_Y;
  }
  // standalone роли — под колонкой segments (сетка до 3 колонок)
  if (standalone.length > 0) {
    let maxSegBottom = 0;
    for (const segId of order) {
      const p = segPos.get(segId)!;
      maxSegBottom = Math.max(maxSegBottom, p.y + p.height);
    }
    const y0 = maxSegBottom + ENTITY_GAP_Y;
    standalone.forEach((r, i) => {
      const col = i % MAX_COLS;
      const row = Math.floor(i / MAX_COLS);
      rolePos.set(r.id, {
        x: col * (ROLE_W + ROLE_GAP_X),
        y: y0 + row * (ROLE_H + ROLE_GAP_Y),
        parentId: null,
      });
    });
  }
  return { mode: 'auto', segments: segPos, roles: rolePos };
}
