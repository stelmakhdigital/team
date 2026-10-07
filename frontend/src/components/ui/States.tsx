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

export function errorMessage(err: unknown): string {
  if (err instanceof ApiClientError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}
