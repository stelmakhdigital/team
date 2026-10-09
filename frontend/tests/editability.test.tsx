import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { createMockAdapter, setMockError } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

// U3: editability — team meta (name/description) inline edit form.
// Backed by saveTopology (Name/Description); the mock mirrors the backend persist.
describe('U3: team meta edit', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });
  it('saveTopology persists name/description (mock mirrors backend)', async () => {
    const api = createMockAdapter();
    const before = await api.teams.getTopology(1);
    expect(before.team.name).toBe('Dev Team');
    await api.teams.saveTopology(1, { name: 'Renamed Team', description: 'new desc' });
    const after = await api.teams.getTopology(1);
    expect(after.team.name).toBe('Renamed Team');
    expect(after.team.description).toBe('new desc');
  });

  it('opens the inline meta form with the current team name prefilled', async () => {
    render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByText('Lead', {}, { timeout: 10_000 });
    await (await screen.findByRole('button', { name: /✎ team/i })).click();
    const form = (await screen.findByRole('heading', { name: /team meta/i })).closest<HTMLElement>('.team-meta-edit')!;
    const nameInput = within(form).getByRole('textbox', { name: 'Name' }) as HTMLInputElement;
    expect(nameInput).toHaveValue('Dev Team');
    expect(within(form).getByRole('textbox', { name: 'Description' })).toBeInTheDocument();
    expect(within(form).getByRole('button', { name: /^save$/i })).toBeInTheDocument();
    expect(within(form).getByRole('button', { name: /cancel/i })).toBeInTheDocument();
  }, 20_000);
});
