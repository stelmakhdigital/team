import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { Badge, ErrorState, PageSkeleton, errorMessage } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import EntityLink from '../components/ui/EntityLink';
import { formatRelative } from '../lib/format';

const stateKind: Record<string, 'entity' | 'warn'> = {
  running: 'entity',
  active: 'entity',
  done: 'entity',
  completed: 'entity',
  skipped: 'entity',
  routed: 'entity',
  pending: 'entity',
  aborted: 'entity',
  waiting: 'warn',
  failed: 'warn',
};

export default function WorkflowInstancePage() {
  const { workflowId, instanceId } = useParams<{ workflowId: string; instanceId: string }>();
  const wid = parseInt(workflowId ?? '', 10) || 0;
  const iid = parseInt(instanceId ?? '', 10) || 0;

  const { data, loading, error, refetch } = useQuery('instance.get', () => api.workflows.getWorkflowInstance(iid), [iid]);
  const wfQ = useQuery('instance.wf', () => api.workflows.getWorkflow(wid), [wid]);
  const teamId = wfQ.data?.workflow?.team_id;
  const teamQ = useQuery('instance.team', () => (teamId ? api.teams.getTeam(teamId) : Promise.resolve(null)), [teamId]);

  const { toast } = useToast();
  const [routeRole, setRouteRole] = useState(0);
  const resume = useMutation(() => api.workflows.resumeWorkflowInstance(iid));
  const abort = useMutation(() => api.workflows.abortWorkflowInstance(iid));
  const route = useMutation((rid: number) => api.workflows.routeWorkflowInstance(iid, rid));

  const doResume = async () => {
    try {
      await resume.mutate();
      toast('success', 'Instance resumed');
      refetch();
    } catch {
      toast('error', errorMessage(resume.error ?? new Error('Failed to resume')));
    }
  };
  const doAbort = async () => {
    if (!window.confirm('Abort инстанс? Все active задачи будут canceled.')) return;
    try {
      await abort.mutate();
      toast('success', 'Instance aborted');
      refetch();
    } catch {
      toast('error', errorMessage(abort.error ?? new Error('Failed to abort')));
    }
  };
  const doRoute = async () => {
    if (!routeRole) return;
    try {
      await route.mutate(routeRole);
      toast('success', 'Step routed');
      setRouteRole(0);
      refetch();
    } catch {
      toast('error', errorMessage(route.error ?? new Error('Failed to route')));
    }
  };

  if (loading) {
    return (
      <div className="page">
        <PageSkeleton label="Loading instance…" />
      </div>
    );
  }
  if (error) {
    return (
      <div className="page">
        <ErrorState error={error} onRetry={refetch} />
      </div>
    );
  }
  if (!data) return null;

  const inst = data.instance;
  const wfName = wfQ.data?.workflow?.name ?? `#${wid}`;
  const steps = [...data.steps];
  const activeIds = data.active_step_ids ?? [];
  const isTerminal = inst.state === 'completed' || inst.state === 'aborted' || inst.state === 'failed';

  return (
    <div className="page">
      <div className="page-head">
        <h1>
          <Link to={`/workflows/${wid}`} className="team-link">
            {wfName}
          </Link>{' '}
          / Instance #{inst.id}
        </h1>
        <div className="page-head-actions">
          <Badge kind={stateKind[inst.state] ?? 'muted'} value={inst.state} />
          {inst.state === 'failed' && (
            <button className="btn btn-primary" disabled={resume.pending} onClick={doResume}>
              {resume.pending ? 'Resuming…' : 'Resume'}
            </button>
          )}
          {(inst.state === 'running' || inst.state === 'waiting') && (
            <button className="btn" disabled={abort.pending} onClick={doAbort}>
              {abort.pending ? 'Aborting…' : 'Abort'}
            </button>
          )}
        </div>
      </div>

      {inst.root_objective && (
        <div className="card">
          <span className="muted small">Root objective</span>
          <p className="small">{inst.root_objective}</p>
        </div>
      )}

      {/* Route active step */}
      {(inst.state === 'running' || inst.state === 'waiting') && activeIds.length > 0 && (
        <div className="card">
          <h2>Route active step</h2>
          <div className="form-row" style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
            <select value={routeRole} onChange={(e) => setRouteRole(parseInt(e.target.value, 10))}>
              <option value={0}>Select target role…</option>
              {(teamQ.data?.roles ?? []).map((r: { id: number; name: string }) => (
                <option key={r.id} value={r.id}>
                  {r.name}
                </option>
              ))}
            </select>
            <button className="btn" disabled={!routeRole || route.pending} onClick={doRoute}>
              {route.pending ? 'Routing…' : 'Route'}
            </button>
            <span className="muted small">Переназначает active-шаг (задачу) на другую роль.</span>
          </div>
        </div>
      )}

      {/* Trail */}
      <div className="card">
        <h2>Trail ({steps.length})</h2>
        {steps.length === 0 ? (
          <div className="muted">No steps yet.</div>
        ) : (
          <div className="table-wrap"><table className="detail-table">
            <thead>
              <tr>
                <th>#</th>
                <th>Block</th>
                <th>Type</th>
                <th>Task</th>
                <th>State</th>
                <th>Exit</th>
                <th>Actor</th>
                <th>Exited</th>
              </tr>
            </thead>
            <tbody>
              {steps.map((s, i) => {
                const active = activeIds.includes(s.id);
                return (
                  <tr key={s.id} style={active ? { fontWeight: 600 } : undefined}>
                    <td className="muted">{i + 1}</td>
                    <td>
                      {s.block_label || `#${s.block_id}`}
                      {active && <span className="muted small"> (current)</span>}
                    </td>
                    <td>
                      <Badge kind="type" value={s.block_type} />
                    </td>
                    <td>
                      {s.task_id ? <EntityLink to={`/tasks/${s.task_id}`}>{s.task_title || `#${s.task_id}`}</EntityLink> : '—'}
                    </td>
                    <td>
                      <Badge kind={stateKind[s.state] ?? 'muted'} value={s.state} />
                    </td>
                    <td className="muted small">{s.exit_kind ?? '—'}</td>
                    <td className="muted small">{s.actor_name ?? '—'}</td>
                    <td className="muted small">{s.exited_at ? formatRelative(s.exited_at) : '—'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table></div>
        )}
        {isTerminal && (
          <div className="muted small" style={{ marginTop: 12 }}>
            {inst.state === 'completed' && 'Workflow завершён успешно.'}
            {inst.state === 'aborted' && 'Workflow отменён (active задачи — canceled).'}
            {inst.state === 'failed' && 'Workflow упал — используйте Resume, чтобы повторить failed-шаг.'}
          </div>
        )}
      </div>
    </div>
  );
}
