import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb, getDb } from '../src/api/mock/data';
import { stateTitle } from '../src/lib/format';
import type { Session } from '../src/types/api';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

const iso = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
function mkSession(id: number, state: Session['state']): Session {
  return {
    id,
    team_id: 1,
    team_name: 'Dev Team',
    role_id: 1,
    role_name: 'Worker',
    runtime_type: 'pi',
    state,
    started_at: iso(5),
  };
}

describe('B-3: state badge tooltips', () => {
  beforeEach(() => resetMockDb());

  it('stateTitle returns descriptions for known states, undefined for unknown', () => {
    expect(stateTitle('running')).toMatch(/actively working/);
    expect(stateTitle('failed')).toMatch(/transcript/);
    expect(stateTitle('pending')).toMatch(/picked up/);
    expect(stateTitle('active')).toMatch(/Active/);
    expect(stateTitle('some-unknown-state')).toBeUndefined();
    expect(stateTitle(undefined)).toBeUndefined();
  });

  it('session state badge carries a title tooltip', async () => {
    const db = getDb();
    db.sessions = [mkSession(1, 'failed')];
    const { container } = renderAt('/sessions');
    await screen.findByRole('heading', { name: 'Sessions' }, { timeout: 10_000 });
    await screen.findAllByText('Worker', {}, { timeout: 10_000 });
    const badge = container.querySelector('table tbody .badge');
    expect(badge).not.toBeNull();
    expect(badge).toHaveAttribute('title');
    expect(badge!.getAttribute('title')).toMatch(/transcript/);
  }, 20_000);
});
