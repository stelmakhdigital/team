import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import { Spinner, errorMessage } from './ui/States';
import { useToast } from './ui/Toast';
import { formatRelative } from '../lib/format';
import type { AckAction, Message } from '../types/api';

// P3.15: Mailbox (inbox/outbox) для роли — входящие с confirm (absorb/deny/drop) + исходящие.
// Mailbox-семантика: входящие требуют подтверждения (undelivered = pending).

function ackBadge(m: Message) {
  const s = m.ack_state ?? 'pending';
  return (
    <span className={`badge mb-ack mb-ack--${s}`} title={`ack_state: ${s}`}>{s}</span>
  );
}

export default function MailboxPanel({ roleId, roleName }: { roleId: number; roleName: string }) {
  const { toast } = useToast();
  const inbox = useQuery(`mailbox.inbox.${roleId}`, () => api.mailbox.getInbox(roleId, { limit: 50 }), [roleId]);
  const outbox = useQuery(`mailbox.outbox.${roleId}`, () => api.mailbox.getOutbox(roleId, { limit: 50 }), [roleId]);
  const ack = useMutation((msgId: number, action: AckAction) => api.mailbox.ack(msgId, action));

  const doAck = async (msgId: number, action: AckAction) => {
    try {
      await ack.mutate(msgId, action);
      toast('success', `Message ${action === 'absorb' ? 'absorbed' : action === 'deny' ? 'denied' : 'dropped'}`);
      inbox.refetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Ack failed');
    }
  };

  const items = inbox.data?.messages ?? [];
  const undelivered = inbox.data?.undelivered_count ?? 0;

  return (
    <div className="card mailbox-panel" data-testid="mailbox-panel">
      <div className="card-head">
        <h2>Mailbox — {roleName}</h2>
        {undelivered > 0 && (
          <span className="badge mb-undelivered" data-testid="undelivered-counter">
            {undelivered} undelivered
          </span>
        )}
      </div>

      <h3 className="mb-section">Inbox <span className="muted small">({items.length})</span></h3>
      {inbox.loading ? (
        <Spinner />
      ) : inbox.error ? (
        <p className="field-error" role="alert" style={{ color: 'var(--red)' }}>{errorMessage(inbox.error)}</p>
      ) : items.length === 0 ? (
        <p className="muted">Входящих direct-сообщений нет — они появятся, когда другой агент напишет этой роли.</p>
      ) : (
        <ul className="mb-list" data-testid="inbox-list">
          {items.map((m) => (
            <li
              key={m.id}
              className={`mb-item${(m.ack_state ?? 'pending') === 'pending' ? ' mb-item--pending' : ''}`}
              data-testid="inbox-item"
            >
              <div className="mb-item-head">
                <strong className="mb-from">{m.from_role_name ?? 'system'}</strong>
                {ackBadge(m)}
                <span className="muted small">{formatRelative(m.created_at)}</span>
              </div>
              <div className="mb-body">{m.body}</div>
              {(m.ack_state ?? 'pending') === 'pending' ? (
                <div className="mb-actions">
                  <button className="btn btn-small btn-primary" disabled={ack.pending} onClick={() => void doAck(m.id, 'absorb')}>✓ Absorb</button>
                  <button className="btn btn-small" disabled={ack.pending} onClick={() => void doAck(m.id, 'deny')}>✗ Deny</button>
                  <button className="btn btn-small" disabled={ack.pending} onClick={() => void doAck(m.id, 'drop')}>⌫ Drop</button>
                </div>
              ) : (
                <div className="muted small mb-acked">
                  acked by {m.acked_by ?? '—'}{m.acked_at ? ` ${formatRelative(m.acked_at)}` : ''}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      <h3 className="mb-section" style={{ marginTop: 16 }}>
        Outbox <span className="muted small">({outbox.data?.messages.length ?? 0})</span>
      </h3>
      {outbox.loading ? (
        <Spinner />
      ) : (outbox.data?.messages.length ?? 0) === 0 ? (
        <p className="muted">Исходящих сообщений нет — они появятся после отправки direct-сообщения.</p>
      ) : (
        <ul className="mb-list mb-list--outbox" data-testid="outbox-list">
          {outbox.data!.messages.map((m) => (
            <li key={m.id} className="mb-item">
              <div className="mb-item-head">
                <span className="muted small">→ {m.to_role_name ?? 'broadcast'}</span>
                <span className="muted small">{formatRelative(m.created_at)}</span>
              </div>
              <div className="mb-body">{m.body}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
