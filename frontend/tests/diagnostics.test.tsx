import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P1.8: Health/Doctor-панель — список чек'ов (status + detail) + run + divergence.

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P1.8: Health/Doctor panel', () => {
  beforeEach(() => resetMockDb());

  it('shows the checks (name + status) + overall', async () => {
    renderAt('/diagnostics');
    expect(await screen.findByRole('heading', { name: 'Health / Doctor' }, { timeout: 15_000 })).toBeInTheDocument();
    // overall banner (mock: fail — есть расхождение)
    expect(await screen.findByText(/Требуют внимания/, {}, { timeout: 15_000 })).toBeInTheDocument();
    // чек'и: name + status
    expect(screen.getByText('sessions.divergence')).toBeInTheDocument();
    expect(screen.getByText('database.ping')).toBeInTheDocument();
    expect(screen.getByText('daemon')).toBeInTheDocument();
    // расхождение: stale-сессия в detail
    expect(screen.getByText(/active in DB but not alive in adapter/)).toBeInTheDocument();
  }, 30_000);

  it('has a «Run full diagnostics» button that re-runs (read-only)', async () => {
    renderAt('/diagnostics');
    expect(await screen.findByRole('heading', { name: 'Health / Doctor' }, { timeout: 15_000 })).toBeInTheDocument();
    const runBtn = await screen.findByRole('button', { name: /Run full diagnostics/ }, { timeout: 15_000 });
    fireEvent.click(runBtn);
    // после run — всё ещё чек'и (read-only, результат обновлён)
    expect(await screen.findByText('sessions.divergence', {}, { timeout: 15_000 })).toBeInTheDocument();
    expect(within(document.body).getByText(/Требуют внимания/)).toBeInTheDocument();
  }, 30_000);

  it('nav has a Health link', async () => {
    renderAt('/');
    const link = await screen.findByRole('link', { name: /Health/ }, { timeout: 15_000 });
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '/diagnostics');
  }, 30_000);

  it('drill-down: EntityLink на сессию-расхождение (stale) + zombie', async () => {
    renderAt('/diagnostics');
    expect(await screen.findByRole('heading', { name: 'Health / Doctor' }, { timeout: 15_000 })).toBeInTheDocument();
    // drill-down: ссылка на stale-сессию #5 (→ /sessions/5)
    const staleLink = await screen.findByRole('link', { name: /Сессия #5/ }, { timeout: 15_000 });
    expect(staleLink).toHaveAttribute('href', '/sessions/5');
    // zombie-ссылка на #8 (→ /sessions/8)
    expect(screen.getByRole('link', { name: /Сессия #8/ })).toHaveAttribute('href', '/sessions/8');
  }, 30_000);
});
