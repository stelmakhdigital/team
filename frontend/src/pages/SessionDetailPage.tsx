import { Link, useParams } from 'react-router-dom';
import { useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { ErrorState, PageSkeleton, Spinner, isNotFoundError } from '../components/ui/States';
import HandoverModal from '../components/HandoverModal';
import CaptureModal from '../components/CaptureModal';
import { formatRelative } from '../lib/format';

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
  const trans = useQuery(`session.transcript.${id}`, () => api.history.getTranscript(id));
  const [handover, setHandover] = useState(false);
  const [capture, setCapture] = useState(false);

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
            <h2>Session #{s.id}</h2>
            <dl className="cfg-meta-grid">
              {metrics.map(([k, v]) => (
                <div key={k} className="cfg-meta-item"><dt>{k}</dt><dd>{v}</dd></div>
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

          <div className="card">
            <div className="card-head">
              <h2>Transcript ({transcript.length})</h2>
              <button type="button" className="btn btn-sm" onClick={() => setCapture(true)}>
                📷 Capture
              </button>
            </div>
            {trans.loading ? (
              <Spinner />
            ) : isNotFoundError(trans.error) ? (
              <p className="muted">No transcript available.</p>
            ) : transcript.length === 0 ? (
              <p className="muted">No messages.</p>
            ) : (
              <ul className="transcript-list">
                {transcript.map((m) => (
                  <li key={m.id} className={`transcript-msg transcript-${m.message_type}`}>
                    <div className="muted small">
                      {m.role ?? m.message_type}
                      {m.metadata?.tool_name ? ` · ${m.metadata.tool_name}` : ''}
                      {m.metadata?.tokens ? ` · ${m.metadata.tokens} tok` : ''}
                      {m.metadata?.model ? ` · ${m.metadata.model}` : ''} · {formatRelative(m.created_at)}
                    </div>
                    <pre className="transcript-content">{m.content}</pre>
                    {m.metadata?.tool_result && <pre className="transcript-content muted">{m.metadata.tool_result}</pre>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>

        <aside className="card detail-pane">
          <h2>History</h2>
          {hist.loading ? (
            <Spinner />
          ) : history.length === 0 ? (
            <p className="muted">No history events.</p>
          ) : (
            <ul className="hist-list">
              {history.map((h) => (
                <li key={h.id} className="hist-entry">
                  <div className="muted small">{formatRelative(h.created_at)} · {h.actor_type}{h.actor_role_name ? ` · ${h.actor_role_name}` : ''}</div>
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
      {capture && <CaptureModal sessionId={s.id} onClose={() => setCapture(false)} />}
    </div>
  );
}
