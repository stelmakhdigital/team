import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';
import { setMockError } from '../src/api/mock/adapter';

// U4b: drill-down — EntityLink + task/session detail pages + audit/message drill-downs.
describe('U4b: drill-down', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });

  it('task detail page renders task + history timeline (fetch-by-id)', async () => {
    render(<MemoryRouter initialEntries={['/tasks/1']}><App /></MemoryRouter>);
    expect(await screen.findByRole('heading', { name: /^task #1$/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: /history/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText(/Implement auth middleware/)).toBeInTheDocument();
  }, 20_000);

  it('session detail page renders session metrics + transcript + history', async () => {
    render(<MemoryRouter initialEntries={['/sessions/1']}><App /></MemoryRouter>);
    expect(await screen.findByRole('heading', { name: 'Session #1' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: /transcript/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /history/i })).toBeInTheDocument();
  }, 20_000);

  it('dashboard task title is a link to the task detail (EntityLink)', async () => {
    render(<MemoryRouter initialEntries={['/']}><App /></MemoryRouter>);
    const link = await screen.findByRole('link', { name: 'Implement auth middleware' }, { timeout: 10_000 });
    expect(link).toHaveAttribute('href', '/tasks/1');
  }, 20_000);

  it('audit row expands to show operation details (actor + params)', async () => {
    render(<MemoryRouter initialEntries={['/history']}><App /></MemoryRouter>);
    const toggles = await screen.findAllByTitle('Show operation details', {}, { timeout: 10_000 });
    expect(toggles.length).toBeGreaterThan(0);
    toggles[0].click();
    expect(await screen.findByText('Operation details', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText(/Actor:/)).toBeInTheDocument();
  }, 20_000);
});
