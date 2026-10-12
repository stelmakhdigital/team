import { describe, expect, it, beforeEach, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { buildRelEdges } from '../src/components/Topology/TopologyCanvas';
import { computeActiveRoleIds } from '../src/lib/fleet';
import { resetMockDb } from '../src/api/mock/data';
import type { Relative, Task } from '../src/types/api';

// W1.3: «пакет» по связи — активен ТОЛЬКО там, где у роли-получателя (to) реально
// есть in_progress-задача (честно, без фейка). Две проверки:
//  (a) семантика: tasks → activeRoleIds → buildRelEdges → edge.active;
//  (b) интеграция: TeamBuilderPage вычисляет activeRoleIds из api.tasks.list и передаёт в канвас.
//  (seed team 1: in_progress-задачи #1 и #3 — обе на destination_role_id=2)

const rel = (id: number, from: number, to: number, type: Relative['type'] = 'delegates_to'): Relative => ({
  id, team_id: 1, from_role_id: from, from_role_name: `R${from}`, to_role_id: to, to_role_name: `R${to}`, type, created_at: '',
});
const task = (id: number, to: number, state: Task['state']): Task => ({
  id, team_id: 1, team_name: 'Dev Team', title: `t${id}`, state, priority: 1,
  destination_role_id: to, destination_role_name: `R${to}`, created_at: '', updated_at: '',
});

describe('W1.3 семантика: task in_progress → ребро к роли активно', () => {
  it('edge to роли с in_progress-задачей active=true, к остальным — false', () => {
    const tasks = [task(1, 2, 'in_progress'), task(2, 3, 'done'), task(3, 4, 'pending')];
    const active = computeActiveRoleIds(tasks); // только роль 2
    const relatives = [rel(1, 1, 2), rel(2, 1, 3), rel(3, 2, 4)];
    const edges = buildRelEdges(relatives, null, active);
    expect(edges.find((e) => e.id === 'rel:1')?.data?.active).toBe(true); // to=2 → активно
    expect(edges.find((e) => e.id === 'rel:2')?.data?.active).toBe(false); // to=3 (done) → нет
    expect(edges.find((e) => e.id === 'rel:3')?.data?.active).toBe(false); // to=4 (pending) → нет
  });

  it('без activeRoleIds (пусто) — ни одно ребро не активно', () => {
    const relatives = [rel(1, 1, 2), rel(2, 1, 3)];
    const edges = buildRelEdges(relatives, null, computeActiveRoleIds([]));
    expect(edges.every((e) => e.data?.active === false)).toBe(true);
  });

  it('несколько in_progress — активны все ребра к этим ролям', () => {
    const tasks = [task(1, 2, 'in_progress'), task(2, 4, 'in_progress')];
    const relatives = [rel(1, 1, 2), rel(2, 1, 4), rel(3, 2, 4)];
    const edges = buildRelEdges(relatives, null, computeActiveRoleIds(tasks));
    expect(edges.find((e) => e.id === 'rel:1')?.data?.active).toBe(true);
    expect(edges.find((e) => e.id === 'rel:2')?.data?.active).toBe(true);
    expect(edges.find((e) => e.id === 'rel:3')?.data?.active).toBe(true); // to=4
  });
});

// (b) интеграция: TeamBuilderPage → activeRoleIds из api.tasks.list → props канваса
describe('W1.3 TeamBuilderPage: activeRoleIds из in_progress-задач роли', () => {
  beforeEach(() => {
    resetMockDb();
    vi.resetModules();
  });

  async function renderPageCapture(
    mutateDb?: (db: { tasks: Array<{ team_id: number; state: string }> }) => void,
  ) {
    // ловим props, переданные TopologyCanvas. Порядок важен: vi.resetModules()
    // (в beforeEach) → doMock → свежий import mock/data (модифицируем) → импорт App.
    const captured: { activeRoleIds?: Set<number> }[] = [];
    vi.doMock(
      '../src/components/Topology/TopologyCanvas',
      async (importOriginal) => {
        const actual = await importOriginal<typeof import('../src/components/Topology/TopologyCanvas')>();
        return {
          ...actual,
          default: (props: { activeRoleIds?: Set<number> }) => {
            captured.push({ activeRoleIds: props.activeRoleIds });
            return null;
          },
        };
      },
    );
    const { getDb } = await import('../src/api/mock/data');
    if (mutateDb) mutateDb(getDb());
    const { default: App } = await import('../src/App');
    const utils = render(
      <MemoryRouter initialEntries={['/teams/1']}>
        <App />
      </MemoryRouter>,
    );
    return { captured, ...utils };
  }

  it('seed (in_progress → роль 2) → канвас получает activeRoleIds с ролью 2', async () => {
    const { captured } = await renderPageCapture();
    await waitFor(
      () => {
        const last = captured[captured.length - 1];
        expect(last?.activeRoleIds).toBeDefined();
        // seed team 1: in_progress-задачи (#1, #3) — обе на destination_role_id=2
        expect(last!.activeRoleIds!.has(2)).toBe(true);
      },
      { timeout: 20_000 },
    );
  }, 40_000);

  it('все задачи done → activeRoleIds пусто (честно, без фейка)', async () => {
    const { captured } = await renderPageCapture((db) => {
      db.tasks.forEach((t) => {
        if (t.team_id === 1) t.state = 'done';
      });
    });
    await waitFor(
      () => {
        const last = captured[captured.length - 1];
        expect(last?.activeRoleIds).toBeDefined();
        expect(last!.activeRoleIds!.size).toBe(0);
      },
      { timeout: 20_000 },
    );
  }, 40_000);
});
