import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMediaQuery } from '../hooks/useMediaQuery';
import type { Project, Team, SessionDetail } from '../types/api';

// W2.1: ContextTree — «где я и над чем работаю» в сайдбаре (Projects → Teams,
// live-точки, bounded, lazy). Сводка: проекты (active) → команды проекта →
// live-статус сессий (green/red/amber/gray).
const COLLAPSE_KEY = 'context-tree.collapsed';
const MAX_PROJECTS = 10;
const POLL_MS = 30_000;

type DotKind = 'running' | 'failed' | 'active' | 'none';

const DOT_LABEL: Record<DotKind, string> = {
  running: 'running session',
  failed: 'failed session',
  active: 'idle / stopped',
  none: 'no session',
};

// live-точка команды: running > failed > idle/stopped > нет сессий
function teamDot(sessions: SessionDetail[], teamId: number): DotKind {
  const own = sessions.filter((s) => s.team_id === teamId);
  if (own.length === 0) return 'none';
  if (own.some((s) => s.state === 'running')) return 'running';
  if (own.some((s) => s.state === 'failed')) return 'failed';
  return 'active';
}

export default function ContextTree({ sessionTick }: { sessionTick: number }) {
  const narrow = useMediaQuery('(max-width: 1200px)');
  const location = useLocation();

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem(COLLAPSE_KEY);
      if (saved !== null) return saved === '1';
    } catch {
      /* no-op */
    }
    return narrow; // по умолчанию: свёрнуто на узком, развёрнуто на широком
  });

  const toggleCollapse = useCallback(() => {
    setCollapsed((c) => {
      const next = !c;
      try {
        localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0');
      } catch {
        /* no-op */
      }
      return next;
    });
  }, []);

  // проекты (active) — bounded до MAX_PROJECTS
  const projectsQ = useQuery('contexttree.projects', () => api.projects.list({ limit: MAX_PROJECTS + 1 }));

  // команды: lazy (первое раскрытие), in-memory cache (один запрос на все)
  const [teams, setTeams] = useState<Team[] | null>(null);
  const ensureTeams = useCallback(() => {
    setTeams((prev) => {
      if (prev !== null) return prev;
      api.teams
        .getTeams()
        .then((r) => setTeams(r.teams))
        .catch(() => setTeams([]));
      return prev;
    });
  }, []);

  // live-точки: сессии (poll 30s + WS через sessionTick из AppShell)
  const [sessions, setSessions] = useState<SessionDetail[]>([]);
  const loadSessions = useCallback(() => {
    api.sessions
      .list()
      .then((r) => setSessions(r.sessions))
      .catch(() => {});
  }, []);
  useEffect(() => {
    loadSessions();
    const id = setInterval(loadSessions, POLL_MS);
    return () => clearInterval(id);
  }, [loadSessions, sessionTick]);

  const [expanded, setExpanded] = useState<Set<number>>(() => new Set());
  const toggleExpanded = useCallback((id: number) => {
    ensureTeams();
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, [ensureTeams]);

  if (collapsed) {
    return (
      <section className="ctx-tree ctx-tree--collapsed" data-testid="context-tree">
        <button type="button" className="ctx-tree-header" onClick={toggleCollapse} aria-expanded={false} data-testid="context-tree-toggle">
          <span aria-hidden="true">▸</span>
          <span className="ctx-tree-title">Context</span>
        </button>
      </section>
    );
  }

  const match = location.pathname.match(/^(\/projects|\/teams)\/(\d+)/);
  const activeKind = match ? match[1].slice(1) : null; // 'projects' | 'teams'
  const activeId = match ? Number(match[2]) : null;

  const projects: Project[] = projectsQ.data?.projects ?? [];
  const total = projectsQ.data?.total ?? projects.length;
  const shown = projects.slice(0, MAX_PROJECTS);
  const hidden = Math.max(0, total - MAX_PROJECTS);

  return (
    <section className="ctx-tree" data-testid="context-tree">
      <button type="button" className="ctx-tree-header" onClick={toggleCollapse} aria-expanded={true} data-testid="context-tree-toggle">
        <span aria-hidden="true">▾</span>
        <span className="ctx-tree-title">Context</span>
      </button>

      {projectsQ.loading ? (
        <div className="muted small ctx-tree-pad">Loading…</div>
      ) : projectsQ.error ? (
        <div className="muted small ctx-tree-pad">No context</div>
      ) : shown.length === 0 ? (
        <div className="ctx-tree-empty-state ctx-tree-pad">
          <div className="muted small">Нет проектов</div>
          <Link to="/projects" className="btn btn-sm" data-testid="context-tree-create">
            Создать
          </Link>
        </div>
      ) : (
        <ul className="ctx-tree-list" role="tree">
          {shown.map((p) => {
            const pTeams = teams?.filter((t) => t.project_id === p.id && t.state === 'active') ?? [];
            const open = expanded.has(p.id);
            const pActive = activeKind === 'projects' && activeId === p.id;
            return (
              <li key={p.id} className="ctx-tree-proj" role="treeitem" aria-expanded={open}>
                <div className="ctx-tree-row">
                  <button
                    type="button"
                    className="ctx-tree-toggle"
                    onClick={() => toggleExpanded(p.id)}
                    aria-expanded={open}
                    aria-label={open ? 'Свернуть' : 'Развернуть'}
                  >
                    {open ? '▾' : '▸'}
                  </button>
                  <Link to={`/projects/${p.id}`} className={`ctx-tree-link${pActive ? ' active' : ''}`}>
                    {p.name}
                  </Link>
                </div>
                {open && (
                  <ul className="ctx-tree-teams" role="group">
                    {pTeams.length === 0 ? (
                      <li className="ctx-tree-empty muted">—</li>
                    ) : (
                      pTeams.map((tm) => {
                        const dot = teamDot(sessions, tm.id);
                        const tActive = activeKind === 'teams' && activeId === tm.id;
                        return (
                          <li key={tm.id}>
                            <Link to={`/teams/${tm.id}`} className={`ctx-tree-link ctx-tree-link--team${tActive ? ' active' : ''}`}>
                              <span className={`ctx-dot ctx-dot--${dot}`} title={DOT_LABEL[dot]} aria-label={DOT_LABEL[dot]} />
                              <span className="ctx-tree-teamname">{tm.name}</span>
                            </Link>
                          </li>
                        );
                      })
                    )}
                  </ul>
                )}
              </li>
            );
          })}
          {hidden > 0 && (
            <li className="ctx-tree-more">
              <Link to="/projects" className="ctx-tree-more-link">
                +{hidden} ещё
              </Link>
            </li>
          )}
        </ul>
      )}
    </section>
  );
}
