import { Fragment, useState, useEffect } from 'react';
import { Time } from '../components/ui/Time';
import { useSearchParams } from 'react-router-dom';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, PageSkeleton, Unavailable, isNotFoundError , Badge } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';
import {formatDateTime} from '../lib/format';

// W5.6 — History: селектор команды/роли → задачи/сессии (раньше — только первая
// из списка), URL-sync (?team=/?role=/?task=/?session=). Audit log — без изменений.
export default function HistoryPage() {
  const audit = useQuery('history.audit', () => api.history.getAuditLog({ limit: 50 }));
  const teams = useQuery('history.teams', () => api.teams.getTeams());
  const allTasks = useQuery('history.tasks', () => api.dashboard.getTasks());
  const allSessions = useQuery('history.sessions', () => api.sessions.list());

  // селекторы: team/role → фильтруют списки; task/session — из URL или первая отфильтрованная
  const [searchParams, setSearchParams] = useSearchParams();
  const [teamId, setTeamId] = useState<number | ''>(
    searchParams.get('team') ? Number(searchParams.get('team')) : '',
  );
  const [roleId, setRoleId] = useState<number | ''>(
    searchParams.get('role') ? Number(searchParams.get('role')) : '',
  );

  const tasks = (allTasks.data?.tasks ?? []).filter(
    (t) => (teamId === '' || t.team_id === teamId) && (roleId === '' || t.destination_role_id === roleId),
  );
  const sessions = (allSessions.data?.sessions ?? []).filter(
    (s) => (teamId === '' || s.team_id === teamId) && (roleId === '' || s.role_id === roleId),
  );

  const urlTask = searchParams.get('task') ? Number(searchParams.get('task')) : null;
  const urlSession = searchParams.get('session') ? Number(searchParams.get('session')) : null;
  const taskId = urlTask ?? tasks[0]?.id ?? null;
  const sessionId = urlSession ?? sessions[0]?.id ?? null;

  // URL-sync: селекты пишут ?team=/?role=/?task=/?session= (replace)
  const syncUrl = (patch: { team?: number | ''; role?: number | ''; task?: number | null; session?: number | null }) => {
    const p = new URLSearchParams();
    const team = patch.team !== undefined ? patch.team : teamId;
    const role = patch.role !== undefined ? patch.role : roleId;
    const task = patch.task !== undefined ? patch.task : taskId;
    const session = patch.session !== undefined ? patch.session : sessionId;
    if (team !== '') p.set('team', String(team));
    if (role !== '') p.set('role', String(role));
    if (task != null) p.set('task', String(task));
    if (session != null) p.set('session', String(session));
    setSearchParams(p, { replace: true });
  };

  // выбранный id вне отфильтрованного списка (URL руками правили) → фильтры не блокируют
  const taskOptions = taskId != null && !tasks.some((t) => t.id === taskId)
    ? [...tasks, ...(allTasks.data?.tasks ?? []).filter((t) => t.id === taskId)]
    : tasks;
  const sessionOptions = sessionId != null && !sessions.some((s) => s.id === sessionId)
    ? [...sessions, ...(allSessions.data?.sessions ?? []).filter((s) => s.id === sessionId)]
    : sessions;

  // роль-имена для лейблов селектов (из сессий/задач не всегда понятно) — из команд
  const taskHistory = useQuery(
    taskId != null ? `history.task.${taskId}` : 'history.task.none',
    taskId != null ? () => api.history.getTaskHistory(taskId) : async () => ({ history: [], total: 0 }),
    [taskId],
  );
  const transcript = useQuery(
    sessionId != null ? `history.transcript.${sessionId}` : 'history.transcript.none',
    sessionId != null ? () => api.history.getTranscript(sessionId) : async () => ({ transcript: [], total: 0, has_more: false }),
    [sessionId],
  );

  // если выбранная сущность исчезла из отфильтрованных (сменили фильтр) — сброс URL-параметров
  useEffect(() => {
    if (urlTask != null && tasks.length > 0 && !tasks.some((t) => t.id === urlTask)) {
      syncUrl({ task: null });
    }
    if (urlSession != null && sessions.length > 0 && !sessions.some((s) => s.id === urlSession)) {
      syncUrl({ session: null });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlTask, urlSession, tasks.length, sessions.length]);

  const [tab, setTab] = useState<'audit' | 'task' | 'transcript'>('audit');
  const [expandedAudit, setExpandedAudit] = useState<number | null>(null);

  const selectedSession = sessionOptions.find((s) => s.id === sessionId);

  return (
    <div className="page">
      <div className="page-head">
        <h1>History</h1>
      </div>

      {/* W5.6: селектор команды/роли → задачи/сессии */}
      <div className="filters history-selectors">
        <label>
          Команда
          <select
            aria-label="История: команда"
            value={teamId === '' ? '' : String(teamId)}
            onChange={(e) => {
              const t = e.target.value === '' ? '' : Number(e.target.value);
              setTeamId(t);
              syncUrl({ team: t, task: null, session: null });
            }}
          >
            <option value="">— все —</option>
            {(teams.data?.teams ?? []).map((tm) => (
              <option key={tm.id} value={tm.id}>{tm.name}</option>
            ))}
          </select>
        </label>
        <label>
          Роль
          <select
            aria-label="История: роль"
            value={roleId === '' ? '' : String(roleId)}
            onChange={(e) => {
              const r = e.target.value === '' ? '' : Number(e.target.value);
              setRoleId(r);
              syncUrl({ role: r, task: null, session: null });
            }}
          >
            <option value="">— все —</option>
            {[...new Map(sessionOptions.map((s) => [s.role_id, s.role_name])).entries()].map(([rid, rname]) => (
              <option key={rid} value={rid}>{rname}</option>
            ))}
          </select>
        </label>
        <label>
          Задача
          <select
            aria-label="История: задача"
            value={taskId == null ? '' : String(taskId)}
            onChange={(e) => {
              const t = e.target.value === '' ? null : Number(e.target.value);
              syncUrl({ task: t });
            }}
          >
            {taskOptions.length === 0 && <option value="">Задач нет</option>}
            {taskOptions.map((t) => (
              <option key={t.id} value={t.id}>#{t.id} {t.title}</option>
            ))}
          </select>
        </label>
        <label>
          Сессия
          <select
            aria-label="История: сессия"
            value={sessionId == null ? '' : String(sessionId)}
            onChange={(e) => {
              const s = e.target.value === '' ? null : Number(e.target.value);
              syncUrl({ session: s });
            }}
          >
            {sessionOptions.length === 0 && <option value="">Сессий нет</option>}
            {sessionOptions.map((s) => (
              <option key={s.id} value={s.id}>#{s.id} {s.role_name}</option>
            ))}
          </select>
        </label>
      </div>

      <div className="tabs" role="tablist">
        {(
          [
            ['audit', 'Audit log'],
            ['task', taskId != null ? `Task #${taskId} history` : 'Task history'],
            ['transcript', sessionId != null ? `Session #${sessionId} transcript` : 'Session transcript'],
          ] as const
        ).map(([key, label]) => (
          <button key={key} role="tab" aria-selected={tab === key} className={`tab${tab === key ? ' active' : ''}`} onClick={() => setTab(key)}>
            {label}
          </button>
        ))}
        {/* W5.6: транскрипт-поиск с предзаполненной ролью */}
        {selectedSession && (
          <EntityLink
            to={`/transcript-search?team_id=${selectedSession.team_id}&role_id=${selectedSession.role_id}`}
            title="Открыть поиск по транскриптам"
            className=""
          >
            Искать в транскриптах →
          </EntityLink>
        )}
      </div>

      {tab === 'audit' && (
        <>
          {audit.loading && <PageSkeleton label="Loading audit log…" />}
          {audit.error &&
          (isNotFoundError(audit.error) ? (
            <Unavailable feature="Audit log" hint="The audit endpoint is not implemented in the backend yet (planned)." />
          ) : (
            <ErrorState error={audit.error} onRetry={audit.refetch} />
          ))}
          {audit.data && audit.data.entries.length === 0 && <EmptyState title="Журнала аудита пока нет" hint="Записи появятся после действий в системе (создание команд, задач, запуски сессий)." />}
          {audit.data && audit.data.entries.length > 0 && (
            <div className="card">
              <div className="table-wrap"><table className="table">
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>User</th>
                    <th>Action</th>
                    <th>Resource</th>
                    <th>Severity</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.data.entries.map((e) => (
                    <Fragment key={e.id}>
                      <tr className={expandedAudit === e.id ? 'row-expanded' : undefined}>
                        <td className="muted">
                          <button className="row-toggle" onClick={() => setExpandedAudit(expandedAudit === e.id ? null : e.id)} aria-expanded={expandedAudit === e.id} title="Show operation details">
                            {expandedAudit === e.id ? '▾' : '▸'} {formatDateTime(e.timestamp)}
                          </button>
                        </td>
                        <td>{e.user_name ?? '—'}</td>
                        <td>
                          <code>{e.action}</code>
                        </td>
                        <td className="muted">{e.resource ?? '—'}</td>
                        <td>
                          <Badge kind="sev" value={e.severity ?? 'info'} />
                        </td>
                      </tr>
                      {expandedAudit === e.id && (
                        <tr><td colSpan={5} className="row-detail">
                          <div className="audit-detail">
                            <div><strong>Actor:</strong> {e.user_name ?? '—'}{e.user_id ? ` (user #${e.user_id})` : ''}{e.api_key_id ? ` · key #${e.api_key_id}` : ''}</div>
                            {e.ip_address && <div><strong>IP:</strong> {e.ip_address}</div>}
                            {e.user_agent && <div><strong>Agent:</strong> {e.user_agent}</div>}
                            {e.details && <><h4>Operation details</h4><pre className="spec-pre">{JSON.stringify(e.details, null, 2)}</pre></>}
                          </div>
                        </td></tr>
                      )}
                    </Fragment>
                  ))}
                </tbody>
              </table></div>
            </div>
          )}
        </>
      )}

      {tab === 'task' && (
        <>
          {taskHistory.loading && <PageSkeleton label="Loading task history…" rows={5} />}
          {taskHistory.error && <ErrorState error={taskHistory.error} onRetry={taskHistory.refetch} />}
          {taskHistory.data && taskHistory.data.history.length === 0 && <EmptyState title="История задач пуста" hint="Выберите задачу — её переходы между состояниями появятся здесь." />}
          {taskHistory.data && taskHistory.data.history.length > 0 && (
            <div className="card">
              <h2>Task #{taskId} timeline</h2>
              <ol className="timeline">
                {taskHistory.data.history.map((h) => (
                  <li key={h.id} className={`timeline-item tl-${h.color ?? 'blue'}`}>
                    <span className="timeline-icon" aria-hidden="true">
                      {h.icon ?? '•'}
                    </span>
                    <div>
                      <strong>
                        {h.from_state ? `${h.from_state} → ${h.to_state}` : h.to_state}
                      </strong>{' '}
                      <span className="muted small">
                        by {h.actor_type}
                        {h.actor_role_name ? ` (${h.actor_role_name})` : ''} · <Time iso={h.created_at}/>
                      </span>
                      {h.comment && <div className="muted">{h.comment}</div>}
                    </div>
                  </li>
                ))}
              </ol>
            </div>
          )}
        </>
      )}

      {tab === 'transcript' && (
        <>
          {transcript.loading && <PageSkeleton label="Loading transcript…" rows={5} />}
          {transcript.error && <ErrorState error={transcript.error} onRetry={transcript.refetch} />}
          {transcript.data && transcript.data.transcript.length === 0 && <EmptyState title="Транскрипта пока нет" hint="События сессии появятся здесь, когда агент начнёт работу." />}
          {transcript.data && transcript.data.transcript.length > 0 && (
            <div className="card">
              <h2>Session #{sessionId} transcript</h2>
              {transcript.data.transcript.map((t) => (
                <div key={t.id} className={`transcript-entry ts-${t.message_type}`}>
                  <div className="muted small">
                    {t.message_type}
                    {t.metadata?.model ? ` · ${t.metadata.model}` : ''}
                    {t.metadata?.tokens ? ` · ${t.metadata.tokens} tokens` : ''}
                    {t.metadata?.latency_ms ? ` · ${t.metadata.latency_ms}ms` : ''}
                  </div>
                  <div>{t.content}</div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
