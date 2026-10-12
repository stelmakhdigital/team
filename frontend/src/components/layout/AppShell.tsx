import { useCallback, useEffect, useRef, useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { getApiConfig } from '../../api/config';
import { api } from '../../api';
import { getMockError, setMockError } from '../../api/mock/adapter';
import type { MockErrorKind } from '../../api/mock/adapter';
import { useWebSocket } from '../../hooks/useWebSocket';
import CommandPalette from '../CommandPalette';
import ContextTree from '../ContextTree';
import OnboardingTour, { type TourStep } from '../OnboardingTour';

// Навигация: единый монохромный набор глифов (текстовая презентация, U+FE0E —
// запрет emoji-рендера на Windows/macOS).
const TEXT = '\uFE0E';
const NAV = [
  { to: '/', label: 'Dashboard', icon: '▦', end: true },
  { to: '/attention', label: 'Attention', icon: `⚑${TEXT}` },
  { to: '/pulse', label: 'Pulse', icon: '∿' },
  { to: '/teams', label: 'Teams', icon: '⬡' },
  { to: '/projects', label: 'Projects', icon: '▣' },
  { to: '/tasks', label: 'Tasks', icon: '☰' },
  { to: '/sessions', label: 'Sessions', icon: '◉' },
  { to: '/workflows', label: 'Workflows', icon: '⑃' },
  { to: '/watchdog/jobs', label: 'Watchdog', icon: '◷' },
  { to: '/messages', label: 'Messages', icon: `✉${TEXT}` },
  { to: '/library', label: 'Library', icon: '▤' },
  { to: '/history', label: 'History', icon: '↺' },
  { to: '/transcript-search', label: 'Search', icon: '⌕' },
  { to: '/diagnostics', label: 'Health', icon: '✚' },
  { to: '/settings', label: 'Settings', icon: `⚙${TEXT}` },
];

const MOCK_ERRORS: (MockErrorKind | '')[] = ['', 'unauthorized', 'forbidden', 'not_found', 'conflict', 'server', 'network'];

// W4.2: onboarding-тур — 6 шагов по data-tour целям (первый экран).
const TOUR_STEPS: TourStep[] = [
  { id: 'nav', title: 'Навигация', body: 'Главная навигация: Dashboard, Attention, Teams, Projects…' },
  { id: 'fleet', title: 'Fleet', body: 'Live-статус всех ролей: состояние, задача, контекст' },
  { id: 'attention', title: 'Attention', body: 'Что требует вашего решения' },
  { id: 'context', title: 'Контекст', body: 'Дерево проектов → команд → ролей с live-точками' },
  { id: 'palette', title: 'Палитра команд', body: 'K — быстрая навигация и действия' },
  { id: 'settings', title: 'Настройки', body: 'Конфиг, ключи, постеры' },
];

const TOUR_RESTART_EVENT = 'team:tour-restart';

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

  // W4.2: onboarding-тур (first visit) — показывается, если tour.dismissed не задан.
  const [tourOpen, setTourOpen] = useState<boolean>(() => {
    try {
      const dismissed = localStorage.getItem('tour.dismissed') === '1';
      if (import.meta.env.MODE === 'test') {
        // test-affordance: в тестах тур показывается только при явном tour.force=1
        // (и не dismissed) — иначе ломал бы сотни full-App тестов (оверлей + Esc-стек).
        return localStorage.getItem('tour.force') === '1' && !dismissed;
      }
      return !dismissed;
    } catch {
      return false;
    }
  });
  const dismissTour = useCallback(() => {
    setTourOpen(false);
    try {
      localStorage.setItem('tour.dismissed', '1');
    } catch {
      /* no-op */
    }
  }, []);
  const restartTour = useCallback(() => {
    try {
      localStorage.removeItem('tour.dismissed');
    } catch {
      /* no-op */
    }
    setTourOpen(true);
  }, []);
  useEffect(() => {
    const onRestart = () => restartTour();
    window.addEventListener(TOUR_RESTART_EVENT, onRestart);
    return () => window.removeEventListener(TOUR_RESTART_EVENT, onRestart);
  }, [restartTour]);

  // W2.1: ContextTree — sessionTick инкрементируется на session.* (WS) →
  // дерево refetch'ит сессии (live-точки), не создавая второй WS (используем
  // единственную связь из AppShell).
  const [sessionTick, setSessionTick] = useState(0);
  const sessLast = useRef(0);

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
      if (ATTENTION_EVENTS.has(m.type)) {
        const now = Date.now();
        if (now - last.current < 3000) return;
        last.current = now;
        void loadAttention();
      }
      // W2.1: session.* → refresh live-точек в ContextTree (debounce 3s)
      if (m.type.startsWith('session.')) {
        const now = Date.now();
        if (now - sessLast.current >= 3000) {
          sessLast.current = now;
          setSessionTick((t) => t + 1);
        }
      }
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
        <nav aria-label="Main" data-tour="nav">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              title={n.label}
              data-tour={n.to === '/settings' ? 'settings' : undefined}
              className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
            >
              <span aria-hidden="true">{n.icon}</span>
              <span className="nav-label">{n.label}</span>
              {n.to === '/attention' && attentionCount > 0 && (
                <span className="nav-badge" data-testid="attention-count" data-tour="attention" title={`${attentionCount} требуют внимания`}>
                  {attentionCount}
                </span>
              )}
            </NavLink>
          ))}
        </nav>
        <ContextTree sessionTick={sessionTick} />
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
        <div
          className={`api-banner api-banner-${cfg.mode}`}
          role="status"
          data-testid="api-mode-banner"
          title={cfg.mode === 'mock' ? 'Frontend works on the in-memory mock adapter (demo data). Set VITE_API_MODE=real to connect to the daemon.' : 'Connected to the real daemon.'}
        >
          {cfg.mode === 'mock' ? (
            <span>
              <span aria-hidden="true">⚠</span> Mock API — demo data. Not connected to the daemon{' '}
              <code>VITE_API_MODE=real</code>
            </span>
          ) : (
            <span>
              <span aria-hidden="true">●</span> Live · {cfg.baseUrl || 'same-origin daemon'}
            </span>
          )}
        </div>
        <Outlet />
      </main>
      <CommandPalette />
      {tourOpen && <OnboardingTour steps={TOUR_STEPS} onClose={dismissTour} />}
    </div>
  );
}
