import { useState, useMemo } from 'react';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { Badge, EmptyState, ErrorState, Spinner, Unavailable, isNotFoundError } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';
import MailboxPanel from '../components/MailboxPanel';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';

// T3: 2-панная раскладка (как в мессенджере): слева — диалоги, сгруппированные в
// «Direct» и «Rooms»; справа — сообщения выбранного диалога + ввод. Direct — по
// P3.15 (role inbox/outbox) если доступно; Rooms — чат-румы.
type Dialog = { kind: 'room'; id: number } | { kind: 'direct'; roleId: number; roleName: string };

export default function MessagesPage() {
  const messages = useQuery('messages.list', () => api.messages.getMessages({ limit: 200 }));
  const chatrooms = useQuery('messages.chatrooms', () => api.messages.getChatrooms());
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const activeRoom = dialog?.kind === 'room' ? dialog.id : null;

  const roomMessages = useQuery(activeRoom ? `messages.room.${activeRoom}` : 'messages.room.none', () =>
    activeRoom ? api.messages.getChatroomMessages(activeRoom) : Promise.resolve({ messages: [], has_more: false }),
    [activeRoom],
  );
  const send = useMutation((body: string) => {
    if (activeRoom == null) throw new Error('No chatroom selected');
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

  // Direct-диалоги: группируем direct-сообщения по отправителю (from_role).
  const directConversations = (() => {
    const map = new Map<number, { roleId: number; roleName: string; count: number }>();
    for (const m of messages.data?.messages ?? []) {
      const id = m.from_role_id ?? 0;
      const name = m.from_role_name ?? 'system';
      const cur = map.get(id) ?? { roleId: id, roleName: name, count: 0 };
      cur.count += 1;
      map.set(id, cur);
    }
    return [...map.values()].sort((a, b) => b.count - a.count);
  })();

  const directUnavailable = messages.error != null && isNotFoundError(messages.error);
  const directMessages = dialog?.kind === 'direct'
    ? (messages.data?.messages ?? []).filter((m) => m.from_role_id === dialog.roleId)
    : [];

  const activeChatroom = chatrooms.data?.chatrooms.find((c) => c.id === activeRoom);
  const [roomsVisible, setRoomsVisible] = useState(15);
  const [roomSearch, setRoomSearch] = useState('');
  // B-1: сортировка комнат по свежести (активные сверху), без сообщений — вниз.
  const roomsSorted = useMemo(
    () =>
      [...(chatrooms.data?.chatrooms ?? [])].sort((a, b) =>
        (b.last_message?.created_at ?? '').localeCompare(a.last_message?.created_at ?? ''),
      ),
    [chatrooms.data],
  );
  // B-2: поиск комнат по имени (30+ комнат — фильтруем).
  const roomsFiltered = useMemo(
    () => roomsSorted.filter((c) => c.name.toLowerCase().includes(roomSearch.trim().toLowerCase())),
    [roomsSorted, roomSearch],
  );
  const roomsShown = roomsFiltered.slice(0, roomsVisible);

  return (
    <div className="page">
      <div className="page-head">
        <h1>Message Center</h1>
      </div>
      {/* P3.15: Mailbox (role inbox/outbox + ack) */}
      <MailboxSection />
      <div className="messages-2panel">
        {/* ЛЕВАЯ ПАНЕЛЬ: диалоги (Direct + Rooms) */}
        <div className="card dialog-list">
          <div className="dialog-section">
            <h3 className="dialog-section-title">Direct</h3>
            {messages.loading && <Spinner />}
            {directUnavailable ? (
              <Unavailable feature="Direct messages" hint="Direct-сообщения появятся позже (P3.15 mailbox выше)." />
            ) : messages.error ? (
              <ErrorState error={messages.error} onRetry={messages.refetch} />
            ) : directConversations.length === 0 ? (
              <p className="muted small dialog-empty-section">Direct-диалогов пока нет — они работают: напишите роли в mailbox, диалог появится здесь.</p>
            ) : (
              <ul className="dialog-list-items">
                {directConversations.map((c) => (
                  <li key={c.roleId}>
                    <button
                      className={`dialog-item${dialog?.kind === 'direct' && dialog.roleId === c.roleId ? ' dialog-item-active' : ''}`}
                      onClick={() => setDialog({ kind: 'direct', roleId: c.roleId, roleName: c.roleName })}
                      aria-pressed={dialog?.kind === 'direct' && dialog.roleId === c.roleId}
                    >
                      <span className="dialog-item-name">{c.roleName}</span>
                      <span className="dialog-item-count">{c.count}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="dialog-section">
            <h3 className="dialog-section-title">Rooms</h3>
            <input
              className="room-search"
              type="search"
              placeholder="Search rooms…"
              aria-label="Search rooms (filter)"
              value={roomSearch}
              onChange={(e) => {
                setRoomSearch(e.target.value);
                setRoomsVisible(15);
              }}
            />
            {chatrooms.loading && <Spinner />}
            {chatrooms.error &&
            (isNotFoundError(chatrooms.error) ? (
              <Unavailable feature="Chatrooms" hint="The chatrooms endpoint is not implemented in the backend yet (planned)." />
            ) : (
              <ErrorState error={chatrooms.error} onRetry={chatrooms.refetch} />
            ))}
            {chatrooms.data && chatrooms.data.chatrooms.length === 0 && (
              <p className="muted small dialog-empty-section">Каналов пока нет · Создайте канал, чтобы команда могла обмениваться сообщениями.</p>
            )}
            {chatrooms.data && chatrooms.data.chatrooms.length > 0 && (
              <>
              <ul className="dialog-list-items">
                {roomsShown.map((c) => (
                  <li key={c.id}>
                    <button
                      className={`dialog-item${dialog?.kind === 'room' && dialog.id === c.id ? ' dialog-item-active' : ''}`}
                      onClick={() => setDialog({ kind: 'room', id: c.id })}
                      aria-pressed={dialog?.kind === 'room' && dialog.id === c.id}
                      data-testid="room-dialog"
                    >
                      <div>
                        <span className="dialog-item-name">{c.name}</span>
                        {c.topic && <div className="muted small">{c.topic}</div>}
                      </div>
                      <div className="muted small">
                        {c.last_message ? c.last_message.body.slice(0, 32) : 'no messages'}
                        {c.unread_count > 0 && <Badge kind="warn">{c.unread_count} new</Badge>}
                      </div>
                    </button>
                  </li>
                ))}
              </ul>
              {roomsFiltered.length > roomsVisible && (
                <div className="show-more">
                  <button className="btn" onClick={() => setRoomsVisible((v) => v + 15)}>
                    Show more ({roomsFiltered.length - roomsVisible})
                  </button>
                </div>
              )}
              </>
            )}
          </div>
        </div>

        {/* ПРАВАЯ ПАНЕЛЬ: сообщения выбранного диалога + ввод */}
        <div className="card dialog-view">
          {dialog == null && (
            <EmptyState title="Select a conversation" hint="Выберите диалог слева (Direct или Room), чтобы увидеть сообщения." />
          )}

          {dialog?.kind === 'room' && (
            <>
              <h3 className="dialog-view-title">#{activeChatroom?.name ?? dialog.id}</h3>
              <div className="chat-scroll">
                {roomMessages.loading && <Spinner />}
                {roomMessages.data && roomMessages.data.messages.length === 0 && (
                  <p className="muted small chat-empty">В этом канале пока нет сообщений · Напишите первым.</p>
                )}
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
            </>
          )}

          {dialog?.kind === 'direct' && (
            <>
              <h3 className="dialog-view-title">{dialog.roleName}</h3>
              <div className="chat-scroll">
                {directMessages.length === 0 && <p className="muted small chat-empty">Прямых сообщений нет · Выберите роль слева и напишите.</p>}
                {directMessages.map((m) => (
                  <div key={m.id} className={`chat-bubble${m.is_mine ? ' chat-mine' : ''}`}>
                    <div className="muted small">
                      {m.from_role_name ?? 'system'}
                      {m.to_role_name ? ` → ${m.to_role_name}` : ''} · {formatRelative(m.created_at)}
                      {m.queue_task_id && (
                        <> · <EntityLink to={`/tasks/${m.queue_task_id}`} title="Open related task">#{m.queue_task_id}</EntityLink></>
                      )}
                    </div>
                    {m.body}
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

// P3.15: Mailbox-секция — выбор команды + роли → MailboxPanel (inbox/outbox + ack).
function MailboxSection() {
  const teamsQ = useQuery('mailbox.teams', () => api.teams.getTeams());
  const teams = teamsQ.data?.teams ?? [];
  const [teamId, setTeamId] = useState<number>();
  const effectiveTeam = teamId ?? teams[0]?.id;
  const teamQ = useQuery(
    effectiveTeam != null ? `mailbox.team.${effectiveTeam}` : 'mailbox.team.none',
    () => (effectiveTeam != null ? api.teams.getTeam(effectiveTeam) : Promise.resolve(null)),
    [effectiveTeam],
  );
  const roles = teamQ.data?.roles ?? [];
  const [roleId, setRoleId] = useState<number>();
  const effectiveRole = roleId ?? roles[0]?.id;
  const role = roles.find((r) => r.id === effectiveRole);

  if (teamsQ.loading) {
    return <div className="card mailbox-panel"><Spinner label="Loading mailbox…" /></div>;
  }
  if (!role) {
    return <div className="card mailbox-panel"><p className="muted">Нет ролей для mailbox — создайте команду с ролями.</p></div>;
  }

  return (
    <div className="mailbox-section" style={{ marginBottom: 16 }}>
      <div className="mb-selects" style={{ marginBottom: 8 }}>
        <strong>Role Mailbox:</strong>
        <select
          value={effectiveTeam}
          onChange={(e) => { setTeamId(Number(e.target.value)); setRoleId(undefined); }}
          aria-label="Select team"
        >
          {teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <select
          value={effectiveRole}
          onChange={(e) => setRoleId(Number(e.target.value))}
          aria-label="Select role"
        >
          {roles.map((r) => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      </div>
      <MailboxPanel roleId={role.id} roleName={role.name} />
    </div>
  );
}
