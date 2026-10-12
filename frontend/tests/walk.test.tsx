import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import WalkPanel from '../src/components/WalkPanel';
import { resetMockDb } from '../src/api/mock/data';

// P3.14: Walk (paced onboarding) — список walk'ов, статус-бейдж, progress,
// advance (→completed), create-форма (редактор шагов), retained→paused, pause/resume.
// Мок-данные: session 1 (Lead) — 1 walk (active, 2/3: delivered + staged + pending).

function renderPanel(sessionId = 1) {
  return render(<WalkPanel sessionId={sessionId} />);
}

describe('P3.14: walk (paced onboarding)', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('list: walk + статус-бейдж (active) + progress (2/3) + 3 шага', async () => {
    renderPanel(1);
    const item = await screen.findByTestId('walk-item', {}, { timeout: 10_000 });
    expect(item).toBeInTheDocument();
    const progress = await screen.findByTestId('walk-progress', {}, { timeout: 10_000 });
    expect(progress).toHaveTextContent('2/3');
    expect(screen.getByText('active')).toBeInTheDocument();
    const steps = await screen.findAllByTestId('walk-step', {}, { timeout: 10_000 });
    expect(steps.length).toBe(3);
    // active → Advance-кнопка
    expect(screen.getByRole('button', { name: /Advance/ })).toBeInTheDocument();
  }, 25_000);

  it('advance: последний шаг → completed (3/3), Advance-кнопка исчезает', async () => {
    renderPanel(1);
    const advance = await screen.findByRole('button', { name: /Advance/ }, { timeout: 10_000 });
    fireEvent.click(advance);
    // seed walk: current_seq 3 → step3 delivered → completed, 3/3.
    await waitFor(() => expect(screen.getByTestId('walk-progress')).toHaveTextContent('3/3'), { timeout: 10_000 });
    await waitFor(() => expect(screen.getByText('completed')).toBeInTheDocument(), { timeout: 10_000 });
    await waitFor(
      () => expect(screen.queryByRole('button', { name: /Advance/ })).not.toBeInTheDocument(),
      { timeout: 10_000 },
    );
  }, 25_000);

  it('create: форма + редактор шагов → новый walk появляется', async () => {
    renderPanel(1);
    await screen.findByTestId('walk-item', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Новый walk/ }));
    fireEvent.change(screen.getByLabelText('Walk name'), { target: { value: 'My walk' } });
    fireEvent.change(screen.getByPlaceholderText('Шаг 1: что отправить'), { target: { value: 'step one' } });
    // добавим второй шаг
    fireEvent.click(screen.getByRole('button', { name: /Добавить шаг/ }));
    fireEvent.change(screen.getByPlaceholderText('Шаг 2: что отправить'), { target: { value: 'step two' } });
    fireEvent.click(screen.getByRole('button', { name: /Create walk/ }));
    await waitFor(() => expect(screen.getAllByTestId('walk-item').length).toBe(2), { timeout: 10_000 });
    // новый walk: My walk (active, 0/2)
    await waitFor(() => expect(screen.getByText('My walk')).toBeInTheDocument(), { timeout: 10_000 });
  }, 25_000);

  it('advance retained ([retain]) → step retained + walk paused + Resume', async () => {
    renderPanel(1);
    await screen.findByTestId('walk-item', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Новый walk/ }));
    fireEvent.change(screen.getByLabelText('Walk name'), { target: { value: 'retain-walk' } });
    fireEvent.change(screen.getByPlaceholderText('Шаг 1: что отправить'), {
      target: { value: '[retain] do you want to proceed? (y/n)' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Create walk/ }));
    // новый walk (retain-walk) — первый в списке (id DESC), active.
    await waitFor(() => expect(screen.getAllByTestId('walk-item').length).toBe(2), { timeout: 10_000 });
    // advance на новый (retain-walk) → retained → paused.
    const advances = screen.getAllByRole('button', { name: /Advance/ });
    fireEvent.click(advances[0]);
    await waitFor(() => expect(screen.getByText('retained')).toBeInTheDocument(), { timeout: 10_000 });
    await waitFor(() => expect(screen.getByText('paused')).toBeInTheDocument(), { timeout: 10_000 });
    await waitFor(() => expect(screen.getByRole('button', { name: /Resume/ })).toBeInTheDocument(), { timeout: 10_000 });
  }, 25_000);

  it('pause/resume: active → paused (Resume) → active', async () => {
    renderPanel(1);
    const pause = await screen.findByRole('button', { name: /Pause/ }, { timeout: 10_000 });
    fireEvent.click(pause);
    await waitFor(() => expect(screen.getByText('paused')).toBeInTheDocument(), { timeout: 10_000 });
    const resume = await screen.findByRole('button', { name: /Resume/ }, { timeout: 10_000 });
    fireEvent.click(resume);
    await waitFor(() => expect(screen.getByText('active')).toBeInTheDocument(), { timeout: 10_000 });
  }, 25_000);
});
