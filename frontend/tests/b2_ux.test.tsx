import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';
import type { Session, Chatroom } from '../src/types/api';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

const iso = (minsAgo: number) => new Date(Date.now() - minsAgo * 60_000).toISOString();
function mkSession(id: number, state: Session['state']): Session {
  return {
    id,
    team_id: 1,
    team_name: 'Dev Team',
    role_id: 1,
    role_name: 'Worker',
    runtime_type: 'pi',
    state,
    started_at: iso(10),
  };
}
function mkRoom(id: number, name: string): Chatroom {
  return { id, team_id: 1, name, unread_count: 0, members_count: 2 };
}

describe('B-2.1: Dashboard «N failed» → /sessions?state=failed', () => {
  beforeEach(() => resetMockDb());

  it('failed count renders as a link to /sessions?state=failed', async () => {
    const db = getDb();
    db.sessions = [mkSession(1, 'failed'), mkSession(2, 'running')];
    renderAt('/');
    const link = await screen.findByRole('link', { name: '1 failed' }, { timeout: 10_000 });
    expect(link).toHaveAttribute('href', '/sessions?state=failed');
  }, 20_000);
});

describe('B-2.2: Sessions ?state=failed pre-filters the list', () => {
  beforeEach(() => resetMockDb());

  it('state filter preset to failed; only failed rows shown', async () => {
    const db = getDb();
    db.sessions = [mkSession(1, 'failed'), mkSession(2, 'running'), mkSession(3, 'failed')];
    renderAt('/sessions?state=failed');
    await screen.findByRole('heading', { name: 'Sessions' }, { timeout: 10_000 });
    const stateSelect = screen.getByLabelText(/State \(filter\)/);
    expect(stateSelect).toHaveValue('failed');
    const rows = await screen.findAllByRole('row', {}, { timeout: 10_000 });
    expect(rows.length - 1).toBe(2); // header + 2 failed (running excluded)
  }, 20_000);
});

describe('B-2.3: Rooms search filter', () => {
  beforeEach(() => resetMockDb());

  it('typing in the search input filters rooms by name', async () => {
    const db = getDb();
    db.chatrooms = [mkRoom(1, 'alpha-room'), mkRoom(2, 'beta-room'), mkRoom(3, 'alpha-deep')];
    renderAt('/messages');
    await screen.findByRole('heading', { name: 'Message Center' }, { timeout: 10_000 });
    const search = await screen.findByLabelText('Search rooms (filter)', {}, { timeout: 10_000 });
    expect(await screen.findByText('alpha-room', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('beta-room', {}, { timeout: 10_000 })).toBeInTheDocument();
    fireEvent.change(search, { target: { value: 'alpha' } });
    expect(screen.getByText('alpha-room')).toBeInTheDocument();
    expect(screen.getByText('alpha-deep')).toBeInTheDocument();
    expect(screen.queryByText('beta-room')).toBeNull();
  }, 20_000);
});
