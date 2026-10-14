import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { Badge, ErrorState, PageSkeleton, errorMessage } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import EntityLink from '../components/ui/EntityLink';
import ProjectArtifacts from '../components/ProjectArtifacts';
import { formatRelative, formatCost } from '../lib/format';

export default function ProjectDetailPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const projectKey = parseInt(projectId ?? '', 10) || 0;

  const { data, loading, error, refetch } = useQuery('project.get', () => api.projects.get(projectKey), [projectKey]);
  const teamsQ = useQuery('project.teams', () => api.teams.getTeams(), []);
  const tasksQ = useQuery('project.tasks', () => api.tasks.list({ project_id: projectKey, limit: 200 }), [projectKey]);

  const { toast } = useToast();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  const update = useMutation((payload: { name?: string; description?: string }) =>
    api.projects.update(projectKey, payload),
  );
  const archive = useMutation(() => api.projects.archive(projectKey));

  const startEdit = () => {
    if (!data) return;
    setName(data.project.name);
    setDescription(data.project.description ?? '');
    setEditing(true);
    setFormError(null);
  };

  const saveEdit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    try {
      await update.mutate({ name: name.trim(), description: description.trim() });
      toast('success', 'Project updated');
      setEditing(false);
      refetch();
    } catch (err) {
      setFormError(errorMessage(err ?? new Error('Failed to update project')));
    }
  };

  const doArchive = async () => {
    try {
      await archive.mutate();
      toast('success', 'Project archived');
      refetch();
    } catch {
      toast('error', errorMessage(archive.error ?? new Error('Failed to archive project')));
    }
  };

  if (loading) {
    return (
      <div className="page">
        <PageSkeleton label="Loading project…" />
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

  const p = data.project;
  const relatedTeams = (teamsQ.data?.teams ?? []).filter((t) => t.project_id === projectKey);
  const relatedTasks = (tasksQ.data?.tasks ?? []).filter((t) => t.project_id === projectKey);

  return (
    <div className="page">
      <div className="page-head">
        <h1>
          <Link to="/projects" className="team-link">
            Projects
          </Link>{' '}
          / {p.name}
        </h1>
        {!p.archived && (
          <div className="page-head-actions">
            <button className="btn" onClick={startEdit}>
              Edit
            </button>
            <button className="btn" disabled={archive.pending} onClick={doArchive}>
              Archive
            </button>
          </div>
        )}
      </div>

      {editing && (
        <div className="card">
          <h2>Edit project</h2>
          <form onSubmit={saveEdit}>
            <label>
              Name *
              <input value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
            <label>
              Description
              <input value={description} onChange={(e) => setDescription(e.target.value)} />
            </label>
            <div className="muted small">Path is immutable after creation and cannot be changed here.</div>
            {formError && <div className="form-error">{formError}</div>}
            <div className="modal-actions">
              <button type="button" className="btn" onClick={() => setEditing(false)}>
                Cancel
              </button>
              <button type="submit" className="btn btn-primary" disabled={update.pending}>
                {update.pending ? 'Saving…' : 'Save'}
              </button>
            </div>
          </form>
        </div>
      )}

      <div className="card">
        <div className="table-wrap"><table className="detail-table">
          <tbody>
            <tr>
              <th>Name</th>
              <td>{p.name}</td>
            </tr>
            <tr>
              <th>Path</th>
              <td className="mono small">{p.path}</td>
            </tr>
            <tr>
              <th>Kind</th>
              <td>
                <Badge kind="entity" value={p.kind} /> {p.git && <Badge kind="type" value="git" />}
              </td>
            </tr>
            <tr>
              <th>State</th>
              <td>{p.archived ? <Badge kind="warn" value="archived" /> : <Badge kind="entity" value="active" />}</td>
            </tr>
            {p.description && (
              <tr>
                <th>Description</th>
                <td>{p.description}</td>
              </tr>
            )}
            <tr>
              <th>Counts</th>
              <td>
                {data.counts.teams} teams · {data.counts.tasks} tasks · {data.counts.active_sessions} active sessions
              </td>
            </tr>
            <tr>
              <th>Estimated cost</th>
              <td>{formatCost(data.estimated_cost)}</td>
            </tr>
            <tr>
              <th>Created / Updated</th>
              <td className="muted">
                {formatRelative(p.created_at)} / {formatRelative(p.updated_at)}
              </td>
            </tr>
          </tbody>
        </table></div>
      </div>

      <div className="card">
        <h2>Artifacts (файлы проекта)</h2>
        <p className="muted small">Файлы и артефакты, созданные в процессе работы над проектом (read-only, по каталогу проекта).</p>
        <ProjectArtifacts projectId={projectKey} root={p.path} />
      </div>

      <div className="card">
        <h2>Teams ({relatedTeams.length})</h2>
        {relatedTeams.length === 0 ? (
          <div className="muted">No teams bound to this project.</div>
        ) : (
          <ul className="entity-list">
            {relatedTeams.map((t) => (
              <li key={t.id}>
                <EntityLink to={`/teams/${t.id}`}>{t.name}</EntityLink>
                <span className="muted small"> · {t.state}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="card">
        <h2>Tasks ({relatedTasks.length})</h2>
        {relatedTasks.length === 0 ? (
          <div className="muted">В этом проекте пока нет задач · Задачи появятся, когда агент начнёт работу.</div>
        ) : (
          <ul className="entity-list">
            {relatedTasks.map((t) => (
              <li key={t.id}>
                <EntityLink to={`/tasks/${t.id}`}>{t.title}</EntityLink>
                <span className="muted small"> · {t.team_name} · {t.state}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
