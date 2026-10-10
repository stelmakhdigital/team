import { describe, expect, it } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';

// P2.7: Поиск по транскриптам — результаты (группы + context + подсветка) + EntityLink + EmptyState.

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

// Вводит q + нажимает «Найти».
async function doSearch(q: string) {
  const input = await screen.findByPlaceholderText(/фраза или решение/, {}, { timeout: 15_000 });
  fireEvent.change(input, { target: { value: q } });
  const btn = screen.getByRole('button', { name: /Найти/ });
  fireEvent.click(btn);
}

describe('P2.7: Transcript search', () => {
  it('renders grouped results with context + EntityLink', async () => {
    renderAt('/transcript-search');
    await doSearch('JWT');
    // результаты: сгруппировано по сессии (role + team + link)
    expect(await screen.findByText('Открыть сессию #1 →', {}, { timeout: 15_000 })).toBeInTheDocument();
    // context-окно: совпадение «JWT» в строке (session 1); текст может быть разбит <mark>
    const ctxs = Array.from(document.querySelectorAll('.ts-context')).map((el) => el.textContent ?? '');
    expect(ctxs.some((c) => c.includes('use JWT with rotation'))).toBe(true);
    expect(ctxs.some((c) => c.includes('JWT secret from env'))).toBe(true);
    // роль + команда решены
    expect(screen.getAllByText('Lead').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Dev Team')).toBeInTheDocument();
    // подсветка: <mark> с совпадением
    expect(document.querySelector('mark.ts-match')).not.toBeNull();
  }, 30_000);

  it('EntityLink ведёт на сессию (href /sessions/{id})', async () => {
    renderAt('/transcript-search');
    await doSearch('JWT');
    const link = await screen.findByText('Открыть сессию #1 →', {}, { timeout: 15_000 });
    expect(link.closest('a')).toHaveAttribute('href', '/sessions/1');
  }, 30_000);

  it('пустой результат → EmptyState', async () => {
    renderAt('/transcript-search');
    await doSearch('zzznotfound');
    expect(await screen.findByText('Ничего не найдено', {}, { timeout: 15_000 })).toBeInTheDocument();
  }, 30_000);

  it('nav has a Search link', async () => {
    renderAt('/');
    const link = await screen.findByRole('link', { name: /Search/ }, { timeout: 15_000 });
    expect(link).toHaveAttribute('href', '/transcript-search');
  }, 30_000);
});
