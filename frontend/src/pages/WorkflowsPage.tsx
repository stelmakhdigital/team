import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, errorMessage, Spinner , Badge } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';

export default function WorkflowsPage() {
  const { data, loading, error, refetch } = useQuery('workflows.list', () =>
    api.workflows.getWorkflows().catch((e) => {
      // GET /workflows is not in the frozen contract yet (blockers #3);
      // surface the error instead of silently failing.
      throw e;
    }),
  );
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [teamId, setTeamId] = useState(1);
  const navigate = useNavigate();
  const { toast } = useToast();
  const create = useMutation((payload: { team_id: number; name: string }) => api.workflows.createWorkflow(payload));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    try {
      const res = await create.mutate({ team_id: teamId, name: name.trim() });
      toast('success', `Workflow '${name.trim()}' created`);
      navigate(`/workflows/${res.id}`);
    } catch {
      toast('error', errorMessage(create.error ?? new Error('Failed to create workflow')));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <h1>Workflows</h1>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>
          + New workflow
        </button>
      </div>

      {loading && <Spinner label="Loading workflows…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {data && data.workflows.length === 0 && <EmptyState title="No workflows" hint="Create a workflow to orchestrate agent tasks." />}
      {data && data.workflows.length > 0 && (
        <div className="card">
          <div className="table-wrap"><table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Team</th>
                <th>State</th>
                <th>Updated</th>
              </tr>
            </thead>
            <tbody>
              {data.workflows.map((w) => (
                <tr key={w.id}>
                  <td>
                    <Link className="team-link" to={`/workflows/${w.id}`}>
                      {w.name}
                    </Link>
                    <div className="muted small">{w.description}</div>
                  </td>
                  <td>team #{w.team_id}</td>
                  <td>
                    <Badge kind="entity" value={w.state} />
                  </td>
                  <td className="muted">{formatRelative(w.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table></div>
        </div>
      )}

      {creating && (
        <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={() => setCreating(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>New workflow</h2>
            <form onSubmit={submit}>
              <label>
                Name *
                <input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
              </label>
              <label>
                Team
                <select value={teamId} onChange={(e) => setTeamId(Number(e.target.value))}>
                  <option value={1}>Team 1</option>
                  <option value={2}>Team 2</option>
                </select>
              </label>
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
