import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import MailboxPanel from '../src/components/MailboxPanel';
import { resetMockDb } from '../src/api/mock/data';

// P3.15: Mailbox (inbox/outbox) — pending-бейдж, undelivered-счётчик, ack-действия, outbox.
// Мок-данные: Worker (role 2) — inbox: msg1 (pending) + msg5 (absorbed) + msg7 (pending);
// outbox: msg2 (Worker→Lead).

function renderPanel(roleId: number, roleName: string) {
  return render(<MailboxPanel roleId={roleId} roleName={roleName} />);
}

describe('P3.15: mailbox (inbox/outbox)', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('inbox: pending-бейджи + undelivered-счётчик (Worker = 2 pending)', async () => {
    renderPanel(2, 'Worker');
    const panel = await screen.findByTestId('mailbox-panel', {}, { timeout: 10_000 });
    expect(panel).toBeInTheDocument();
    // undelivered counter = 2 (msg1 + msg7 pending)
    const counter = await screen.findByTestId('undelivered-counter', {}, { timeout: 10_000 });
    expect(counter).toHaveTextContent('2');
    // pending-бейджи (2 pending) + Absorb-кнопки (по одной на pending)
    await screen.findByTestId('inbox-list', {}, { timeout: 10_000 });
    expect(screen.getAllByText('pending').length).toBe(2);
    expect(screen.getAllByRole('button', { name: /Absorb/ }).length).toBe(2);
  }, 25_000);

  it('ack absorb: pending → absorbed (undelivered падает, acked_by)', async () => {
    renderPanel(2, 'Worker');
    await screen.findByTestId('mailbox-panel', {}, { timeout: 10_000 });
    // ждём загрузку inbox (Absorb-кнопки появляются)
    await screen.findAllByRole('button', { name: /Absorb/ }, { timeout: 10_000 });
    const absorbsBefore = screen.getAllByRole('button', { name: /Absorb/ }).length;
    expect(absorbsBefore).toBe(2);
    // клик Absorb на первый pending → absorbed
    fireEvent.click(screen.getAllByRole('button', { name: /Absorb/ })[0]);
    // acked_by operator появляется (только для свеже-аккного)
    await waitFor(() => expect(screen.getByText(/acked by operator/)).toBeInTheDocument(), { timeout: 10_000 });
    // Absorb-кнопок стало на 1 меньше (2 → 1)
    await waitFor(() => expect(screen.getAllByRole('button', { name: /Absorb/ }).length).toBe(absorbsBefore - 1), { timeout: 10_000 });
  }, 25_000);

  it('outbox: исходящие (Worker → Lead, "Understood. ETA ~2h.")', async () => {
    renderPanel(2, 'Worker');
    const outbox = await screen.findByTestId('outbox-list', {}, { timeout: 10_000 });
    expect(outbox).toBeInTheDocument();
    // Worker's outbox = msg2 (Worker→Lead)
    await screen.findByText('Understood. ETA ~2h.', {}, { timeout: 10_000 });
  }, 25_000);
});
