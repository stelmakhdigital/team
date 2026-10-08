import { describe, expect, it } from 'vitest';
import type { GetTeamResponse, Role, Segment } from '../src/types/api';
import { buildMergePlan, isSelfContained, teamToYaml, yamlToSpec } from '../src/lib/specYaml';

const iso = '2026-01-01T00:00:00Z';
const segment = (id: number, name: string): Segment => ({
  id,
  team_id: 1,
  name,
  config: {},
  roles_count: 1,
  created_at: iso,
  updated_at: iso,
});
const role = (id: number, segment_id: number, segment_name: string, name: string, agent_spec = 'pi-worker'): Role => ({
  id,
  team_id: 1,
  segment_id,
  segment_name,
  name,
  address: `team1:${segment_name}.${name}`,
  agent_spec,
  state: 'active',
  created_at: iso,
  updated_at: iso,
});

function fixture(): GetTeamResponse {
  return {
    team: {
      id: 1,
      name: 'Dev Team',
      description: 'Backend + review',
      state: 'active',
      segments_count: 2,
      roles_count: 3,
      created_at: iso,
      updated_at: iso,
    },
    segments: [segment(1, 'Backend'), segment(2, 'Review')],
    roles: [
      role(1, 1, 'Backend', 'Lead', 'pi-lead'),
      role(2, 1, 'Backend', 'Worker'),
      role(3, 2, 'Review', 'Reviewer', 'pi-reviewer'),
    ],
    relatives: [
      { id: 1, team_id: 1, from_role_id: 1, from_role_name: 'Lead', to_role_id: 2, to_role_name: 'Worker', type: 'delegates_to', created_at: iso },
      { id: 2, team_id: 1, from_role_id: 1, from_role_name: 'Lead', to_role_id: 3, to_role_name: 'Reviewer', type: 'can_observe', created_at: iso },
    ],
  };
}

describe('teamToYaml', () => {
  it('экспортирует TeamSpec с адресами Segment.Role', () => {
    const { yaml } = teamToYaml(fixture());
    expect(yaml).toContain('name: Dev Team');
    expect(yaml).toContain('segments:');
    expect(yaml).toContain('- name: Backend');
    expect(yaml).toContain('agent_spec: pi-lead');
    expect(yaml).toContain('segment: Backend');
    expect(yaml).toContain('from: Backend.Lead');
    expect(yaml).toContain('to: Review.Reviewer');
    expect(yaml).toContain('type: delegates_to');
  });

  it('roundtrip: export → parse даёт тот же spec', () => {
    const { yaml } = teamToYaml(fixture());
    const res = yamlToSpec(yaml);
    expect(res.errors).toEqual([]);
    expect(res.spec?.segments.map((s) => s.name)).toEqual(['Backend', 'Review']);
    expect(res.spec?.roles).toHaveLength(3);
    expect(res.spec?.relatives).toHaveLength(2);
    expect(res.spec?.relatives?.[0]).toMatchObject({ from: 'Backend.Lead', to: 'Backend.Worker', type: 'delegates_to' });
  });
});

describe('yamlToSpec', () => {
  it('валидный YAML', () => {
    const res = yamlToSpec(`
name: T
segments:
  - name: A
roles:
  - name: r1
    agent_spec: pi-worker
    segment: A
relatives: []
`);
    expect(res.errors).toEqual([]);
    expect(res.name).toBe('T');
  });

  it('битый YAML → ошибка парсинга', () => {
    const res = yamlToSpec('name: [unclosed');
    expect(res.errors.length).toBeGreaterThan(0);
    expect(res.errors[0]).toContain('invalid YAML');
    expect(res.spec).toBeUndefined();
  });

  it('отсутствует agent_spec', () => {
    const res = yamlToSpec(`
segments:
  - name: A
roles:
  - name: r1
    segment: A
`);
    expect(res.errors.join(' ')).toContain('agent_spec required');
  });

  it('неизвестный segment у роли', () => {
    const res = yamlToSpec(`
segments:
  - name: A
roles:
  - name: r1
    agent_spec: pi-worker
    segment: Ghost
`);
    expect(res.errors.join(' ')).toContain("unknown segment 'Ghost'");
  });

  it('некорректный тип relative', () => {
    const res = yamlToSpec(`
segments:
  - name: A
roles:
  - name: r1
    agent_spec: pi-worker
    segment: A
  - name: r2
    agent_spec: pi-worker
    segment: A
relatives:
  - from: A.r1
    to: A.r2
    type: teleport
`);
    expect(res.errors.join(' ')).toContain('relatives[0].type');
  });

  it('неизвестный адрес в relative', () => {
    const res = yamlToSpec(`
segments:
  - name: A
roles:
  - name: r1
    agent_spec: pi-worker
    segment: A
relatives:
  - from: A.r1
    to: A.nobody
    type: delegates_to
`);
    expect(res.errors.join(' ')).toContain("unknown role address 'A.nobody'");
  });

  it('пустой spec → ошибка', () => {
    const res = yamlToSpec('name: X');
    expect(res.errors.join(' ')).toContain('at least one segment and one role');
  });
});

describe('isSelfContained', () => {
  it('spec, ссылающийся на внешние роли, не self-contained', () => {
    const res = yamlToSpec(
      `
segments:
  - name: A
roles:
  - name: r1
    agent_spec: pi-worker
    segment: A
relatives:
  - from: A.r1
    to: A.external
    type: delegates_to
`,
      new Set(['A|external']),
    );
    expect(res.errors).toEqual([]);
    expect(isSelfContained(res.spec!)).toBe(false);
  });
  it('замкнутый spec — self-contained', () => {
    const res = yamlToSpec(
      `
segments:
  - name: A
roles:
  - name: r1
    agent_spec: pi-worker
    segment: A
  - name: r2
    agent_spec: pi-worker
    segment: A
relatives:
  - from: A.r1
    to: A.r2
    type: delegates_to
`,
    );
    expect(res.errors).toEqual([]);
    expect(isSelfContained(res.spec!)).toBe(true);
  });
});

describe('buildMergePlan', () => {
  const currentKeys = new Set(['Backend|Lead', 'Backend|Worker', 'Review|Reviewer']);

  it('пропускает существующие, оставляет новые', () => {
    const spec = yamlToSpec(`
segments:
  - name: Backend
  - name: Infra
roles:
  - name: Lead
    agent_spec: pi-lead
    segment: Backend
  - name: Ops
    agent_spec: pi-ops
    segment: Infra
relatives:
  - from: Backend.Lead
    to: Backend.Worker
    type: delegates_to
`, currentKeys);
    expect(spec.errors).toEqual([]);
    const plan = buildMergePlan(spec.spec!, fixture());
    expect(plan.segments.map((s) => s.name)).toEqual(['Infra']);
    expect(plan.roles.map((r) => r.name)).toEqual(['Ops']);
    // relative Lead→Worker уже есть → не входит
    expect(plan.relatives).toEqual([]);
  });

  it('всё новое — всё включается', () => {
    const spec = yamlToSpec(`
segments:
  - name: New
roles:
  - name: N1
    agent_spec: pi-worker
    segment: New
  - name: N2
    agent_spec: pi-worker
    segment: New
relatives:
  - from: New.N1
    to: New.N2
    type: collaborates_with
`);
    const plan = buildMergePlan(spec.spec!, fixture());
    expect(plan.segments).toHaveLength(1);
    expect(plan.roles).toHaveLength(2);
    expect(plan.relatives).toHaveLength(1);
  });
});

describe('mock adapter: createTeam with spec', () => {
  it('создаёт segments/roles/relatives из spec', async () => {
    const { createMockAdapter } = await import('../src/api/mock/adapter');
    const api = createMockAdapter();
    const res = await api.teams.createTeam({
      name: 'FromYaml',
      spec: {
        segments: [{ name: 'Dev' }],
        roles: [
          { name: 'Lead', agent_spec: 'pi-lead', segment: 'Dev' },
          { name: 'Worker', agent_spec: 'pi-worker', segment: 'Dev' },
        ],
        relatives: [{ from: 'Dev.Lead', to: 'Dev.Worker', type: 'delegates_to' }],
      },
    });
    const team = await api.teams.getTeam(res.id);
    expect(team.segments.map((s) => s.name)).toEqual(['Dev']);
    expect(team.roles).toHaveLength(2);
    expect(team.relatives).toHaveLength(1);
    expect(team.relatives[0].type).toBe('delegates_to');
  });
});
