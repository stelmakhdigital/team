import { describe, expect, it, beforeEach, vi } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from '../src/App';
import { getDb, resetMockDb } from '../src/api/mock/data';
import SpecModal from '../src/components/SpecModal';
import AgentResourcesPanel from '../src/components/TeamBuilder/AgentResourcesPanel';
import type { AgentSpec } from '../src/types/api';

// Polish-волна (A1–A5): микрокопирайтинг (убить «+ New \w+»), Esc-консистентность
// (useEscapeStack) на модалках создания, ContextTree CTA → /projects?new=1,
// ProjectArtifacts Esc-flag, SpecModal download (createObjectURL).
const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = resolve(HERE, '..', 'src');

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

function walkTs(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const st = statSync(full);
    if (st.isDirectory()) walkTs(full, out);
    else if (entry.endsWith('.tsx') || entry.endsWith('.ts')) out.push(full);
  }
  return out;
}

const AGENT_SPEC: AgentSpec = {
  name: 'pi-worker',
  version: '1.0',
  runtime: { type: 'pi', version: '0.9' },
  pi_config: {
    model: 'claude-3-7-sonnet',
    temperature: 0.7,
    max_tokens: 8192,
    plugins: [{ name: 'base-plugin', enabled: true, config: {}, description: '' }],
    skills: [{ path: 'skills/base.md', enabled: true, description: '' }],
    hooks: [],
    mcp: { servers: [] },
  },
  resources: { cpu: '1.0', memory: '1G', gpu: false },
  startup: {
    files: [{ path: 'startup/base.md', orientation: 'context', delivery_hint: 'send_text' }],
    scripts: [],
  },
};

describe('A1: микрокопирайтинг — убить микс языка', () => {
  it('скан src: ноль «+ New \\w+» в пользовательских CTA', () => {
    const files = walkTs(SRC);
    expect(files.length).toBeGreaterThan(0);
    const hits: string[] = [];
    for (const f of files) {
      const content = readFileSync(f, 'utf8');
      for (const m of content.matchAll(/\+ New \w+/g)) hits.push(`${f}: ${m[0]}`);
    }
    expect(hits).toEqual([]);
  });

  it('TasksPage overdue-empty hint — статусы в кавычках, остальное по-русски', async () => {
    getDb().tasks = [];
    renderAt('/tasks');
    fireEvent.click(await screen.findByRole('tab', { name: /overdue/i }, { timeout: 10_000 }));
    expect(
      await screen.findByText('Все «in-progress» и «blocked» задачи в графике. ✓', {}, { timeout: 10_000 }),
    ).toBeInTheDocument();
  }, 20_000);
});

describe('A2: Esc закрывает модалки создания (useEscapeStack)', () => {
  beforeEach(() => {
    resetMockDb();
    localStorage.clear();
  });

  const targets: { name: string; path: string; heading: string; open: string; isOpen: () => boolean }[] = [
    { name: 'Projects', path: '/projects', heading: 'Projects', open: '+ Новый проект', isOpen: () => screen.queryByPlaceholderText('my-project') !== null },
    { name: 'Teams', path: '/teams', heading: 'Teams', open: '+ Новая команда', isOpen: () => screen.queryByPlaceholderText('e.g. Dev Team') !== null },
    { name: 'Workflows', path: '/workflows', heading: 'Workflows', open: '+ Новый workflow', isOpen: () => screen.queryByLabelText('Workflow name (create)') !== null },
    { name: 'Tasks', path: '/tasks', heading: 'Tasks', open: '+ Новая задача', isOpen: () => screen.queryByRole('heading', { name: 'New task' }) !== null },
  ];

  it.each(targets)('$name: dispatch Esc → модалка создания закрывается', async (t) => {
    renderAt(t.path);
    await screen.findByRole('heading', { name: t.heading }, { timeout: 15_000 });
    fireEvent.click(await screen.findByRole('button', { name: t.open }, { timeout: 10_000 }));
    // модалка открылась
    await waitFor(() => expect(t.isOpen()).toBe(true), { timeout: 10_000 });
    // Esc (стек: модалка — верхний оверлей) → закрытие
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(t.isOpen()).toBe(false), { timeout: 5000 });
  }, 30_000);

  it('AgentResourcesPanel: dispatch Esc → панель закрывается', async () => {
    // AgentResourcesPanel — оверлейная поверхность TeamBuilder (role-panel).
    // Проверяем напрямую: render-им с onClose-spy, Esc → onClose вызван.
    const onClose = vi.fn();
    render(
      <AgentResourcesPanelHost onClose={onClose} spec={AGENT_SPEC} />,
    );
    // панель открыта (видны секции Plugins/Skills)
    expect(screen.getByText('Plugins')).toBeInTheDocument();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });
});

// Хелпер: AgentResourcesPanel без полного TeamBuilder (roleId + roleSpec + config).
function AgentResourcesPanelHost({ onClose, spec }: { onClose: () => void; spec: AgentSpec }) {
  return <AgentResourcesPanel roleId={1} roleSpec={spec} initialConfig={{}} onClose={onClose} onSaved={() => {}} />;
}

describe('A3: ContextTree CTA → /projects?new=1', () => {
  beforeEach(() => resetMockDb());

  it('empty ContextTree: CTA ведёт на /projects?new=1', async () => {
    getDb().projects = [];
    renderAt('/');
    const cta = await screen.findByTestId('context-tree-create', {}, { timeout: 10_000 });
    expect(cta).toHaveAttribute('href', '/projects?new=1');
  }, 20_000);

  it('/projects?new=1 → форма создания проекта открыта', async () => {
    renderAt('/projects?new=1');
    // input name в форме создания
    expect(await screen.findByPlaceholderText('my-project', {}, { timeout: 15_000 })).toBeInTheDocument();
  }, 30_000);
});

describe('A4: ProjectArtifacts — Esc закрывает preview (active-flag)', () => {
  beforeEach(() => resetMockDb());

  it('Esc закрывает preview-модалку (когда preview открыт)', async () => {
    renderAt('/projects/1');
    await screen.findByText('Artifacts (файлы проекта)', {}, { timeout: 15_000 });
    const previewBtns = await screen.findAllByRole('button', { name: 'Preview' }, { timeout: 15_000 });
    fireEvent.click(previewBtns[0]);
    await screen.findByText(/Mock artifact content/, {}, { timeout: 10_000 });
    // Esc → preview закрывается (active = preview != null)
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByText(/Mock artifact content/)).toBeNull(), { timeout: 5000 });
  }, 30_000);

  it('без preview Esc не трогает компоненты (toolbar на месте)', async () => {
    renderAt('/projects/1');
    await screen.findByText('Artifacts (файлы проекта)', {}, { timeout: 15_000 });
    // preview закрыт → хук неактивен; Esc ничего не ломает
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.getByText('Artifacts (файлы проекта)')).toBeInTheDocument();
    expect(screen.queryByText(/Mock artifact content/)).toBeNull();
  }, 30_000);
});

describe('A5: SpecModal — Download (createObjectURL)', () => {
  beforeEach(() => resetMockDb());

  it('Download: createObjectURL вызван с yaml-контентом, anchor.download = name.yaml', async () => {
    const RealBlob = (globalThis as unknown as { Blob: typeof Blob }).Blob;
    let blobParts: unknown[] | undefined;
    const blobSpy = vi
      .spyOn(globalThis as unknown as { Blob: typeof Blob }, 'Blob')
      .mockImplementation(function (this: unknown, parts?: unknown[], opts?: BlobPropertyBag) {
        blobParts = parts;
        return new RealBlob(parts as BlobPart[], opts);
      });
    const createSpy = vi.fn(() => 'blob:mock');
    const revokeSpy = vi.fn();
    const realCreate = (URL as unknown as { createObjectURL?: unknown }).createObjectURL;
    const realRevoke = (URL as unknown as { revokeObjectURL?: unknown }).revokeObjectURL;
    (URL as unknown as { createObjectURL: unknown }).createObjectURL = createSpy;
    (URL as unknown as { revokeObjectURL: unknown }).revokeObjectURL = revokeSpy;
    let capturedAnchor: HTMLAnchorElement | null = null;
    const clickSpy = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(function (this: HTMLAnchorElement) {
        capturedAnchor = this;
      });

    try {
      render(<SpecModal name="pi-lead" onClose={() => {}} />);
      const download = await screen.findByTestId('spec-download', {}, { timeout: 5000 });
      fireEvent.click(download);

      expect(createSpy).toHaveBeenCalledTimes(1);
      expect(Array.isArray(blobParts) && (blobParts[0] as string)).toContain('name: pi-lead');
      expect(capturedAnchor).not.toBeNull();
      expect(capturedAnchor!.download).toBe('pi-lead.yaml');
      expect(revokeSpy).toHaveBeenCalledWith('blob:mock');
    } finally {
      blobSpy.mockRestore();
      (URL as unknown as { createObjectURL: unknown }).createObjectURL = realCreate;
      (URL as unknown as { revokeObjectURL: unknown }).revokeObjectURL = realRevoke;
      clickSpy.mockRestore();
    }
  }, 15_000);
});
