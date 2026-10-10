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

  it('TeamBuilder: «Брифинг» opens the preview modal with the briefing text', async () => {
    renderAt('/teams/1');
    // wait for the team builder to load
    await screen.findByText(/Validate/, {}, { timeout: 15_000 });
    const btn = await screen.findByRole('button', { name: /Брифинг/ }, { timeout: 15_000 });
    fireEvent.click(btn);
    // modal opens
    const modal = await screen.findByRole('dialog', {}, { timeout: 15_000 });
    expect(within(modal).getByText('Предпросмотр брифинга')).toBeInTheDocument();
    // briefing text (mock preview) + delivery
    expect(await within(modal).findByText(/# Session briefing/, {}, { timeout: 15_000 })).toBeInTheDocument();
    expect(within(modal).getByText(/pi → system_prompt/)).toBeInTheDocument();
  }, 30_000);
});
