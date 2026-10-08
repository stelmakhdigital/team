// Auto-layout для workflow-блоков (R6.4, референс openRIG applyTreeLayout).
// Чистая функция: connections → позиции блоков (колонки по топологическим уровням).
import type { WorkflowBlock, WorkflowConnection } from '../../types/api';

export const BLOCK_W = 180;
export const BLOCK_H = 64;
export const GAP_X = 64;
export const GAP_Y = 36;

/**
 * Топологические уровни (уровень = длина longest path от истоков).
 * Циклы: оставшиеся узлы идут после DAG-части (уровень max+1+idx),
 * чтобы layout не зависал на невалидном графе.
 */
export function workflowLevels(
  blocks: WorkflowBlock[],
  connections: WorkflowConnection[],
): Map<number, number> {
  const ids = new Set(blocks.map((b) => b.id));
  const level = new Map<number, number>();
  const indeg = new Map<number, number>();
  const out = new Map<number, number[]>();
  for (const b of blocks) {
    indeg.set(b.id, 0);
    out.set(b.id, []);
  }
  for (const c of connections) {
    if (!ids.has(c.from_block_id) || !ids.has(c.to_block_id)) continue;
    out.get(c.from_block_id)!.push(c.to_block_id);
    indeg.set(c.to_block_id, (indeg.get(c.to_block_id) ?? 0) + 1);
  }
  // Kahn + longest path
  let frontier = blocks.filter((b) => (indeg.get(b.id) ?? 0) === 0).map((b) => b.id);
  for (const id of frontier) level.set(id, 0);
  const placed = new Set(frontier);
  while (frontier.length > 0) {
    const next: number[] = [];
    for (const id of frontier) {
      const lv = level.get(id) ?? 0;
      for (const to of out.get(id) ?? []) {
        level.set(to, Math.max(level.get(to) ?? 0, lv + 1));
        indeg.set(to, (indeg.get(to) ?? 1) - 1);
        if ((indeg.get(to) ?? 0) === 0) {
          next.push(to);
          placed.add(to);
        }
      }
    }
    frontier = next;
  }
  // циклический остаток — после DAG
  const rest = blocks.filter((b) => !placed.has(b.id));
  const maxLv = Math.max(0, ...level.values());
  rest.forEach((b, i) => level.set(b.id, maxLv + 1 + i));
  return level;
}

export interface WorkflowLayoutResult {
  positions: Map<number, { x: number; y: number }>;
  width: number;
  height: number;
}

export function layoutWorkflow(
  blocks: WorkflowBlock[],
  connections: WorkflowConnection[],
): WorkflowLayoutResult {
  const levels = workflowLevels(blocks, connections);
  const byLevel = new Map<number, WorkflowBlock[]>();
  for (const b of blocks) {
    const lv = levels.get(b.id) ?? 0;
    byLevel.set(lv, [...(byLevel.get(lv) ?? []), b]);
  }
  const positions = new Map<number, { x: number; y: number }>();
  let maxRows = 0;
  for (const [lv, list] of byLevel) {
    maxRows = Math.max(maxRows, list.length);
    list.forEach((b, i) => {
      positions.set(b.id, { x: lv * (BLOCK_W + GAP_X), y: i * (BLOCK_H + GAP_Y) });
    });
  }
  return {
    positions,
    width: Math.max(1, byLevel.size) * (BLOCK_W + GAP_X) + GAP_X,
    height: Math.max(1, maxRows) * (BLOCK_H + GAP_Y) + GAP_Y,
  };
}

/** Простая проверка циклов (для локальных hints): Kahn не обработал всех. */
export function hasCycle(blocks: WorkflowBlock[], connections: WorkflowConnection[]): boolean {
  const placed = new Set<number>();
  const ids = new Set(blocks.map((b) => b.id));
  const indeg = new Map<number, number>();
  const out = new Map<number, number[]>();
  for (const b of blocks) {
    indeg.set(b.id, 0);
    out.set(b.id, []);
  }
  for (const c of connections) {
    if (!ids.has(c.from_block_id) || !ids.has(c.to_block_id)) continue;
    out.get(c.from_block_id)!.push(c.to_block_id);
    indeg.set(c.to_block_id, (indeg.get(c.to_block_id) ?? 0) + 1);
  }
  let frontier = blocks.filter((b) => (indeg.get(b.id) ?? 0) === 0).map((b) => b.id);
  for (const id of frontier) placed.add(id);
  while (frontier.length > 0) {
    const next: number[] = [];
    for (const id of frontier) {
      for (const to of out.get(id) ?? []) {
        indeg.set(to, (indeg.get(to) ?? 1) - 1);
        if ((indeg.get(to) ?? 0) === 0) {
          next.push(to);
          placed.add(to);
        }
      }
    }
    frontier = next;
  }
  return placed.size < blocks.length;
}

/** Изолированные блоки (нет ни входа, ни выхода). */
export function isolatedBlocks(blocks: WorkflowBlock[], connections: WorkflowConnection[]): Set<number> {
  const touched = new Set<number>();
  for (const c of connections) {
    touched.add(c.from_block_id);
    touched.add(c.to_block_id);
  }
  return new Set(blocks.filter((b) => !touched.has(b.id)).map((b) => b.id));
}
