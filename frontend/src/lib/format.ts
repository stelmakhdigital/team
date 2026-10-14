export function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString(undefined, {
    year: undefined,
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function formatRelative(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso).getTime();
  if (Number.isNaN(d)) return '—';
  const diff = Date.now() - d;
  const min = Math.round(diff / 60_000);
  if (min < 1) return 'just now';
  if (min < 60) return `${min}m ago`;
  const h = Math.round(min / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

// W11: разделитель дат для лент («Сегодня», «Вчера», «12 октября»).
const DAY_FMT = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' });

export function dayLabel(iso?: string): string {
  const d = new Date(iso ?? '');
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const s = d.toDateString();
  if (s === now.toDateString()) return 'Сегодня';
  if (s === new Date(now.getTime() - 86_400_000).toDateString()) return 'Вчера';
  return DAY_FMT.format(d);
}

// P0.2: осталось до дедлайна (секунды) → "45m" / "3h" / "2d" (или просрочка)
export function formatRelativeFromSecs(secs: number): string {
  const neg = secs < 0;
  const s = Math.abs(secs);
  const min = Math.round(s / 60);
  let core: string;
  if (min < 1) core = '<1m';
  else if (min < 60) core = `${min}m`;
  else if (min < 24 * 60) core = `${Math.round(min / 60)}h`;
  else core = `${Math.round(min / (24 * 60))}d`;
  return neg ? `${core} overdue` : core;
}

export function formatUptime(seconds?: number): string {
  if (seconds === undefined || seconds < 0) return '—';
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export function formatBytes(bytes?: number): string {
  if (bytes === undefined) return '—';
  const mb = bytes / (1024 * 1024);
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${Math.round(mb)} MB`;
}

// P2.14: стоимость (USD) → "~$7.50" / "$7.50" / "нет данных" (честно, не 0).
// cost = null (нет данных) → «нет данных»; estimate → «~» (оценка).
export interface CostInfo {
  cost_usd: number | null;
  has_data: boolean;
  estimate: boolean;
}

export function formatCost(cost?: CostInfo | null): string {
  if (!cost || !cost.has_data || cost.cost_usd === null || cost.cost_usd === undefined) {
    return 'нет данных';
  }
  const usd = `$${cost.cost_usd.toFixed(2)}`;
  return cost.estimate ? `~${usd}` : usd;
}

// B-3: подсказки (title) к state-бейджам — что значит состояние.
const STATE_TITLES: Record<string, string> = {
  // сессии
  starting: 'Agent is starting up (runtime launching).',
  running: 'Agent is actively working.',
  idle: 'Agent is waiting for work.',
  stopping: 'Agent is being stopped.',
  stopped: 'Agent has stopped (completed or stopped by us).',
  failed: 'Agent exited with an error — open the session to see the transcript.',
  // задачи
  pending: 'Task is waiting to be picked up by an agent.',
  in_progress: 'Task is being worked on.',
  blocked: 'Task is blocked (waiting on a dependency or a person).',
  done: 'Task is complete.',
  cancelled: 'Task was cancelled.',
  // команды / workflow
  active: 'Active (in use).',
  archived: 'Archived (inactive, hidden from default lists).',
  draft: 'Draft (not yet enabled).',
  enabled: 'Enabled (running).',
  disabled: 'Disabled (not running).',
};
export function stateTitle(state?: string): string | undefined {
  return state ? STATE_TITLES[state] : undefined;
}
