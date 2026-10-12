import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { createMockAdapter } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('B-4: team archive/delete — mock adapter', () => {
  beforeEach(() => resetMockDb());

  it('setTeamState archives, filter excludes, restore brings back', async () => {
    const api = createMockAdapter();
    await api.teams.setTeamState(1, 'archived');
    const archived = await api.teams.getTeams({ state: 'archived' });
    expect(archived.teams.map((t) => t.id)).toContain(1);
    const active = await api.teams.getTeams({ state: 'active' });
    expect(active.teams.map((t) => t.id)).not.toContain(1);
    await api.teams.setTeamState(1, 'active');
    const active2 = await api.teams.getTeams({ state: 'active' });
    expect(active2.teams.map((t) => t.id)).toContain(1);
  });

  it('setTeamState rejects an invalid state', async () => {
    const api = createMockAdapter();
    await expect(api.teams.setTeamState(1, 'bogus')).rejects.toMatchObject({ status: 400 });
  });

  it('deleteTeam(hard) removes the team', async () => {
    const api = createMockAdapter();
    await api.teams.deleteTeam(2, true);
    const all = await api.teams.getTeams();
    expect(all.teams.map((t) => t.id)).not.toContain(2);
    // second delete → 404
    await expect(api.teams.deleteTeam(2, true)).rejects.toMatchObject({ status: 404 });
  });
});

describe('B-4: Teams page — Show archived toggle + Actions', () => {
  beforeEach(() => resetMockDb());

  it('archived team hidden by default, shown after toggle; Actions column present', async () => {
    const api = createMockAdapter();
    await api.teams.setTeamState(2, 'archived'); // Platform Team
    renderAt('/teams');
    await screen.findByRole('heading', { name: 'Teams' }, { timeout: 10_000 });
    expect(await screen.findByRole('columnheader', { name: 'Actions' }, { timeout: 10_000 })).toBeInTheDocument();
    const checkbox = screen.getByLabelText('Show archived');
    expect(checkbox).toBeInTheDocument();
    // archived team hidden by default
    expect(screen.queryByText('Platform Team')).toBeNull();
    // toggle → appears
    fireEvent.click(checkbox);
    expect(await screen.findByText('Platform Team', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('active team row has Archive + Delete actions', async () => {
    renderAt('/teams');
    await screen.findByRole('heading', { name: 'Teams' }, { timeout: 10_000 });
    expect((await screen.findAllByRole('button', { name: 'Archive' }, { timeout: 10_000 })).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByRole('button', { name: 'Delete' }).length).toBeGreaterThanOrEqual(1);
  }, 20_000);
});
