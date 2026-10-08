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

  it('renders Team Builder topology graph (R1: React Flow + role cards)', async () => {
    render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    // role-карточки рендерятся в React Flow-канвасе
    expect(await screen.findByText('Lead', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('Worker', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getAllByText('Reviewer').length).toBeGreaterThan(0);
    // segment-фрейм с count
    const segHead = await screen.findByText(/Backend/, {}, { timeout: 10_000 });
    expect(segHead).toBeInTheDocument();
    // live-слоты метрик (R4 пока «--»)
    expect(screen.getAllByText('ctx --%').length).toBeGreaterThan(0);
    expect(screen.getAllByText('tok --').length).toBeGreaterThan(0);
    // activity-dot для running-сессии
    expect(document.querySelector('.activity-dot.act-running')).not.toBeNull();
  });

  it('Team Builder edit mode: toggle reveals palette, nodes become draggable', async () => {
    render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByText('Lead', {}, { timeout: 10_000 });
    // до edit: палитры нет
    expect(screen.queryByLabelText('Builder palette')).toBeNull();
    // включаем edit
    (await screen.findByRole('button', { name: /edit/i })).click();
    expect(await screen.findByLabelText('Builder palette', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /done/i })).toBeInTheDocument();
  });

  it('Team Builder YAML panel: export + valid spec + create/merge buttons', async () => {
    render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByText('Lead', {}, { timeout: 10_000 });
    await (await screen.findByRole('button', { name: /yaml/i })).click();
    const ta = await screen.findByRole('textbox', { name: 'Team YAML spec' });
    const val = (ta as HTMLTextAreaElement).value;
    expect(val).toContain('name: Dev Team');
    expect(val).toContain('segments:');
    expect(screen.getByText(/✓ valid YAML/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create team from yaml/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /merge into team/i })).toBeInTheDocument();
  }, 15_000);

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

  it('renders Library with save-to-library + apply controls (team item)', async () => {
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Library' }, { timeout: 10_000 })).toBeInTheDocument();
    // save-row: team-выбор + кнопка
    expect(await screen.findByRole('button', { name: /save team to library/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByLabelText('Team to save', {}, { timeout: 10_000 })).toBeInTheDocument();
    // открыть item (team) → Apply-секция
    const card = await screen.findByText('Standard Dev Team', {}, { timeout: 10_000 });
    card.closest('button')!.click();
    expect(await screen.findByRole('button', { name: /apply as new team/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByLabelText('Merge into team', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Merge' })).toBeInTheDocument();
    expect(screen.getByText('Versions')).toBeInTheDocument();
  }, 20_000);

  it('renders Library workflow item → apply to team select', async () => {
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    const card = await screen.findByText('Build & Review', {}, { timeout: 10_000 });
    card.closest('button')!.click();
    expect(await screen.findByLabelText('Apply workflow to team', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Apply' })).toBeInTheDocument();
  }, 20_000);

  it('Team Builder Table view: roles with state/session/runtime', async () => {
    render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    await screen.findByText('Lead', {}, { timeout: 10_000 });
    await (await screen.findByRole('tab', { name: 'Table' })).click();
    const table = await screen.findByRole('table', { name: 'Topology table' });
    expect(table).toBeInTheDocument();
    expect(screen.getAllByText('pi-go-backend').length).toBeGreaterThan(0);
    expect(screen.getAllByText('running').length).toBeGreaterThan(0);
  }, 15_000);
});
