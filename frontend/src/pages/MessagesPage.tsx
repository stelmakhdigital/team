import { useState } from 'react';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { ErrorState, Spinner, Unavailable, isNotFoundError } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';

export default function MessagesPage() {
  const messages = useQuery('messages.list', () => api.messages.getMessages({ limit: 50 }));
  const chatrooms = useQuery('messages.chatrooms', () => api.messages.getChatrooms());
  const [activeRoom, setActiveRoom] = useState<number | null>(null);
  const roomMessages = useQuery(activeRoom ? `messages.room.${activeRoom}` : 'messages.room.none', () =>
    activeRoom ? api.messages.getChatroomMessages(activeRoom) : Promise.resolve({ messages: [], has_more: false }),
    [activeRoom],
  );
  const send = useMutation((body: string) => {
    if (!activeRoom) throw new Error('No chatroom selected');
    return api.messages.sendChatroomMessage(activeRoom, { body });
  });
  const { toast } = useToast();
  const [draft, setDraft] = useState('');

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!draft.trim()) return;
    try {
      await send.mutate(draft.trim());
      setDraft('');
      toast('success', 'Message sent');
      roomMessages.refetch();
      chatrooms.refetch();
    } catch {
      toast('error', 'Failed to send message');
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <h1>Message Center</h1>
      </div>
      <div className="messages-layout">
        <div className="card">
          <h2>Direct messages</h2>
          {messages.loading && <Spinner />}
          {messages.error &&
          (isNotFoundError(messages.error) ? (
            <Unavailable feature="Direct messages" hint="The messages endpoint is not implemented in the backend yet (planned)." />
          ) : (
            <ErrorState error={messages.error} onRetry={messages.refetch} />
          ))}
          {messages.data && messages.data.messages.length === 0 && <p className="muted">No messages yet.</p>}
          <ul className="message-list">
            {messages.data?.messages.map((m) => (
              <li key={m.id} className={`message${m.is_mine ? ' message-mine' : ''}${m.type === 'watchdog' ? ' message-watchdog' : ''}`}>
                <div className="message-head">
                  <strong>{m.from_role_name ?? 'system'}</strong>
                  {m.to_role_name && <span className="muted">→ {m.to_role_name}</span>}
                  <span className="badge">{m.type}</span>
                  <span className="muted small">{formatRelative(m.created_at)}</span>
                </div>
                <div className="message-body">{m.body}</div>
              </li>
            ))}
          </ul>
        </div>

        <div className="card">
          <h2>Chatrooms</h2>
          {chatrooms.loading && <Spinner />}
          {chatrooms.error &&
          (isNotFoundError(chatrooms.error) ? (
            <Unavailable feature="Chatrooms" hint="The chatrooms endpoint is not implemented in the backend yet (planned)." />
          ) : (
            <ErrorState error={chatrooms.error} onRetry={chatrooms.refetch} />
          ))}
          <ul className="chatroom-list">
            {chatrooms.data?.chatrooms.map((c) => (
              <li key={c.id}>
                <button className="chatroom" onClick={() => setActiveRoom(c.id)} aria-pressed={activeRoom === c.id}>
                  <div>
                    <strong>{c.name}</strong>
                    {c.topic && <div className="muted small">{c.topic}</div>}
                  </div>
                  <div className="muted small">
                    {c.last_message ? `${c.last_message.from_role_name}: ${c.last_message.body.slice(0, 40)}` : 'no messages'}
                    {c.unread_count > 0 && <span className="badge badge-warn">{c.unread_count} new</span>}
                  </div>
                </button>
              </li>
            ))}
          </ul>

          {activeRoom !== null && (
            <div className="chat-view">
              <h3>#{chatrooms.data?.chatrooms.find((c) => c.id === activeRoom)?.name ?? activeRoom}</h3>
              <div className="chat-scroll">
                {roomMessages.loading && <Spinner />}
                {roomMessages.data?.messages.map((m) => (
                  <div key={m.id} className={`chat-bubble${m.is_mine ? ' chat-mine' : ''}`}>
                    <div className="muted small">
                      {m.from_role_name} · {formatRelative(m.created_at)}
                    </div>
                    {m.body}
                  </div>
                ))}
              </div>
              <form onSubmit={submit} className="chat-form">
                <input value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Type a message…" aria-label="Message" />
                <button type="submit" className="btn btn-primary" disabled={send.pending || !draft.trim()}>
                  Send
                </button>
              </form>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
