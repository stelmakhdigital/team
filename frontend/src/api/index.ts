import { getApiConfig } from './config';
import type { Api } from './types';
import { createMockAdapter } from './mock/adapter';
import { createRealAdapter } from './real';

let instance: Api | null = null;

/** Single API facade. Mock or real depending on VITE_API_MODE. */
export function getApi(): Api {
  if (!instance) {
    instance = getApiConfig().mode === 'real' ? createRealAdapter() : createMockAdapter();
  }
  return instance;
}

export const api = new Proxy({} as Api, {
  get: (_t, prop: keyof Api) => getApi()[prop],
});
