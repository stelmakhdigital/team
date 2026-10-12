import { useState, useMemo, useEffect } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, errorMessage, PageSkeleton , Badge } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative, formatDateTime, stateTitle } from '../lib/format';
import { useEscapeStack } from '../hooks/useEscapeStack';
import type { Team } from '../types/api';

function TeamRow({
  team,
  busy,
  onSetState,
  onDelete,
}: {
  team: Team;
  busy: boolean;
  onSetState: (team: Team, state: string) => void;
  onDelete: (team: Team) => void;
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

  return (
    <div className="page">
      <div className="page-head">
        <h1>Teams</h1>
        <div className="row-actions">
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: '0.9rem', color: 'var(--muted)' }} title="Show archived teams in the list">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
            Show archived
          </label>
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
                <TeamRow key={t.id} team={t} busy={busyId === t.id} onSetState={setState} onDelete={delTeam} />
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
    </div>
  );
}
