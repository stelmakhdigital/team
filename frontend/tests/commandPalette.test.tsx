import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P2.10: Command palette (Cmd+K) — открытие, закрытие Esc, навигация Enter, разделы.
const PH = /type a command or search/;

function renderApp(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}
async function openPalette() {
  await screen.findByRole('navigation', { name: 'Main' }, { timeout: 10_000 });
  fireEvent.keyDown(document.body, { key: 'k', metaKey: true });
  return await screen.findByPlaceholderText(PH, {}, { timeout: 5000 });
}

describe('P2.10: Command palette (Cmd+K)', () => {
  beforeEach(() => resetMockDb());

  it('opens on Cmd+K and closes on Esc', async () => {
    renderApp('/');
    const input = await openPalette();
    expect(input).toBeInTheDocument();
    // разделы видны при пустом запросе
    expect(await screen.findByText('Страницы')).toBeInTheDocument();
    expect(screen.getAllByText('Действия').length).toBeGreaterThanOrEqual(1);
    // Esc закрывает
    fireEvent.keyDown(input, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByPlaceholderText(PH)).not.toBeInTheDocument(), { timeout: 5000 });
  }, 20_000);

  it('renders Pages + Actions sections with known items', async () => {
    renderApp('/');
    await openPalette();
    // страницы (pages) — Dashboard
    expect((await screen.findAllByText('Dashboard', {}, { timeout: 5000 })).length).toBeGreaterThanOrEqual(1);
    // действия (actions) — New team
    expect(await screen.findByText('New team', {}, { timeout: 5000 })).toBeInTheDocument();
    // aria: listbox + option + aria-activedescendant
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    expect(screen.getAllByRole('option').length).toBeGreaterThanOrEqual(1);
  }, 20_000);

  it('type + Enter navigates to the target page (Settings)', async () => {
    renderApp('/');
    const input = await openPalette();
    fireEvent.change(input, { target: { value: 'settings' } });
    // option «Settings» (страница) в разделе Страницы
    await screen.findAllByText('Settings', {}, { timeout: 5000 });
    fireEvent.keyDown(input, { key: 'Enter' });
    // палитра закрылась + навигация на /settings (h1 «Settings»)
    await waitFor(() => expect(screen.queryByPlaceholderText(PH)).not.toBeInTheDocument(), { timeout: 5000 });
    expect(await screen.findByRole('heading', { name: 'Settings' }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('ArrowDown moves the active option (aria-activedescendant)', async () => {
    renderApp('/');
    const input = await openPalette();
    // пустой запрос: несколько страниц — активен первый
    const combobox = screen.getByRole('combobox', { name: 'Command palette' });
    expect(combobox).toHaveAttribute('aria-activedescendant', 'palette-opt-0');
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(combobox).toHaveAttribute('aria-activedescendant', 'palette-opt-1');
  }, 20_000);
});
