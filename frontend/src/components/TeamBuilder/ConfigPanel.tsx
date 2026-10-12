import { useEffect, useState } from 'react';
import { api } from '../../api';
import { useQuery } from '../../hooks/useQuery';
import { useMutation } from '../../hooks/useMutation';
import { errorMessage, Spinner } from '../ui/States';
import { useToast } from '../ui/Toast';
import type { Role, Segment, UpdateRoleConfigRequest } from '../../types/api';
import AgentResourcesPanel from './AgentResourcesPanel';

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

const THINKING_LEVELS = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const;
const AGENT_SPEC_SUGGESTIONS = ['pi-lead', 'pi-worker', 'pi-reviewer', 'pi-watchdog', 'pi-go-backend', 'pi-go-worker'];

interface McpServerDraft { name: string; url: string; enabled: boolean; }

// Hint — «исходный value из agent_spec» (дефолт) vs текущий override.
function DefaultHint({ label, def }: { label: string; def: string }) {
  return <span className="muted small"> ({label}: {def})</span>;
}

function RoleConfig({ roleId, onSaved }: { roleId: number; onSaved: () => void }) {
  const { data, loading, error, refetch } = useQuery(`role.config.${roleId}`, () => api.teams.getRoleConfig(roleId));
  const save = useMutation((req: UpdateRoleConfigRequest) => api.teams.updateRoleConfig(roleId, req));
  const { toast } = useToast();

  const [agentSpec, setAgentSpec] = useState('');
  const [profile, setProfile] = useState('');
  const [model, setModel] = useState('');
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(0);
  const [thinking, setThinking] = useState('');
  const [mcpServers, setMcpServers] = useState<McpServerDraft[]>([]);
  // T5: Agent resources (plugins/skills/startup_files) — отдельная модалка.
  const [showResources, setShowResources] = useState(false);

  const [initial, setInitial] = useState<string>('');

  useEffect(() => {
    if (data) {
      const spec = data.agent_spec;
      const ov = (data.config ?? {}) as Record<string, any>;
      setAgentSpec(data.role.agent_spec);
      setProfile(data.role.profile ?? '');
      setModel(ov.model ?? spec.pi_config.model);
      setTemperature(ov.temperature ?? spec.pi_config.temperature);
      setMaxTokens(ov.max_tokens ?? spec.pi_config.max_tokens ?? 0);
      setThinking(ov.thinking ?? spec.pi_config.thinking ?? '');
      const mcps = Array.isArray(ov.mcp) ? (ov.mcp as McpServerDraft[]) : spec.pi_config.mcp.servers;
      setMcpServers((mcps as McpServerDraft[]).map((m) => ({ ...m })));
      setInitial(JSON.stringify({
        agentSpec: data.role.agent_spec, profile: data.role.profile ?? '', model, temperature, maxTokens, thinking, mcpServers: mcps,
      }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data?.role.id]);

  if (loading) return <Spinner label="Loading config…" />;
  if (error || !data) return <div className="form-error">Failed to load config. <button className="btn" onClick={refetch}>Retry</button></div>;

  const cur = JSON.stringify({ agentSpec, profile, model, temperature, maxTokens, thinking, mcpServers });
  const dirty = cur !== initial;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!agentSpec.trim()) {
      toast('error', 'Agent spec is required');
      return;
    }
    // T4/T5: config override — flat-карта scalar pi_config + mcp (backend merge'ит c layout).
    // plugins/skills/startup_files — в agent_resources (Agent resources panel).
    const cfg: Record<string, unknown> = {
      model,
      temperature,
      max_tokens: maxTokens || undefined,
      thinking: thinking || undefined,
      mcp: mcpServers,
    };
    try {
      await save.mutate({ agent_spec: agentSpec.trim(), profile: profile || undefined, config: cfg });
      toast('success', 'Role config saved (applied at next session launch)');
      setInitial(cur);
      onSaved();
    } catch {
      toast('error', errorMessage(save.error ?? new Error('Failed to save')));
    }
  };

  const spec = data.agent_spec;

  return (
    <form onSubmit={submit} className="config-form">
      <section>
        <h4>Basic</h4>
        <label>
          Agent spec *
          <input value={agentSpec} onChange={(e) => setAgentSpec(e.target.value)} list="agent-specs" placeholder="pi-worker" />
        </label>
        <datalist id="agent-specs">
          {AGENT_SPEC_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <label>
          Profile
          <select value={profile} onChange={(e) => setProfile(e.target.value)}>
            <option value="">— default —</option>
            {data.available_profiles.map((p) => (
              <option key={p.name} value={p.name}>{p.name}</option>
            ))}
          </select>
        </label>
      </section>

      <section>
        <h4>Pi config</h4>
        <label>
          Model
          <select value={model} onChange={(e) => setModel(e.target.value)}>
            {[spec.pi_config.model, 'claude-3-7-sonnet', 'claude-3-5-haiku', 'gpt-4o', 'gemini-2.0']
              .filter((v, i, a) => v && a.indexOf(v) === i)
              .map((m) => (
                <option key={m} value={m}>{m}</option>
              ))}
          </select>
          <DefaultHint label="spec" def={spec.pi_config.model} />
        </label>
        <label>
          Temperature: {temperature.toFixed(1)}
          <input type="range" min={0} max={2} step={0.1} value={temperature} onChange={(e) => setTemperature(Number(e.target.value))} />
          <DefaultHint label="spec" def={spec.pi_config.temperature.toFixed(1)} />
        </label>
        <label>
          Max tokens
          <input type="number" min={0} step={256} value={maxTokens || ''} placeholder={String(spec.pi_config.max_tokens ?? '')} onChange={(e) => setMaxTokens(Number(e.target.value) || 0)} />
          <DefaultHint label="spec" def={String(spec.pi_config.max_tokens ?? '—')} />
        </label>
        <label>
          Thinking
          <select value={thinking} onChange={(e) => setThinking(e.target.value)}>
            <option value="">— default —</option>
            {THINKING_LEVELS.map((l) => (
              <option key={l} value={l}>{l}</option>
            ))}
          </select>
          <DefaultHint label="spec" def={spec.pi_config.thinking || '—'} />
        </label>
        <fieldset>
          <legend>MCP servers</legend>
          {mcpServers.map((m, i) => (
            <div key={i} className="kv-row">
              <input className="kv-key" value={m.name} placeholder="name" onChange={(e) => setMcpServers(mcpServers.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
              <input className="kv-val" value={m.url} placeholder="url" onChange={(e) => setMcpServers(mcpServers.map((x, j) => (j === i ? { ...x, url: e.target.value } : x)))} />
              <label className="check"><input type="checkbox" checked={m.enabled} onChange={(e) => setMcpServers(mcpServers.map((x, j) => (j === i ? { ...x, enabled: e.target.checked } : x)))} /> on</label>
              <button type="button" className="btn-icon" aria-label="Remove server" onClick={() => setMcpServers(mcpServers.filter((_, j) => j !== i))}>✕</button>
            </div>
          ))}
          <button type="button" className="btn" onClick={() => setMcpServers([...mcpServers, { name: '', url: '', enabled: true }])}>+ Add server</button>
        </fieldset>
      </section>

      {/* T5: Agent resources (plugins/skills/startup_files) — отдельная панель. */}
      <section>
        <h4>Agent resources</h4>
        <div className="muted small">
          Plugins ({spec.pi_config.plugins.length}) · Skills ({spec.pi_config.skills.length}) ·
          Startup files ({spec.startup.files.length}) — base (spec) + override (role).
        </div>
        <button type="button" className="btn" onClick={() => setShowResources(true)}>⚙ Configure resources…</button>
      </section>

      <section>
        <h4>Resources</h4>
        <div className="muted small">
          CPU {spec.resources.cpu} · {spec.resources.memory}{spec.resources.gpu ? ' · GPU' : ''}
        </div>
      </section>

      <div className="panel-actions">
        <button type="submit" className="btn btn-primary" disabled={!dirty || save.pending}>
          {save.pending ? 'Saving…' : 'Save'}
        </button>
      </div>
      {!dirty && <div className="muted small">No changes</div>}

      {showResources && (
        <AgentResourcesPanel
          roleId={roleId}
          roleSpec={spec}
          initialConfig={(data.config ?? {}) as Record<string, unknown>}
          onClose={() => setShowResources(false)}
          onSaved={() => { setShowResources(false); onSaved(); }}
        />
      )}
    </form>
  );
}

function SegmentConfig({ segment, onSaved }: { segment: Segment; onSaved: () => void }) {
  const { toast } = useToast();
  const [name, setName] = useState(segment.name);
  const [description, setDescription] = useState(segment.description ?? '');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setName(segment.name);
    setDescription(segment.description ?? '');
  }, [segment.id, segment.name, segment.description]);

  const apply = async () => {
    if (!name.trim() || saving) return;
    setSaving(true);
    try {
      await api.teams.updateSegment(segment.id, { name: name.trim(), description: description.trim() });
      toast('success', 'Segment renamed');
      onSaved();
    } catch (e) {
      toast('error', errorMessage(e ?? new Error('Failed to rename segment')));
    } finally {
      setSaving(false);
    }
  };

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
      <button className="btn btn-primary" onClick={apply} disabled={saving || !name.trim()}>
        {saving ? 'Saving…' : 'Apply'}
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
      {selection?.type === 'segment' && segment && <SegmentConfig segment={segment} onSaved={onChanged} />}
    </aside>
  );
}
