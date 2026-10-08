import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Badge } from '../src/components/ui/States';

describe('Badge (R5: единый status-badge, фикс битых template literals)', () => {
  it('kind=task: badge-{state}', () => {
    render(<Badge kind="task" value="in_progress" />);
    const el = screen.getByText('in_progress');
    expect(el.className).toBe('badge badge-in_progress');
  });
  it('kind=entity: badge-state-{state}', () => {
    render(<Badge kind="entity" value="active" />);
    expect(screen.getByText('active').className).toBe('badge badge-state-active');
  });
  it('kind=sev: badge-sev-{sev}, fallback info', () => {
    const { rerender } = render(<Badge kind="sev" value="critical" />);
    expect(screen.getByText('critical').className).toBe('badge badge-sev-critical');
    rerender(<Badge kind="sev" value={undefined} />);
    expect(screen.getByText('info').className).toBe('badge badge-sev-info');
  });
  it('kind=type: badge-type-{type} (LibraryPage ранее не работал — без backticks)', () => {
    render(<Badge kind="type" value="segment" />);
    expect(screen.getByText('segment').className).toBe('badge badge-type-segment');
  });
  it('kind=warn: badge-warn + children', () => {
    render(<Badge kind="warn">action required</Badge>);
    expect(screen.getByText('action required').className).toBe('badge badge-warn');
  });
  it('children переопределяют value', () => {
    render(<Badge kind="task" value="done">{`done · stale`}</Badge>);
    expect(screen.getByText('done · stale').className).toBe('badge badge-done');
  });
});
