import { afterEach, describe, expect, it } from 'vitest';
import { getApiConfig } from '../src/api/config';

function setEnv(name: string, value: string | undefined): void {
  const env = import.meta.env as Record<string, string | undefined>;
  if (value === undefined) delete env[name];
  else env[name] = value;
}

describe('api config', () => {
  afterEach(() => {
    setEnv('VITE_WS_URL', undefined);
    setEnv('VITE_API_KEY', undefined);
    setEnv('VITE_API_MODE', undefined);
    setEnv('VITE_API_BASE_URL', undefined);
  });

  it('wsUrl без ключа — как есть', () => {
    setEnv('VITE_WS_URL', 'ws://example/ws');
    expect(getApiConfig().wsUrl).toBe('ws://example/ws');
    expect(getApiConfig().apiKey).toBeUndefined();
  });

  it('VITE_API_KEY → ?api_key= в wsUrl (браузерный WS не шлёт заголовки)', () => {
    setEnv('VITE_WS_URL', 'ws://example/ws');
    setEnv('VITE_API_KEY', 'secret key');
    expect(getApiConfig().wsUrl).toBe('ws://example/ws?api_key=secret%20key');
    expect(getApiConfig().apiKey).toBe('secret key');
  });

  it('существующие query-параметры сохраняются (&api_key=)', () => {
    setEnv('VITE_WS_URL', 'ws://example/ws?probe=1');
    setEnv('VITE_API_KEY', 'k');
    expect(getApiConfig().wsUrl).toBe('ws://example/ws?probe=1&api_key=k');
  });

  it('mode: default mock, real по VITE_API_MODE', () => {
    expect(getApiConfig().mode).toBe('mock');
    setEnv('VITE_API_MODE', 'real');
    expect(getApiConfig().mode).toBe('real');
  });
});
