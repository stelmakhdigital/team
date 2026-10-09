import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { createMockAdapter, setMockError } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

// U3-rename (round 2): PATCH segment/workflow/library — mock persist + UI affordances.
describe('U3-rename: mock persist', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });

  it('updateSegment persists name/description (mock mirrors backend)', async () => {
    const api = createMockAdapter();
    const seg = await api.teams.createSegment(1, { name: 'backend' });
    const res = await api.teams.updateSegment(seg.id, { name: 'platform', description: 'core' });
    expect(res).toMatchObject({ id: seg.id, name: 'platform', status: 'updated' });
    const topo = await api.teams.getTopology(1);
    const renamed = topo.segments.find((s) => s.id === seg.id)!;
    expect(renamed.name).toBe('platform');
    expect(renamed.description).toBe('core');
  });

  it('updateSegment: 404 unknown, 400 empty name, 409 duplicate', async () => {
    const api = createMockAdapter();
    const seg = await api.teams.createSegment(1, { name: 'backend' });
    await api.teams.createSegment(1, { name: 'other' });
    await expect(api.teams.updateSegment(999999, { name: 'x' })).rejects.toMatchObject({ status: 404 });
    await expect(api.teams.updateSegment(seg.id, { name: '   ' })).rejects.toMatchObject({ status: 400 });
    await expect(api.teams.updateSegment(seg.id, { name: 'other' })).rejects.toMatchObject({ status: 409 });
  });

  it('updateWorkflow persists name (mock mirrors backend)', async () => {
    const api = createMockAdapter();
    const wf = await api.workflows.createWorkflow({ team_id: 1, name: 'orig' });
    const res = await api.workflows.updateWorkflow(wf.id, { name: 'renamed' });
    expect(res).toMatchObject({ id: wf.id, name: 'renamed', status: 'updated' });
    const got = await api.workflows.getWorkflow(wf.id);
    expect(got.workflow.name).toBe('renamed');
  });

  it('updateLibraryItem persists name (mock mirrors backend)', async () => {
    const api = createMockAdapter();
    const item = await api.library.saveToLibrary({ type: 'team', source_id: 1, name: 'lib-orig' });
    const res = await api.library.updateLibraryItem(item.id, { name: 'lib-renamed' });
    expect(res).toMatchObject({ id: item.id, name: 'lib-renamed', status: 'updated' });
    const got = await api.library.getLibraryItem(item.id);
    expect(got.item.name).toBe('lib-renamed');
  });
});

describe('U3-rename: UI affordances', () => {
  beforeEach(() => {
    resetMockDb();
    setMockError(null);
  });

  it('workflow editor exposes a rename control that prefills the name', async () => {
    const api = createMockAdapter();
    const wf = await api.workflows.createWorkflow({ team_id: 1, name: 'My Pipeline' });
    render(
      <MemoryRouter initialEntries={['/workflows/' + wf.id]}>
        <App />
      </MemoryRouter>,
    );
    const btn = await screen.findByRole('button', { name: /✎ rename/i }, { timeout: 10_000 });
    await btn.click();
    const input = await screen.findByRole('textbox', { name: 'Workflow name' });
    expect(input).toHaveValue('My Pipeline');
  }, 20_000);
});
