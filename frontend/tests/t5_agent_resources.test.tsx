import { describe, expect, it, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import AgentResourcesPanel from '../src/components/TeamBuilder/AgentResourcesPanel';
import { resetMockDb, getDb } from '../src/api/mock/data';
import type { AgentSpec } from '../src/types/api';

// T5 — Agent resources: отдельная панель (plugins/skills/startup_files), base vs override,
// add/edit/remove + persist override в role.Config["agent_resources"].
const baseSpec: AgentSpec = {
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

describe('T5: Agent resources', () => {
  beforeEach(() => {
    resetMockDb();
  });

  it('renders base entries (plugins/skills/startup) with base source badges', () => {
    render(
      <AgentResourcesPanel roleId={1} roleSpec={baseSpec} initialConfig={{}} onClose={() => {}} onSaved={() => {}} />,
    );
    expect(screen.getByDisplayValue('skills/base.md')).toBeInTheDocument();
    expect(screen.getByDisplayValue('base-plugin')).toBeInTheDocument();
    expect(screen.getByDisplayValue('startup/base.md')).toBeInTheDocument();
    // source-бейджи «base» (base-записи)
    expect(screen.getAllByText('base').length).toBeGreaterThanOrEqual(3);
  });

  it('adding a skill persists the override to role.Config["agent_resources"]', async () => {
    const db = getDb();
    const role = db.roles[0];
    render(
      <AgentResourcesPanel roleId={role.id} roleSpec={baseSpec} initialConfig={{}} onClose={() => {}} onSaved={() => {}} />,
    );
    // добавляем новый скилл
    fireEvent.click(screen.getByRole('button', { name: '+ Add skill' }));
    const skillInputs = screen.getAllByLabelText(/^Skill \d+ path$/);
    fireEvent.change(skillInputs[skillInputs.length - 1], { target: { value: 'skills/new.md' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    // override персистится в role.config.agent_resources.skills
    await waitFor(
      () => {
        const ar = role.config?.agent_resources as { skills?: { path: string }[] } | undefined;
        expect(ar?.skills?.some((s) => s.path === 'skills/new.md')).toBe(true);
      },
      { timeout: 10_000 },
    );
  }, 20_000);

  it('shows base vs override source (a modified base entry becomes override)', () => {
    // initialConfig с override'ом для base-скилла → source = override
    const initial = {
      agent_resources: {
        skills: [{ path: 'skills/base.md', enabled: false }],
      },
    };
    render(
      <AgentResourcesPanel roleId={1} roleSpec={baseSpec} initialConfig={initial} onClose={() => {}} onSaved={() => {}} />,
    );
    // base-скилл теперь override (enabled=false из override)
    expect(screen.getAllByText('override').length).toBeGreaterThanOrEqual(1);
    // переключатель base-скилла выключен (override enabled=false)
    const skillInput = screen.getByDisplayValue('skills/base.md');
    const row = skillInput.closest('.agent-res-row');
    expect(row?.querySelector('.agent-res-src-override')).toBeInTheDocument();
  });
});
