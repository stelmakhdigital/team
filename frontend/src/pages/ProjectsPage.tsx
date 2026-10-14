import { useState, useEffect, useRef } from 'react';
import { Time } from '../components/ui/Time';
import { Link, useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { useFocusTrap } from '../hooks/useFocusTrap';
import { useEscapeStack } from '../hooks/useEscapeStack';
import { Badge, EmptyState, ErrorState, PageSkeleton, errorMessage } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import {} from '../lib/format';
import type { ProjectKind } from '../types/api';

export default function ProjectsPage() {
  const [showArchived, setShowArchived] = useState(false);
  const [creating, setCreating] = useState(false);
  // W2.4: ?new=1 — открыть форму создания (CTA из empty-state ContextTree)
  const [searchParams] = useSearchParams();
  useEffect(() => {
    if (searchParams.get('new') === '1') setCreating(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const { data, loading, error, refetch } = useQuery(
    'projects.list',
    () => api.projects.list({ include_archived: showArchived || undefined }),
    [showArchived],
  );
  const { toast } = useToast();
  const archive = useMutation((id: number) => api.projects.archive(id));

  const doArchive = async (id: number, name: string) => {
    try {
      await archive.mutate(id);
      toast('success', `Project '${name}' archived`);
      refetch();
    } catch {
      toast('error', errorMessage(archive.error ?? new Error('Failed to archive project')));
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <h1>Projects</h1>
        <div className="page-head-actions">
          <label className="check">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
            show archived
          </label>
          <button className="btn btn-primary" onClick={() => setCreating(true)}>
            + Новый проект
          </button>
        </div>
      </div>

      {loading && <PageSkeleton label="Loading projects…" />}
      {error && <ErrorState error={error} onRetry={refetch} />}
      {data && data.projects.length === 0 && (
        <EmptyState
          title={showArchived ? 'Нет проектов' : 'Нет активных проектов'}
          hint="Проект — рабочая директория, над которой работают команды, задачи и сессии. Создайте первый проект."
          action={<button className="btn btn-primary" onClick={() => setCreating(true)}>+ Новый проект</button>}
        />
      )}
      {data && data.projects.length > 0 && (
        <div className="card">
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Path</th>
                  <th>Kind</th>
                  <th>Git</th>
                  <th>State</th>
                  <th>Updated</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {data.projects.map((p) => (
                  <tr key={p.id} className={p.archived ? 'row-muted' : undefined}>
                    <td>
                      <Link className="team-link" to={`/projects/${p.id}`}>
                        {p.name}
                      </Link>
                      {p.description && <div className="muted small">{p.description}</div>}
                    </td>
                    <td className="mono small">{p.path}</td>
                    <td>
                      <Badge kind="entity" value={p.kind} />
                    </td>
                    <td>{p.git ? <Badge kind="type" value="git" /> : <span className="muted">—</span>}</td>
                    <td>{p.archived ? <Badge kind="warn" value="archived" /> : <Badge kind="entity" value="active" />}</td>
                    <td className="muted"><Time iso={p.updated_at}/></td>
                    <td>
                      {!p.archived && (
                        <button className="btn btn-small" disabled={archive.pending} onClick={() => doArchive(p.id, p.name)}>
                          Archive
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {creating && (
        <CreateProjectModal
          onClose={() => setCreating(false)}
          onDone={() => {
            setCreating(false);
            refetch();
          }}
        />
      )}
    </div>
  );
}

function CreateProjectModal({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const { toast } = useToast();
  useEscapeStack(onClose, true);
  const [kind, setKind] = useState<ProjectKind>('existing');
  const [name, setName] = useState('');
  const [path, setPath] = useState('');
  const [parentPath, setParentPath] = useState('');
  const [description, setDescription] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  const create = useMutation(
    (payload: { name: string; kind: ProjectKind; path?: string; parent_path?: string; description?: string }) =>
      api.projects.create(payload),
  );

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    try {
      const res = await create.mutate({
        name: name.trim(),
        kind,
        path: kind === 'existing' ? path.trim() : undefined,
        parent_path: kind === 'new' ? parentPath.trim() : undefined,
        description: description.trim() || undefined,
      });
      toast('success', `Project '${name.trim()}' created`);
      if (res.warning) toast('info', res.warning);
      onDone();
    } catch (err) {
      setFormError(errorMessage(err ?? new Error('Failed to create project')));
    }
  };

  const backdropRef = useRef<HTMLDivElement>(null);
  useFocusTrap(backdropRef, true);

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose} ref={backdropRef}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>New project</h2>
        <form onSubmit={submit}>
          <label>
            Kind
            <select value={kind} onChange={(e) => setKind(e.target.value as ProjectKind)}>
              <option value="existing">existing — an existing directory</option>
              <option value="new">new — create a directory (+ git init)</option>
            </select>
          </label>
          <label>
            Name *
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-project" required />
          </label>
          {kind === 'existing' ? (
            <label>
              Path *
              <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/abs/path/to/dir" required />
            </label>
          ) : (
            <label>
              Parent path *
              <input value={parentPath} onChange={(e) => setParentPath(e.target.value)} placeholder="/abs/parent/dir" required />
            </label>
          )}
          <label>
            Description
            <input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="optional" />
          </label>
          {formError && <div className="form-error">{formError}</div>}
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="btn btn-primary" disabled={create.pending}>
              {create.pending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
