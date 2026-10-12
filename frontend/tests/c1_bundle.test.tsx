import { describe, expect, it, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { resetMockDb, getDb } from '../src/api/mock/data';
import type { TeamBundle } from '../src/types/api';

// C1/P4.3 — Team bundle: export (download) + import (file → preview → confirm).
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

let lastBlob: Blob | null = null;
beforeEach(() => {
  resetMockDb();
  localStorage.clear();
  lastBlob = null;
  URL.createObjectURL = vi.fn((b: Blob) => {
    lastBlob = b;
    return 'blob:mock';
  });
  URL.revokeObjectURL = vi.fn();
});

function bundleFixture(overrides?: Partial<TeamBundle>): TeamBundle {
  const base: TeamBundle = {
    version: 1,
    kind: 'team-bundle',
    team: {
      name: 'Dev Team',
      description: 'Imported team',
      segments: [
        {
          name: 'Backend',
          roles: [
            { name: 'Lead', role: 'lead', agent_spec: 'pi-go-backend' },
            { name: 'Worker', agent_spec: 'pi-go-worker' },
          ],
        },
        { name: 'Review', roles: [{ name: 'Reviewer', agent_spec: 'pi-reviewer' }] },
      ],
      relatives: [{ from: 'Backend.Lead', to: 'Backend.Worker', kind: 'delegates_to', label: null }],
      layout: {
        segments: [{ segment_id: 0, position: { x: 10, y: 20, width: 420, height: 260 }, collapsed: false }],
        roles: [{ role_id: 0, segment_id: 0, position: { x: 100, y: 120 } }],
        relatives: [{ relative_id: 0, from_role_id: 0, to_role_id: 1 }],
      },
    },
    workflows: [{ name: 'Build & Review', description: null, config: { blocks: [], connections: [] }, engine_enabled: true }],
  };
  return { ...base, ...overrides };
}

async function pickBundleFile(content: string, name = 'dev.teambundle.json') {
  const input = screen.getByLabelText('Team bundle file') as HTMLInputElement;
  const file = new File([content], name, { type: 'application/json' });
  fireEvent.change(input, { target: { files: [file] } });
  // модалка появляется асинхронно (FileReader)
  return screen.findByRole('dialog', { name: 'Import team bundle' }, { timeout: 10_000 });
}

// jsdom-Blob без .text() — читаем через FileReader
function readBlob(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader();
    fr.onload = () => resolve(String(fr.result ?? ''));
    fr.onerror = () => reject(fr.error);
    fr.readAsText(blob);
  });
}

describe('C1: team bundle export', () => {
  it('TeamsPage: Export-клик → download JSON с bundle-структурой', async () => {
    renderAt('/teams');
    // ждём список
    const link = await screen.findByText('Dev Team', {}, { timeout: 10_000 });
    const row = link.closest('tr')!;
    const exportBtn = Array.from(row.querySelectorAll('button')).find((b) => b.textContent === 'Export');
    expect(exportBtn).toBeTruthy();
    fireEvent.click(exportBtn!);
    await waitFor(() => expect(lastBlob).not.toBeNull(), { timeout: 10_000 });
    const parsed = JSON.parse(await readBlob(lastBlob!)) as TeamBundle;
    expect(parsed.version).toBe(1);
    expect(parsed.kind).toBe('team-bundle');
    expect(parsed.team.name).toBe('Dev Team');
    expect(parsed.team.segments.length).toBe(2);
    expect(parsed.team.segments[0].roles.length).toBe(3);
    expect(parsed.team.segments[0].roles[0]).toMatchObject({ name: 'Lead', agent_spec: 'pi-go-backend' });
    // profile в seed не задан → role: null
    expect(parsed.team.segments[0].roles[0].role).toBeNull();
    // config роли не несёт layout
    expect((parsed.team.segments[0].roles[0].config ?? {}).layout).toBeUndefined();
    // relatives — адресный формат
    expect(parsed.team.relatives[0].from).toBe('Backend.Lead');
    expect(parsed.team.relatives[0].kind).toBe('delegates_to');
    // workflows команды
    expect(parsed.workflows.length).toBe(2);
    expect(parsed.workflows[0]).toMatchObject({ name: 'Build & Review', engine_enabled: true });
    expect(Array.isArray(parsed.workflows[0].config.blocks)).toBe(true);
  }, 20_000);

  it('TeamBuilder: Export в toolbar → download текущего билда', async () => {
    renderAt('/teams/1');
    await screen.findByText('Lead', {}, { timeout: 10_000 });
    const exportBtn = screen.getByRole('button', { name: /Export/ });
    fireEvent.click(exportBtn);
    await waitFor(() => expect(lastBlob).not.toBeNull(), { timeout: 10_000 });
    const parsed = JSON.parse(await readBlob(lastBlob!)) as TeamBundle;
    expect(parsed.team.name).toBe('Dev Team');
    expect(parsed.version).toBe(1);
  }, 20_000);
});

describe('C1: team bundle import', () => {
  it('file → preview (имя/segments/roles/workflows + заметка про проект) → confirm → навигация на новую команду (auto-suffix из ответа)', async () => {
    renderAt('/teams');
    await screen.findByText('Dev Team', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Import/ }));
    const modal = await pickBundleFile(JSON.stringify(bundleFixture()));
    expect(modal).toHaveTextContent('Dev Team');
    expect(modal).toHaveTextContent('Segments');
    expect(modal).toHaveTextContent('Workflows');
    expect(modal).toHaveTextContent('Проект не переносится (будет назначен после)');
    // counts: 2 сегмента, 3 роли, 1 воркфлоу
    const dds = Array.from(modal.querySelectorAll('dd'));
    expect(dds.map((d) => d.textContent)).toEqual(['Dev Team', '2', '3', '1']);
    // confirm
    fireEvent.click(screen.getByRole('button', { name: 'Import team' }));
    // 'Dev Team' уже есть → auto-suffix: новая команда 'Dev Team (2)', навигация на неё
    const newTeam = await screen.findByText(/Dev Team \(2\)/, {}, { timeout: 10_000 });
    expect(newTeam).toBeInTheDocument();
    // новая команда в mock-бд с топологией
    const db = getDb();
    const team = db.teams.find((t) => t.name === 'Dev Team (2)')!;
    expect(team).toBeTruthy();
    expect(db.roles.filter((r) => r.team_id === team.id).length).toBe(3);
    expect(db.workflows.filter((w) => w.team_id === team.id)[0]).toMatchObject({
      name: 'Build & Review',
      state: 'draft',
      engine_enabled: true,
    });
    // layout перенесён (позиция сегмента 10/20)
    const seg = db.segments.find((s) => s.team_id === team.id && s.name === 'Backend')!;
    const lv = db.layouts!.segments.find((l) => l.segment_id === seg.id)!;
    expect(lv.position).toEqual({ x: 10, y: 20, width: 420, height: 260 });
  }, 20_000);

  it('ошибка валидации (relative на несуществующую роль) → список field-ошибок в модалке', async () => {
    renderAt('/teams');
    await screen.findByText('Dev Team', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Import/ }));
    const bad = bundleFixture();
    bad.team.relatives.push({ from: 'Nope.Ghost', to: 'Backend.Lead', kind: 'delegates_to', label: null });
    await pickBundleFile(JSON.stringify(bad));
    fireEvent.click(await screen.findByRole('button', { name: 'Import team' }, { timeout: 5000 }));
    const errs = await screen.findByRole('alert', {}, { timeout: 10_000 });
    expect(errs).toHaveTextContent('team.relatives[1].from');
    expect(errs).toHaveTextContent('unknown role address Nope.Ghost');
    // команда НЕ создана
    const db = getDb();
    expect(db.teams.some((t) => t.name === 'Dev Team (2)')).toBe(false);
  }, 20_000);

  it('некорректный JSON → parse-ошибка в модалке (без создания команды)', async () => {
    renderAt('/teams');
    await screen.findByText('Dev Team', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Import/ }));
    await pickBundleFile('{ это не json');
    expect(await screen.findByText(/Некорректный JSON/, {}, { timeout: 10_000 })).toBeInTheDocument();  });

  it('version ≠ 1 → ошибка unsupported_version (тост, команда не создана)', async () => {
    renderAt('/teams');
    await screen.findByText('Dev Team', {}, { timeout: 10_000 });
    fireEvent.click(screen.getByRole('button', { name: /Import/ }));
    const v2 = bundleFixture();
    v2.version = 2;
    await pickBundleFile(JSON.stringify(v2));
    fireEvent.click(await screen.findByRole('button', { name: 'Import team' }, { timeout: 5000 }));
    // ошибка показана в модалке (сырое сообщение unsupported_version)
    const alert = await screen.findByRole('alert', {}, { timeout: 10_000 });
    expect(alert).toHaveTextContent('unsupported bundle version 2');
    // команда НЕ создана
    const db = getDb();
    expect(db.teams.some((t) => t.name === 'Dev Team (2)')).toBe(false);
  }, 20_000);
});
