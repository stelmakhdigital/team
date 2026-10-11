import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { createMockAdapter } from '../src/api/mock/adapter';
import { resetMockDb } from '../src/api/mock/data';

// P2.14: cost tracking — Settings (цены/бюджет), Dashboard (chart), detail views (honest «нет данных»).
describe('P2.14: cost tracking (frontend)', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('config exposes a cost section: model_prices (json_object) + budget integer', async () => {
    const api = createMockAdapter();
    const res = await api.config.getConfig();
    const cost = res.sections.find((s) => s.name === 'cost');
    expect(cost).toBeTruthy();
    const prices = cost!.keys.find((k) => k.key === 'cost.model_prices')!;
    expect(prices.json_object).toBe(true);
    const budget = cost!.keys.find((k) => k.key === 'cost.budget_alert_cents')!;
    expect(budget.type).toBe('integer');
    expect(budget.json_object).toBeFalsy();
  });

  it('session detail shows cost (estimate) from estimated_cost', async () => {
    render(<MemoryRouter initialEntries={['/sessions/1']}><App /></MemoryRouter>);
    expect(await screen.findByRole('heading', { name: 'Session #1' }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('~$7.52', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('task detail shows cost (estimate) when data present, «нет данных» otherwise', async () => {
    render(<MemoryRouter initialEntries={['/tasks/1']}><App /></MemoryRouter>);
    expect(await screen.findByRole('heading', { name: /task #1/i }, { timeout: 10_000 })).toBeInTheDocument();
    expect(await screen.findByText('~$12.34', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('task detail without cost data shows «нет данных» (honest, not 0)', async () => {
    render(<MemoryRouter initialEntries={['/tasks/2']}><App /></MemoryRouter>);
    expect(await screen.findByRole('heading', { name: /task #2/i }, { timeout: 10_000 })).toBeInTheDocument();
    // task 2 has no estimated_cost → «нет данных»
    expect(await screen.findByText('нет данных', {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('dashboard metrics include an LLM cost (USD) series', async () => {
    render(<MemoryRouter initialEntries={['/']}><App /></MemoryRouter>);
    // T3: label «LLM cost» + единицы «USD» отдельно (значения + min/max + единицы)
    expect(await screen.findByText('LLM cost', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getAllByText('USD').length).toBeGreaterThan(0);
    // старая серия LLM tokens осталась
    expect(screen.getByText('LLM tokens')).toBeInTheDocument();
  }, 20_000);
});
