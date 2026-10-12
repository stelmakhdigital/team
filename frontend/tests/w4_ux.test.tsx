import { describe, expect, it, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb } from '../src/api/mock/data';
import OnboardingTour, { type TourStep } from '../src/components/OnboardingTour';

// UX Wave 4 — W4.1 Graph/Table toggle (+таблица, narrow-default) + W4.2 onboarding-тур.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

// matchMedia-мок: jsdom по умолчанию всегда matches:false (setup.ts). Переопределяем
// для narrow-тестов (1200px) и восстанавливаем в afterEach.
const ORIGINAL_MEDIA = window.matchMedia;
function setNarrow(narrow: boolean) {
  window.matchMedia = ((q: string) => ({
    matches: narrow && q === '(max-width: 1200px)',
    media: q,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}
const hasTable = () => screen.queryByRole('table', { name: 'Topology table' }) !== null;

beforeEach(() => {
  resetMockDb();
  localStorage.clear();
});
afterEach(() => {
  window.matchMedia = ORIGINAL_MEDIA;
  localStorage.clear();
});

describe('W4.1: Graph/Table toggle', () => {
  it('toggle: Table показывает таблицу, Graph — обратно', async () => {
    renderAt('/teams/1');
    const tableTab = await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    // по умолчанию Graph → таблицы нет
    expect(hasTable()).toBe(false);
    fireEvent.click(tableTab);
    expect(await screen.findByRole('table', { name: 'Topology table' }, { timeout: 5000 })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: 'Graph' }));
    await waitFor(() => expect(hasTable()).toBe(false), { timeout: 5000 });
  }, 30_000);

  it('localStorage-персист: выбранный Table переживает перерендер', async () => {
    renderAt('/teams/1');
    await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    fireEvent.click(screen.getByRole('tab', { name: 'Table' }));
    expect(await screen.findByRole('table', { name: 'Topology table' }, { timeout: 5000 })).toBeInTheDocument();
    expect(localStorage.getItem('teambuilder.view')).toBe('table');
    // перерендер — таблица остаётся (без клика)
    const { unmount } = renderAt('/teams/1');
    await waitFor(() => expect(hasTable()).toBe(true), { timeout: 5000 });
    unmount();
  }, 30_000);

  it('table: строка роли — имя, agent_spec (mono), state dot, ctx%', async () => {
    renderAt('/teams/1');
    await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    fireEvent.click(screen.getByRole('tab', { name: 'Table' }));
    await screen.findByRole('table', { name: 'Topology table' }, { timeout: 5000 });
    // ждём live-сессии (отдельный query) — иначе state/ctx ещё «—»
    await screen.findByText('ctx 42%', {}, { timeout: 10_000 });
    expect(screen.getAllByText('pi-go-backend').length).toBeGreaterThan(0);
    expect(document.querySelector('.fleet-dot[data-activity-state="running"]')).toBeTruthy();
  }, 30_000);

  it('table: секции по segment (имя + count)', async () => {
    renderAt('/teams/1');
    await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    fireEvent.click(screen.getByRole('tab', { name: 'Table' }));
    await screen.findByRole('table', { name: 'Topology table' }, { timeout: 5000 });
    // Backend (3 роли: Lead/Worker/Worker2) + Review (1: Reviewer)
    const segNames = Array.from(document.querySelectorAll('.topo-table-seg-name')).map((e) => e.textContent);
    expect(segNames).toContain('Backend');
    expect(segNames).toContain('Review');
    const backendSeg = Array.from(document.querySelectorAll('.topo-table-seg')).find((e) =>
      e.textContent?.includes('Backend'),
    );
    expect(backendSeg?.querySelector('.topo-table-seg-count')?.textContent).toBe('3');
  }, 30_000);

  it('table: ctx% пороги (ок/warn/crit) по токенам', async () => {
    renderAt('/teams/1');
    await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    fireEvent.click(screen.getByRole('tab', { name: 'Table' }));
    await screen.findByRole('table', { name: 'Topology table' }, { timeout: 5000 });
    await screen.findByText('ctx 42%', {}, { timeout: 10_000 }); // live-сессии загружены
    // Lead 42% → ok, Worker 67% → warn, Reviewer 88% → crit
    const ctxEl = (pct: number) => screen.getByText(`ctx ${pct}%`).closest('.fleet-ctx') as HTMLElement;
    expect(ctxEl(42)).toHaveClass('fleet-ctx-ok');
    expect(ctxEl(67)).toHaveClass('fleet-ctx-warn');
    expect(ctxEl(88)).toHaveClass('fleet-ctx-crit');
  }, 30_000);

  it('узкий viewport (<1200px) → Table по умолчанию', async () => {
    setNarrow(true);
    renderAt('/teams/1');
    const tableTab = await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    // без ручного выбора → таблица, Graph-таб неактивна
    expect(hasTable()).toBe(true);
    expect(tableTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'Graph' })).toHaveAttribute('aria-selected', 'false');
  }, 30_000);

  it('явный выбор Graph переживает narrow (приоритет над дефолтом)', async () => {
    setNarrow(true);
    renderAt('/teams/1');
    const tableTab = await screen.findByRole('tab', { name: 'Table' }, { timeout: 15_000 });
    expect(hasTable()).toBe(true); // narrow-дефолт
    // явный Graph → остаётся Graph, несмотря на narrow
    fireEvent.click(screen.getByRole('tab', { name: 'Graph' }));
    await waitFor(() => expect(hasTable()).toBe(false), { timeout: 5000 });
    expect(screen.getByRole('tab', { name: 'Graph' })).toHaveAttribute('aria-selected', 'true');
    expect(tableTab).toHaveAttribute('aria-selected', 'false');
  }, 30_000);
});

describe('W4.2: onboarding-тур', () => {
  it('первый вход (force) → тур виден (шаг 1 / 6)', async () => {
    localStorage.setItem('tour.force', '1');
    renderAt('/');
    expect(await screen.findByText('Навигация', {}, { timeout: 10_000 })).toBeInTheDocument();
    expect(screen.getByText('1 / 6')).toBeInTheDocument();
  }, 20_000);

  it('«Далее» → шаг 2 (Fleet)', async () => {
    localStorage.setItem('tour.force', '1');
    renderAt('/');
    await screen.findByText('1 / 6', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: 'Далее' }));
    expect(await screen.findByText('2 / 6', {}, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.getByText('Live-статус всех ролей: состояние, задача, контекст')).toBeInTheDocument();
  }, 20_000);

  it('«Пропустить» → dismissed (localStorage) + тур исчезает', async () => {
    localStorage.setItem('tour.force', '1');
    renderAt('/');
    await screen.findByText('1 / 6', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: 'Пропустить' }));
    await waitFor(() => expect(localStorage.getItem('tour.dismissed')).toBe('1'), { timeout: 5000 });
    await waitFor(() => expect(screen.queryByText('1 / 6')).toBeNull(), { timeout: 5000 });
  }, 20_000);

  it('Esc → dismissed', async () => {
    localStorage.setItem('tour.force', '1');
    renderAt('/');
    await screen.findByText('1 / 6', {}, { timeout: 10_000 });
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(localStorage.getItem('tour.dismissed')).toBe('1'), { timeout: 5000 });
    await waitFor(() => expect(screen.queryByText('1 / 6')).toBeNull(), { timeout: 5000 });
  }, 20_000);

  it('повторный вход (dismissed) → нет тура', async () => {
    localStorage.setItem('tour.dismissed', '1');
    renderAt('/');
    await screen.findByText('Dashboard', {}, { timeout: 10_000 });
    expect(screen.queryByText('1 / 6')).toBeNull();
    expect(screen.queryByText('Навигация')).toBeNull();
  }, 20_000);

  it('«Повторить тур» в Settings → тур снова', async () => {
    renderAt('/settings');
    await screen.findByRole('heading', { name: 'Settings' }, { timeout: 15_000 });
    fireEvent.click(await screen.findByRole('button', { name: /Повторить тур/ }, { timeout: 5000 }));
    expect(await screen.findByText('1 / 6', {}, { timeout: 5000 })).toBeInTheDocument();
    expect(localStorage.getItem('tour.dismissed')).toBeNull();
  }, 20_000);

  it('недоступная цель → пропуск шага (компонент)', async () => {
    // t2 отсутствует в DOM → при «Далее» с t1 тур пропускает t2 и показывает t3
    const steps: TourStep[] = [
      { id: 't1', title: 'A', body: 'body-one' },
      { id: 't2', title: 'B', body: 'body-two' },
      { id: 't3', title: 'C', body: 'body-three' },
    ];
    render(
      <div>
        <div data-tour="t1">one</div>
        <div data-tour="t3">three</div>
        <OnboardingTour steps={steps} onClose={() => {}} />
      </div>,
    );
    expect(await screen.findByText('body-one', {}, { timeout: 5000 })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Далее' }));
    expect(await screen.findByText('body-three', {}, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.queryByText('body-two')).toBeNull();
  });
});
