import { useMemo, useState } from 'react';
import { api } from '../../api';
import { useMutation } from '../../hooks/useMutation';
import { errorMessage } from '../ui/States';
import { useToast } from '../ui/Toast';
import type { AgentSpec } from '../../types/api';

// T5 — Agent resources: отдельная панель (не ConfigPanel) для роли. Три под-секции
// (plugins/skills/startup_files) — редактируемые списки (add/edit/remove). Override
// (role.Config["agent_resources"]) перекрывает base (agent_spec) по key (name/path).

export interface PluginEntry {
  name: string;
  enabled: boolean;
  description: string;
  config: string; // JSON (string в UI; парсится при сохранении)
}
export interface SkillEntry {
  path: string;
  enabled: boolean;
  description: string;
}
export interface StartupEntry {
  path: string;
  orientation: string;
  delivery_hint: string;
}
type Source = 'base' | 'override';

interface Merged<T> {
  value: T;
  source: Source;
}

interface AgentResourcesPanelProps {
  roleId: number;
  roleSpec: AgentSpec;
  initialConfig: Record<string, unknown>;
  onClose: () => void;
  onSaved: () => void;
}

function readOverride<T extends { name?: string; path?: string }>(cfg: Record<string, unknown>, key: string): T[] {
  const ar = (cfg.agent_resources ?? {}) as Record<string, unknown>;
  const list = ar[key] as T[] | undefined;
  return Array.isArray(list) ? list : [];
}

function mergeByBase<TBase extends { name?: string; path?: string }, TOverride extends { name?: string; path?: string }>(
  base: TBase[],
  override: TOverride[],
  key: 'name' | 'path',
  mergeOne: (b: TBase, o: TOverride) => TBase,
): Merged<TBase>[] {
  const keyOf = (e: { name?: string; path?: string }) => (key === 'name' ? e.name : e.path) ?? '';
  const out: Merged<TBase>[] = [];
  const overrideKeys = new Set<string>();
  for (const b of base) {
    const k = keyOf(b);
    const o = override.find((x) => keyOf(x) === k);
    if (o) {
      overrideKeys.add(k);
      out.push({ value: mergeOne(b, o), source: 'override' });
    } else {
      out.push({ value: b, source: 'base' });
    }
  }
  for (const o of override) {
    const k = keyOf(o);
    if (!overrideKeys.has(k)) {
      out.push({ value: o as unknown as TBase, source: 'override' });
    }
  }
  return out;
}

export default function AgentResourcesPanel({ roleId, roleSpec, initialConfig, onClose, onSaved }: AgentResourcesPanelProps) {
  const { toast } = useToast();
  const save = useMutation((req: { config: Record<string, unknown> }) => api.teams.updateRoleConfig(roleId, req));

  // override (delta) — из role.Config["agent_resources"]
  const [plugins, setPlugins] = useState<PluginEntry[]>(() =>
    readOverride<PluginEntry>(initialConfig, 'plugins').map((p) => ({ ...p, config: typeof p.config === 'string' ? p.config : JSON.stringify(p.config ?? {}) })),
  );
  const [skills, setSkills] = useState<SkillEntry[]>(() => readOverride<SkillEntry>(initialConfig, 'skills'));
  const [startupFiles, setStartupFiles] = useState<StartupEntry[]>(() => readOverride<StartupEntry>(initialConfig, 'startup_files'));
  const [error, setError] = useState<string | null>(null);

  const basePlugins: PluginEntry[] = roleSpec.pi_config.plugins.map((p) => ({
    name: p.name,
    enabled: p.enabled,
    description: p.description ?? '',
    config: JSON.stringify(p.config ?? {}),
  }));
  const baseSkills: SkillEntry[] = roleSpec.pi_config.skills.map((s) => ({
    path: s.path,
    enabled: s.enabled,
    description: s.description ?? '',
  }));
  const baseStartup: StartupEntry[] = roleSpec.startup.files.map((f) => ({
    path: f.path,
    orientation: f.orientation,
    delivery_hint: f.delivery_hint,
  }));

  const mergedPlugins = useMemo(
    () => mergeByBase(basePlugins, plugins, 'name', (b, o) => ({ ...b, enabled: o.enabled, description: o.description ?? b.description, config: o.config ?? b.config })),
    [plugins], // eslint-disable-line react-hooks/exhaustive-deps
  );
  const mergedSkills = useMemo(
    () => mergeByBase(baseSkills, skills, 'path', (b, o) => ({ ...b, enabled: o.enabled, description: o.description ?? b.description })),
    [skills], // eslint-disable-line react-hooks/exhaustive-deps
  );
  const mergedStartup = useMemo(
    () => mergeByBase(baseStartup, startupFiles, 'path', (b, o) => ({ ...b, orientation: o.orientation, delivery_hint: o.delivery_hint })),
    [startupFiles], // eslint-disable-line react-hooks/exhaustive-deps
  );

  // override (delta) для persist: только записи, отличающиеся от base (или новые).
  const buildOverride = () => ({
    agent_resources: {
      plugins: plugins.map((p) => {
        const cfg: Record<string, unknown> = {};
        try {
          const parsed = JSON.parse(p.config || '{}');
          if (parsed && typeof parsed === 'object') Object.assign(cfg, parsed);
        } catch {
          /* config не-JSON → пустой */
        }
        return { name: p.name, enabled: p.enabled, description: p.description, config: cfg };
      }),
      skills,
      startup_files: startupFiles,
    },
  });

  const submit = async () => {
    setError(null);
    // валидация: name/path не пустые; config — валидный JSON
    for (const p of plugins) {
      if (!p.name.trim()) {
        setError('Plugin name must not be empty');
        return;
      }
      try {
        JSON.parse(p.config || '{}');
      } catch {
        setError(`Plugin "${p.name}": config must be valid JSON`);
        return;
      }
    }
    for (const s of skills) {
      if (!s.path.trim()) {
        setError('Skill path must not be empty');
        return;
      }
    }
    for (const f of startupFiles) {
      if (!f.path.trim()) {
        setError('Startup file path must not be empty');
        return;
      }
    }
    try {
      await save.mutate({ config: buildOverride() });
      toast('success', 'Agent resources saved (applied at next session launch)');
      onSaved();
    } catch (e) {
      toast('error', errorMessage(e ?? new Error('Failed to save')));
    }
  };

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label="Agent resources">
      <div className="modal agent-resources-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <div>
            <h2>{'⚙\uFE0E'} Agent resources</h2>
            <div className="muted small">base (spec: {roleSpec.name}) · override перекрывает по key</div>
          </div>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="modal-body">
          {/* Plugins */}
          <section>
            <h4>Plugins</h4>
            {mergedPlugins.map((m, i) => (
              <ResourceRow key={m.value.name} source={m.source}>
                <input
                  className="kv-key"
                  value={m.value.name}
                  disabled={m.source === 'base' && !plugins.some((p) => p.name === m.value.name)}
                  onChange={(e) => upsertOverride(plugins, setPlugins, 'name', m.value.name, { name: e.target.value })}
                  aria-label={`Plugin ${i} name`}
                />
                <input
                  className="kv-val"
                  value={m.value.config}
                  disabled={m.source === 'base' && !plugins.some((p) => p.name === m.value.name)}
                  onChange={(e) => upsertOverride(plugins, setPlugins, 'name', m.value.name, { config: e.target.value })}
                  aria-label={`Plugin ${i} config`}
                />
                <label className="check">
                  <input
                    type="checkbox"
                    checked={m.value.enabled}
                    onChange={(e) => upsertOverride(plugins, setPlugins, 'name', m.value.name, { enabled: e.target.checked })}
                  />
                  on
                </label>
                <RemoveBtn
                  onRemove={() =>
                    m.source === 'override'
                      ? setPlugins(plugins.filter((p) => p.name !== m.value.name))
                      : upsertOverride(plugins, setPlugins, 'name', m.value.name, { enabled: false })
                  }
                />
              </ResourceRow>
            ))}
            <button type="button" className="btn" onClick={() => setPlugins([...plugins, { name: '', enabled: true, description: '', config: '{}' }])}>
              + Add plugin
            </button>
          </section>

          {/* Skills */}
          <section>
            <h4>Skills</h4>
            {mergedSkills.map((m, i) => (
              <ResourceRow key={m.value.path} source={m.source}>
                <input
                  className="kv-key"
                  value={m.value.path}
                  disabled={m.source === 'base' && !skills.some((s) => s.path === m.value.path)}
                  onChange={(e) => upsertOverride(skills, setSkills, 'path', m.value.path, { path: e.target.value })}
                  aria-label={`Skill ${i} path`}
                />
                <label className="check">
                  <input
                    type="checkbox"
                    checked={m.value.enabled}
                    onChange={(e) => upsertOverride(skills, setSkills, 'path', m.value.path, { enabled: e.target.checked })}
                  />
                  on
                </label>
                <RemoveBtn
                  onRemove={() =>
                    m.source === 'override'
                      ? setSkills(skills.filter((s) => s.path !== m.value.path))
                      : upsertOverride(skills, setSkills, 'path', m.value.path, { enabled: false })
                  }
                />
              </ResourceRow>
            ))}
            <button type="button" className="btn" onClick={() => setSkills([...skills, { path: '', enabled: true, description: '' }])}>
              + Add skill
            </button>
          </section>

          {/* Startup files */}
          <section>
            <h4>Startup files</h4>
            {mergedStartup.map((m, i) => (
              <ResourceRow key={m.value.path} source={m.source}>
                <input
                  className="kv-key"
                  value={m.value.path}
                  disabled={m.source === 'base' && !startupFiles.some((f) => f.path === m.value.path)}
                  onChange={(e) => upsertOverride(startupFiles, setStartupFiles, 'path', m.value.path, { path: e.target.value })}
                  aria-label={`Startup file ${i} path`}
                />
                <select
                  value={m.value.orientation}
                  disabled={m.source === 'base' && !startupFiles.some((f) => f.path === m.value.path)}
                  onChange={(e) => upsertOverride(startupFiles, setStartupFiles, 'path', m.value.path, { orientation: e.target.value })}
                  aria-label={`Startup file ${i} orientation`}
                >
                  {['system', 'role', 'context', 'culture'].map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </select>
                <select
                  value={m.value.delivery_hint}
                  disabled={m.source === 'base' && !startupFiles.some((f) => f.path === m.value.path)}
                  onChange={(e) => upsertOverride(startupFiles, setStartupFiles, 'path', m.value.path, { delivery_hint: e.target.value })}
                  aria-label={`Startup file ${i} delivery`}
                >
                  {['send_text', 'project_file'].map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                </select>
                <RemoveBtn
                  onRemove={() =>
                    m.source === 'override'
                      ? setStartupFiles(startupFiles.filter((f) => f.path !== m.value.path))
                      : upsertOverride(startupFiles, setStartupFiles, 'path', m.value.path, { orientation: 'role' })
                  }
                />
              </ResourceRow>
            ))}
            <button type="button" className="btn" onClick={() => setStartupFiles([...startupFiles, { path: '', orientation: 'context', delivery_hint: 'send_text' }])}>
              + Add file
            </button>
          </section>
        </div>
        <div className="modal-actions">
          {error && <div className="form-error small">{error}</div>}
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="button" className="btn btn-primary" onClick={submit} disabled={save.pending}>
            {save.pending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}

function ResourceRow({ source, children }: { source: Source; children: React.ReactNode }) {
  return (
    <div className="kv-row agent-res-row">
      <span className={`badge agent-res-src agent-res-src-${source}`}>{source}</span>
      {children}
    </div>
  );
}

function RemoveBtn({ onRemove }: { onRemove: () => void }) {
  return (
    <button type="button" className="btn-icon" aria-label="Remove" onClick={onRemove}>
      ✕
    </button>
  );
}

// upsertOverride — обновляет/добавляет override-запись по key (base → override при edit).
function upsertOverride<T extends { name?: string; path?: string }>(
  list: T[],
  setList: (v: T[]) => void,
  key: 'name' | 'path',
  currentKey: string,
  patch: Partial<T>,
): void {
  const exists = list.some((e) => (key === 'name' ? e.name : e.path) === currentKey);
  if (exists) {
    setList(list.map((e) => ((key === 'name' ? e.name : e.path) === currentKey ? { ...e, ...patch } : e)));
  } else {
    const entry: T = (key === 'name' ? { name: currentKey } : { path: currentKey }) as T;
    setList([...list, { ...entry, ...patch }]);
  }
}
