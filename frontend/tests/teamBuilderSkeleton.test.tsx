import { describe, expect, it, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { setMockError } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

declare global {
  interface Window {
    __MOCK_LATENCY_MS?: number;
  }
}

// P1: Team Builder initial load must use the unified PageSkeleton (X2 pattern),
// not a bare full-page <Spinner>. Hold the initial topology fetch in flight via
// the mock latency override so the loading state is observable.
describe('P1: Team Builder initial load → PageSkeleton', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });
  afterEach(() => {
    window.__MOCK_LATENCY_MS = undefined;
  });

  it('renders the page-level skeleton (header + rows) while topology loads, with no bare full-page spinner', async () => {
    window.__MOCK_LATENCY_MS = 5000; // keep getTopology pending so the skeleton is observable
    const { container, unmount } = render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    const sk = await screen.findByRole('status', { name: /topology/i }, { timeout: 5000 });
    expect(sk).toHaveClass('page-skeleton');
    expect(container.querySelector('.sk-head')).toBeInTheDocument();
    expect(container.querySelectorAll('.sk-row').length).toBeGreaterThan(0);
    // the unified skeleton replaces a bare full-page spinner
    expect(container.querySelector('.spinner-wrap')).not.toBeInTheDocument();
    unmount();
  }, 20000);
});
