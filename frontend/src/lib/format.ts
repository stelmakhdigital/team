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
