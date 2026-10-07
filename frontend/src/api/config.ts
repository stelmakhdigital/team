export type ApiMode = 'mock' | 'real';

export interface ApiConfig {
  mode: ApiMode;
  baseUrl: string;
  wsUrl: string;
  apiKey?: string;
}

function env(name: string): string | undefined {
  if (typeof import.meta !== 'undefined' && import.meta.env) {
    const v = import.meta.env[name];
    if (typeof v === 'string' && v.length > 0) return v;
  }
  return undefined;
}

export function getApiConfig(): ApiConfig {
  const mode = (env('VITE_API_MODE') ?? 'mock') as ApiMode;
  return {
    mode: mode === 'real' ? 'real' : 'mock',
    baseUrl: env('VITE_API_BASE_URL') ?? 'http://localhost:8080',
    wsUrl: env('VITE_WS_URL') ?? 'ws://localhost:8080/ws',
    apiKey: env('VITE_API_KEY'),
  };
}
