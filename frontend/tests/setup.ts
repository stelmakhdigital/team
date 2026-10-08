import '@testing-library/jest-dom/vitest';

// jsdom: ResizeObserver отсутствует — React Flow (@xyflow/react) использует его.
if (typeof globalThis.ResizeObserver === 'undefined') {
  class ResizeObserverPolyfill {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverPolyfill as unknown as typeof ResizeObserver;
}
