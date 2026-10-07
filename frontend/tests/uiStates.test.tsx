import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import TaskList from '../src/components/Dashboard/TaskList';
import { ErrorState, EmptyState, Spinner } from '../src/components/ui/States';

describe('UI states', () => {
  it('TaskList renders empty state when no tasks', () => {
    render(<TaskList tasks={[]} />);
    expect(screen.getByText('No active tasks')).toBeInTheDocument();
  });

  it('TaskList renders rows with state badges', () => {
    render(
      <TaskList
        tasks={[
          {
            id: 1,
            team_id: 1,
            team_name: 'T',
            title: 'Do thing',
            state: 'in_progress',
            priority: 1,
            destination_role_id: 1,
            destination_role_name: 'Worker',
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
            progress: 40,
          },
        ]}
      />,
    );
    expect(screen.getByText('Do thing')).toBeInTheDocument();
    expect(screen.getByText('Worker')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '40');
  });

  it('ErrorState shows friendly unauthorized message with retry', () => {
    const { getByRole } = render(<ErrorState error={{ status: 401, code: 'unauthorized', message: 'x' }} onRetry={() => {}} />);
    expect(screen.getByText('No access')).toBeInTheDocument();
    expect(getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('Spinner renders a status role', () => {
    const { container } = render(<Spinner label="Loading…" />);
    expect(container.querySelector('[role="status"]')).not.toBeNull();
  });

  it('EmptyState renders hint', () => {
    render(<EmptyState title="Nothing" hint="Wait" />);
    expect(screen.getByText('Wait')).toBeInTheDocument();
  });
});
