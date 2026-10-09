import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { errorMessage } from '../src/components/ui/States';
import { ApiClientError } from '../src/api/errors';

// X3: human-readable, code-specific feedback + a global real-time status
// badge (auto-reconnect) present on every page via the AppShell.
describe('X3: specific error messages + global WS status', () => {
  it('errorMessage maps API codes to specifics (409/403/404), passes through plain errors', () => {
    expect(errorMessage(new ApiClientError(409, 'conflict', 'raw'))).toMatch(/conflict/i);
    expect(errorMessage(new ApiClientError(403, 'forbidden', 'raw'))).toMatch(/not permitted|forbidden/i);
    expect(errorMessage(new ApiClientError(404, 'not_found', 'raw'))).toMatch(/not found/i);
    expect(errorMessage(new ApiClientError(0, 'network', 'raw'))).toMatch(/network/i);
    expect(errorMessage(new Error('boom'))).toBe('boom');
  });

  it('AppShell renders a global real-time status badge (visible on every page)', async () => {
    render(
      <MemoryRouter initialEntries={['/tasks']}>
        <App />
      </MemoryRouter>,
    );
    // mock mode → the shell badge reads "live · mock"
    expect(await screen.findByText(/live.*mock/i, {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
