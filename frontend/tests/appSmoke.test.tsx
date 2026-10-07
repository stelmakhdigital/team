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

  it('renders Tasks page with mock data + lifecycle actions', async () => {
    render(
      <MemoryRouter initialEntries={['/tasks']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByText('Implement auth middleware', {}, { timeout: 10_000 })).toBeInTheDocument();
    // pending task exposes transition actions
    const startButtons = await screen.findAllByText('▶ start', {}, { timeout: 10_000 });
    expect(startButtons.length).toBeGreaterThan(0);
    // create modal renders destination-role select
    const newTask = await screen.findByRole('button', { name: /new task/i });
    newTask.click();
    expect(await screen.findByText('Destination role *', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
