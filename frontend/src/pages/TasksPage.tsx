import { useState } from 'react';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { Badge, EmptyState, ErrorState, errorMessage, Spinner } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatDateTime, formatRelative } from '../lib/format';
import { CLOSURE_REASONS, TASK_TRANSITIONS, isTerminalTaskState } from '../lib/task';
import type { ClosureReason, Task, TaskState } from '../types/api';

const STATE_FILTERS: (TaskState | 'all')[] = ['all', 'pending', 'in_progress', 'blocked', 'done', 'canceled'];

const ACTION_LABEL: Record<TaskState, string> = {
  in_progress: '▶ start',
  done: '✓ done',
  blocked: '⛔ block',
  canceled: '✕ cancel',
  pending: '↩ reopen',
};

export default function TasksPage() {
  const { toast } = useToast();
  const [teamFilter, setTeamFilter] = useState<number | 'all'>('all');
  const [stateFilter, setStateFilter] = useState<TaskState | 'all'>('all');
  const [creating, setCreating] = useState(false);
  const [closedId, setClosedId] = useState<number | null>(null);
  const [handoffId, setHandoffId] = useState<number | null>(null);
  const [expandedId, setExpandedId] = useState<number | null>(null);

  const teams = useQuery('tasks.teams', () => api.teams.getTeams());
  const list = useQuery(
    `tasks.list.${teamFilter}.${stateFilter}`,
    () =>
      api.tasks.list({
        team_id: teamFilter === 'all' ? undefined : teamFilter,
        state: stateFilter === 'all' ? undefined : stateFilter,
      }),
    [teamFilter, stateFilter],
  );

  const refetch = () => list.refetch();

  const act = (label: string, fn: () => Promise<unknown>) =>
    fn()
      .then(() => {
        toast('success', label);
        refetch();
      })
      .catch((e) => toast('error', e instanceof Error ? e.message : String(e)));

  return (
    <div className="page">
      <div className="page-head">
        <h1>Tasks</h1>
        <button className="btn btn-primary" onClick={() => setCreating(true)}>
          + New task
        </button>
      </div>

      <div className="filters">
        <label>
          Team
          <select
            value={String(teamFilter)}
            onChange={(e) => setTeamFilter(e.target.value === 'all' ? 'all' : Number(e.target.value))}
          >
            <option value="all">All teams</option>
            {teams.data?.teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          State
          <select value={stateFilter} onChange={(e) => setStateFilter(e.target.value as TaskState | 'all')}>
            {STATE_FILTERS.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>
      </div>

      {teams.loading && <Spinner label="Loading teams…" />}
      {list.loading && <Spinner label="Loading tasks…" />}
      {list.error && <ErrorState error={list.error} onRetry={refetch} />}
      {list.data && list.data.tasks.length === 0 && (
        <EmptyState title="No tasks" hint="Create a task to put it into a role's queue." />
      )}
      {list.data && list.data.tasks.length > 0 && (
        <div className="card">
          <div className="table-wrap"><table className="table">
            <thead>
              <tr>
                <th>#</th>
                <th>Title</th>
                <th>Team</th>
                <th>Role</th>
                <th>State</th>
                <th>Prio</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {list.data.tasks.map((t) => (
                <TaskRow
                  key={t.id}
                  task={t}
                  expanded={expandedId === t.id}
                  onToggle={() => setExpandedId(expandedId === t.id ? null : t.id)}
                  onAction={(to) =>
                    to === 'done'
                      ? setClosedId(t.id)
                      : act(`Task #${t.id} → ${to}`, () => api.tasks.updateState(t.id, { state: to }))
                  }
                  onHandoff={() => setHandoffId(t.id)}
                />
              ))}
            </tbody>
          </table></div>
        </div>
      )}

      {creating && (
        <CreateTaskModal
          teams={teams.data?.teams ?? []}
          onClose={() => setCreating(false)}
          onCreated={(id) => {
            setCreating(false);
            toast('success', `Task #${id} created`);
            refetch();
          }}
        />
      )}
      {closedId != null && (
        <CloseTaskModal
          taskId={closedId}
          onClose={() => setClosedId(null)}
          onDone={(reason) => {
            act(`Task #${closedId} → done (${reason})`, () => api.tasks.updateState(closedId, { state: 'done', closure_reason: reason }));
            setClosedId(null);
          }}
        />
      )}
      {handoffId != null && (
        <HandoffTaskModal
          taskId={handoffId}
          onClose={() => setHandoffId(null)}
          onDone={() => {
            setHandoffId(null);
            refetch();
          }}
        />
      )}
    </div>
  );
}

function TaskRow({
  task,
  expanded,
  onToggle,
  onAction,
  onHandoff,
}: {
  task: Task;
  expanded: boolean;
  onToggle: () => void;
  onAction: (to: TaskState) => void;
  onHandoff: () => void;
}) {
  const transitions = TASK_TRANSITIONS[task.state];
  return (
    <>
      <tr className={expanded ? 'row-expanded' : undefined}>
        <td className="muted">
          <button className="row-toggle" onClick={onToggle} aria-expanded={expanded} aria-label={`Task ${task.id} details`}>
            {expanded ? '▾' : '▸'} {task.id}
          </button>
        </td>
        <td>
          {task.title}
          {task.body && <div className="muted small">{task.body}</div>}
          {task.closure_reason && <div className="muted small">closure: {task.closure_reason}</div>}
        </td>
        <td className="muted">{task.team_name}</td>
        <td>
          {task.destination_role_name}
          {task.source_role_name && <div className="muted small">from {task.source_role_name}</div>}
        </td>
        <td>
          <Badge kind="task" value={task.state}>
            {task.state}
            {task.is_stale ? ' · stale' : ''}
          </Badge>
        </td>
        <td className="muted">{task.priority}</td>
        <td className="muted">{formatRelative(task.updated_at)}</td>
        <td>
          <div className="row-actions">
            {transitions.map((to) => (
              <button key={to} className="btn btn-mini" onClick={() => onAction(to)}>
                {ACTION_LABEL[to]}
              </button>
            ))}
            {!isTerminalTaskState(task.state) && (
              <button className="btn btn-mini" onClick={onHandoff}>
                ⇄ handoff
              </button>
            )}
          </div>
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={8} className="row-detail">
            <TaskDetail taskId={task.id} />
          </td>
        </tr>
      )}
    </>
  );
}

function TaskDetail({ taskId }: { taskId: number }) {
  const detail = useQuery(`tasks.detail.${taskId}`, () => api.tasks.get(taskId));
  const history = useQuery(`tasks.history.${taskId}`, () => api.history.getTaskHistory(taskId));

  if (detail.loading || history.loading) return <Spinner label="Loading details…" />;
  if (detail.error || history.error) {
    return <ErrorState error={detail.error ?? history.error!} onRetry={() => {
      detail.refetch();
      history.refetch();
    }} />;
  }

  return (
    <div className="detail-grid">
      <div>
        <h4>History</h4>
        {history.data!.history.length === 0 && <div className="muted">No history yet</div>}
        <ol className="timeline">
          {history.data!.history.map((h) => (
            <li key={h.id} className={`timeline-item tl-${h.color ?? 'blue'}`}>
              <span className="timeline-icon" aria-hidden="true">
                {h.icon ?? '•'}
              </span>
              <div>
                <strong>
                  {h.from_state ? `${h.from_state} → ${h.to_state}` : h.to_state}
                  {h.closure_reason ? ` (${h.closure_reason})` : ''}
                </strong>{' '}
                <span className="muted small">
                  by {h.actor_type} · {formatDateTime(h.created_at)}
                </span>
                {h.comment && <div className="muted">{h.comment}</div>}
              </div>
            </li>
          ))}
        </ol>
      </div>
      <div>
        <h4>Subtasks ({detail.data!.subtasks.length})</h4>
        {detail.data!.subtasks.length === 0 && <div className="muted">No subtasks</div>}
        <ul className="subtask-list">
          {detail.data!.subtasks.map((s) => (
            <li key={s.id}>
              <Badge kind="task" value={s.state} /> {s.title}
              <span className="muted small"> → {s.destination_role_name}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function CreateTaskModal({
  teams,
  onClose,
  onCreated,
}: {
  teams: { id: number; name: string }[];
  onClose: () => void;
  onCreated: (id: number) => void;
}) {
  const [teamId, setTeamId] = useState(teams[0]?.id ?? 1);
  const [roleId, setRoleId] = useState<number | ''>('');
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [priority, setPriority] = useState(3);

  const roles = useQuery(`tasks.create.roles.${teamId}`, () => api.teams.getTopology(teamId), [teamId]);

  const create = useMutation((req: Parameters<typeof api.tasks.create>[0]) => api.tasks.create(req));
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim() || roleId === '') return;
    try {
      const res = await create.mutate({ team_id: teamId, destination_role_id: Number(roleId), title: title.trim(), body, priority });
      onCreated(res.id);
    } catch (err) {
      alert(errorMessage(err instanceof Error ? err : new Error('Failed to create task')));
    }
  };

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>New task</h2>
        <form onSubmit={submit}>
          <label>
            Team *
            <select value={teamId} onChange={(e) => { setTeamId(Number(e.target.value)); setRoleId(''); }}>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Destination role *
            <select value={String(roleId)} onChange={(e) => setRoleId(e.target.value ? Number(e.target.value) : '')}>
              <option value="">— select role —</option>
              {roles.data?.roles.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.segment_name}/{r.name}
                </option>
              ))}
            </select>
            {roles.loading && <span className="muted small">Loading roles…</span>}
          </label>
          <label>
            Title *
            <input value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
          </label>
          <label>
            Body
            <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={3} />
          </label>
          <label>
            Priority (0–9)
            <input type="number" min={0} max={9} value={priority} onChange={(e) => setPriority(Number(e.target.value))} />
          </label>
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={create.pending || roleId === '' || !title.trim()}>
              {create.pending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function CloseTaskModal({
  taskId,
  onClose,
  onDone,
}: {
  taskId: number;
  onClose: () => void;
  onDone: (reason: ClosureReason) => void;
}) {
  const [reason, setReason] = useState<ClosureReason>('no_follow_on');
  const [comment, setComment] = useState('');
  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>Close task #{taskId}</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            onDone(reason);
          }}
        >
          <label>
            Closure reason *
            <select value={reason} onChange={(e) => setReason(e.target.value as ClosureReason)}>
              {CLOSURE_REASONS.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </label>
          <label>
            Comment
            <input value={comment} onChange={(e) => setComment(e.target.value)} />
          </label>
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary">
              Close as done
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function HandoffTaskModal({
  taskId,
  onClose,
  onDone,
}: {
  taskId: number;
  onClose: () => void;
  onDone: () => void;
}) {
  const detail = useQuery(`tasks.handoff.${taskId}`, () => api.tasks.get(taskId));
  const teamId = detail.data?.task.team_id;
  const topology = useQuery(
    teamId != null ? `tasks.handoff.roles.${teamId}` : 'tasks.handoff.roles.none',
    teamId != null ? () => api.teams.getTopology(teamId) : async () => ({ team: undefined as never, segments: [], roles: [], relatives: [] }) as never,
    [teamId],
  );
  const [roleId, setRoleId] = useState<number | ''>('');
  const [comment, setComment] = useState('');
  const { toast } = useToast();

  const handoff = useMutation((req: { to_role_id: number; comment?: string }) => api.tasks.handoff(taskId, req));
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (roleId === '') return;
    try {
      const res = await handoff.mutate({ to_role_id: Number(roleId), comment: comment || undefined });
      toast('success', `Task #${taskId} handed off → new task #${res.new_task_id}`);
      onDone();
    } catch (err) {
      toast('error', err instanceof Error ? err.message : String(err));
    }
  };

  if (detail.loading || (teamId != null && topology.loading)) return <Spinner label="Loading task…" />;
  if (detail.error) return <ErrorState error={detail.error} onRetry={detail.refetch} />;

  const currentRole = detail.data?.task.destination_role_id;
  const roles = (topology.data?.roles ?? []).filter((r) => r.id !== currentRole);

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>Hand off task #{taskId}</h2>
        <form onSubmit={submit}>
          <label>
            To role *
            <select value={String(roleId)} onChange={(e) => setRoleId(e.target.value ? Number(e.target.value) : '')}>
              <option value="">— select role —</option>
              {roles.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.segment_name}/{r.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Comment
            <input value={comment} onChange={(e) => setComment(e.target.value)} />
          </label>
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={handoff.pending || roleId === ''}>
              {handoff.pending ? 'Handing off…' : 'Hand off'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
