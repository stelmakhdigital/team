// YAML-конфигурирование команды: экспорт текущей топологии в YAML (TeamSpec)
// и импорт: создание новой команды из YAML или merge в текущую команду.
import { useMemo, useState } from 'react';
import { api } from '../../api';
import { useMutation } from '../../hooks/useMutation';
import { useToast } from '../ui/Toast';
import { errorMessage } from '../ui/States';
import { buildMergePlan, isSelfContained, teamToYaml, yamlToSpec } from '../../lib/specYaml';
import type { GetTopologyResponse } from '../../types/api';

interface Props {
  data: GetTopologyResponse;
  onCreated: (teamId: number) => void;
  onMerged: () => void;
  onClose: () => void;
}

export default function SpecYamlPanel({ data, onCreated, onMerged, onClose }: Props) {
  const { toast } = useToast();
  const initial = useMemo(() => teamToYaml(data).yaml, [data]);
  const [text, setText] = useState(initial);

  // lenient validation: relatives may reference roles that already exist in the team
  const currentRoleKeys = useMemo(
    () => new Set(data.roles.map((r) => `${r.segment_name}|${r.name}`)),
    [data],
  );
  const parsed = useMemo(() => yamlToSpec(text, currentRoleKeys), [text, currentRoleKeys]);
  const selfContained = useMemo(
    () => (parsed.spec ? isSelfContained(parsed.spec) : false),
    [parsed],
  );
  const mergePlan = useMemo(
    () => (parsed.spec ? buildMergePlan(parsed.spec, data) : null),
    [parsed, data],
  );

  const create = useMutation((req: Parameters<typeof api.teams.createTeam>[0]) => api.teams.createTeam(req));
  const createSegment = useMutation((teamId: number, req: Parameters<typeof api.teams.createSegment>[1]) => api.teams.createSegment(teamId, req));
  const createRole = useMutation((segmentId: number, req: Parameters<typeof api.teams.createRole>[1]) => api.teams.createRole(segmentId, req));
  const createRelative = useMutation((teamId: number, req: Parameters<typeof api.teams.createRelative>[1]) => api.teams.createRelative(teamId, req));

  const onCreateTeam = async () => {
    if (!parsed.spec || !parsed.name) return;
    if (!selfContained) {
      toast('error', 'Create requires a self-contained spec: all relative addresses must reference roles declared in the YAML');
      return;
    }
    try {
      const res = await create.mutate({ name: parsed.name, description: parsed.description, spec: parsed.spec });
      toast('success', `Team '${parsed.name}' created from YAML`);
      onCreated(res.id);
    } catch {
      toast('error', errorMessage(create.error ?? new Error('Failed to create team')));
    }
  };

  const onMerge = async () => {
    if (!mergePlan) return;
    const { segments, roles, relatives } = mergePlan;
    if (segments.length === 0 && roles.length === 0 && relatives.length === 0) {
      toast('success', 'Nothing to merge — team already matches YAML');
      return;
    }
    try {
      const segIdByName = new Map<string, number>();
      for (const s of segments) {
        const res = await createSegment.mutate(data.team.id, { name: s.name, description: s.description });
        segIdByName.set(s.name, res.id);
      }
      // id сегментов: существующие + созданные
      const existingSegId = new Map(data.segments.map((s) => [s.name, s.id]));
      for (const r of roles) {
        const segId = existingSegId.get(r.segment) ?? segIdByName.get(r.segment);
        if (segId === undefined) {
          toast('error', `Segment '${r.segment}' not found for role '${r.name}'`);
          continue;
        }
        await createRole.mutate(segId, { name: r.name, agent_spec: r.agent_spec, profile: r.profile });
      }
      // relatives: нужен from/to по адресам → id
      const rolesAfter = await refetchRoles();
      for (const rel of relatives) {
        const fromId = roleIdByAddr(rolesAfter, rel.from);
        const toId = roleIdByAddr(rolesAfter, rel.to);
        if (fromId === undefined || toId === undefined) continue;
        await createRelative.mutate(data.team.id, { from_role_id: fromId, to_role_id: toId, type: rel.type });
      }
      toast('success', `Merged: ${segments.length} segment(s), ${roles.length} role(s), ${relatives.length} relative(s)`);
      onMerged();
    } catch {
      toast('error', errorMessage(new Error('Merge failed — see API console')));
    }
  };

  const refetchRoles = async () => {
    const fresh = await api.teams.getTopology(data.team.id);
    return fresh.roles;
  };

  const roleIdByAddr = (roles: GetTopologyResponse['roles'], addr: string): number | undefined => {
    const dot = addr.indexOf('.');
    if (dot <= 0) return undefined;
    const seg = addr.slice(0, dot);
    const name = addr.slice(dot + 1);
    return roles.find((r) => r.segment_name === seg && r.name === name)?.id;
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      toast('success', 'YAML copied to clipboard');
    } catch {
      toast('error', 'Clipboard unavailable — select and copy manually');
    }
  };

  const planTotal = mergePlan
    ? mergePlan.segments.length + mergePlan.roles.length + mergePlan.relatives.length
    : 0;

  return (
    <aside className="config-panel spec-panel" aria-label="Team YAML spec">
      <div className="panel-head">
        <h3>⧉ YAML spec</h3>
        <button className="btn-icon" onClick={onClose} aria-label="Close YAML panel">✕</button>
      </div>

      <p className="muted small">
        TeamSpec format: <code>segments</code>, <code>roles</code> (with <code>segment</code>),
        <code> relatives</code> (<code>Segment.Role</code> addresses). Create a new team or
        merge into the current one.
      </p>

      <textarea
        className="spec-yaml"
        value={text}
        onChange={(e) => setText(e.target.value)}
        spellCheck={false}
        aria-label="Team YAML spec"
        rows={16}
      />

      <div className="spec-status" aria-live="polite">
        {parsed.errors.length === 0 ? (
          <span className="spec-ok">✓ valid YAML{parsed.name ? ` — team '${parsed.name}'` : ''}</span>
        ) : (
          parsed.errors.slice(0, 5).map((e, i) => (
            <div key={i} className="spec-err">✖ {e}</div>
          ))
        )}
        {parsed.errors.length > 5 && <div className="spec-err">… and {parsed.errors.length - 5} more</div>}
      </div>

      <div className="spec-actions">
        <button className="btn" onClick={() => setText(teamToYaml(data).yaml)} title="Re-export current team">
          ⟳ Refresh
        </button>
        <button className="btn" onClick={copy}>⧉ Copy</button>
      </div>
      <div className="spec-actions">
        <button
          className="btn btn-primary"
          onClick={onCreateTeam}
          disabled={!parsed.spec || !parsed.name || !selfContained || create.pending}
          title={selfContained ? 'POST /teams with this spec' : 'Spec references existing team roles - merge only'}
        >
          {create.pending ? 'Creating…' : '＋ Create team from YAML'}
        </button>
      </div>
      <div className="spec-actions">
        <button
          className="btn"
          onClick={onMerge}
          disabled={!mergePlan || planTotal === 0}
          title={planTotal === 0 ? 'Team already matches YAML' : `Will create ${planTotal} resource(s)`}
        >
          {mergePlan && planTotal > 0 ? `⇲ Merge into team (${planTotal})` : '⇲ Merge into team'}
        </button>
      </div>
    </aside>
  );
}
