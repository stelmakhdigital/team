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
  // Default same-origin: в dev Vite-прокси шлёт /api и /ws на backend (см. vite.config.ts),
  // в production ожидается отдача SPA backend'ом.
  const baseUrl = env('VITE_API_BASE_URL') ?? '';
  const apiKey = env('VITE_API_KEY');
  const rawWsUrl =
    env('VITE_WS_URL') ??
    (typeof window !== 'undefined'
      ? `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/ws`
      : 'ws://localhost:8080/ws');
  // Браузерный WebSocket не шлёт заголовки: auth — только query `?api_key=` (backend WS-хендлер)
  const wsUrl = apiKey
    ? `${rawWsUrl}${rawWsUrl.includes('?') ? '&' : '?'}api_key=${encodeURIComponent(apiKey)}`
    : rawWsUrl;
  return {
    mode: mode === 'real' ? 'real' : 'mock',
    baseUrl,
    wsUrl,
    apiKey,
  };
}
