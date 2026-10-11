import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P3.12: agent images / session snapshots — Library (тип/карточка/детали/launch) +
// Session Detail (кнопка → модалка → snapshot).

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P3.12: agent images / session snapshots', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('Library: shows the agent_image item + type filter option', async () => {
    renderAt('/library');
    // карточка agent_image (mock-данные: go-dev-optimized)
    await screen.findByText('go-dev-optimized', {}, { timeout: 10_000 });
    // тип-фильтр имеет "Agent images"
    expect(screen.getByRole('option', { name: 'Agent images' })).toBeInTheDocument();
    // PINNED бейдж (mock-снапшот закреплён)
    expect(screen.getByText('PINNED')).toBeInTheDocument();
  }, 25_000);

  it('Library: agent_image detail shows agent config + pin/prune/launch controls', async () => {
    renderAt('/library');
    await screen.findByText('go-dev-optimized', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByText('go-dev-optimized'));
    const details = await screen.findByRole('heading', { name: /Agent image/ }, { timeout: 10_000 });
    expect(details).toBeInTheDocument();
    // agent config из spec (mock)
    expect(screen.getByText(/role: worker/)).toBeInTheDocument();
    expect(screen.getByText('go-review')).toBeInTheDocument(); // скилл
    // pin/prune/launch контролы
    expect(screen.getByRole('button', { name: /Unpin/ })).toBeInTheDocument(); // закреплён → Unpin
    expect(screen.getByRole('button', { name: /Prune/ })).toBeDisabled(); // pinned → prune disabled
    expect(screen.getByRole('button', { name: /Launch/ })).toBeInTheDocument();
  }, 25_000);

  it('Session detail: "Agent image" button opens the snapshot modal', async () => {
    renderAt('/sessions/1');
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: /Agent image/ }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    await within(modal).findByText('Save as agent image', {}, { timeout: 10_000 });
    // префилл имени (agent-<role>)
    const nameInput = within(modal).getByLabelText('Image name') as HTMLInputElement;
    expect(nameInput.value).toMatch(/^agent-/);
  }, 25_000);

  it('Snapshot modal: submit creates the image and closes', async () => {
    renderAt('/sessions/1');
    await screen.findByText(/Transcript \(/, {}, { timeout: 10_000 });
    fireEvent.click(await screen.findByRole('button', { name: /Agent image/ }, { timeout: 10_000 }));
    const modal = await screen.findByRole('dialog', {}, { timeout: 10_000 });
    const saveBtn = within(modal).getByRole('button', { name: /Save image/ });
    fireEvent.click(saveBtn);
    // модалка закрывается после успешного snapshot
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument(), { timeout: 10_000 });
  }, 25_000);
});
