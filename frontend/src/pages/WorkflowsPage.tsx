import { useEffect, useMemo, useState, useRef } from 'react';
import { Time } from '../components/ui/Time';
import { Link, useNavigate } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, errorMessage, PageSkeleton , Badge } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { useEscapeStack } from '../hooks/useEscapeStack';
import { useFocusTrap } from '../hooks/useFocusTrap';
import {} from '../lib/format';

export default function WorkflowsPage() {
  const { data, loading, error, refetch } = useQuery('workflows.list', () =>
    api.workflows.getWorkflows().catch((e) => {
      // GET /workflows is not in the frozen contract yet (blockers #3);
      // surface the error instead of silently failing.
      throw e;
    }),
  );
  const [creating, setCreating] = useState(false);
  // A2: Esc закрывает модалку создания (через useEscapeStack)
  useEscapeStack(() => setCreating(false), creating);
  // U-X4: focus-trap модалки создания
  const wfCreateRef = useRef<HTMLDivElement>(null);
  useFocusTrap(wfCreateRef, creating);
  const [name, setName] = useState('');
  const [nameTouched, setNameTouched] = useState(false);
  const [teamId, setTeamId] = useState<number | null>(null);
  // T6: inline-rename (клик на имя/кнопку → input).
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editName, setEditName] = useState('');
  const navigate = useNavigate();
  const { toast } = useToast();
  // P0: populate the "New workflow" team select with real active teams (was hardcoded #1/#2).
  const teamsQ = useQuery('workflows.teams', () => api.teams.getTeams());
  const activeTeams = teamsQ.data?.teams.filter((t) => t.state === 'active') ?? [];
  useEffect(() => {
    setTeamId((cur) => {
      if (activeTeams.length === 0) return cur;
      if (cur != null && activeTeams.some((t) => t.id === cur)) return cur;
      return activeTeams[0].id;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [teamsQ.data]);
  const create = useMutation((payload: { team_id: number; name: string }) => api.workflows.createWorkflow(payload));
  const run = useMutation((id: number) => api.workflows.runWorkflow(id, {}));
  const toggleEngine = useMutation((req: { id: number; name: string; on: boolean }) =>
    api.workflows.updateWorkflow(req.id, { name: req.name, engine_enabled: req.on }),
  );
  const rename = useMutation((req: { id: number; name: string }) => api.workflows.updateWorkflow(req.id, { name: req.name }));

  // T6: читаемое дефолтное имя {team.name}-flow (+ -2, -3 при дубликатах в команде). Не Date.now().
  const teamName = activeTeams.find((t) => t.id === teamId)?.name ?? '';
  const defaultName = useMemo(() => {
    if (!teamName) return '';
    const base = `${teamName}-flow`;
    const existing = (data?.workflows ?? []).filter((w) => w.team_id === teamId).map((w) => w.name);
    if (!existing.includes(base)) return base;
    let i = 2;
    while (existing.includes(`${base}-${i}`)) i += 1;
    return `${base}-${i}`;
  }, [teamName, teamId, data?.workflows]);
  useEffect(() => {
    if (creating && !nameTouched) setName(defaultName);
  }, [creating, defaultName, nameTouched]);
  const openCreate = () => { setNameTouched(false); setCreating(true); };
  const doRename = async (id: number) => {
    const n = editName.trim();
    if (!n) { setEditingId(null); return; }
    try {
      await rename.mutate({ id, name: n });
      toast('success', `Workflow переименован в '${n}'`);
      setEditingId(null);
      refetch();
    } catch {
      toast('error', errorMessage(rename.error ?? new Error('Failed to rename workflow')));
    }
  };

  const doRun = async (w: { id: number }) => {
    try {
      const res = await run.mutate(w.id);
      toast('success', 'Workflow запущен');
      navigate(`/workflows/${w.id}/instances/${res.instance.id}`);
    } catch {
      toast('error', errorMessage(run.error ?? new Error('Failed to run workflow')));
    }
  };
  const doToggle = async (w: { id: number; name: string; engine_enabled?: boolean }) => {
    const on = !w.engine_enabled;
    try {
      await toggleEngine.mutate({ id: w.id, name: w.name, on });
      toast('success', on ? 'Engine включён' : 'Engine выключен');
      refetch();
    } catch {
      toast('error', errorMessage(toggleEngine.error ?? new Error('Failed to update workflow')));
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    if (teamId == null) {
      toast('error', 'No active team to attach the workflow to');
      return;
    }
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
        <button className="btn btn-primary" onClick={openCreate}>
          + Новый workflow
        </button>
      </div>

      {loading && <PageSkeleton label="Loading workflows…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {data && data.workflows.length === 0 && (
        <EmptyState
          title="No workflows"
          hint="Create a workflow to orchestrate agent tasks."
          action={<button className="btn btn-primary" onClick={openCreate}>+ Новый workflow</button>}
        />
      )}
      {data && data.workflows.length > 0 && (
        <div className="card">
          <div className="table-wrap"><table className="table">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Team</th>
                <th scope="col">State</th>
                <th scope="col">Engine</th>
                <th scope="col">Updated</th>
              </tr>
            </thead>
            <tbody>
              {data.workflows.map((w) => (
                <tr key={w.id}>
                  <td>
                    {editingId === w.id ? (
                      <input
                        className="wf-rename"
                        value={editName}
                        onChange={(e) => setEditName(e.target.value)}
                        onKeyDown={(e) => { if (e.key === 'Enter') doRename(w.id); if (e.key === 'Escape') setEditingId(null); }}
                        onBlur={() => doRename(w.id)}
                        autoFocus
                        aria-label="Workflow name"
                      />
                    ) : (
                      <>
                        <Link className="team-link" to={`/workflows/${w.id}`}>{w.name}</Link>
                        <button className="btn-icon wf-rename-btn" title="Переименовать" aria-label={`Переименовать ${w.name}`} onClick={() => { setEditingId(w.id); setEditName(w.name); }}>✎</button>
                      </>
                    )}
                    <div className="muted small">{w.description}</div>
                  </td>
                  <td>team #{w.team_id}</td>
                  <td>
                    <Badge kind="entity" value={w.state} />
                  </td>
                  <td>
                    <label className="small" style={{ display: 'inline-flex', alignItems: 'center', gap: 6, marginRight: 12 }}>
                      <input
                        type="checkbox"
                        checked={!!w.engine_enabled}
                        onChange={() => doToggle(w)}
                        disabled={toggleEngine.pending}
                      />
                      run
                    </label>
                    {w.engine_enabled && (
                      <button className="btn btn-primary" style={{ padding: '2px 10px' }} disabled={run.pending} onClick={() => doRun(w)}>
                        {run.pending ? '…' : '▶ Run'}
                      </button>
                    )}
                  </td>
                  <td className="muted"><Time iso={w.updated_at}/></td>
                </tr>
              ))}
            </tbody>
          </table></div>
        </div>
      )}

      {creating && (
        <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={() => setCreating(false)} ref={wfCreateRef}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2>New workflow</h2>
            <form onSubmit={submit}>
              <label>
                Name *
                <input value={name} onChange={(e) => { setName(e.target.value); setNameTouched(true); }} placeholder={defaultName || 'name'} aria-label="Workflow name (create)" autoFocus />
              </label>
              <label>
                Team
                <select value={teamId ?? ''} onChange={(e) => setTeamId(Number(e.target.value))} data-testid="new-workflow-team-select">
                  {teamsQ.loading ? (
                    <option value="" disabled>Loading teams…</option>
                  ) : activeTeams.length === 0 ? (
                    <option value="" disabled>No active teams</option>
                  ) : (
                    activeTeams.map((t) => (
                      <option key={t.id} value={t.id}>{t.name} (#{t.id})</option>
                    ))
                  )}
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
