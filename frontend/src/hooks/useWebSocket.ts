import { useEffect, useRef, useState } from 'react';
import { getApiConfig } from '../api/config';
import type { WSServerMessage } from '../types/api';

export interface WSHook {
  status: 'connected' | 'reconnecting' | 'disconnected' | 'mock';
  lastMessage: WSServerMessage | null;
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
  const [lastMessage, setLastMessage] = useState<WSServerMessage | null>(null);
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
      ];
      const t = setInterval(() => {
        handlerRef.current(pool[tick % pool.length]);
        setLastMessage(pool[tick % pool.length]);
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
        if (msg) {
          handlerRef.current(msg);
          setLastMessage(msg);
        }
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

  return { status, lastMessage, onMessage };
}
