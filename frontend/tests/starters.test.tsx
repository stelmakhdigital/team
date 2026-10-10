import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { createMockAdapter } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

// P2.13 — Starter bundles: UI (badge + grouping + one-click apply + no-edit).
// Mock db has one starter: id 1 "Standard Dev Team" (team, is_starter=true).
beforeEach(() => {
  resetMockDb();
});

describe('P2.13: starter bundles UI', () => {
  it('shows STARTER badge + Starters banner', async () => {
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    // STARTER badge (grid card)
    expect(await screen.findByText('STARTER', {}, { timeout: 10_000 })).toBeInTheDocument();
    // Starters banner
    expect(
      await screen.findByText('— ready-made bundles, apply in one click (not editable)'),
    ).toBeInTheDocument();
  }, 20_000);

  it('starter detail: rename disabled + project select + apply button', async () => {
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    const card = await screen.findByRole('button', { name: /standard dev team/i }, { timeout: 10_000 });
    fireEvent.click(card);
    // no-edit: rename control is the disabled lock
    const lock = await screen.findByRole('button', { name: /rename disabled \(starter\)/i });
    expect(lock).toBeDisabled();
    // team-starter: project select
    expect(await screen.findByLabelText('Apply starter to project')).toBeInTheDocument();
    // one-click apply
    expect(screen.getByRole('button', { name: /apply this starter/i })).toBeInTheDocument();
  }, 20_000);

  it('apply this starter creates a new team', async () => {
    const before = (await createMockAdapter().teams.getTeams()).teams.length;
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    const card = await screen.findByRole('button', { name: /standard dev team/i }, { timeout: 10_000 });
    fireEvent.click(card);
    const applyBtn = await screen.findByRole('button', { name: /apply this starter/i });
    fireEvent.click(applyBtn);
    await waitFor(
      async () => {
        const after = (await createMockAdapter().teams.getTeams()).teams.length;
        expect(after).toBeGreaterThan(before);
      },
      { timeout: 10_000 },
    );
  }, 20_000);

  it('non-starter item keeps an enabled rename control', async () => {
    // id 2 is a workflow, non-starter
    const api = createMockAdapter();
    const item = await api.library.getLibraryItem(2);
    expect(item.item.is_starter).toBeFalsy();
    render(
      <MemoryRouter initialEntries={['/library']}>
        <App />
      </MemoryRouter>,
    );
    const card = await screen.findByRole('button', { name: /build & review/i }, { timeout: 10_000 });
    fireEvent.click(card);
    // rename (✎) present + enabled
    const rename = await screen.findByRole('button', { name: /rename item/i });
    expect(rename).toBeEnabled();
  }, 20_000);
});
