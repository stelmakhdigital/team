import type { RelativeType } from '../../types/api';

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
