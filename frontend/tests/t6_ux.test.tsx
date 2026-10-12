import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';

// T6 — мелкие UX: workflow-имена (readable default + rename), briefing-hint,
// select aria-label (фильтры «(filter)» vs формы), Settings ⏻-индикатор.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe('T6.1: Workflow-имена — читаемое дефолтное имя + rename', () => {
  beforeEach(() => resetMockDb());

  it('new workflow default name = "{team.name}-flow" (не timestamp)', async () => {
    renderAt('/workflows');
    await screen.findByRole('heading', { name: 'Workflows' }, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: '+ New workflow' }));
    // дефолтное имя подставлено (active team = Dev Team), input адресуется без неоднозначности
    await waitFor(
      () => {
        const input = screen.getByLabelText('Workflow name (create)') as HTMLInputElement;
        expect(input.value).toBe('Dev Team-flow');
      },
      { timeout: 10_000 },
    );
  }, 20_000);

  it('inline rename: клик ✎ → input → Enter → workflow переименован', async () => {
    renderAt('/workflows');
    await screen.findByText('Hotfix', {}, { timeout: 10_000 });
    // кнопка rename у workflow «Hotfix»
    fireEvent.click(screen.getByRole('button', { name: 'Переименовать Hotfix' }));
    // input для редактирования
    const input = screen.getByLabelText('Workflow name') as HTMLInputElement;
    expect(input.value).toBe('Hotfix');
    fireEvent.change(input, { target: { value: 'Hotfix v2' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    // в списке имя обновилось
    await waitFor(() => expect(screen.getByText('Hotfix v2')).toBeInTheDocument(), { timeout: 10_000 });
    // mock-дб обновился
    const db = getDb();
    expect(db.workflows.find((w) => w.name === 'Hotfix v2')).toBeTruthy();
  }, 20_000);
});

describe('T6.3: Select — фильтры страницы отличаются от полей форм по aria-label', () => {
  beforeEach(() => resetMockDb());

  it('Tasks: page-фильтры имеют aria-label «(filter)»', async () => {
    renderAt('/tasks');
    await screen.findByRole('heading', { name: /Tasks/ }, { timeout: 10_000 });
    // фильтры страницы (aria-label с «(filter)»)
    expect(await screen.findByLabelText('Team (filter)', undefined, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByLabelText('State (filter)')).toBeInTheDocument();
    expect(screen.getByLabelText('Project (filter)')).toBeInTheDocument();
  }, 20_000);
});

describe('T6.4: Settings ⏻ — индикатор, а не кнопка (понятный tooltip)', () => {
  beforeEach(() => resetMockDb());

  it('⏻ имеет tooltip «перезапуск демона» (не «нажми»)', async () => {
    renderAt('/settings');
    await screen.findByRole('heading', { name: 'Settings' }, { timeout: 10_000 });
    // хотя бы один ⏻-индикатор с русским tooltip про перезапуск демона
    await waitFor(
      () => {
        const indicators = screen.getAllByText('⏻');
        expect(indicators.length).toBeGreaterThanOrEqual(1);
        const span = indicators[0] as HTMLElement;
        expect(span.getAttribute('title')).toContain('перезапуск демона');
      },
      { timeout: 10_000 },
    );
    // «↻ Refresh» — рабочая кнопка (btn-primary)
    expect(screen.getByRole('button', { name: /Refresh/ })).toBeInTheDocument();
  }, 20_000);
});
