import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';
import type { Session } from '../src/types/api';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

const iso = (minsAgo: number) => new Date(Date.now() - minsAgo * 60_000).toISOString();

function mkSession(id: number, startedAt: string, role: string): Session {
  return {
    id,
    team_id: 1,
    team_name: 'Dev Team',
    role_id: 1,
    role_name: role,
    runtime_type: 'pi',
    state: 'stopped',
    started_at: startedAt,
  };
}

// B-1: сортировка списков по свежести (новые сверху) + pagination («Show more»).
describe('B-1.1: Sessions — sort by recency (newest first)', () => {
  beforeEach(() => resetMockDb());

  it('newest session renders first, oldest last', async () => {
    const db = getDb();
    db.sessions = [
      mkSession(1, iso(300), 'OldRole'), // oldest
      mkSession(2, iso(10), 'NewestRole'), // newest
      mkSession(3, iso(100), 'MidRole'), // mid
    ];
    renderAt('/sessions');
    await screen.findByRole('heading', { name: 'Sessions' }, { timeout: 10_000 });
    const rows = await screen.findAllByRole('row', {}, { timeout: 10_000 });
    // rows[0] = header, rows[1..3] = data (sorted newest→oldest)
    const data = rows.slice(1).map((r) => r.textContent ?? '');
    expect(data[0]).toContain('NewestRole');
    expect(data[1]).toContain('MidRole');
    expect(data[2]).toContain('OldRole');
  }, 20_000);

  it('shows a «Show more» button when there are > 25 sessions', async () => {
    const db = getDb();
    db.sessions = Array.from({ length: 30 }, (_, i) => mkSession(i + 1, iso(i), `Role${i}`));
    renderAt('/sessions');
    await screen.findByRole('heading', { name: 'Sessions' }, { timeout: 10_000 });
    const btn = await screen.findByRole('button', { name: /Показать ещё/ }, { timeout: 10_000 });
    expect(btn).toBeInTheDocument();
    expect(btn.textContent).toMatch(/5 осталось/); // 30 - 25
  }, 20_000);
});

describe('B-1.2: Teams — sort by recency (newest first)', () => {
  beforeEach(() => resetMockDb());

  it('newest team renders first', async () => {
    const db = getDb();
    db.teams = [
      { id: 1, name: 'OldTeam', state: 'active', segments_count: 0, roles_count: 0, created_at: iso(500), updated_at: iso(500) },
      { id: 2, name: 'NewTeam', state: 'active', segments_count: 0, roles_count: 0, created_at: iso(20), updated_at: iso(20) },
      { id: 3, name: 'MidTeam', state: 'active', segments_count: 0, roles_count: 0, created_at: iso(200), updated_at: iso(200) },
    ];
    renderAt('/teams');
    await screen.findByRole('heading', { name: 'Teams' }, { timeout: 10_000 });
    const rows = await screen.findAllByRole('row', {}, { timeout: 10_000 });
    const data = rows.slice(1).map((r) => r.textContent ?? '');
    expect(data[0]).toContain('NewTeam');
    expect(data[1]).toContain('MidTeam');
    expect(data[2]).toContain('OldTeam');
  }, 20_000);
});
