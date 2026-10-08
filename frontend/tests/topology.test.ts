import { describe, expect, it } from 'vitest';
import { findCycle, validateTopologyGraph } from '../src/lib/topology';
import type { Relative, Role, Segment } from '../src/types/api';

function role(id: number, name: string, segmentId = 1, agent_spec = 'pi-worker'): Role {
  return {
    id,
    team_id: 1,
    segment_id: segmentId,
    segment_name: 'S',
    name,
    address: `t:S.${name}`,
    agent_spec,
    state: 'inactive',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

function segment(id: number, name: string): Segment {
  return { id, team_id: 1, name, config: {}, roles_count: 0, created_at: '', updated_at: '' };
}

function rel(id: number, from: number, to: number, type: Relative['type'] = 'delegates_to'): Relative {
  return {
    id,
    team_id: 1,
    from_role_id: from,
    to_role_id: to,
    from_role_name: `r${from}`,
    to_role_name: `r${to}`,
    type,
    created_at: '',
  };
}

describe('findCycle', () => {
  it('returns null for a DAG', () => {
    expect(findCycle([{ from: 1, to: 2 }, { from: 2, to: 3 }])).toBeNull();
  });
  it('detects a cycle', () => {
    const c = findCycle([{ from: 1, to: 2 }, { from: 2, to: 3 }, { from: 3, to: 1 }]);
    expect(c).not.toBeNull();
    expect(c!.length).toBeGreaterThanOrEqual(3);
  });
});

describe('validateTopologyGraph', () => {
  it('flags roles without agent_spec', () => {
    const res = validateTopologyGraph([role(1, 'W', 1, '')], [segment(1, 'S')], []);
    expect(res.is_valid).toBe(false);
    expect(res.errors[0].code).toBe('ROLE_NO_AGENT_SPEC');
  });
  it('flags circular dependencies', () => {
    const res = validateTopologyGraph([role(1, 'A'), role(2, 'B')], [segment(1, 'S')], [rel(1, 1, 2), rel(2, 2, 1)]);
    expect(res.is_valid).toBe(false);
    expect(res.errors.some((e) => e.code === 'CIRCULAR_DEPENDENCY')).toBe(true);
  });
  it('warns about orphan roles', () => {
    const res = validateTopologyGraph([role(1, 'A'), role(2, 'B')], [segment(1, 'S')], [rel(1, 1, 2)]);
    expect(res.warnings.some((w) => w.code === 'ORPHAN_ROLE' || w.code === 'NO_INCOMING_EDGES' || w.code === 'NO_OUTGOING_EDGES')).toBe(true);
  });
  it('passes a valid topology', () => {
    const roles = [role(1, 'Lead'), role(2, 'Worker'), role(3, 'Reviewer')];
    const res = validateTopologyGraph(roles, [segment(1, 'S')], [rel(1, 1, 2), rel(2, 2, 3)]);
    expect(res.errors).toEqual([]);
  });
});
