import { useState, useMemo, useEffect, useRef } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, errorMessage, PageSkeleton , Badge } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative, formatDateTime, stateTitle } from '../lib/format';
import { useEscapeStack } from '../hooks/useEscapeStack';
import { downloadJson } from '../lib/download';
import type { Team, TeamBundle } from '../types/api';

function TeamRow({
  team,
  busy,
  onSetState,
  onDelete,
  onExport,
}: {
  team: Team;
  busy: boolean;
  onSetState: (team: Team, state: string) => void;
  onDelete: (team: Team) => void;
  onExport: (team: Team) => void;
}) {
  const isArchived = team.state === 'archived';
  return (
    <tr className={isArchived ? 'row-archived' : undefined}>
      <td>
        <Link to={`/teams/${team.id}`} className="team-link">
          {team.name}
        </Link>
        <div className="muted small">{team.description}</div>
      </td>
      <td>
        <Badge kind="entity" value={team.state} title={stateTitle(team.state)} />
      </td>
      <td>{team.segments_count}</td>
      <td>{team.roles_count}</td>
      <td className="muted" title={formatDateTime(team.updated_at)}>{formatRelative(team.updated_at)}</td>
      <td>
        <div className="row-actions">
          {isArchived ? (
            <button className="btn btn-sm" disabled={busy} onClick={() => onSetState(team, 'active')} title="Restore to active">
              Restore
            </button>
          ) : team.state === 'active' ? (
            <button className="btn btn-sm" disabled={busy} onClick={() => onSetState(team, 'archived')} title="Archive (hide from default list, keep data)">
              Archive
            </button>
          ) : (
            <button className="btn btn-sm" disabled={busy} onClick={() => onSetState(team, 'active')} title="Set to active">
              Activate
            </button>
          )}
          <button className="btn btn-sm btn-danger" disabled={busy} onClick={() => onDelete(team)} title="Permanently delete team and all its data">
            Delete
          </button>
          <button className="btn btn-sm" disabled={busy} onClick={() => onExport(team)} title="Download team bundle (JSON): topology + workflows, for import into another daemon/project">
            Export
          </button>
        </div>
      </td>
    </tr>
  );
}

export default function TeamsPage() {
  const { data, loading, error, refetch } = useQuery('teams.list', () => api.teams.getTeams());
  const [visible, setVisible] = useState(25);
  const [showArchived, setShowArchived] = useState(false);
  const [busyId, setBusyId] = useState<number | null>(null);
  // B-1: сортировка по свежести (новые сверху). Архивные скрыты, если не showArchived.
  const teamsSorted = useMemo(() => {
    const all = data?.teams ?? [];
    const filtered = showArchived ? all : all.filter((t) => t.state !== 'archived');
    return [...filtered].sort((a, b) => (b.updated_at ?? '').localeCompare(a.updated_at ?? ''));
  }, [data, showArchived]);
  const shown = teamsSorted.slice(0, visible);
  const projectsQ = useQuery('teams.create.projects', () => api.projects.list({}), []);
  const [creating, setCreating] = useState(false);
  // A2: Esc закрывает модалку создания (через useEscapeStack)
  useEscapeStack(() => setCreating(false), creating);
  // W2.4: ?new=1 — открыть форму создания (CTA с first-run дашборда)
  const [searchParams] = useSearchParams();
  useEffect(() => {
    if (searchParams.get('new') === '1') setCreating(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [projectId, setProjectId] = useState<number | ''>('');
  const [formError, setFormError] = useState<string | null>(null);
  const navigate = useNavigate();
  const { toast } = useToast();

  const create = useMutation(async (payload: { name: string; description?: string; project_id?: number }) => {
    const res = await api.teams.createTeam(payload);
    return res;
  });

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setFormError('Team name is required');
      return;
    }
    setFormError(null);
    try {
      const res = await create.mutate({
        name: trimmed,
        description: description.trim() || undefined,
        project_id: projectId === '' ? undefined : projectId,
      });
      toast('success', `Team '${trimmed}' created`);
      setCreating(false);
      navigate(`/teams/${res.id}`);
    } catch {
      setFormError(errorMessage(create.error ?? new Error('Failed to create team')));
    }
  };

  const setState = async (team: Team, state: string) => {
    setBusyId(team.id);
    try {
      await api.teams.setTeamState(team.id, state);
      toast('success', `Team '${team.name}' → ${state}`);
      refetch();
    } catch (e) {
      toast('error', errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };
  const delTeam = async (team: Team) => {
    if (
      !window.confirm(
        `Permanently delete team '${team.name}' and ALL its data (segments, roles, tasks, sessions, rooms)? This cannot be undone.`,
      )
    )
      return;
    setBusyId(team.id);
    try {
      await api.teams.deleteTeam(team.id, true);
      toast('success', `Team '${team.name}' deleted`);
      refetch();
    } catch (e) {
      toast('error', errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  // C1/P4.3: Export (download .teambundle.json) + Import (file → preview → confirm).
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [importBundle, setImportBundle] = useState<TeamBundle | null>(null);
  const [importSource, setImportSource] = useState<string | null>(null);
  const [importParseError, setImportParseError] = useState<string | null>(null);
  const [importFieldErrors, setImportFieldErrors] = useState<string[]>([]);
  const [importing, setImporting] = useState(false);
  useEscapeStack(() => setImportOpen(false), importOpen);

  const exportTeam = async (team: Team) => {
    setBusyId(team.id);
    try {
      const bundle = await api.teams.exportTeam(team.id);
      downloadJson(`team-${team.name}.teambundle.json`, bundle);
      toast('success', `Team '${team.name}' exported`);
    } catch (e) {
      toast('error', errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const onImportFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ''; // повторный выбор того же файла
    if (!file) return;
    const text = await new Promise<string>((resolve, reject) => {
      const fr = new FileReader();
      fr.onload = () => resolve(String(fr.result ?? ''));
      fr.onerror = () => reject(new Error('Не удалось прочитать файл'));
      fr.readAsText(file);
    }).catch((err) => err as Error);
    if (typeof text !== 'string') {
      setImportBundle(null);
      setImportSource(file.name);
      setImportParseError(text.message);
      setImportFieldErrors([]);
      setImportOpen(true);
      return;
    }
    let parsed: TeamBundle;
    try {
      parsed = JSON.parse(text) as TeamBundle;
    } catch {
      setImportBundle(null);
      setImportSource(file.name);
      setImportParseError('Некорректный JSON — файл не похож на .teambundle.json');
      setImportFieldErrors([]);
      setImportOpen(true);
      return;
    }
    setImportBundle(parsed);
    setImportSource(file.name);
    setImportParseError(null);
    setImportFieldErrors([]);
    setImportOpen(true);
  };

  const confirmImport = async () => {
    if (!importBundle) return;
    setImporting(true);
    setImportFieldErrors([]);
    try {
      const res = await api.teams.importTeam(importBundle);
      toast('success', `Team '${res.team.name}' imported${res.workflows.length ? ` (+${res.workflows.length} workflow(s))` : ''}`);
      setImportOpen(false);
      setImportBundle(null);
      navigate(`/teams/${res.team.id}`);
    } catch (e) {
      const errs: string[] = [];
      const details = (e as { details?: { errors?: Array<{ field: string; reason: string }> } })?.details;
      if (Array.isArray(details?.errors)) {
        for (const fe of details.errors) errs.push(`${fe.field}: ${fe.reason}`);
      } else if (e instanceof Error && e.message) {
        // без field-ошибок — показываем сырое сообщение (напр. unsupported_version) в модалке
        errs.push(e.message);
      }
      if (errs.length) {
        setImportFieldErrors(errs);
      } else {
        toast('error', errorMessage(e));
      }
    } finally {
      setImporting(false);
    }
  };

  const importCounts = importBundle
    ? {
        segments: importBundle.team?.segments?.length ?? 0,
        roles: (importBundle.team?.segments ?? []).reduce((n, s) => n + (s.roles?.length ?? 0), 0),
        workflows: importBundle.workflows?.length ?? 0,
      }
    : null;

  return (
    <div className="page">
      <div className="page-head">
        <h1>Teams</h1>
        <div className="row-actions">
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: '0.9rem', color: 'var(--muted)' }} title="Show archived teams in the list">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
            Show archived
          </label>
          <button className="btn" onClick={() => fileInputRef.current?.click()} title="Import team bundle (.teambundle.json) from another daemon/project">
            ⇪ Import
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept=".teambundle.json,.json,application/json"
            style={{ display: 'none' }}
            aria-label="Team bundle file"
            onChange={onImportFile}
          />
          <button className="btn btn-primary" onClick={() => setCreating(true)}>
            + Новая команда
          </button>
        </div>
      </div>

      {loading && <PageSkeleton label="Loading teams…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {data && data.teams.length === 0 && (
        <EmptyState title="No teams yet" hint="Create your first team to build an agent topology." action={<button className="btn btn-primary" onClick={() => setCreating(true)}>Create team</button>} />
      )}
      {data && data.teams.length > 0 && (
        <div className="card">
          <div className="table-wrap"><table className="table">
            <thead>
              <tr>
                <th>Team</th>
                <th>State</th>
                <th>Segments</th>
                <th>Roles</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((t) => (
                <TeamRow key={t.id} team={t} busy={busyId === t.id} onSetState={setState} onDelete={delTeam} onExport={exportTeam} />
              ))}
            </tbody>
          </table></div>
          {teamsSorted.length > visible && (
            <div className="show-more">
              <button className="btn" onClick={() => setVisible((v) => v + 25)}>
                Show more ({teamsSorted.length - visible} remaining)
              </button>
            </div>
          )}
        </div>
      )}

      {creating && (
        <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label="Create team" onClick={() => setCreating(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>New team</h2>
            <form onSubmit={submit}>
              <label>
                Name *
                <input value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="e.g. Dev Team" />
              </label>
              <label>
                Description
                <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
              </label>
              <label>
                Project (optional)
                <select value={projectId} onChange={(e) => setProjectId(e.target.value === '' ? '' : Number(e.target.value))}>
                  <option value="">— none —</option>
                  {(projectsQ.data?.projects ?? []).map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} ({p.path})
                    </option>
                  ))}
                </select>
              </label>
              {formError && <div className="form-error">{formError}</div>}
              <div className="modal-actions">
                <button type="button" className="btn" onClick={() => setCreating(false)}>
                  Cancel
                </button>
                <button type="submit" className="btn btn-primary" disabled={create.pending}>
                  {create.pending ? 'Creating…' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {importOpen && (
        <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label="Import team bundle" onClick={() => !importing && setImportOpen(false)}>
          <div className="modal import-modal" onClick={(e) => e.stopPropagation()}>
            <h2>Import team</h2>
            {importParseError ? (
              <div className="form-error">{importParseError}</div>
            ) : importBundle ? (
              <div className="import-preview" aria-live="polite">
                <dl className="import-preview-grid">
                  <dt>Team</dt>
                  <dd className="import-preview-name">{importBundle.team?.name}</dd>
                  <dt>Segments</dt>
                  <dd>{importCounts?.segments}</dd>
                  <dt>Roles</dt>
                  <dd>{importCounts?.roles}</dd>
                  <dt>Workflows</dt>
                  <dd>{importCounts?.workflows}</dd>
                </dl>
                <p className="muted small import-note">
                  Проект не переносится (будет назначен после). Сессии, задачи и история не переносятся.
                </p>
                {importSource && <p className="muted small mono">{importSource}</p>}
              </div>
            ) : null}
            {importFieldErrors.length > 0 && (
              <div className="form-error import-field-errors" role="alert">
                <strong>Ошибки валидации:</strong>
                <ul>
                  {importFieldErrors.map((fe) => (
                    <li key={fe}>{fe}</li>
                  ))}
                </ul>
              </div>
            )}
            <div className="modal-actions">
              <button type="button" className="btn" onClick={() => setImportOpen(false)} disabled={importing}>
                Cancel
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={confirmImport}
                disabled={importing || !importBundle}
              >
                {importing ? 'Importing…' : 'Import team'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
