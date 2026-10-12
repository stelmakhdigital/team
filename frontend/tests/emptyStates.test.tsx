import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// X1: unified empty states — every list/panel shows a meaningful EmptyState
// (what the data is, how to get it; CTA where the action is natural) when the
// data source is empty. We clear the seeded mock DB to exercise this path.
function clearLists() {
  resetMockDb();
  const db = getDb();
  db.tasks = [];
  db.sessions = [];
  db.alerts = [];
  db.library = [];
  db.workflows = [];
}

describe('X1: empty states (mock with empty data)', () => {
  beforeEach(() => clearLists());

  it('Dashboard: tasks / sessions / alerts panels show empty states', async () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Dashboard' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('No active tasks', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Сессий пока нет', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('No alerts', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('Tasks: empty list shows a CTA to create', async () => {
    render(
      <MemoryRouter initialEntries={['/tasks']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Tasks' }, { timeout: 10_000 })).toBeInTheDocument();
    const empty = (await screen.findByText('Нет задач', {}, { timeout: 10_000 })).closest<HTMLElement>('.empty-state')!;
    expect(within(empty).getByRole('button', { name: /новая задача/i })).toBeInTheDocument();
  }, 20_000);

  it('Workflows: empty list shows a CTA to create', async () => {
    render(
      <MemoryRouter initialEntries={['/workflows']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Workflows' }, { timeout: 10_000 })).toBeInTheDocument();
    const empty = (await screen.findByText('No workflows', {}, { timeout: 10_000 })).closest<HTMLElement>('.empty-state')!;
    expect(within(empty).getByRole('button', { name: /новый workflow/i })).toBeInTheDocument();
  }, 20_000);

  it('Library: empty list shows a meaningful empty state', async () => {
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Library' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Ничего не найдено', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);
});
