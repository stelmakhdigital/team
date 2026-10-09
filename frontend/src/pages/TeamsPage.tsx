import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, errorMessage, PageSkeleton , Badge } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';
import type { Team } from '../types/api';

function TeamRow({ team }: { team: Team }) {
  return (
    <tr>
      <td>
        <Link to={`/teams/${team.id}`} className="team-link">
          {team.name}
        </Link>
        <div className="muted small">{team.description}</div>
      </td>
      <td>
        <Badge kind="entity" value={team.state} />
      </td>
      <td>{team.segments_count}</td>
      <td>{team.roles_count}</td>
      <td className="muted">{formatRelative(team.updated_at)}</td>
    </tr>
  );
}

export default function TeamsPage() {
  const { data, loading, error, refetch } = useQuery('teams.list', () => api.teams.getTeams());
  const projectsQ = useQuery('teams.create.projects', () => api.projects.list({}), []);
  const [creating, setCreating] = useState(false);
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

  return (
    <div className="page">
      <div className="page-head">
        <h1>Teams</h1>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>
          + New team
        </button>
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
              </tr>
            </thead>
            <tbody>
              {data.teams.map((t) => (
                <TeamRow key={t.id} team={t} />
              ))}
            </tbody>
          </table></div>
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
