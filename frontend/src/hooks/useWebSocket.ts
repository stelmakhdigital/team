import { useEffect, useRef, useState } from 'react';
import { getApiConfig } from '../api/config';
import type { WSServerMessage } from '../types/api';

export interface WSHook {
  status: 'connected' | 'reconnecting' | 'disconnected' | 'mock';
  // R6.1 perf: lastMessage удалён из state — он никем не использовался, а каждое
  // сообщение (и mock-tick) вызывало setLastMessage → ререндер всей подписанной страницы
  onMessage: (handler: (m: WSServerMessage) => void) => void;
}

function parseMessage(raw: string): WSServerMessage | null {
  try {
    const msg = JSON.parse(raw) as WSServerMessage;
    if (typeof msg.type !== 'string') return null;
    return msg;
  } catch {
    return null;
  }
}

/** WebSocket client with reconnect/backoff. In mock mode emits synthetic
 * contract-shaped events so UI live-panels can be exercised. */
export function useWebSocket(channels: string[]): WSHook {
  const [status, setStatus] = useState<WSHook['status']>('disconnected');
  const channelsRef = useRef(channels);
  channelsRef.current = channels;
  const handlerRef = useRef<(m: WSServerMessage) => void>(() => {});

  const { mode } = getApiConfig();

  useEffect(() => {
    if (mode === 'mock') {
      setStatus('mock');
      let tick = 0;
      const pool: WSServerMessage[] = [
        {
          type: 'task.state_changed',
          data: { task_id: 1, from_state: 'in_progress', to_state: 'in_progress', updated_at: new Date().toISOString() },
          timestamp: new Date().toISOString(),
        },
        {
          type: 'alert.created',
          data: { alert_id: 100 + tick, event_type: 'idle', severity: 'low', description: 'Session idle (synthetic)', requires_action: false },
          timestamp: new Date().toISOString(),
        },
        {
          type: 'message.sent',
          data: { message_id: 1000 + tick, from_role_name: 'Worker', body: 'progress update (synthetic)', type: 'direct' },
          timestamp: new Date().toISOString(),
        },
        {
          // slice 7: live-терминал (мок-батчи строк лога; session_id 1 — синтетика,
          // как и остальные события пула; реальная привязка — по session:{id} в real-режиме)
          type: 'session.output',
          data: {
            session_id: 1,
            role_name: 'Worker',
            lines: [
              { ts: new Date().toISOString(), text: `mock: processing step ${tick}…`, stream: 'stdout' },
              { ts: new Date().toISOString(), text: `mock: step ${tick} done`, stream: 'stdout' },
            ],
          },
          timestamp: new Date().toISOString(),
        },
      ];
      const t = setInterval(() => {
        handlerRef.current(pool[tick % pool.length]);
        tick += 1;
      }, 8000);
      return () => clearInterval(t);
    }

    let ws: WebSocket | null = null;
    let closed = false;
    let retry: ReturnType<typeof setTimeout> | null = null;
    let attempt = 0;

    const connect = () => {
      if (closed) return;
      // статус честный: 'connected' только после onopen, до этого — disconnected/reconnecting
      setStatus(attempt === 0 ? 'disconnected' : 'reconnecting');
      try {
        ws = new WebSocket(getApiConfig().wsUrl);
      } catch {
        scheduleReconnect();
        return;
      }
      ws.onopen = () => {
        attempt = 0;
        setStatus('connected');
        ws?.send(JSON.stringify({ type: 'subscribe', channels: channelsRef.current }));
      };
      ws.onmessage = (ev) => {
        const msg = parseMessage(typeof ev.data === 'string' ? ev.data : '');
        if (msg) handlerRef.current(msg);
      };
      ws.onclose = () => {
        if (!closed) scheduleReconnect();
      };
      ws.onerror = () => ws?.close();
    };

    const scheduleReconnect = () => {
      if (closed) return;
      attempt += 1;
      setStatus('reconnecting');
      const delay = Math.min(15_000, 500 * 2 ** attempt);
      retry = setTimeout(connect, delay);
    };

    connect();
    return () => {
      closed = true;
      if (retry) clearTimeout(retry);
      ws?.close();
    };
  }, [mode]);

  const onMessage = (handler: (m: WSServerMessage) => void) => {
    handlerRef.current = handler;
  };

  return { status, onMessage };
}
