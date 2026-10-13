import { useCallback, useRef, useState } from 'react';
import { useQuery } from '../hooks/useQuery';
import { api } from '../api';
import type { EventItem, WSServerMessage } from '../types/api';
import { Badge, EmptyState, ErrorState, PageSkeleton } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';
import { useWebSocket } from '../hooks/useWebSocket';

// P2.9 — Pulse: единая лента событий системы (задачи/сессии/сообщения/алерты/
// воркфлоу) с фильтрами, историей из API + live-prepend по WS (те же события).

// Релевантные WS-события (session.output — live-поток, не событие — НЕ в ленту).
const PULSE_EVENTS = new Set([
  'task.created', 'task.state_changed', 'session.started', 'session.stopped',
  'message.sent', 'alert.created', 'workflow.instance_updated',
]);
const EVENT_TYPES = [
  'task.created', 'task.state_changed', 'session.started', 'session.stopped',
  'message.sent', 'alert.created', 'workflow.instance_updated',
];

// type → категория-бейдж (цвет)
const TYPE_KIND: Record<string, string> = {
  'task.created': 'task', 'task.state_changed': 'task',
  'session.started': 'session', 'session.stopped': 'session',
  'message.sent': 'message', 'alert.created': 'alert', 'workflow.instance_updated': 'workflow',
};

function entityLink(ev: EventItem): string {
  switch (ev.entity_kind) {
    case 'task': return `/tasks/${ev.entity_id}`;
    case 'session': return `/sessions/${ev.entity_id}`;
    case 'message': return '/messages';
    case 'alert': return '/attention';
    case 'workflow': return '/workflows';
    default: return '/';
  }
}
function entityLabel(ev: EventItem): string {
  if (ev.entity_title) return ev.entity_title;
  switch (ev.entity_kind) {
    case 'task': return `Task #${ev.entity_id}`;
    case 'session': return `Session #${ev.entity_id}`;
    case 'message': return 'Messages';
    case 'alert': return 'Attention';
    case 'workflow': return 'Workflows';
    default: return ev.entity_kind + (ev.entity_id ? ` #${ev.entity_id}` : '');
  }
}
function humanElapsed(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 0) return 'сейчас';
  const m = Math.round(ms / 60000);
  if (m < 1) return 'только что';
  if (m < 60) return `${m}м назад`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}ч назад`;
  return `${Math.floor(h / 24)}д назад`;
}

// WS-событие → EventItem (для live-prepend). session.output/прочее → null.
let liveSeq = 1;
function wsToEvent(m: WSServerMessage): EventItem | null {
  const d = (m.data ?? {}) as Record<string, any>;
  const id64 = (v: unknown): number => (typeof v === 'number' ? v : 0);
  const s = (v: unknown): string => (typeof v === 'string' ? v : '');
  let kind = '';
  let eid = 0;
  let summary: string = m.type;
  switch (m.type) {
    case 'task.created':
      kind = 'task'; eid = id64(d.task_id);
      summary = s(d.title) ? `Task #${eid} created: ${s(d.title)}` : `Task #${eid} created`; break;
    case 'task.state_changed':
      kind = 'task'; eid = id64(d.task_id);
      summary = s(d.to_state) ? `Task #${eid} → ${s(d.to_state)}` : `Task #${eid} state changed`; break;
    case 'session.started':
      kind = 'session'; eid = id64(d.session_id);
      summary = `Session #${eid} started${s(d.role_name) ? ` (${s(d.role_name)})` : ''}`; break;
    case 'session.stopped':
      kind = 'session'; eid = id64(d.session_id);
      summary = `Session #${eid} stopped`; break;
    case 'message.sent':
      kind = 'message'; eid = id64(d.message_id);
      summary = `Message${s(d.from_role_name) ? ` from ${s(d.from_role_name)}` : ''}`; break;
    case 'alert.created':
      kind = 'alert'; eid = id64(d.alert_id);
      summary = `Alert [${s(d.severity)}] ${s(d.event_type)}`; break;
    case 'workflow.instance_updated':
      kind = 'workflow'; eid = id64(d.instance_id);
      summary = `Workflow instance #${eid}${s(d.state) ? ` → ${s(d.state)}` : ''}`; break;
    default:
      return null;
  }
  return {
    id: -liveSeq--,
    type: m.type,
    team_id: id64(d.team_id) || null,
    role_id: id64(d.role_id) || null,
    role_name: s(d.role_name) || null,
    entity_kind: kind,
    entity_id: eid,
    entity_title: s(d.title) || null,
    summary,
    created_at: m.timestamp || new Date().toISOString(),
  };
}

function distinctRoles(events: EventItem[]): { id: number; name: string }[] {
  const seen = new Map<number, string>();
  for (const e of events) {
    if (e.role_id != null && e.role_name) seen.set(e.role_id, e.role_name);
  }
  return [...seen.entries()].map(([id, name]) => ({ id, name }));
}

export default function PulsePage() {
  const [typeF, setTypeF] = useState('');
  const [teamF, setTeamF] = useState('');
  const [roleF, setRoleF] = useState('');
  const key = `pulse.${typeF}.${teamF}.${roleF}`;
  const q = useQuery(key, () =>
    api.events.list({
      type: typeF || undefined,
      team_id: teamF ? Number(teamF) : undefined,
      role_id: roleF ? Number(roleF) : undefined,
    }),
  );
  const teamsQ = useQuery('pulse.teams', () => api.teams.getTeams());
  const [live, setLive] = useState<EventItem[]>([]);
  const { onMessage } = useWebSocket(['dashboard']);

  // live-prepend по WS (W5.1): события копятся в ref-буфере и сбрасываются одним
  // setState по debounce 300мс — всплеск событий в окне дебаунса не теряется
  // (раньше был discard-throttle: всё после первого события молча выбрасывалось).
  // Нет событий → буфер не растёт и flush не запускается.
  const buffer = useRef<EventItem[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const onEvent = useCallback((m: WSServerMessage) => {
    if (!PULSE_EVENTS.has(m.type)) return;
    const ev = wsToEvent(m);
    if (!ev) return;
    buffer.current.push(ev);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const buf = buffer.current;
      buffer.current = [];
      if (buf.length === 0) return;
      // prepend всего буфера одним setState; новейший сверху (буфер в порядке прихода)
      setLive((prev) => [...buf.reverse(), ...prev].slice(0, 50));
    }, 300);
  }, []);
  onMessage(onEvent);

  if (q.loading) {
    return <div className="page"><PageSkeleton label="Загружаю ленту событий…" /></div>;
  }
  if (q.error) {
    return <div className="page"><ErrorState error={q.error} onRetry={q.refetch} /></div>;
  }

  // feed = live (новые) + api (история), dedup по (type+entity+created_at).
  // live-события тоже прогоняются через активные фильтры (они client-side).
  const apiEvents = q.data?.events ?? [];
  const teamNum = teamF ? Number(teamF) : null;
  const roleNum = roleF ? Number(roleF) : null;
  const liveFiltered = live.filter(
    (ev) =>
      (!typeF || ev.type === typeF) &&
      (teamNum == null || ev.team_id === teamNum) &&
      (roleNum == null || ev.role_id === roleNum),
  );
  const seen = new Set<string>();
  const feed: EventItem[] = [];
  for (const ev of [...liveFiltered, ...apiEvents]) {
    const k = `${ev.type}:${ev.entity_kind}:${ev.entity_id}:${ev.created_at}`;
    if (seen.has(k)) continue;
    seen.add(k);
    feed.push(ev);
  }
  const teams = teamsQ.data?.teams ?? [];
  const roles = distinctRoles(apiEvents);

  return (
    <div className="page">
      <div className="page-head">
        <h1>Pulse</h1>
        <span className="muted small">Единая лента событий · {feed.length} в ленте · {q.data?.total ?? 0} всего</span>
      </div>

      <div className="card pulse-filters">
        <label className="pulse-filter">
          Тип
          <select value={typeF} onChange={(e) => setTypeF(e.target.value)}>
            <option value="">Все типы</option>
            {EVENT_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
          </select>
        </label>
        <label className="pulse-filter">
          Команда
          <select value={teamF} onChange={(e) => setTeamF(e.target.value)}>
            <option value="">Все команды</option>
            {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
        </label>
        <label className="pulse-filter">
          Роль
          <select value={roleF} onChange={(e) => setRoleF(e.target.value)}>
            <option value="">Все роли</option>
            {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
          </select>
        </label>
      </div>

      {feed.length === 0 ? (
        <div className="card">
          <EmptyState
            title="Событий нет"
            hint="Здесь появится лента событий системы: задачи, сессии, сообщения, алерты, воркфлоу."
          />
        </div>
      ) : (
        <div className="card pulse-feed">
          <ul className="pulse-list">
            {feed.map((ev) => (
              <li key={`${ev.id}:${ev.created_at}`} className="pulse-item">
                <Badge kind="type" value={TYPE_KIND[ev.type] ?? 'info'}>{ev.type}</Badge>
                <div className="pulse-summary">
                  <span className="pulse-text">{ev.summary}</span>
                  {ev.entity_id > 0 ? (
                    <EntityLink to={entityLink(ev)} title={entityLabel(ev)} className="pulse-entity">
                      {entityLabel(ev)}
                    </EntityLink>
                  ) : null}
                  {ev.team_name ? <span className="muted small"> · {ev.team_name}</span> : null}
                </div>
                <span className="muted small pulse-time">{humanElapsed(ev.created_at)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
