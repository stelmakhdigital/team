import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import ConfigPanel from '../src/components/TeamBuilder/ConfigPanel';
import { resetMockDb, getDb } from '../src/api/mock/data';

// T4 — Roles-менеджмент: (1) создание роли через форму, (2) полная конфигурация
// pi_config в ConfigPanel (max_tokens/thinking/mcp/startup.files) + персист override'а.
describe('T4: Roles-менеджмент', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('creates a role via the Add role form (name + agent_spec + segment)', async () => {
    const db = getDb();
    const before = db.roles.length;
    render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    // edit mode (palette with the form)
    const editBtn = await screen.findByRole('button', { name: /✎ Edit/ }, { timeout: 10_000 });
    fireEvent.click(editBtn);
    // fill the form
    const nameInput = await screen.findByLabelText('New role name', {}, { timeout: 10_000 });
    fireEvent.change(nameInput, { target: { value: 'qa-bot' } });
    fireEvent.change(screen.getByLabelText('New role agent spec'), { target: { value: 'pi-worker' } });
    fireEvent.change(screen.getByLabelText('New role target segment'), { target: { value: '1' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add role' }));
    // роль появляется в команде
    await waitFor(() => expect(db.roles.length).toBe(before + 1), { timeout: 10_000 });
    expect(db.roles.some((r) => r.name === 'qa-bot')).toBe(true);
  }, 20_000);

  it('ConfigPanel shows full pi_config fields (max_tokens/thinking/mcp/startup.files)', async () => {
    const db = getDb();
    const role = db.roles[0]; // Lead, id 1
    render(
      <ConfigPanel
        selection={{ type: 'role', id: role.id, name: role.name }}
        role={role}
        onClose={() => {}}
        onChanged={() => {}}
      />,
    );
    await screen.findByText('Pi config', {}, { timeout: 10_000 });
    expect(screen.getByText('Max tokens')).toBeInTheDocument();
    expect(screen.getByText('Thinking')).toBeInTheDocument();
    expect(screen.getByText('MCP servers')).toBeInTheDocument();
    expect(screen.getByText('Startup files')).toBeInTheDocument();
    // «исходный value из agent_spec» виден как hint
    expect(screen.getAllByText(/spec: 8192/).length).toBeGreaterThan(0);
  }, 20_000);

  it('ConfigPanel save persists the pi_config override to the role', async () => {
    const db = getDb();
    const role = db.roles[0];
    render(
      <ConfigPanel
        selection={{ type: 'role', id: role.id, name: role.name }}
        role={role}
        onClose={() => {}}
        onChanged={() => {}}
      />,
    );
    const maxInput = await screen.findByLabelText(/Max tokens/, {}, { timeout: 10_000 });
    fireEvent.change(maxInput, { target: { value: '4096' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    // override персистится в role.config
    await waitFor(() => expect(role.config?.max_tokens).toBe(4096), { timeout: 10_000 });
  }, 20_000);
});
