import { useCallback, useEffect, useRef, useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { getApiConfig } from '../../api/config';
import { api } from '../../api';
import { getMockError, setMockError } from '../../api/mock/adapter';
import type { MockErrorKind } from '../../api/mock/adapter';
import { useWebSocket } from '../../hooks/useWebSocket';

const NAV = [
  { to: '/', label: 'Dashboard', icon: '▦', end: true },
  { to: '/attention', label: 'Attention', icon: '⚑' },
  { to: '/teams', label: 'Teams', icon: '⬡' },
  { to: '/projects', label: 'Projects', icon: '▣' },
  { to: '/tasks', label: 'Tasks', icon: '☰' },
  { to: '/workflows', label: 'Workflows', icon: '⑃' },
  { to: '/watchdog/jobs', label: 'Watchdog', icon: '⏰' },
  { to: '/messages', label: 'Messages', icon: '✉' },
  { to: '/library', label: 'Library', icon: '▤' },
  { to: '/history', label: 'History', icon: '⏱' },
  { to: '/diagnostics', label: 'Health', icon: '🩺' },
  { to: '/settings', label: 'Settings', icon: '⚙' },
];

const MOCK_ERRORS: (MockErrorKind | '')[] = ['', 'unauthorized', 'forbidden', 'not_found', 'conflict', 'server', 'network'];

// X3: global real-time status (one connection in the shell → badge on every page).
const WS_LABEL: Record<string, string> = {
  mock: 'live · mock',
  connected: 'real-time: connected',
  reconnecting: 'real-time: reconnecting…',
  disconnected: 'real-time: offline',
};

// P0.1: счётчик «внимание» в навигации обновляется live по WS-событиям
const ATTENTION_EVENTS = new Set([
  'task.created',
  'task.state_changed',
  'session.started',
  'session.stopped',
  'session.failed',
  'alert.created',
]);

export default function AppShell() {
  const cfg = getApiConfig();
  const [simError, setSimError] = useState<MockErrorKind | ''>(getMockError() ?? '');
  const { status: wsStatus, onMessage } = useWebSocket([]);

  // P0.1: бейдж-счётчик «внимание» (total), live по task./session./alert.-событиям
  const [attentionCount, setAttentionCount] = useState(0);
  const loadAttention = useCallback(async () => {
    try {
      setAttentionCount((await api.attention.list({ limit: 1 })).total);
    } catch {
      /* best-effort badge */
    }
  }, []);
  useEffect(() => {
    void loadAttention();
  }, [loadAttention]);
  const last = useRef(0);
  const onAttentionEvent = useCallback(
    (m: { type: string }) => {
      if (!ATTENTION_EVENTS.has(m.type)) return;
      const now = Date.now();
      if (now - last.current < 3000) return;
      last.current = now;
      void loadAttention();
    },
    [loadAttention],
  );
  onMessage(onAttentionEvent);

  const changeSimError = (k: MockErrorKind | '') => {
    setSimError(k);
    setMockError(k || null);
  };
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-icon" aria-hidden="true">◧</span>
          <span className="brand-label">Team Console</span>
        </div>
        <nav aria-label="Main">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              title={n.label}
              className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
            >
              <span aria-hidden="true">{n.icon}</span>
              <span className="nav-label">{n.label}</span>
              {n.to === '/attention' && attentionCount > 0 && (
                <span className="nav-badge" data-testid="attention-count" title={`${attentionCount} требуют внимания`}>
                  {attentionCount}
                </span>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className={`ws-badge ws-${wsStatus}`} title="WebSocket real-time status (auto-reconnects on drop)">
            {WS_LABEL[wsStatus] ?? wsStatus}
          </div>
          <div className={`mode-badge mode-${cfg.mode}`}>{cfg.mode === 'mock' ? 'MOCK API' : 'LIVE API'}</div>
          {cfg.mode === 'mock' && (
            <label className="dev-error">
              Simulate error:
              <select
                aria-label="Simulate mock error"
                value={simError}
                onChange={(e) => changeSimError(e.target.value as MockErrorKind | '')}
              >
                {MOCK_ERRORS.map((k) => (
                  <option key={k} value={k}>
                    {k === '' ? 'none' : k}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
      </aside>
      <main className="content">
        <Outlet />
      </main>
    </div>
  );
}
