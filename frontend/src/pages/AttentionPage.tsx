import { useCallback, useRef } from 'react';
import { useQuery } from '../hooks/useQuery';
import { api } from '../api';
import { Badge, EmptyState, ErrorState, PageSkeleton } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';
import HumanRequestActions from '../components/HumanRequestActions';
import { useWebSocket } from '../hooks/useWebSocket';
import type { AttentionItem } from '../types/api';

// P0.1 — Attention view «Что требует меня»: единый список всего, что требует
// действия оператора, с live-обновлением по WS-событиям (task./session./alert.).
const ATTENTION_EVENTS = new Set([
  'task.created',
  'task.state_changed',
  'session.started',
  'session.stopped',
  'session.failed',
  'alert.created',
  'human_request.parked', // P3.16: pending sensitive-op (human-led)
  'human_request.decided',
]);

// «normal» отображаем существующим accent-классом (badge-sev-info), чтобы не
// вводить новый цвет (контраст — в scripts/contrast-check.mjs).
const SEV_BADGE: Record<string, string> = { critical: 'critical', high: 'high', normal: 'info' };

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

export default function AttentionPage() {
  const q = useQuery('attention.list', () => api.attention.list());
  const { onMessage } = useWebSocket([]);

  // live-рефетч по релевантным событиям (debounce ~3с + 300мс)
  const refetchRef = useRef(q.refetch);
  refetchRef.current = q.refetch;
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const last = useRef(0);
  const onEvent = useCallback((_m: { type: string }) => {
    if (!ATTENTION_EVENTS.has(_m.type)) return;
    const now = Date.now();
    if (now - last.current < 3000) return;
    last.current = now;
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => refetchRef.current(), 300);
  }, []);
  onMessage(onEvent);

  if (q.loading) {
    return (
      <div className="page">
        <PageSkeleton label="Загружаю список внимания…" />
      </div>
    );
  }
  if (q.error) {
    return (
      <div className="page">
        <ErrorState error={q.error} onRetry={q.refetch} />
      </div>
    );
  }

  const items = q.data?.items ?? [];
  // W2.3: группировка — «Решения» (human_request, approve/deny CTA) и
  // «События» (остальное, read-only CTA). Пустая группа скрывается.
  const decisions = items.filter((i) => i.kind === 'human_request');
  const events = items.filter((i) => i.kind !== 'human_request');

  const renderRows = (list: AttentionItem[]) =>
    list.map((it) => (
      <tr key={it.kind + it.id} data-severity={it.severity}>
        <td>
          <Badge kind="sev" value={SEV_BADGE[it.severity]}>
            {it.severity}
          </Badge>
        </td>
        <td>
          <div className="attention-title">{it.title}</div>
          <div className="muted small attention-detail">{it.detail}</div>
        </td>
        <td className="muted small attention-elapsed">{humanElapsed(it.created_at)}</td>
        <td className="attention-action">
          {it.kind === 'human_request' ? (
            <HumanRequestActions
              id={parseInt(it.id.replace('hreq-', ''), 10)}
              operation={it.title}
              onDecided={() => q.refetch()}
            />
          ) : (
            // T3: «Suggested action» — действие (ссылка на entity), без
            // отдельной безымянной колонки «Открыть».
            <EntityLink to={it.link.path} title={`${it.suggested_action} → ${it.link.label}`}>
              {it.suggested_action}
            </EntityLink>
          )}
        </td>
      </tr>
    ));

  const thead = (
    <thead>
      <tr>
        <th className="atd-w-sev">Severity</th>
        <th>Item</th>
        <th className="atd-w-elapsed">Elapsed</th>
        <th className="atd-w-action">Suggested action</th>
      </tr>
    </thead>
  );

  return (
    <div className="page">
      <div className="page-head">
        <h1>Внимание</h1>
        <span className="muted small">
          Что требует вашего действия · {q.data?.total ?? 0} шт.
        </span>
      </div>

      {items.length === 0 ? (
        <div className="card">
          <EmptyState
            title="Всё спокойно"
            hint="Ничего не требует вашего внимания. Новые эскалации, блокировки и алерты появятся здесь."
          />
        </div>
      ) : (
        <div className="card attention-card">
          {decisions.length > 0 && (
            <section className="attention-group" data-testid="attention-group-decisions">
              <h2 className="attention-group-title">
                Решения <span className="attention-group-count">{decisions.length}</span>
              </h2>
              <div className="table-wrap">
                <table className="table attention-table">
                  {thead}
                  <tbody>{renderRows(decisions)}</tbody>
                </table>
              </div>
            </section>
          )}
          {events.length > 0 && (
            <section className="attention-group" data-testid="attention-group-events">
              <h2 className="attention-group-title">
                События <span className="attention-group-count">{events.length}</span>
              </h2>
              <div className="table-wrap">
                <table className="table attention-table">
                  {thead}
                  <tbody>{renderRows(events)}</tbody>
                </table>
              </div>
            </section>
          )}
        </div>
      )}
    </div>
  );
}
