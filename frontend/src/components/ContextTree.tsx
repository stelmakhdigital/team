import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMediaQuery } from '../hooks/useMediaQuery';
import type { Project, Team, SessionDetail, Role } from '../types/api';

// W2.1 + W3.3: ContextTree — «где я и над чем работаю» в сайдбаре
// (Projects → Teams → Roles, live-точки, bounded, lazy).
// W3.3: 3-й уровень — роли команды (lazy через getTeam, кэш в памяти),
// live-точка роли (сессии по role_id), ссылка роли → /teams/{id},
// максимум 20 ролей + «+N».
const COLLAPSE_KEY = 'context-tree.collapsed';
const MAX_PROJECTS = 10;
const MAX_ROLES = 20;
const POLL_MS = 30_000;

type DotKind = 'running' | 'failed' | 'active' | 'none';

const DOT_LABEL: Record<DotKind, string> = {
  running: 'running session',
  failed: 'failed session',
  active: 'idle / stopped',
  none: 'no session',
};

// live-точка по сессиям: running > failed > idle/stopped > нет сессий
function dotFrom(sessions: SessionDetail[]): DotKind {
  if (sessions.length === 0) return 'none';
  if (sessions.some((s) => s.state === 'running')) return 'running';
  if (sessions.some((s) => s.state === 'failed')) return 'failed';
  return 'active';
}

function teamDot(sessions: SessionDetail[], teamId: number): DotKind {
  return dotFrom(sessions.filter((s) => s.team_id === teamId));
}

// W3.3: live-точка роли (сессии по role_id)
function roleDot(sessions: SessionDetail[], roleId: number): DotKind {
  return dotFrom(sessions.filter((s) => s.role_id === roleId));
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

  // W3.3: роли команд — lazy (первое раскрытие команды), in-memory cache
  const [teamRoles, setTeamRoles] = useState<Map<number, Role[]>>(new Map());
  const rolesRequested = useRef<Set<number>>(new Set());
  const ensureRoles = useCallback((teamId: number) => {
    if (rolesRequested.current.has(teamId)) return;
    rolesRequested.current.add(teamId);
    api.teams
      .getTeam(teamId)
      .then((r) => setTeamRoles((prev) => new Map(prev).set(teamId, r.roles ?? [])))
      .catch(() => setTeamRoles((prev) => new Map(prev).set(teamId, [])));
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

  // раскрытые проекты (2-й уровень)
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

  // W3.3: раскрытые команды (3-й уровень)
  const [teamOpen, setTeamOpen] = useState<Set<number>>(() => new Set());
  const toggleTeam = useCallback((teamId: number) => {
    ensureRoles(teamId);
    setTeamOpen((prev) => {
      const next = new Set(prev);
      if (next.has(teamId)) next.delete(teamId);
      else next.add(teamId);
      return next;
    });
  }, [ensureRoles]);

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
                        // W3.3: роли команды (lazy; null = ещё не загружены)
                        const roles = teamRoles.get(tm.id) ?? null;
                        const teamExpanded = teamOpen.has(tm.id);
                        return (
                          <li key={tm.id}>
                            <div className="ctx-tree-row ctx-tree-row--team">
                              <button
                                type="button"
                                className="ctx-tree-toggle"
                                onClick={() => toggleTeam(tm.id)}
                                aria-expanded={teamExpanded}
                                aria-label={teamExpanded ? 'Свернуть команду' : 'Развернуть команду'}
                                data-testid={`ctx-team-toggle-${tm.id}`}
                              >
                                {teamExpanded ? '▾' : '▸'}
                              </button>
                              <Link to={`/teams/${tm.id}`} className={`ctx-tree-link ctx-tree-link--team${tActive ? ' active' : ''}`}>
                                <span className={`ctx-dot ctx-dot--${dot}`} title={DOT_LABEL[dot]} aria-label={DOT_LABEL[dot]} />
                                <span className="ctx-tree-teamname">{tm.name}</span>
                              </Link>
                            </div>
                            {teamExpanded && (
                              <ul className="ctx-tree-roles" role="group" data-testid={`ctx-roles-${tm.id}`}>
                                {!roles ? (
                                  <li className="ctx-tree-empty muted">…</li>
                                ) : roles.length === 0 ? (
                                  <li className="ctx-tree-empty muted">—</li>
                                ) : (
                                  <>
                                    {roles.slice(0, MAX_ROLES).map((r) => {
                                      const rd = roleDot(sessions, r.id);
                                      return (
                                        <li key={r.id}>
                                          <Link
                                            to={`/teams/${tm.id}`}
                                            className="ctx-tree-link ctx-tree-link--role"
                                            title={`${r.name} (${r.agent_spec})`}
                                            data-testid={`ctx-role-${r.id}`}
                                          >
                                            <span className={`ctx-dot ctx-dot--${rd}`} title={DOT_LABEL[rd]} aria-label={DOT_LABEL[rd]} />
                                            <span className="ctx-tree-rolename">{r.name}</span>
                                          </Link>
                                        </li>
                                      );
                                    })}
                                    {roles.length > MAX_ROLES && (
                                      <li className="ctx-tree-more">
                                        <Link to={`/teams/${tm.id}`} className="ctx-tree-more-link">
                                          +{roles.length - MAX_ROLES} ещё
                                        </Link>
                                      </li>
                                    )}
                                  </>
                                )}
                              </ul>
                            )}
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
