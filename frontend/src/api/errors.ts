/** Normalized API error. Code is a stable machine-readable code
 * (e.g. "not_found", "conflict", "unauthorized", "validation", "server"). */
export class ApiClientError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: unknown;

  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message);
    this.name = 'ApiClientError';
    this.status = status;
    this.code = code;
    this.details = details;
  }

  get isNetwork(): boolean {
    return this.status === 0;
  }
}

/** Map an HTTP status to a stable code (error model TBD — blockers #2). */
export function codeFromStatus(status: number): string {
  switch (status) {
    case 0:
      return 'network';
    case 400:
      return 'bad_request';
    case 401:
      return 'unauthorized';
    case 403:
      return 'forbidden';
    case 404:
      return 'not_found';
    case 409:
      return 'conflict';
    case 422:
      return 'validation';
    default:
      return status >= 500 ? 'server' : 'error';
  }
}

/** Flex-parse an error body of unknown shape into { message, code, details }.
 * Accepts { error: { code, message } }, { code, message }, { message }, plain text. */
export function parseErrorBody(body: unknown, status: number): { message: string; code: string; details?: unknown } {
  const fallback = codeFromStatus(status);
  if (body == null) return { message: `Request failed (${status})`, code: fallback };
  if (typeof body === 'string') return { message: body, code: fallback };
  if (typeof body === 'object') {
    const obj = body as Record<string, unknown>;
    const err = (typeof obj.error === 'object' && obj.error !== null ? obj.error : obj) as Record<string, unknown>;
    const message =
      typeof err.message === 'string'
        ? err.message
        : typeof obj.message === 'string'
          ? (obj.message as string)
          : `Request failed (${status})`;
    const code = typeof err.code === 'string' ? err.code : fallback;
    const details = err.details ?? obj.details;
    return { message, code, details };
  }
  return { message: `Request failed (${status})`, code: fallback };
}
