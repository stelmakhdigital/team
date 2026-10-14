import { Link, useParams } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { CopyBtn } from '../components/ui/CopyBtn';
import { Time } from '../components/ui/Time';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useWebSocket } from '../hooks/useWebSocket';
import { ErrorState, PageSkeleton, Spinner, isNotFoundError } from '../components/ui/States';
import HandoverModal from '../components/HandoverModal';
import CaptureModal from '../components/CaptureModal';
import SendToSession from '../components/SendToSession';
import WalkPanel from '../components/WalkPanel';
import PosturePanel from '../components/PosturePanel';
import SnapshotModal from '../components/SnapshotModal';
import {formatCost} from '../lib/format';

// U4b: session drill-down — session metrics + history + transcript (messages), fetch-by-id.
function fmtTokens(n?: number): string {
  if (n === undefined) return '--';
  if (n >= 1000) return `${Math.round(n / 100) / 10}k`;
  return String(n);
}

export default function SessionDetailPage() {
  const { sessionId } = useParams();
  const id = Number(sessionId);
  const sess = useQuery(`session.${id}`, () => api.sessions.get(id));
  const hist = useQuery(`session.history.${id}`, () => api.history.getSessionHistory(id));
  // W5.5: транскрипт — последние N (default 50), «подгрузить ещё» увеличивает limit
  const [transLimit, setTransLimit] = useState(50);
  const trans = useQuery(`session.transcript.${id}.${transLimit}`, () => api.history.getTranscript(id, { limit: transLimit }), [transLimit]);
  // W5.3: контекст задачи для CaptureModal «→ Evidence» — активная задача этой роли
  // (сессии в модели не знают task_id; берём активную задачу destination_role = роль сессии)
  const sessData = sess.data;
  const teamTasks = useQuery(
    sessData ? `session.tasks.${sessData.team_id}` : 'session.tasks.none',
    () => (sessData ? api.tasks.list({ team_id: sessData.team_id }) : Promise.resolve({ tasks: [], total: 0 })),
    [sessData?.team_id],
  );
  const candidateTask = sessData
    ? (teamTasks.data?.tasks ?? []).find(
        (t) => t.destination_role_id === sessData.role_id && ['pending', 'in_progress', 'blocked'].includes(t.state),
      )
    : undefined;
  const [handover, setHandover] = useState(false);
  const [capture, setCapture] = useState(false);
  const [snapshotOpen, setSnapshotOpen] = useState(false); // P3.12

  // W5.5: панель «Live output» — WS session:{id}, последние 20 строк, prepend
  // (паттерн RoleNode live-терминала; mock-пул шлёт session.output для сессии 1)
  const [liveLines, setLiveLines] = useState<string[]>([]);
  const { onMessage: onWs } = useWebSocket([`session:${id}`]);
  useEffect(() => {
    onWs((m) => {
      if (m.type !== 'session.output') return;
      const d = (m.data ?? {}) as { session_id?: number; lines?: Array<{ text?: string }> };
      if (d.session_id !== id) return;
      const texts = (d.lines ?? []).map((l) => l.text ?? '').filter(Boolean);
      if (texts.length === 0) return;
      setLiveLines((prev) => [...texts.reverse(), ...prev].slice(0, 20));
    });
    return () => onWs(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  if (sess.loading) return <div className="page"><PageSkeleton label="Loading session…" /></div>;
  if (sess.error) return <div className="page"><ErrorState error={sess.error} onRetry={sess.refetch} /></div>;

  const s = sess.data!;
  const history = hist.data?.history ?? [];
  const transcript = trans.data?.transcript ?? [];

  const contextAtWall = s.context_used_percentage !== undefined && s.context_used_percentage >= 80;
  const canHandover = ['running', 'starting', 'idle', 'stopping'].includes(s.state);

  const metrics: Array<[string, string]> = [
    ['State', s.state],
    ['Runtime', s.runtime_type],
    ...(s.model ? ([['Model', s.model]] as Array<[string, string]>) : []),
    ['Context', s.context_used_percentage !== undefined ? `${Math.round(s.context_used_percentage)}%` : '--'],
    ['Tokens', `${fmtTokens(s.context_total_input_tokens)} / ${fmtTokens(s.context_total_output_tokens)}`],
    ['Cost', formatCost(s.estimated_cost)], // P2.14 (оценка; «нет данных» если нет)
    ...(s.log_path ? ([['Log', s.log_path]] as Array<[string, string]>) : []),
    ...(s.runtime_ref ? ([['Ref', s.runtime_ref]] as Array<[string, string]>) : []),
  ];

  return (
    <div className="page">
      <div className="page-head">
        <h1>
          <Link to="/" className="crumb">Dashboard</Link> / Session <code>#{s.id}</code> ({s.role_name})
        </h1>
        <span className="badge">{s.state}</span>
      </div>

      {contextAtWall && (
        <div className="context-wall-banner">
          <div>
            <strong>Сессия у контекст-стены ({Math.round(s.context_used_percentage!)}%).</strong>{' '}
            Продолжите работу в новой сессии той же роли с переданным контекстом.
          </div>
          {canHandover && (
            <button type="button" className="btn btn-primary btn-sm" onClick={() => setHandover(true)}>
              Продолжить в новой сессии
            </button>
          )}
        </div>
      )}

      <div className="detail-layout">
        <div className="detail-main">
          <div className="card">
            <div className="h-inline">
              <h2>Session #{s.id}</h2>
              <CopyBtn text={String(s.id)} label="ID сессии" />
            </div>
            <dl className="cfg-meta-grid">
              {metrics.map(([k, v]) => (
                <div key={k} className="cfg-meta-item">
                  <dt>{k}</dt>
                  <dd>
                    {v}
                    {(k === 'Log' || k === 'Ref') && (
                      <CopyBtn text={v} label={`поле ${k}`} />
                    )}
                  </dd>
                </div>
              ))}
            </dl>
          </div>

          {s.briefing_applied ? (
            <div className="card briefing-card">
              <h2>Briefing (startup context)</h2>
              <div className="briefing-delivery">
                <span className="badge badge--entity">{s.briefing_applied}</span>
                {s.briefing_path ? <span className="muted small"> · {s.briefing_path}</span> : null}
              </div>
              {s.briefing_text ? (
                <pre className="briefing-text">{s.briefing_text}</pre>
              ) : (
                <p className="muted">Брифинг сформирован (текст в {s.briefing_path || 'конфиге сессии'}).</p>
              )}
            </div>
          ) : null}

          {(s.runtime_type === 'tmux' || s.runtime_type === 'pi') && (
            <SendToSession sessionId={s.id} />
          )}

          {/* W5.5: live-вывод сессии (WS session:{id}, prepend, последние 20 строк) */}
          <div className="card live-output-card" data-testid="live-output">
            <h2>Live output</h2>
            {liveLines.length === 0 ? (
              <p className="muted">Пока нет вывода — строки появятся по мере работы агента.</p>
            ) : (
              <pre className="live-output">{liveLines.join('\n')}</pre>
            )}
          </div>

          <WalkPanel sessionId={s.id} />

          <PosturePanel kind="session" id={s.id} current={s.posture ?? null} teamPosture={s.team_posture} />

          <div className="card">
            <div className="card-head">
              <h2>Transcript ({transcript.length})</h2>
              <div className="wf-config-actions">
                <button type="button" className="btn btn-sm" onClick={() => setCapture(true)}>
                  ⎙ Capture
                </button>
                <button type="button" className="btn btn-sm" onClick={() => setSnapshotOpen(true)} title="Save this session's agent config as a reusable image">
                  ▣ Agent image
                </button>
              </div>
            </div>
            {trans.loading ? (
              <Spinner />
            ) : isNotFoundError(trans.error) ? (
              <p className="muted">Транскрипта пока нет — появится, когда агент начнёт работу.</p>
            ) : transcript.length === 0 ? (
              <p className="muted">В транскрипте пока нет сообщений.</p>
            ) : (
              <ul className="transcript-list">
                {transcript.map((m) => (
                  <li key={m.id} className={`transcript-msg transcript-${m.message_type}`}>
                    <div className="muted small">
                      {m.role ?? m.message_type}
                      {m.metadata?.tool_name ? ` · ${m.metadata.tool_name}` : ''}
                      {m.metadata?.tokens ? ` · ${m.metadata.tokens} tok` : ''}
                      {m.metadata?.model ? ` · ${m.metadata.model}` : ''} · <Time iso={m.created_at}/>
                    </div>
                    <pre className="transcript-content">{m.content}</pre>
                    {m.metadata?.tool_result && <pre className="transcript-content muted">{m.metadata.tool_result}</pre>}
                  </li>
                ))}
              </ul>
            )}
            {trans.data?.has_more ? (
              <div className="show-more">
                <button className="btn" onClick={() => setTransLimit((l) => l + 50)}>
                  Подгрузить ещё
                </button>
              </div>
            ) : null}
          </div>
        </div>

        <aside className="card detail-pane">
          <h2>History</h2>
          {hist.loading ? (
            <Spinner />
          ) : history.length === 0 ? (
            <p className="muted">Истории пока нет — переходы состояния появятся по мере работы.</p>
          ) : (
            <ul className="hist-list">
              {history.map((h) => (
                <li key={h.id} className="hist-entry">
                  <div className="muted small"><Time iso={h.created_at}/> · {h.actor_type}{h.actor_role_name ? ` · ${h.actor_role_name}` : ''}</div>
                  <div className="hist-transition">
                    {h.from_state && <span className="muted">{h.from_state} →</span>} <strong>{h.to_state}</strong>
                  </div>
                  {h.comment && <div className="small">{h.comment}</div>}
                </li>
              ))}
            </ul>
          )}
        </aside>
      </div>

      {handover && <HandoverModal sessionId={s.id} onClose={() => { setHandover(false); sess.refetch(); }} />}
      {capture && <CaptureModal sessionId={s.id} taskId={candidateTask?.id} onClose={() => setCapture(false)} />}
      {snapshotOpen && (
        <SnapshotModal
          sessionId={s.id}
          defaultName={`agent-${s.role_name.replace(/\s+/g, '-').toLowerCase()}`}
          onClose={() => setSnapshotOpen(false)}
        />
      )}
    </div>
  );
}
