import type { ClosureReason, TaskState } from '../types/api';

/** Allowed task state transitions (mirrors backend slice 2 semantics). */
export const TASK_TRANSITIONS: Record<TaskState, TaskState[]> = {
  pending: ['in_progress', 'done', 'blocked', 'canceled'],
  in_progress: ['done', 'blocked', 'canceled'],
  blocked: ['pending', 'in_progress', 'done', 'canceled'],
  done: [],
  canceled: [],
};

export const CLOSURE_REASONS: ClosureReason[] = [
  'handed_off_to',
  'blocked_on',
  'denied',
  'canceled',
  'no_follow_on',
  'escalation',
];

export const isTerminalTaskState = (s: TaskState) => s === 'done' || s === 'canceled';

export const canTransition = (from: TaskState, to: TaskState) => TASK_TRANSITIONS[from].includes(to);
