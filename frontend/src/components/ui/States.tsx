import type { ReactNode } from 'react';
import { ApiClientError } from '../../api/errors';

export function Spinner({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="spinner-wrap" role="status">
      <div className="spinner" aria-hidden="true" />
      <span className="spinner-label">{label}</span>
    </div>
  );
}

export function EmptyState({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <div className="empty-icon" aria-hidden="true">◌</div>
      <h3>{title}</h3>
      {hint && <p>{hint}</p>}
      {action}
    </div>
  );
}

/** Neutral state for a panel whose backend endpoint is not implemented yet
 * (list/detail GET → 404). Shown instead of a scary error; retrying a 404
 * is pointless, so no Retry button. */
export function Unavailable({ feature, hint }: { feature: string; hint?: string }) {
  return (
    <div className="empty-state unavailable" aria-live="polite">
      <div className="empty-icon" aria-hidden="true">▦</div>
      <h3>{feature}: not available yet</h3>
      <p>{hint ?? 'The backend endpoint for this feature is not implemented yet. It will appear here once the backend ships it.'}</p>
    </div>
  );
}

/** True when a query error is a 404 (resource/endpoint absent). */
export function isNotFoundError(error: ErrorInfo | null | undefined): boolean {
  return !!error && error.status === 404;
}

export interface ErrorInfo {
  status: number;
  code: string;
  message: string;
}

function friendlyMessage(code: string, message: string): { title: string; hint?: string } {
  switch (code) {
    case 'unauthorized':
      return { title: 'No access', hint: 'Your session or API key is not valid. Check credentials and retry.' };
    case 'forbidden':
      return { title: 'Forbidden', hint: 'You do not have permission to perform this action.' };
    case 'not_found':
      return { title: 'Not found', hint: 'The requested resource does not exist or was removed.' };
    case 'conflict':
      return { title: 'Conflict', hint: 'The data changed on the server. Refresh and try again.' };
    case 'validation':
    case 'validation_failed':
      return { title: 'Validation error', hint: message };
    case 'network':
      return { title: 'Network error', hint: 'Cannot reach the server. Check connectivity and retry.' };
    default:
      return { title: 'Something went wrong', hint: message };
  }
}

export function ErrorState({ error, onRetry }: { error: ErrorInfo; onRetry?: () => void }) {
  const f = friendlyMessage(error.code, error.message);
  return (
    <div className="error-state" role="alert">
      <div className="error-icon" aria-hidden="true">⚠</div>
      <h3>{f.title}</h3>
      {f.hint && <p>{f.hint}</p>}
      {onRetry && (
        <button className="btn btn-primary" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  );
}

/** Единый status-badge (R5-рефакторинг: раньше 5 разных паттернов
 * `badge badge-…` расписаны по 10 местам, 2 из них битые — без backticks).
 *
 * kind:
 *  - 'task'   — задачи/сессии/подзадачи: badge-{pending|in_progress|blocked|done|canceled|running|stopped|failed|sent}
 *  - 'entity' — команды/воркфлоу/роли:   badge-state-{active|stopped|archived|inactive|blocked}
 *  - 'sev'    — алерты:                  badge-sev-{critical|high|medium|low|info}
 *  - 'type'   — библиотека:              badge-type-{team|workflow|role|segment}
 *  - 'warn'   — предупреждение:          badge-warn
 */
export function Badge({ kind, value, children, title }: {
  kind: 'task' | 'entity' | 'sev' | 'type' | 'warn';
  value?: string;
  children?: ReactNode;
  title?: string;
}) {
  const cls =
    kind === 'task' ? `badge badge-${value}` :
    kind === 'entity' ? `badge badge-state-${value}` :
    kind === 'sev' ? `badge badge-sev-${value ?? 'info'}` :
    kind === 'type' ? `badge badge-type-${value}` :
    'badge badge-warn';
  const shown = children ?? (value ?? (kind === 'sev' ? 'info' : ''));
  return (
    <span className={cls} title={title}>
      {shown}
    </span>
  );
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiClientError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}
