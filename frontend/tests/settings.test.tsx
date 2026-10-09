import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { createMockAdapter, setMockError } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

// U4: Settings / Configuration — mock config behavior + page render.
describe('U4: config API (mock)', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });

  it('getConfig returns 7 sections / 20 keys + meta + environment, with layered sources', async () => {
    const api = createMockAdapter();
    const res = await api.config.getConfig();
    expect(res.sections.map((s) => s.name)).toEqual(['server', 'storage', 'sessions', 'watchdog', 'security', 'logging', 'workspace']);
    expect(res.sections.reduce((n, s) => n + s.keys.length, 0)).toBe(20);
    expect(res.meta.project).toBe('team');
    expect(res.meta.config_hash).toMatch(/^sha256:/);
    // layered sources are demonstrable
    const listen = res.sections.find((s) => s.name === 'server')!.keys.find((k) => k.key === 'server.listen_addr')!;
    expect(listen.source).toBe('env');
    expect(listen.source_detail).toBe('DAEMON_LISTEN_ADDR');
    expect(listen.current_value).toBe('127.0.0.1:8080');
    const dsn = res.sections.find((s) => s.name === 'storage')!.keys.find((k) => k.key === 'storage.dsn')!;
    expect(dsn.source).toBe('file');
    const root = res.sections.find((s) => s.name === 'workspace')!.keys.find((k) => k.key === 'workspace.root')!;
    expect(root.default_is_derived).toBe(true);
    // environment provenance
    const envListen = res.environment.find((e) => e.env_var === 'DAEMON_LISTEN_ADDR')!;
    expect(envListen.source_active).toBe(true);
    expect(envListen.feeds_key).toBe('server.listen_addr');
  });

  it('setConfig sets an override (needs_restart) and resetConfig reverts to the next layer', async () => {
    const api = createMockAdapter();
    const set = await api.config.setConfig('logging.level', 'warn');
    expect(set.source).toBe('override');
    expect(set.needs_restart).toBe(true);
    let res = await api.config.getConfig();
    let level = res.sections.find((s) => s.name === 'logging')!.keys.find((k) => k.key === 'logging.level')!;
    expect(level.source).toBe('override');
    expect(level.current_value).toBe('warn');
    const reset = await api.config.resetConfig('logging.level');
    expect(reset.source).toBe('env'); // mock env has DAEMON_LOG_LEVEL=debug
    res = await api.config.getConfig();
    level = res.sections.find((s) => s.name === 'logging')!.keys.find((k) => k.key === 'logging.level')!;
    expect(level.source).toBe('env');
    expect(level.current_value).toBe('debug');
  });

  it('setConfig: type validation 400, read-only 403, unknown key 404', async () => {
    const api = createMockAdapter();
    await expect(api.config.setConfig('storage.migrate', 'not-a-bool')).rejects.toMatchObject({ status: 400 });
    await expect(api.config.setConfig('security.secret_key', 'x')).rejects.toMatchObject({ status: 403 });
    await expect(api.config.setConfig('nope.nope', 'x')).rejects.toMatchObject({ status: 404 });
  });
});

describe('U4: Settings page', () => {
  it('renders Meta, Workspace, Environment and the registry with edit/readonly controls', async () => {
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Settings' }, { timeout: 10_000 })).toBeInTheDocument();
    // sections
    expect(await screen.findByRole('heading', { name: /^workspace$/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /environment/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /meta/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'server' })).toBeInTheDocument();
    // a workspace key row is editable (has an edit control)
    const ws = screen.getByRole('heading', { name: /^workspace$/i }).closest<HTMLElement>('.cfg-section')!;
    expect(within(ws).getAllByRole('button').length).toBeGreaterThan(0);
    // environment provenance lists DAEMON_LISTEN_ADDR (env + source detail)
    expect(screen.getAllByText('DAEMON_LISTEN_ADDR').length).toBeGreaterThan(0);
    // a read-only secret key shows the lock (no edit button) in the security section
    const sec = screen.getByRole('heading', { name: 'security' }).closest<HTMLElement>('.cfg-section')!;
    expect(within(sec).getAllByText('🔒').length).toBeGreaterThan(0);
  }, 20_000);

  it('M2: renders the default value per key (host-derived marked as derived)', async () => {
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Settings' }, { timeout: 10_000 })).toBeInTheDocument();
    // each key shows a "default:" line
    const defaults = await screen.findAllByText(/default:/, {}, { timeout: 10_000 });
    expect(defaults.length).toBeGreaterThanOrEqual(20);
    // server.listen_addr default = :8080
    expect(screen.getAllByText(/default: :8080/).length).toBeGreaterThan(0);
    // workspace.* keys are host-derived → their default lines carry a (derived) marker
    const ws = screen.getByRole('heading', { name: /^workspace$/i }).closest<HTMLElement>('.cfg-section')!;
    expect(within(ws).getAllByText(/\(derived\)/).length).toBeGreaterThan(0);
  }, 20_000);
});
