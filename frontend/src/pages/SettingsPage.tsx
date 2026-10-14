import { Fragment, useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import { useToast } from '../components/ui/Toast';
import { ErrorState, Spinner, errorMessage } from '../components/ui/States';
import { formatUptime } from '../lib/format';
import type { ConfigKeyView, ConfigSection, ConfigMeta, ConfigEnvVar } from '../types/api';

// U4: Settings / Configuration (OpenRIG-style).
// Sections: Meta (readonly), Workspace (editable, restart-marked), Environment (env provenance),
// plus the full typed registry of all daemon keys (set override / reset, layered source provenance).

function fmtValue(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—';
  if (typeof v === 'boolean') return v ? 'yes' : 'no';
  if (Array.isArray(v)) return v.length ? v.join(', ') : '—';
  return String(v);
}

// P2.14: JSON-ключ (cost.model_prices) — current_value = строка с JSON; pretty-print для чтения.
function fmtJSONValue(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—';
  if (typeof v === 'object') return JSON.stringify(v);
  try {
    return JSON.stringify(JSON.parse(String(v)), null, 0);
  } catch {
    return String(v);
  }
}

function ConfigKeyRow({ kv, onChanged }: { kv: ConfigKeyView; onChanged: () => void }) {
  const { toast } = useToast();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const setM = useMutation((v: unknown) => api.config.setConfig(kv.key, v));
  const resetM = useMutation(() => api.config.resetConfig(kv.key));
  const isBool = kv.type === 'boolean';
  const boolOn = draft === 'true';

  const openEdit = () => {
    if (isBool) setDraft(kv.current_value ? 'true' : 'false');
    else if (Array.isArray(kv.current_value)) setDraft(kv.current_value.join(', '));
    else setDraft(String(kv.current_value ?? ''));
    setEditing(true);
  };
  const commitValue = (): unknown => {
    if (isBool) return boolOn;
    if (kv.type === 'integer') return Number(draft);
    if (kv.type === 'string-list') return draft.split(',').map((s) => s.trim()).filter(Boolean);
    return draft;
  };
  const save = async () => {
    try {
      const res = await setM.mutate(commitValue());
      toast('success', res.needs_restart ? `Saved ${kv.key} — restart required to apply` : `Saved ${kv.key}`);
      setEditing(false);
      onChanged();
    } catch {
      toast('error', errorMessage(setM.error ?? new Error('Save failed')));
    }
  };
  const reset = async () => {
    try {
      const res = await resetM.mutate();
      toast('info', `Reset ${kv.key} → source ${res.source}`);
      setEditing(false);
      onChanged();
    } catch {
      toast('error', errorMessage(resetM.error ?? new Error('Reset failed')));
    }
  };

  return (
    <tr className={kv.source === 'override' ? 'cfg-row-override' : undefined}>
      <td>
        <code className="cfg-key">
          {kv.key.split('.').slice(1).join(' ').split('_').map((p, i, arr) => (
            <Fragment key={i}>{p}{i < arr.length - 1 ? <>{'_'}<wbr /></> : null}</Fragment>
          ))}
        </code>
        <div className="muted small">
          {kv.description}
          {kv.default_is_derived && <span className="derived-tag"> derived</span>}
        </div>
      </td>
      <td><span className="cfg-type">{kv.type}</span></td>
      <td>
        {editing ? (
          isBool ? (
            <label className="cfg-bool">
              <input type="checkbox" checked={boolOn} onChange={(e) => setDraft(e.target.checked ? 'true' : 'false')} />
              {boolOn ? 'yes' : 'no'}
            </label>
          ) : kv.json_object ? (
            <textarea
              className="input cfg-input cfg-json-input"
              rows={5}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              aria-label={kv.key}
              spellCheck={false}
            />
          ) : (
            <input
              className="input cfg-input"
              type={kv.type === 'integer' ? 'number' : 'text'}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              aria-label={kv.key}
            />
          )
        ) : (
          <span className={kv.json_object ? 'cfg-value cfg-json-value' : 'cfg-value'}>{kv.json_object ? fmtJSONValue(kv.current_value) : fmtValue(kv.current_value)}</span>
        )}
        <div className="muted small cfg-default">
          default: {fmtValue(kv.default_value)}
          {kv.default_is_derived && <span className="derived-tag" title="host-derived default (from cwd / workspace root, not a universal constant)"> (derived)</span>}
        </div>
      </td>
      <td>
        <span className={`cfg-source cfg-source-${kv.source}`}>{kv.source}</span>
        {kv.source_detail && <div className="muted small"><code>{kv.source_detail}</code></div>}
      </td>
      <td className="cfg-center" role="img" aria-label="Индикатор: изменение требует перезапуск демона">{kv.requires_restart ? <span className="cfg-restart" title="Изменение требует перезапуск демона">⏻</span> : <span className="muted">·</span>}</td>
      <td className="cfg-actions">
        {editing ? (
          <span className="cfg-inline-actions">
            <button className="btn btn-small btn-primary" onClick={save} disabled={setM.pending}>Save</button>
            <button className="btn btn-small" onClick={() => setEditing(false)}>Cancel</button>
          </span>
        ) : (
          <span className="cfg-inline-actions">
            {kv.editable ? (
              <button className="btn btn-small" onClick={openEdit} title="Edit (set override)" aria-label={`Edit ${kv.key}`}>✎</button>
            ) : (
              <span className="muted small" title="read-only">⊘</span>
            )}
            {kv.source === 'override' && (
              <button className="btn btn-small" onClick={reset} disabled={resetM.pending} title="Reset to previous layer">Reset</button>
            )}
          </span>
        )}
      </td>
    </tr>
  );
}

function SectionCard({ section, highlight, onChanged }: { section: ConfigSection; highlight?: boolean; onChanged: () => void }) {
  return (
    <div className={highlight ? 'card cfg-section cfg-section-workspace' : 'card cfg-section'}>
      <h2>{highlight ? 'Workspace' : section.name}</h2>
      <p className="muted small">{section.description}</p>
      <div className="table-wrap">
      <table className="table cfg-table">
        <thead>
          <tr>
            <th scope="col">Key</th>
            <th scope="col">Type</th>
            <th scope="col">Current value</th>
            <th scope="col">Source</th>
            <th scope="col">Restart</th>
            <th scope="col"></th>
          </tr>
        </thead>
        <tbody>
          {section.keys.map((kv) => (
            <ConfigKeyRow key={kv.key} kv={kv} onChanged={onChanged} />
          ))}
        </tbody>
      </table>
      </div>
    </div>
  );
}

function MetaCard({ meta }: { meta: ConfigMeta }) {
  const rows: Array<[string, string]> = [
    ['Project', meta.project],
    ['Instance', meta.instance],
    ['Version', meta.version],
    ['Uptime', formatUptime(meta.uptime_seconds)],
    ['Git', meta.git_commit ? `${meta.git_commit.slice(0, 10)} (${meta.git_branch ?? '—'})` : '—'],
    ['Config hash', meta.config_hash],
    ['Started', new Date(meta.started_at).toLocaleString()],
  ];
  return (
    <div className="card settings-meta">
      <h2>Meta <span className="muted small">— instance identity (readonly)</span></h2>
      <dl className="cfg-meta-grid">
        {rows.map(([k, v]) => (
          <div key={k} className="cfg-meta-item">
            <dt>{k}</dt>
            <dd>{k === 'Config hash' ? <code>{v}</code> : v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function EnvironmentCard({ environment }: { environment: ConfigEnvVar[] }) {
  return (
    <div className="card">
      <h2>Environment <span className="muted small">— env variables affecting config (provenance)</span></h2>
      <div className="table-wrap">
      <table className="table cfg-table">
        <thead>
          <tr>
            <th scope="col">Env var</th>
            <th scope="col">Value</th>
            <th scope="col">Feeds key</th>
            <th scope="col">Active source</th>
          </tr>
        </thead>
        <tbody>
          {environment.map((e) => (
            <tr key={e.env_var}>
              <td><code>{e.env_var}</code></td>
              <td>{e.value || <span className="muted">—</span>}</td>
              <td><code>{e.feeds_key}</code></td>
              <td>{e.source_active ? <span className="cfg-source cfg-source-env">env</span> : <span className="muted">·</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
    </div>
  );
}

export default function SettingsPage() {
  const q = useQuery('config.get', () => api.config.getConfig());
  if (q.loading) return <div className="page"><Spinner label="Loading settings…" /></div>;
  if (q.error) return <div className="page"><ErrorState error={q.error} onRetry={q.refetch} /></div>;
  const { meta, sections, environment } = q.data!;
  const workspace = sections.find((s) => s.name === 'workspace');
  const others = sections.filter((s) => s.name !== 'workspace');

  return (
    <div className="page">
      <div className="page-head">
        <h1>Settings</h1>
        <div className="head-actions">
          <span className="muted small">resolution: default &lt; file &lt; env &lt; override</span>
          <button className="btn" onClick={() => window.dispatchEvent(new CustomEvent('team:tour-restart'))} title="Повторить onboarding-тур" aria-label="Повторить тур">
            ▶ Повторить тур
          </button>
          <button className="btn btn-primary" onClick={q.refetch} title="Обновить конфигурацию">↻ Refresh</button>
        </div>
      </div>

      <MetaCard meta={meta} />

      {workspace && <SectionCard section={workspace} highlight onChanged={q.refetch} />}

      <EnvironmentCard environment={environment} />

      <div className="settings-sections-grid">
        {others.map((s) => (
          <SectionCard key={s.name} section={s} onChanged={q.refetch} />
        ))}
      </div>
    </div>
  );
}
