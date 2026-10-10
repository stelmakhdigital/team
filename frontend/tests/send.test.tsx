import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P2.11: Send to session + verify — delivered (badge + tail), staged, retained (guard), pending.
function renderSession() {
  return render(
    <MemoryRouter initialEntries={['/sessions/1']}>
      <App />
    </MemoryRouter>,
  );
}
async function fillAndSend(text: string) {
  const ta = await screen.findByLabelText('Текст для отправки в сессию', {}, { timeout: 10_000 });
  fireEvent.change(ta, { target: { value: text } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
}

describe('P2.11: Send to session + verify', () => {
  beforeEach(() => resetMockDb());

  it('send → delivered (status badge + capture tail)', async () => {
    renderSession();
    expect(await screen.findByText('Send to session', {}, { timeout: 10_000 })).toBeInTheDocument();
    // warning (действие на живой процесс)
    expect(screen.getByText(/будет введён в терминал/)).toBeInTheDocument();
    await fillAndSend('check the logs please');
    // delivered-бейдж
    expect(await screen.findByText('delivered', {}, { timeout: 5000 })).toBeInTheDocument();
    // verified → tail: раскрыть + содержимое (последняя строка «Lead $ …»)
    fireEvent.click(await screen.findByRole('button', { name: /Capture tail/ }, { timeout: 5000 }));
    await waitFor(
      () => expect(screen.getByText(/Lead \$ check the logs please/)).toBeInTheDocument(),
      { timeout: 5000 },
    );
  }, 20_000);

  it('send → staged (введено, не подтверждено)', async () => {
    renderSession();
    await screen.findByText('Send to session', {}, { timeout: 10_000 });
    await fillAndSend('staged noreply text');
    expect(await screen.findByText('staged', {}, { timeout: 5000 })).toBeInTheDocument();
    // staged → нет tail-кнопки
    expect(screen.queryByRole('button', { name: /Capture tail/ })).not.toBeInTheDocument();
  }, 20_000);

  it('send → retained (interactive-prompt guard, text not sent)', async () => {
    renderSession();
    await screen.findByText('Send to session', {}, { timeout: 10_000 });
    await fillAndSend('DO NOT RETAIN this');
    expect(await screen.findByText('retained', {}, { timeout: 5000 })).toBeInTheDocument();
    // guard-причина
    expect(await screen.findByText(/interactive-prompt/, {}, { timeout: 5000 })).toBeInTheDocument();
  }, 20_000);

  it('pending state during send (then resolves)', async () => {
    renderSession();
    await screen.findByText('Send to session', {}, { timeout: 10_000 });
    const ta = await screen.findByLabelText('Текст для отправки в сессию');
    fireEvent.change(ta, { target: { value: 'hello' } });
    // до отправки: Send доступен
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    // pending: «Отправляем…» + кнопка «…»
    expect(screen.getByText('Отправляем…')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '…' })).toBeInTheDocument();
    // резолвится в delivered
    expect(await screen.findByText('delivered', {}, { timeout: 5000 })).toBeInTheDocument();
  }, 20_000);
});
