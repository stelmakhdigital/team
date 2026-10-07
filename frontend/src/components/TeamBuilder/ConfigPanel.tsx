import { useEffect, useState } from 'react';
import { api } from '../../api';
import { useQuery } from '../../hooks/useQuery';
import { useMutation } from '../../hooks/useMutation';
import { errorMessage, Spinner } from '../ui/States';
import { useToast } from '../ui/Toast';
import type { Role, Segment, UpdateRoleConfigRequest } from '../../types/api';

export type ConfigSelection =
  | { type: 'role'; id: number; name: string }
  | { type: 'segment'; id: number; name: string }
  | null;

interface ConfigPanelProps {
  selection: ConfigSelection;
  role?: Role;
  segment?: Segment;
  onClose: () => void;
  onChanged: () => void;
}

function RoleConfig({ roleId, onSaved }: { roleId: number; onSaved: () => void }) {
  const { data, loading, error, refetch } = useQuery(`role.config.${roleId}`, () => api.teams.getRoleConfig(roleId));
  const save = useMutation((req: UpdateRoleConfigRequest) => api.teams.updateRoleConfig(roleId, req));
  const { toast } = useToast();

  const [agentSpec, setAgentSpec] = useState('');
  const [profile, setProfile] = useState('');
  const [model, setModel] = useState('');
  const [temperature, setTemperature] = useState(0.7);
  const [plugins, setPlugins] = useState<Record<string, boolean>>({});
  const [skills, setSkills] = useState<string[]>([]);

  useEffect(() => {
    if (data) {
      setAgentSpec(data.role.agent_spec);
      setProfile(data.role.profile ?? '');
      setModel(data.agent_spec.pi_config.model);
      setTemperature(data.agent_spec.pi_config.temperature);
      const pl: Record<string, boolean> = {};
      for (const p of data.agent_spec.pi_config.plugins) pl[p.name] = p.enabled;
      setPlugins(pl);
      setSkills(data.agent_spec.pi_config.skills.filter((s) => s.enabled).map((s) => s.path));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data?.role.id]);

  if (loading) return <Spinner label="Loading config…" />;
  if (error || !data) return <div className="form-error">Failed to load config. <button className="btn" onClick={refetch}>Retry</button></div>;

  const dirty =
    agentSpec !== data.role.agent_spec ||
    model !== data.agent_spec.pi_config.model ||
    temperature !== data.agent_spec.pi_config.temperature ||
    JSON.stringify(plugins) !== JSON.stringify(Object.fromEntries(data.agent_spec.pi_config.plugins.map((p) => [p.name, p.enabled]))) ||
    JSON.stringify([...skills].sort()) !== JSON.stringify(data.agent_spec.pi_config.skills.filter((s) => s.enabled).map((s) => s.path).sort());

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!agentSpec.trim()) {
      toast('error', 'Agent spec is required');
      return;
    }
    try {
      const res = await save.mutate({
        agent_spec: agentSpec.trim(),
        profile: profile || undefined,
        pi_config: {
          model,
          temperature,
          plugins,
          skills,
        },
      });
      toast('success', `Role config updated${res.requires_restart ? ' (restart required)' : ''}`);
      onSaved();
    } catch {
      toast('error', errorMessage(save.error ?? new Error('Failed to save')));
    }
  };

  return (
    <form onSubmit={submit} className="config-form">
      <section>
        <h4>Basic</h4>
        <label>
          Agent spec *
          <input value={agentSpec} onChange={(e) => setAgentSpec(e.target.value)} list="agent-specs" placeholder="pi-worker" />
        </label>
        <datalist id="agent-specs">
          {['pi-lead', 'pi-worker', 'pi-reviewer', 'pi-watchdog', 'pi-go-backend', 'pi-go-worker'].map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <label>
          Profile
          <select value={profile} onChange={(e) => setProfile(e.target.value)}>
            <option value="">— default —</option>
            {data.available_profiles.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      </section>

      <section>
        <h4>Pi config</h4>
        <label>
          Model
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            {['claude-3-7-sonnet', 'claude-3-5-haiku', 'gpt-4o', 'gemini-2.0'].map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </label>
        <label>
          Temperature: {temperature.toFixed(1)}
          <input
            type="range"
            min={0}
            max={2}
            step={0.1}
            value={temperature}
            onChange={(e) => setTemperature(Number(e.target.value))}
          />
        </label>
        <fieldset>
          <legend>Plugins</legend>
          {data.available_plugins.map((p) => (
            <label key={p.name} className="check">
              <input
                type="checkbox"
                checked={!!plugins[p.name]}
                onChange={(e) => setPlugins({ ...plugins, [p.name]: e.target.checked })}
              />
              {p.name}
            </label>
          ))}
        </fieldset>
        <fieldset>
          <legend>Skills</legend>
          {data.available_skills.map((s) => (
            <label key={s.path} className="check">
              <input
                type="checkbox"
                checked={skills.includes(s.path)}
                onChange={(e) => setSkills(e.target.checked ? [...skills, s.path] : skills.filter((x) => x !== s.path))}
              />
              {s.path}
            </label>
          ))}
        </fieldset>
      </section>

      <section>
        <h4>Resources</h4>
        <div className="muted small">
          CPU {data.agent_spec.resources.cpu} · {data.agent_spec.resources.memory}
          {data.agent_spec.resources.gpu ? ' · GPU' : ''}
        </div>
      </section>

      <section>
        <h4>Startup files</h4>
        <ul className="file-list">
          {data.agent_spec.startup.files.map((f) => (
            <li key={f.path} className="muted small">
              {f.path} <span className="badge">{f.orientation}</span>
            </li>
          ))}
          {data.agent_spec.startup.files.length === 0 && <li className="muted small">none</li>}
        </ul>
      </section>

      <div className="panel-actions">
        <button type="submit" className="btn btn-primary" disabled={!dirty || save.pending}>
          {save.pending ? 'Saving…' : 'Save'}
        </button>
      </div>
      {!dirty && <div className="muted small">No changes</div>}
    </form>
  );
}

function SegmentConfig({ segment }: { segment: Segment }) {
  const { toast } = useToast();
  const [name, setName] = useState(segment.name);
  const [description, setDescription] = useState(segment.description ?? '');

  useEffect(() => {
    setName(segment.name);
    setDescription(segment.description ?? '');
  }, [segment.id, segment.name, segment.description]);

  if (!name.trim()) {
    return (
      <div>
        <label>
          Name *
          <input value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <div className="form-error">Segment name is required</div>
      </div>
    );
  }

  return (
    <div>
      <label>
        Name
        <input value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label>
        Description
        <input value={description} onChange={(e) => setDescription(e.target.value)} />
      </label>
      <div className="muted small">
        Roles in segment: {segment.roles_count} · created {segment.created_at.slice(0, 10)}
      </div>
      <button className="btn" onClick={() => toast('info', 'Segment rename will use POST/PATCH once backend supports it (tracked)')}>
        Apply
      </button>
    </div>
  );
}

export default function ConfigPanel({ selection, role, segment, onClose, onChanged }: ConfigPanelProps) {
  return (
    <aside className="config-panel" aria-label="Configuration panel">
      <div className="panel-header">
        <h3>{selection ? (selection.type === 'role' ? `Role: ${selection.name}` : `Segment: ${selection.name}`) : 'Configuration'}</h3>
        <button className="btn-icon" onClick={onClose} aria-label="Close panel">
          ✕
        </button>
      </div>
      {!selection && <p className="muted">Select a role or segment on the canvas to configure it.</p>}
      {selection?.type === 'role' && role && <RoleConfig roleId={selection.id} onSaved={onChanged} />}
      {selection?.type === 'segment' && segment && <SegmentConfig segment={segment} />}
    </aside>
  );
}
