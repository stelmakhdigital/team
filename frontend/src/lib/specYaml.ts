// YAML-конфигурирование команды (TeamSpec-формат, контракт 21).
// Экспорт: текущая команда → YAML. Импорт: YAML → создание новой команды
// (POST /teams c spec) или merge в существующую (create*-эндпоинты).
// Адрес роли в relatives: "SegmentName.RoleName" (первая точка — разделитель).

import { parse, stringify } from 'yaml';
import type { GetTeamResponse, RelativeType, Role, Relative, Segment, TeamSpec } from '../types/api';

export const RELATIVE_TYPES: RelativeType[] = [
  'delegates_to',
  'spawned_by',
  'can_observe',
  'collaborates_with',
];

export interface SpecExport {
  yaml: string;
}

export interface SpecImportResult {
  spec?: TeamSpec;
  name?: string;
  description?: string;
  errors: string[];
}

export function teamToSpec(data: GetTeamResponse): TeamSpec {
  const segName = new Map(data.segments.map((s) => [s.id, s.name]));
  const spec: TeamSpec = {
    segments: data.segments.map((s) => ({
      name: s.name,
      ...(s.description ? { description: s.description } : {}),
    })),
    roles: data.roles.map((r) => ({
      name: r.name,
      agent_spec: r.agent_spec,
      ...(r.profile ? { profile: r.profile } : {}),
      ...(segName.get(r.segment_id) ? { segment: segName.get(r.segment_id)! } : {}),
    })),
    relatives: data.relatives.map((rel: Relative) => ({
      from: roleAddress(data, rel.from_role_id),
      to: roleAddress(data, rel.to_role_id),
      type: rel.type,
    })),
  };
  return spec;
}

function roleAddress(data: GetTeamResponse, roleId: number): string {
  const role = data.roles.find((r) => r.id === roleId);
  const seg = role ? data.segments.find((s) => s.id === role.segment_id) : undefined;
  return `${seg?.name ?? `seg-${role?.segment_id ?? '?'}`}.${role?.name ?? `role-${roleId}`}`;
}

export function teamToYaml(data: GetTeamResponse): SpecExport {
  const doc = {
    name: data.team.name,
    ...(data.team.description ? { description: data.team.description } : {}),
    ...teamToSpec(data),
  };
  return { yaml: stringify(doc, { lineWidth: 100 }) };
}

export function yamlToSpec(text: string, externalRoleKeys?: Set<string>): SpecImportResult {
  let doc: unknown;
  try {
    doc = parse(text);
  } catch (e) {
    return { errors: [`invalid YAML: ${e instanceof Error ? e.message : String(e)}`] };
  }
  if (typeof doc !== 'object' || doc === null || Array.isArray(doc)) {
    return { errors: ['root must be a mapping (name/segments/roles/relatives)'] };
  }
  const d = doc as Record<string, unknown>;
  const errors: string[] = [];

  const name = typeof d.name === 'string' && d.name.trim() ? d.name.trim() : undefined;
  const description = typeof d.description === 'string' ? d.description : undefined;

  // segments
  const segNames = new Set<string>();
  const segments: TeamSpec['segments'] = [];
  if (d.segments !== undefined) {
    if (!Array.isArray(d.segments)) errors.push('segments must be an array');
    else {
      for (const [i, s] of (d.segments as unknown[]).entries()) {
        const so = s as Record<string, unknown> | null;
        if (typeof so?.name !== 'string' || !so.name.trim()) {
          errors.push(`segments[${i}].name: required string`);
          continue;
        }
        const n = so.name.trim();
        if (segNames.has(n)) {
          errors.push(`segments: duplicate name '${n}'`);
          continue;
        }
        segNames.add(n);
        segments.push({
          name: n,
          ...(typeof so.description === 'string' ? { description: so.description } : {}),
        });
      }
    }
  }

  // roles
  const roleKeys = new Set<string>(); // `${segment}|${name}`
  const roles: TeamSpec['roles'] = [];
  if (d.roles !== undefined) {
    if (!Array.isArray(d.roles)) errors.push('roles must be an array');
    else {
      for (const [i, r] of (d.roles as unknown[]).entries()) {
        const ro = r as Record<string, unknown> | null;
        if (typeof ro?.name !== 'string' || !ro.name.trim()) {
          errors.push(`roles[${i}].name: required string`);
          continue;
        }
        const rn = ro.name.trim();
        if (typeof ro.agent_spec !== 'string' || !ro.agent_spec.trim()) {
          errors.push(`roles[${i}] (${rn}): agent_spec required`);
          continue;
        }
        const seg = typeof ro.segment === 'string' ? ro.segment.trim() : '';
        if (!seg) {
          errors.push(`roles[${i}] (${rn}): segment required`);
          continue;
        }
        if (!segNames.has(seg)) {
          errors.push(`roles[${i}] (${rn}): unknown segment '${seg}'`);
          continue;
        }
        const key = `${seg}|${rn}`;
        if (roleKeys.has(key)) {
          errors.push(`roles: duplicate '${seg}.${rn}'`);
          continue;
        }
        roleKeys.add(key);
        roles.push({
          name: rn,
          agent_spec: ro.agent_spec.trim(),
          ...(typeof ro.profile === 'string' ? { profile: ro.profile } : {}),
          segment: seg,
        });
      }
    }
  }

  // relatives
  const relatives: TeamSpec['relatives'] = [];
  if (d.relatives !== undefined) {
    if (!Array.isArray(d.relatives)) errors.push('relatives must be an array');
    else {
      for (const [i, r] of (d.relatives as unknown[]).entries()) {
        const re = r as Record<string, unknown> | null;
        const from = resolveAddress(re?.from, roleKeys, externalRoleKeys, `relatives[${i}].from`, errors);
        const to = resolveAddress(re?.to, roleKeys, externalRoleKeys, `relatives[${i}].to`, errors);
        if (typeof re?.type !== 'string' || !RELATIVE_TYPES.includes(re.type as RelativeType)) {
          errors.push(`relatives[${i}].type: must be one of ${RELATIVE_TYPES.join(', ')}`);
          continue;
        }
        if (!from || !to) continue;
        relatives.push({ from, to, type: re.type as RelativeType });
      }
    }
  }

  if (errors.length === 0 && (segments.length === 0 || roles.length === 0)) {
    errors.push('spec must declare at least one segment and one role');
  }

  return {
    ...(name ? { name } : {}),
    ...(description ? { description } : {}),
    ...(errors.length === 0 ? { spec: { segments, roles, relatives } } : {}),
    errors,
  };
}

function resolveAddress(
  raw: unknown,
  roleKeys: Set<string>,
  externalRoleKeys: Set<string> | undefined,
  where: string,
  errors: string[],
): string | null {
  if (typeof raw !== 'string' || !raw.trim()) {
    errors.push(`${where}: required "Segment.Role" address`);
    return null;
  }
  const addr = raw.trim();
  const dot = addr.indexOf('.');
  if (dot <= 0 || dot === addr.length - 1) {
    errors.push(`${where}: expected "Segment.Role" address, got '${addr}'`);
    return null;
  }
  const seg = addr.slice(0, dot);
  const role = addr.slice(dot + 1);
  const key = `${seg}|${role}`;
  if (!roleKeys.has(key) && !externalRoleKeys?.has(key)) {
    errors.push(`${where}: unknown role address '${addr}'`);
    return null;
  }
  return addr;
}

/** Self-contained: all relative addresses resolve to spec roles (needed for "Create team"). */
export function isSelfContained(spec: TeamSpec): boolean {
  const keys = new Set(spec.roles.map((r) => `${r.segment}|${r.name}`));
  for (const rel of spec.relatives ?? []) {
    if (!refResolves(rel.from, keys) || !refResolves(rel.to, keys)) return false;
  }
  return true;
}

function refResolves(addr: string, keys: Set<string>): boolean {
  const dot = addr.indexOf('.');
  if (dot <= 0 || dot === addr.length - 1) return false;
  return keys.has(`${addr.slice(0, dot)}|${addr.slice(dot + 1)}`);
}

/** Merge-план: какие сущности создать в команде, чтобы применить spec. */
export interface MergePlan {
  segments: Array<{ name: string; description?: string }>;
  roles: Array<{ segment: string; name: string; agent_spec: string; profile?: string }>;
  relatives: Array<{ from: string; to: string; type: RelativeType }>;
}

export function buildMergePlan(spec: TeamSpec, current: { segments: Segment[]; roles: Role[]; relatives: Relative[] }): MergePlan {
  const segNames = new Set(current.segments.map((s) => s.name));
  const roleKeys = new Set(current.roles.map((r) => `${r.segment_name}|${r.name}`));

  const segments: MergePlan['segments'] = [];
  for (const s of spec.segments) {
    if (!segNames.has(s.name)) segments.push(s);
  }

  const roles: MergePlan['roles'] = [];
  const segAfter = new Set([...segNames, ...segments.map((s) => s.name)]);
  for (const r of spec.roles) {
    const seg = r.segment!;
    if (!segAfter.has(seg)) continue;
    const key = `${seg}|${r.name}`;
    if (!roleKeys.has(key)) roles.push({ segment: seg, name: r.name, agent_spec: r.agent_spec, profile: r.profile });
  }

  // relatives: после добавления ролей; дубли (from,to) пропускаем
  const relKeys = new Set(current.relatives.map((r) => `${addressOf(current, r.from_role_id)}→${addressOf(current, r.to_role_id)}`));
  const roleAfter = new Set([...roleKeys, ...roles.map((r) => `${r.segment}|${r.name}`)]);
  const relatives: MergePlan['relatives'] = [];
  for (const rel of spec.relatives ?? []) {
    const f = resolveRef(rel.from, roleAfter);
    const t = resolveRef(rel.to, roleAfter);
    if (!f || !t) continue;
    const key = `${f}→${t}`;
    if (relKeys.has(key)) continue;
    relKeys.add(key);
    relatives.push(rel);
  }
  return { segments, roles, relatives };
}

function resolveRef(addr: string, roleKeys: Set<string>): string | null {
  const dot = addr.indexOf('.');
  if (dot <= 0) return null;
  const key = `${addr.slice(0, dot)}|${addr.slice(dot + 1)}`;
  return roleKeys.has(key) ? addr : null;
}

function addressOf(current: { roles: Role[]; segments: Segment[] }, roleId: number): string {
  const role = current.roles.find((r) => r.id === roleId);
  return role ? `${role.segment_name}.${role.name}` : `?`;
}
