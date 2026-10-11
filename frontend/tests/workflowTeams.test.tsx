import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';
import type { Team } from '../src/types/api';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

// Wait for a team-name link to /teams/{id} to appear in the rendered DOM (task rows load async).
async function expectTeamLink(container: HTMLElement, teamId: number, name: string) {
  await waitFor(
    () => {
      const links = Array.from(container.querySelectorAll(`a[href="/teams/${teamId}"]`));
      expect(links.some((a) => a.textContent?.includes(name))).toBe(true);
    },
    { timeout: 10_000 },
  );
}

// QA batch 1 (T-20261011185655): [P0] New workflow team-селект — реальные active
// команды (не захардкоженные #1/#2) + [P] Team-ссылки (Tasks + Dashboard).
describe('[P0] New workflow — team select (real active teams)', () => {
  beforeEach(() => resetMockDb());

  it('shows all active teams incl. user-created, not hardcoded "Team 1"/"Team 2"', async () => {
    // user-created active team (id 3) must appear in the select
    const now = new Date().toISOString();
    getDb().teams.push({
      id: 3, name: 'QA Test Team', description: 'created in test', state: 'active',
      segments_count: 0, roles_count: 0, created_at: now, updated_at: now,
    } as Team);

    renderAt('/workflows');
    await screen.findByRole('heading', { name: 'Workflows' }, { timeout: 10_000 });
    // open the "New workflow" modal
    fireEvent.click(await screen.findByRole('button', { name: /New workflow/ }, { timeout: 10_000 }));
    // wait for the teams to actually load into the select (was "Loading teams…" first)
    await screen.findByText('Dev Team (#1)', {}, { timeout: 10_000 });

    const select = screen.getByTestId('new-workflow-team-select') as HTMLSelectElement;
    const options = Array.from(select.querySelectorAll('option')).map((o) => o.textContent ?? '');
    // real team names (was hardcoded "Team 1"/"Team 2")
    expect(options).toContain('Dev Team (#1)');
    expect(options).toContain('Platform Team (#2)');
    // user-created team is included
    expect(options).toContain('QA Test Team (#3)');
    // old hardcoded labels are gone
    expect(options).not.toContain('Team 1');
    expect(options).not.toContain('Team 2');
    // default selection = first active team (#1)
    expect(select.value).toBe('1');
  }, 20_000);
});

describe('[P] Team name is a link in Tasks + Dashboard Active tasks', () => {
  beforeEach(() => resetMockDb());

  it('Tasks: team cell links to /teams/{id}', async () => {
    const { container } = renderAt('/tasks');
    await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 });
    await expectTeamLink(container, 1, 'Dev Team');
  }, 20_000);

  it('Dashboard Active tasks: team cell links to /teams/{id}', async () => {
    const { container } = renderAt('/');
    await screen.findByText('Active tasks', {}, { timeout: 10_000 });
    await expectTeamLink(container, 1, 'Dev Team');
  }, 20_000);
});
