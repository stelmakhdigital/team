import { getApiConfig } from './config';
import { ApiClientError, parseErrorBody } from './errors';

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
}

/** Minimal fetch wrapper over the REST API (real mode). */
export async function http<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const cfg = getApiConfig();
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  // Auth: X-API-Key (lead-решение, _workspace/blockers.md #9; backend: DAEMON_API_KEYS)
  if (cfg.apiKey) headers['X-API-Key'] = cfg.apiKey;

  let res: Response;
  try {
    res = await fetch(`${cfg.baseUrl}${path}`, {
      method: opts.method ?? (opts.body !== undefined ? 'POST' : 'GET'),
      headers,
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: opts.signal,
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e;
    throw new ApiClientError(0, 'network', 'Network error — check backend connectivity');
  }

  const text = await res.text();
  let json: unknown = null;
  if (text) {
    try {
      json = JSON.parse(text);
    } catch {
      json = text;
    }
  }

  if (!res.ok) {
    const parsed = parseErrorBody(json, res.status);
    throw new ApiClientError(res.status, parsed.code, parsed.message, parsed.details);
  }
  return json as T;
}

export function toQuery(params: Record<string, string | number | undefined>): string {
  const parts: string[] = [];
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') parts.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`);
  }
  return parts.length ? `?${parts.join('&')}` : '';
}
