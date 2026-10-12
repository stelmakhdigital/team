import { useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { EmptyState, ErrorState, errorMessage, Spinner } from './ui/States';
import { formatRelative, formatBytes } from '../lib/format';
import type { ProjectFileEntry } from '../types/api';

interface Props {
  projectId: number;
  root?: string;
}

interface PreviewState {
  path: string;
  content: string;
  contentType: string;
  filename: string;
}

/** P4: Artifacts — файлы проекта (read-only): раскрываемое дерево (lazy),
 * превью + download. Список на один уровень за запрос (GET /projects/{id}/files[?path=]). */
export default function ProjectArtifacts({ projectId, root }: Props) {
  const { data, loading, error, refetch } = useQuery(`project.files.${projectId}`, () => api.projects.listFiles(projectId), [projectId]);
  const [preview, setPreview] = useState<PreviewState | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  // children[path] = записи подкаталога (после первого раскрытия); null = грузится
  const [children, setChildren] = useState<Map<string, ProjectFileEntry[] | null>>(() => new Map());

  const openPreview = async (path: string) => {
    setPreviewError(null);
    try {
      const res = await api.projects.getFile(projectId, path);
      setPreview({ path, content: res.content, contentType: res.contentType, filename: res.filename });
    } catch (e) {
      setPreviewError(errorMessage(e ?? new Error('Failed to open file')));
    }
  };

  const download = async (path: string, name: string) => {
    try {
      const res = await api.projects.getFile(projectId, path);
      const blob = new Blob([res.content], { type: res.contentType || 'application/octet-stream' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = name;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setPreviewError(errorMessage(e ?? new Error('Download failed')));
    }
  };

  const toggle = async (path: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
        return next;
      }
      next.add(path);
      return next;
    });
    // при раскрытии — lazy-запрос подкаталога (один раз)
    if (!children.has(path)) {
      setChildren((prev) => new Map(prev).set(path, null));
      try {
        const res = await api.projects.listFiles(projectId, path);
        setChildren((prev) => new Map(prev).set(path, res.files));
      } catch {
        setChildren((prev) => new Map(prev).set(path, []));
      }
    }
  };

  if (loading) return <div className="muted small">Loading files…</div>;
  if (error) return <ErrorState error={error} onRetry={refetch} />;
  if (!data) return null;

  const files = data.files ?? [];
  if (files.length === 0) {
    return <EmptyState title="No files" hint={root ? `Каталог ${root} пуст (или нет доступа).` : 'Каталог проекта пуст.'} />;
  }

  const renderRows = (entries: ProjectFileEntry[], depth: number): ReactNode[] => {
    const rows: ReactNode[] = [];
    for (const f of entries) {
      const isOpen = expanded.has(f.path);
      const kids = children.get(f.path);
      rows.push(
        <tr key={f.path} className={f.is_dir ? 'row-dir' : ''}>
          <td className="mono small cell-name" style={{ paddingLeft: 10 + depth * 18 }}>
            {f.is_dir ? (
              <button type="button" className="artifacts-toggle" onClick={() => toggle(f.path)} aria-expanded={isOpen} aria-label={isOpen ? 'Collapse' : 'Expand'}>
                {isOpen ? '▾' : '▸'}
              </button>
            ) : (
              <span className="artifacts-filedot" aria-hidden="true">
                ·
              </span>
            )}
            <span className={f.is_dir ? 'artifacts-dirname' : ''}>{f.name}</span>
          </td>
          <td className="col-size">{f.is_dir ? '—' : formatBytes(f.size)}</td>
          <td className="col-modified muted small">{formatRelative(f.modified_at)}</td>
          <td className="col-actions">
            {!f.is_dir && (
              <span className="row-actions">
                <button type="button" className="btn btn-sm" onClick={() => openPreview(f.path)}>
                  Preview
                </button>
                <button type="button" className="btn btn-sm" onClick={() => download(f.path, f.name)}>
                  Download
                </button>
              </span>
            )}
          </td>
        </tr>,
      );
      if (f.is_dir && isOpen) {
        if (kids === null || kids === undefined) {
          rows.push(
            <tr key={f.path + ':loading'} className="row-loading">
              <td colSpan={4} style={{ paddingLeft: 10 + (depth + 1) * 18 }} className="muted small">
                <Spinner label="Loading…" />
              </td>
            </tr>,
          );
        } else if (kids.length === 0) {
          rows.push(
            <tr key={f.path + ':empty'} className="row-loading">
              <td colSpan={4} style={{ paddingLeft: 10 + (depth + 1) * 18 }} className="muted small">
                (пусто)
              </td>
            </tr>,
          );
        } else {
          rows.push(...renderRows(kids, depth + 1));
        }
      }
    }
    return rows;
  };

  return (
    <>
      <div className="artifacts-toolbar">
        <span className="muted small mono" title="Каталог проекта">
          {data.root}
        </span>
        {data.truncated && <span className="hint hint-warn">… список обрезан (лимит записей)</span>}
        <button type="button" className="btn" onClick={() => refetch()}>
          ↻ Refresh
        </button>
      </div>
      <div className="artifacts-scroll">
        <table className="artifacts-table">
          <thead>
            <tr>
              <th>Name</th>
              <th className="col-size">Size</th>
              <th className="col-modified">Modified</th>
              <th className="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>{renderRows(files, 0)}</tbody>
        </table>
      </div>

      {preview && (
        <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={() => setPreview(null)}>
          <div className="modal artifacts-preview-modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-head">
              <h2 className="mono small">{preview.path}</h2>
              <button type="button" className="modal-close" onClick={() => setPreview(null)} aria-label="Close">
                ×
              </button>
            </div>
            <div className="modal-body">
              {previewError && <div className="form-error">{previewError}</div>}
              <pre className="artifacts-preview mono small">{preview.content}</pre>
            </div>
            <div className="modal-actions">
              <button type="button" className="btn" onClick={() => download(preview.path, preview.filename)}>
                ⬇ Download
              </button>
              <button type="button" className="btn" onClick={() => setPreview(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
