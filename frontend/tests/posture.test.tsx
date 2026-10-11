import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import PosturePanel from '../src/components/PosturePanel';
import HumanRequestActions from '../src/components/HumanRequestActions';
import { api } from '../src/api';
import { resetMockDb } from '../src/api/mock/data';

// P3.16: Mode/permission posture — panel (optimistic toggle) + human-request decide.
// Мок: team 1 = human_led; human_request 1 (pending, session.stop).

describe('P3.16: posture (panel + human-request)', () => {
  beforeEach(() => {
    resetMockDb();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('team panel: human_led badge (seeded) → toggle to delegated (optimistic + persist)', async () => {
    render(<PosturePanel kind="team" id={1} current="human_led" />);
    // badge human-led (seeded team 1)
    const badge = await screen.findByTestId('posture-badge', {}, { timeout: 10_000 });
    expect(badge).toHaveTextContent(/Human-led/);
    // клик по Delegated → optimistic badge + persist (mock team 1 posture=delegated)
    const radios = screen.getAllByRole('radio');
    const delegated = radios.find((r) => r.closest('label')?.textContent?.includes('Delegated'));
    expect(delegated).toBeTruthy();
    fireEvent.click(delegated as unknown as HTMLElement);
    // optimistic: badge сразу delegated
    await waitFor(() => expect(screen.getByTestId('posture-badge')).toHaveTextContent(/Delegated/), { timeout: 10_000 });
    // persist: mock team 1 posture = delegated
    await waitFor(async () => {
      const t = (await api.teams.getTopology(1)).team;
      expect(t.posture).toBe('delegated');
    }, { timeout: 10_000 });
  }, 25_000);

  it('session panel: inherit → effective = team posture (human_led)', async () => {
    // session 1 (team 1 = human_led), posture=null (inherit) → effective human_led
    render(<PosturePanel kind="session" id={1} current={null} teamPosture="human_led" />);
    const badge = await screen.findByTestId('posture-badge', {}, { timeout: 10_000 });
    expect(badge).toHaveTextContent(/Human-led/);
  }, 25_000);

  it('human-request: approve → replayed (mock decide) + onDecided', async () => {
    vi.spyOn(window, 'prompt').mockReturnValue('ok');
    const onDecided = vi.fn();
    render(<HumanRequestActions id={1} operation="session.stop" onDecided={onDecided} />);
    const approve = await screen.findByRole('button', { name: /Approve/ }, { timeout: 10_000 });
    fireEvent.click(approve);
    await waitFor(async () => {
      const hr = await api.posture.getHumanRequest(1);
      expect(hr.status).toBe('replayed');
    }, { timeout: 10_000 });
    expect(onDecided).toHaveBeenCalled();
  }, 25_000);

  it('mock: setSession override (delegated) + reset to inherit (null)', async () => {
    await api.posture.setSession(1, 'delegated');
    let s = await api.sessions.get(1);
    expect(s.posture).toBe('delegated');
    await api.posture.setSession(1, null);
    s = await api.sessions.get(1);
    expect(s.posture ?? null).toBeNull();
  }, 25_000);
});
