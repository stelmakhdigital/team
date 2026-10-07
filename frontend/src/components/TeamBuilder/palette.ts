import type { RelativeType, TopologyLayout } from '../../types/api';

export interface SegmentTemplate {
  name: string;
  description: string;
}

export interface RoleTemplate {
  name: string;
  agent_spec: string;
}

export interface Palette {
  segments: SegmentTemplate[];
  roles: RoleTemplate[];
  relations: RelativeType[];
}

export const DEFAULT_PALETTE: Palette = {
  segments: [
    { name: 'Backend', description: 'Backend roles' },
    { name: 'Frontend', description: 'Frontend roles' },
    { name: 'Review', description: 'Review roles' },
    { name: 'Infra', description: 'Infra roles' },
  ],
  roles: [
    { name: 'Lead', agent_spec: 'pi-lead' },
    { name: 'Worker', agent_spec: 'pi-worker' },
    { name: 'Reviewer', agent_spec: 'pi-reviewer' },
    { name: 'Watchdog', agent_spec: 'pi-watchdog' },
  ],
  relations: ['delegates_to', 'spawned_by', 'can_observe', 'collaborates_with'],
};

export const DRAG_TYPES = {
  segment: 'application/x-team-segment',
  role: 'application/x-team-role',
};

export const ROLE_W = 150;
export const ROLE_H = 52;

/** Bounding box of all segments+roles in canvas coordinates, or null when empty. */
export function contentBounds(layout: TopologyLayout | undefined): { x: number; y: number; w: number; h: number } | null {
  if (!layout) return null;
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const s of layout.segments ?? []) {
    const p = s.position;
    if (!p) continue;
    minX = Math.min(minX, p.x);
    minY = Math.min(minY, p.y);
    maxX = Math.max(maxX, p.x + p.width);
    maxY = Math.max(maxY, p.y + p.height);
  }
  for (const r of layout.roles ?? []) {
    const p = r.position;
    if (!p) continue;
    minX = Math.min(minX, p.x);
    minY = Math.min(minY, p.y);
    maxX = Math.max(maxX, p.x + ROLE_W);
    maxY = Math.max(maxY, p.y + ROLE_H);
  }
  if (!isFinite(minX) || !isFinite(minY)) return null;
  return { x: minX, y: minY, w: maxX - minX, h: maxY - minY };
}
