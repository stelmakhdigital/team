import { useState, useRef, useCallback, useMemo, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useWebSocket } from '../hooks/useWebSocket';
import CaptureModal from '../components/CaptureModal';
import { Badge, EmptyState, ErrorState, PageSkeleton, Spinner } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';
import { formatDateTime, stateTitle } from '../lib/format';
import type { SessionState } from '../types/api';

const STATE_FILTERS: (SessionState | 'all')[] = ['all', 'starting', 'running', 'idle', 'stopping', 'stopped', 'failed'];

// T3: экран /sessions — таблица списка сессий (id, role, team, state, runtime,
// model, started) + фильтры (team, state, role) + live-обновление по WS.
// Клик по сессии → /sessions/{id} (существующая detail).
export default function SessionsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initial = searchParams.get('state');
  const [teamFilter, setTeamFilter] = useState<number | 'all'>(
    searchParams.get('team') && searchParams.get('team') !== 'all' ? Number(searchParams.get('team')) : 'all',
  );
  const [stateFilter, setStateFilter] = useState<SessionState | 'all'>(
    initial && initial !== 'all' ? (initial as SessionState) : 'all',
  );
  const [roleFilter, setRoleFilter] = useState<number | 'all'>(
    searchParams.get('role') && searchParams.get('role') !== 'all' ? Number(searchParams.get('role')) : 'all',
  );
  // B-2 + W5.5: URL — одна точка правды (state/team/role), ссылки дашборда работают
  useEffect(() => {
    const s = searchParams.get('state');
    const t = searchParams.get('team');
    const r = searchParams.get('role');
    setStateFilter(s && s !== 'all' ? (s as SessionState) : 'all');
    setTeamFilter(t && t !== 'all' ? Number(t) : 'all');
    setRoleFilter(r && r !== 'all' ? Number(r) : 'all');
  }, [searchParams]);

  // W5.5: фильтры → URL (replace, чтобы не растить историю)
  const syncUrl = (team: number | 'all', state: SessionState | 'all', role: number | 'all') => {
    const p = new URLSearchParams();
    if (team !== 'all') p.set('team', String(team));
    if (state !== 'all') p.set('state', state);
    if (role !== 'all') p.set('role', String(role));
    setSearchParams(p, { replace: true });
  };

  const teams = useQuery('sessions.teams', () => api.teams.getTeams());
  const roles = useQuery(
    teamFilter !== 'all' ? `sessions.roles.${teamFilter}` : 'sessions.roles.none',
    () => (teamFilter !== 'all' ? api.teams.getTeam(teamFilter) : Promise.resolve(null)),
    [teamFilter],
  );

  const list = useQuery(
    `sessions.list.${teamFilter}.${stateFilter}.${roleFilter}`,
    () =>
      api.sessions.list({
        team_id: teamFilter === 'all' ? undefined : teamFilter,
        state: stateFilter === 'all' ? undefined : stateFilter,
        role_id: roleFilter === 'all' ? undefined : roleFilter,
      }),
    [teamFilter, stateFilter, roleFilter],
  );

  // live-refetch по WS session.-событиям (стандарт X3, как AttentionPage/TasksPage).
  const { onMessage } = useWebSocket([]);
  const listRef = useRef(list.refetch);
  listRef.current = list.refetch;
  const wsTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const wsLast = useRef(0);
  const onWsEvent = useCallback((m: { type: string }) => {
    if (!m.type.startsWith('session.')) return;
    const now = Date.now();
    if (now - wsLast.current < 3000) return;
    wsLast.current = now;
    if (wsTimer.current) clearTimeout(wsTimer.current);
    wsTimer.current = setTimeout(() => listRef.current(), 300);
  }, []);
  onMessage(onWsEvent);

  const [visible, setVisible] = useState(25);
  // W5.3: capture из строки списка — кнопка ⎙ открывает CaptureModal
  const [captureId, setCaptureId] = useState<number | null>(null);
  // B-1: сортировка по свежести (новые сверху) — старые тестовые сессии опускаются вниз.
  const sessions = useMemo(
    () =>
      [...(list.data?.sessions ?? [])].sort(
        (a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''),
      ),
    [list.data],
  );
  const shown = sessions.slice(0, visible);
  const teamRoles = (teamFilter !== 'all' ? roles.data?.roles ?? [] : []);

  return (
    <div className="page">
      <div className="page-head">
        <h1>Sessions</h1>
        <span className="muted small">{list.data?.total ?? 0} всего</span>
      </div>

      <div className="filters">
        <label>
          Team
          <select
            aria-label="Team (filter)"
            value={String(teamFilter)}
            onChange={(e) => {
              const t = e.target.value === 'all' ? 'all' : Number(e.target.value);
              setTeamFilter(t);
              setRoleFilter('all');
              syncUrl(t, stateFilter, 'all');
            }}
          >
            <option value="all">All teams</option>
            {teams.data?.teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          State
          <select
            aria-label="State (filter)"
            value={stateFilter}
            onChange={(e) => {
              const s = e.target.value as SessionState | 'all';
              setStateFilter(s);
              syncUrl(teamFilter, s, roleFilter);
            }}
          >
            {STATE_FILTERS.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </label>
        <label>
          Role
          <select
            aria-label="Role (filter)"
            value={String(roleFilter)}
            onChange={(e) => {
              const r = e.target.value === 'all' ? 'all' : Number(e.target.value);
              setRoleFilter(r);
              syncUrl(teamFilter, stateFilter, r);
            }}
            disabled={teamFilter === 'all'}
            title={teamFilter === 'all' ? 'Select a team to filter by role' : undefined}
          >
            <option value="all">All roles</option>
            {teamRoles.map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}
              </option>
            ))}
          </select>
        </label>
      </div>

      {/* W5.5: ссылка на поиск по транскриптам с предзаполненным role_id */}
      {roleFilter !== 'all' && (
        <div className="muted small" style={{ marginTop: -8, marginBottom: 12 }}>
          <EntityLink
            to={`/transcript-search?team_id=${teamFilter}&role_id=${roleFilter}`}
            title="Открыть поиск по транскриптам этой роли"
            className=""
          >
            Искать в транскриптах →
          </EntityLink>
        </div>
      )}
      {list.loading && <PageSkeleton label="Загружаю сессии…" />}
      {list.error && <ErrorState error={list.error} onRetry={list.refetch} />}
      {list.data && sessions.length === 0 && (
        <div className="card">
          <EmptyState
            title={teamFilter === 'all' && stateFilter === 'all' ? 'Нет сессий' : 'Нет сессий по заданным фильтрам'}
            hint="Сессии появятся, когда вы запустите агента в команде — или сбросьте фильтры выше."
          />
        </div>
      )}
      {list.data && sessions.length > 0 && (
        <div className="card">
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>#</th>
                  <th>Role</th>
                  <th>Team</th>
                  <th>State</th>
                  <th>Runtime</th>
                  <th>Model</th>
                  <th>Started</th>
                  <th aria-label="Capture">⎙</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((s) => (
                  <tr key={s.id}>
                    <td className="muted">
                      <EntityLink to={`/sessions/${s.id}`} title={`Open session ${s.id}`}>
                        {s.id}
                      </EntityLink>
                    </td>
                    <td>
                      <EntityLink to={`/sessions/${s.id}`} title={`Open session ${s.id}`}>
                        {s.role_name}
                      </EntityLink>
                    </td>
                    <td className="muted">
                      <EntityLink to={`/teams/${s.team_id}`} title="Open team">
                        {s.team_name}
                      </EntityLink>
                    </td>
                    <td>
                      <Badge kind="task" value={s.state} title={stateTitle(s.state)}>
                        {s.state}
                      </Badge>
                    </td>
                    <td className="muted">{s.runtime_type}</td>
                    <td className="muted">{s.model ?? '—'}</td>
                    <td className="muted">{s.created_at ? formatDateTime(s.created_at) : '—'}</td>
                    <td>
                      <button
                        type="button"
                        className="btn btn-sm"
                        aria-label={`Capture session ${s.id}`}
                        title="Стл-кадр экрана сессии"
                        onClick={() => setCaptureId(s.id)}
                      >
                        ⎙
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {sessions.length > visible && (
            <div className="show-more">
              <button className="btn" onClick={() => setVisible((v) => v + 25)}>
                Показать ещё ({sessions.length - visible} осталось)
              </button>
            </div>
          )}
        </div>
      )}
      {roles.loading && teamFilter !== 'all' && <Spinner label="Loading roles…" />}
      {captureId != null && <CaptureModal sessionId={captureId} onClose={() => setCaptureId(null)} />}
    </div>
  );
}
