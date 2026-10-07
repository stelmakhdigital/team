// Topology validation. The SERVER is the source of truth (POST /teams/:id/validate);
// this module mirrors the same codes for instant client-side UX hints.
import type { Relative, RelativeType, Role, Segment, TopologyError, TopologyWarning } from '../types/api';

export interface TopologyCheckOptions {
  check_circular?: boolean;
  check_orphans?: boolean;
  check_required_fields?: boolean;
}

export interface TopologyCheckResult {
  is_valid: boolean;
  errors: TopologyError[];
  warnings: TopologyWarning[];
}

const DEFAULTS: Required<TopologyCheckOptions> = {
  check_circular: true,
  check_orphans: true,
  check_required_fields: true,
};

export function validateTopologyGraph(
  roles: Role[],
  segments: Segment[],
  relatives: Relative[],
  opts: TopologyCheckOptions = {},
): TopologyCheckResult {
  const o = { ...DEFAULTS, ...opts };
  const errors: TopologyError[] = [];
  const warnings: TopologyWarning[] = [];

  if (o.check_required_fields) {
    for (const role of roles) {
      if (!role.agent_spec || !role.agent_spec.trim()) {
        errors.push({
          code: 'ROLE_NO_AGENT_SPEC',
          message: `Role '${role.name}' has no agent_spec`,
          severity: 'error',
          location: { type: 'role', id: role.id, name: role.name },
          fix_suggestion: 'Open the role in the config panel and set an agent spec',
        });
      }
    }
    for (const segment of segments) {
      if (!segment.name || !segment.name.trim()) {
        errors.push({
          code: 'SEGMENT_NO_NAME',
          message: 'Segment has no name',
          severity: 'error',
          location: { type: 'segment', id: segment.id, name: `#${segment.id}` },
          fix_suggestion: 'Give the segment a name',
        });
      }
    }
  }

  const edges = relatives.map((r) => ({ from: r.from_role_id, to: r.to_role_id }));

  if (o.check_circular) {
    const cycle = findCycle(edges);
    if (cycle) {
      const names = cycle.map((id) => roles.find((r) => r.id === id)?.name ?? `#${id}`);
      errors.push({
        code: 'CIRCULAR_DEPENDENCY',
        message: `Circular dependency detected: ${names.join(' → ')} → ${names[0]}`,
        severity: 'error',
        location: { type: 'relative', id: relatives[0]?.id ?? 0, name: names[0] },
        fix_suggestion: 'Remove one of the edges in the cycle',
      });
    }
  }

  if (o.check_orphans) {
    const connected = new Set<number>();
    for (const e of edges) {
      connected.add(e.from);
      connected.add(e.to);
    }
    for (const role of roles) {
      if (!connected.has(role.id)) {
        warnings.push({
          code: 'ORPHAN_ROLE',
          message: `Role '${role.name}' has no incoming or outgoing edges`,
          severity: 'warning',
          location: { type: 'role', id: role.id, name: role.name },
          fix_suggestion: 'Connect the role to the team topology',
        });
      }
    }
  }

  const outCount = new Map<number, number>();
  const inCount = new Map<number, number>();
  for (const e of edges) {
    outCount.set(e.from, (outCount.get(e.from) ?? 0) + 1);
    inCount.set(e.to, (inCount.get(e.to) ?? 0) + 1);
  }
  for (const role of roles) {
    if ((outCount.get(role.id) ?? 0) === 0 && (inCount.get(role.id) ?? 0) > 0) {
      warnings.push({
        code: 'NO_OUTGOING_EDGES',
        message: `Role '${role.name}' has no outgoing edges`,
        severity: 'warning',
        location: { type: 'role', id: role.id, name: role.name },
      });
    }
    if ((inCount.get(role.id) ?? 0) === 0 && (outCount.get(role.id) ?? 0) > 0 && !isLeadLike(role)) {
      warnings.push({
        code: 'NO_INCOMING_EDGES',
        message: `Role '${role.name}' has no incoming edges`,
        severity: 'warning',
        location: { type: 'role', id: role.id, name: role.name },
      });
    }
  }

  // duplicate role names within a segment
  const bySegment = new Map<number, Map<string, number>>();
  for (const role of roles) {
    const m = bySegment.get(role.segment_id) ?? new Map<string, number>();
    m.set(role.name.toLowerCase(), (m.get(role.name.toLowerCase()) ?? 0) + 1);
    bySegment.set(role.segment_id, m);
  }
  for (const [segId, names] of bySegment) {
    for (const [name, count] of names) {
      if (count > 1) {
        const seg = segments.find((s) => s.id === segId);
        warnings.push({
          code: 'DUPLICATE_ROLE_NAME',
          message: `Duplicate role name '${name}' in segment '${seg?.name ?? `#${segId}`}'`,
          severity: 'warning',
          location: { type: 'segment', id: segId, name: seg?.name ?? `#${segId}` },
        });
      }
    }
  }

  return { is_valid: errors.length === 0, errors, warnings };
}

function isLeadLike(role: Role): boolean {
  return role.name.toLowerCase().includes('lead') || role.name.toLowerCase().includes('coord');
}

/** DFS cycle detection over role edges. Returns node ids of one cycle or null. */
export function findCycle(edges: { from: number; to: number }[]): number[] | null {
  const adj = new Map<number, number[]>();
  const nodes = new Set<number>();
  for (const e of edges) {
    adj.set(e.from, [...(adj.get(e.from) ?? []), e.to]);
    nodes.add(e.from);
    nodes.add(e.to);
  }
  const color = new Map<number, number>(); // 0 white 1 gray 2 black
  const stack: number[] = [];

  const dfs = (node: number): number[] | null => {
    color.set(node, 1);
    stack.push(node);
    for (const next of adj.get(node) ?? []) {
      const c = color.get(next) ?? 0;
      if (c === 1) {
        const i = stack.indexOf(next);
        return [...stack.slice(i), next];
      }
      if (c === 0) {
        const found = dfs(next);
        if (found) return found;
      }
    }
    stack.pop();
    color.set(node, 2);
    return null;
  };

  for (const n of nodes) {
    if ((color.get(n) ?? 0) === 0) {
      const found = dfs(n);
      if (found) return found;
    }
  }
  return null;
}

export const RELATIVE_TYPES: RelativeType[] = [
  'delegates_to',
  'spawned_by',
  'can_observe',
  'collaborates_with',
];

export const RELATIVE_TYPE_LABELS: Record<RelativeType, string> = {
  delegates_to: 'Delegates to',
  spawned_by: 'Spawned by',
  can_observe: 'Can observe',
  collaborates_with: 'Collaborates with',
};
