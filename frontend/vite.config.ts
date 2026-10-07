/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev-режим real mode: браузер ходит same-origin, Vite проксирует на backend.
// Для нестандартного адреса: BACKEND_URL=http://host:port npm run dev
const BACKEND = (globalThis as { process?: { env?: Record<string, string> } }).process?.env
  ?.BACKEND_URL ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Dev-режим real mode: браузер ходит same-origin (/api, /ws), Vite проксирует
    // на backend — CORS в dev не нужен. WS подключается нативно (ws: true).
    proxy: {
      '/api': { target: BACKEND, changeOrigin: true },
      '/healthz': { target: BACKEND, changeOrigin: true },
      '/ws': { target: BACKEND, changeOrigin: true, ws: true },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./tests/setup.ts'],
  },
});
