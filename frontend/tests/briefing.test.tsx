import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P1.7: Session briefing — Session Detail block + TeamBuilder preview modal.

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P1.7: Session briefing', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('Session Detail: shows the briefing block (session 1)', async () => {
    renderAt('/sessions/1');
    // briefing block heading
    const heading = await screen.findByText('Briefing (startup context)', {}, { timeout: 15_000 });
    expect(heading).toBeInTheDocument();
    const card = heading.closest<HTMLElement>('.briefing-card')!;
    // delivery badge (applied)
    expect(within(card).getByText('builtin')).toBeInTheDocument();
    // briefing text (canonical markdown)
    expect(await within(card).findByText(/# Session briefing/, {}, { timeout: 10_000 })).toBeInTheDocument();
  }, 25_000);

  it('Session Detail: no briefing block for a session without briefing', async () => {
    renderAt('/sessions/2'); // session 2: no briefing fields
    await screen.findByText(/Transcript \(/, {}, { timeout: 15_000 });
    expect(screen.queryByText('Briefing (startup context)')).not.toBeInTheDocument();
  }, 25_000);

  it('TeamBuilder: «Брифинг» opens the modal with the briefing preview', async () => {
    renderAt('/teams/1');
    // wait for the team builder to load
    await screen.findByText(/Validate/, {}, { timeout: 15_000 });
    const btn = await screen.findByRole('button', { name: /Брифинг/ }, { timeout: 15_000 });
    fireEvent.click(btn);
    // modal opens
    const modal = await screen.findByRole('dialog', {}, { timeout: 15_000 });
    expect(within(modal).getByText('Брифинг сессии')).toBeInTheDocument();
    // role/task selects present (launch panel)
    expect(within(modal).getByLabelText(/Роль/)).toBeInTheDocument();
    expect(within(modal).getByLabelText(/Задача/)).toBeInTheDocument();
    // briefing text (mock preview) + delivery + launch button
    expect(await within(modal).findByText(/# Session briefing/, {}, { timeout: 15_000 })).toBeInTheDocument();
    expect(within(modal).getByText(/pi → system_prompt/)).toBeInTheDocument();
    expect(within(modal).getByRole('button', { name: /Запустить с этим брифингом/ })).toBeInTheDocument();
  }, 30_000);

  it('TeamBuilder: launch a session with an override briefing → result (applied=override + link)', async () => {
    renderAt('/teams/1');
    await screen.findByText(/Validate/, {}, { timeout: 15_000 });
    fireEvent.click(await screen.findByRole('button', { name: /Брифинг/ }, { timeout: 15_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 15_000 });
    // вводим override
    fireEvent.change(within(modal).getByLabelText(/Override/), { target: { value: 'МОЙ CUSTOM BRIEFING' } });
    // запускаем с этим брифингом
    fireEvent.click(await within(modal).findByRole('button', { name: /Запустить с этим брифингом/ }, { timeout: 15_000 }));
    // результат: сессия запущена + applied=override + link на сессию
    expect(await within(modal).findByText(/Сессия #\d+ запущена/, {}, { timeout: 15_000 })).toBeInTheDocument();
    expect(within(modal).getByText('override')).toBeInTheDocument();
    // link на новую сессию
    expect(within(modal).getByText(/^\/sessions\/\d+$/)).toBeInTheDocument();
  }, 30_000);
});
