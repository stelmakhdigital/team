import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';

// P1.4: Workflow engine — Run button (WorkflowsPage) + trail/abort (Instance page).
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('P1.4: Workflow engine (run + trace)', () => {
  beforeEach(() => resetMockDb());

  it('WorkflowsPage: engine-enabled workflow shows the Run button', async () => {
    renderAt('/workflows');
    expect(await screen.findByRole('heading', { name: 'Workflows' }, { timeout: 10_000 })).toBeInTheDocument();
    // 'Build & Review' (id=1) is engine_enabled in the seed
    expect(await screen.findByText('Build & Review', {}, { timeout: 10_000 })).toBeInTheDocument();
    // Run button present for the engine-enabled workflow
    expect(await screen.findByRole('button', { name: /Run/ }, { timeout: 10_000 })).toBeInTheDocument();
  }, 20_000);

  it('WorkflowInstancePage: shows the running instance trail + abort action', async () => {
    renderAt('/workflows/1/instances/1');
    // header: "Build & Review / Instance #1"
    expect(await screen.findByText(/Instance #1/, {}, { timeout: 10_000 })).toBeInTheDocument();
    // running state badge
    expect(await screen.findByText('running', {}, { timeout: 10_000 })).toBeInTheDocument();
    // trail: Implement (done) + Worker runs (active) — each appears in the
    // block-label cell and (for Implement) the task title, so use findAllByText
    const impl = await screen.findAllByText(/Implement/, {}, { timeout: 10_000 });
    expect(impl.length).toBeGreaterThanOrEqual(1);
    const wr = await screen.findAllByText(/Worker runs/, {}, { timeout: 10_000 });
    expect(wr.length).toBeGreaterThanOrEqual(1);
    // abort action available while running
    expect(screen.getByRole('button', { name: /Abort/ })).toBeInTheDocument();
  }, 20_000);
});
