import { useState } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { getApiConfig } from '../../api/config';
import { getMockError, setMockError } from '../../api/mock/adapter';
import type { MockErrorKind } from '../../api/mock/adapter';

const NAV = [
  { to: '/', label: 'Dashboard', icon: '▦', end: true },
  { to: '/teams', label: 'Teams', icon: '⬡' },
  { to: '/tasks', label: 'Tasks', icon: '☰' },
  { to: '/workflows', label: 'Workflows', icon: '⑃' },
  { to: '/messages', label: 'Messages', icon: '✉' },
  { to: '/library', label: 'Library', icon: '▤' },
  { to: '/history', label: 'History', icon: '⏱' },
];

const MOCK_ERRORS: (MockErrorKind | '')[] = ['', 'unauthorized', 'forbidden', 'not_found', 'conflict', 'server', 'network'];

export default function AppShell() {
  const cfg = getApiConfig();
  const [simError, setSimError] = useState<MockErrorKind | ''>(getMockError() ?? '');
  const changeSimError = (k: MockErrorKind | '') => {
    setSimError(k);
    setMockError(k || null);
  };
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-icon" aria-hidden="true">◧</span>
          <span>Team Console</span>
        </div>
        <nav aria-label="Main">
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
            >
              <span aria-hidden="true">{n.icon}</span>
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">
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
