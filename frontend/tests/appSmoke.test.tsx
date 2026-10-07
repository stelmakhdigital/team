import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';

// Smoke test: full app (routes + mock API + dashboard) renders without crashing.
describe('App smoke', () => {
  it('renders Dashboard with mock data', async () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByText('Dashboard', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Active tasks', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Agent sessions', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Watchdog alerts', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Metrics', {}, { timeout: 10_000 })).toBeInTheDocument();
  });

  it('renders Teams list with mock data', async () => {
    render(
      <MemoryRouter initialEntries={['/teams']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByText('Dev Team', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Platform Team', {}, { timeout: 10_000 })).toBeInTheDocument();
  });
});
