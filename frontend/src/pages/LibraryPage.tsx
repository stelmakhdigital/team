import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import YAML from 'yaml';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import {  EmptyState, ErrorState, PageSkeleton, Spinner, Unavailable, isNotFoundError , Badge, errorMessage } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';

export default function LibraryPage() {
  const [search, setSearch] = useState('');
  const [type, setType] = useState('');
  const [detailId, setDetailId] = useState<number | null>(null);
  const [libRename, setLibRename] = useState<string | null>(null);
  const [saveTeamId, setSaveTeamId] = useState<number | null>(null);
  const [applyTargetId, setApplyTargetId] = useState<number | null>(null);
  const [starterProjectId, setStarterProjectId] = useState<number | null>(null);
  // P3.12: agent_image — launch (team/project) + pin/prune
  const [launchTeamId, setLaunchTeamId] = useState<number | null>(null);
  const [launchProjectId, setLaunchProjectId] = useState<number | null>(null);
  const list = useQuery(`library.list.${type}.${search}`, () =>
    api.library.getLibrary({ type: (type || undefined) as never, search: search || undefined }),
    [type, search],
  );
  const detail = useQuery(detailId ? `library.item.${detailId}` : 'library.item.none', () =>
    detailId ? api.library.getLibraryItem(detailId) : Promise.resolve(null),
    [detailId],
  );
  const teams = useQuery('library.teams', () => api.teams.getTeams(), []);
  const projects = useQuery('library.projects', () => api.projects.list(), []);
  // P2.13: стартеры первыми (стабильная сортировка в пределах)
  const items = list.data
    ? [...list.data.items].sort((a, b) => Number(!!b.is_starter) - Number(!!a.is_starter))
    : [];
  const save = useMutation((sourceId: number, teamName: string) =>
    api.library.saveToLibrary({
      type: 'team',
      source_id: sourceId,
      name: `${teamName} — ${new Date().toISOString().slice(0, 10)}`,
      group: 'Teams',
    }),
  );
  const apply = useMutation((itemId: number, req: { target_team_id?: number; project_id?: number; overrides?: Record<string, unknown> }) =>
    api.library.applyLibrary(itemId, req),
  );
  // P3.12: agent_image — pin/unpin/prune/launch
  const pinM = useMutation((id: number) => api.library.pin(id));
  const unpinM = useMutation((id: number) => api.library.unpin(id));
  const pruneM = useMutation((id: number) => api.library.delete(id));
  const launchM = useMutation((id: number, req: { team_id: number; project_id?: number }) => api.library.launch(id, req));
  const { toast } = useToast();
  const navigate = useNavigate();

  const saveTeam = teams.data?.teams.find((t) => t.id === saveTeamId) ?? teams.data?.teams[0];

  // R6.3: после apply — редирект на результат (команда → builder, workflow → редактор)
  const openResult = async (item: { id: number; type: string; name: string }, targetTeamId?: number, newTeamId?: number) => {
    list.refetch();
    teams.refetch();
    if (item.type === 'team') {
      if (newTeamId) navigate(`/teams/${newTeamId}`);
      else if (targetTeamId) navigate(`/teams/${targetTeamId}`);
      return;
    }
    if (targetTeamId) {
      if (item.type === 'workflow') {
        try {
          const wf = await api.workflows.getWorkflows({ team_id: targetTeamId });
          const found = wf.workflows.find((w) => w.name === item.name);
          if (found) {
            toast('success', `Workflow "${item.name}" created — opening editor`);
            navigate(`/workflows/${found.id}`);
            return;
          }
        } catch {
          /* fallback: открыть team */
        }
      }
      navigate(`/teams/${targetTeamId}`);
    }
  };

  return (
    <div className="page">
      <div className="page-head">
        <h1>Library</h1>
        <div className="library-save-row">
          {teams.data && teams.data.teams.length > 0 && (
            <select
              value={saveTeam?.id ?? ''}
              onChange={(e) => setSaveTeamId(Number(e.target.value))}
              aria-label="Team to save"
              className="input"
            >
              {teams.data.teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          )}
          <button
            className="btn btn-primary"
            onClick={async () => {
              if (!saveTeam) {
                toast('error', 'No team to save');
                return;
              }
              try {
                await save.mutate(saveTeam.id, saveTeam.name);
                toast('success', `Saved "${saveTeam.name}" to library`);
                list.refetch();
              } catch {
                toast('error', 'Failed to save to library');
              }
            }}
            disabled={save.pending || !saveTeam}
          >
            {save.pending ? 'Saving…': '+ Save team to library'}
          </button>
        </div>
      </div>

      <div className="library-filters">
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search…"
          aria-label="Search library"
        />
        <select value={type} onChange={(e) => setType(e.target.value)} aria-label="Filter by type">
          <option value="">All types</option>
          <option value="team">Teams</option>
          <option value="workflow">Workflows</option>
          <option value="role">Roles</option>
          <option value="segment">Segments</option>
          <option value="agent_image">Agent images</option>
        </select>
        <div className="group-chips">
          {list.data?.groups.map((g) => (
            <span key={g.name} className="chip">
              {g.name} ({g.items_count})
            </span>
          ))}
        </div>
      </div>

      <div className="library-layout">
        <div>
          {list.loading && <PageSkeleton label="Loading library…" rows={3} />}
          {list.error &&
          (isNotFoundError(list.error) ? (
            <Unavailable feature="Library" hint="The library endpoint is not implemented in the backend yet (planned)." />
          ) : (
            <ErrorState error={list.error} onRetry={list.refetch} />
          ))}
          {list.data && items.length === 0 && <EmptyState title="Ничего не найдено" hint="Попробуйте другой запрос или другой тип (стартеры / спецификации / шаблоны)." />}
          {items.some((i) => i.is_starter) && (
            <div className="library-starters-banner">
              <span className="library-starters-title">Starters</span>
              <span className="muted small">— ready-made bundles, apply in one click (not editable)</span>
            </div>
          )}
          <div className="library-grid">
            {items.map((item) => (
              <button key={item.id} className={`card library-card${item.is_starter ? ' library-card--starter' : ''}`} onClick={() => setDetailId(item.id)}>
                <div className="library-card-head">
                  {item.is_starter && <span className="badge starter-badge" title="Ready-made starter (not editable)">STARTER</span>}
                  {item.type === 'agent_image' && item.is_pinned && <span className="badge pinned-badge" title="Pinned (protected from prune)">PINNED</span>}
                  <Badge kind="type" value={item.type} />
                  <span className="muted small">v{item.version}</span>
                </div>
                <h3>{item.name}</h3>
                {item.description && <p className="muted small">{item.description}</p>}
                <div className="muted small">
                  {item.downloads_count} downloads · updated {formatRelative(item.updated_at)}
                </div>
                {item.tags && item.tags.length > 0 && (
                  <div className="group-chips">
                    {item.tags.map((t) => (
                      <span key={t} className="chip">
                        {t}
                      </span>
                    ))}
                  </div>
                )}
              </button>
            ))}
          </div>
        </div>

        <aside className="card detail-pane">
          <h2>Details</h2>
          {detailId === null && <p className="muted">Select an item to see its spec and versions.</p>}
          {detail.loading && <Spinner />}
          {detail.error && <ErrorState error={detail.error} onRetry={detail.refetch} />}
          {detail.data && (
            <div>
              <h3>
                {detail.data.item.name} <span className="badge">v{detail.data.item.version}</span>
                {detail.data.item.is_starter && <span className="badge starter-badge" title="Ready-made starter">STARTER</span>}
                {libRename === null ? (
                  detail.data.item.is_starter ? (
                    <button className="btn btn-small lib-rename-btn" title="Starters are pinned (not editable)" aria-label="Rename disabled (starter)" disabled>
                      ⊘
                    </button>
                  ) : (
                    <button className="btn btn-small lib-rename-btn" title="Rename item" aria-label="Rename item" onClick={() => setLibRename(detail.data!.item.name)}>
                      ✎
                    </button>
                  )
                ) : null}
              </h3>
              {detail.data.item.is_starter && (
                <p className="muted small starter-note">Ready-made starter — apply it to get a fresh copy. Starters cannot be renamed or edited.</p>
              )}
              {libRename !== null && (
                <form
                  className="lib-rename"
                  onSubmit={async (e) => {
                    e.preventDefault();
                    const name = libRename.trim();
                    if (!name) return;
                    try {
                      await api.library.updateLibraryItem(detail.data!.item.id, { name });
                      toast('success', 'Library item renamed');
                      setLibRename(null);
                      detail.refetch();
                      list.refetch();
                    } catch (err) {
                      toast('error', errorMessage(err ?? new Error('Rename failed')));
                    }
                  }}
                >
                  <input className="input" value={libRename} onChange={(e) => setLibRename(e.target.value)} placeholder="Item name" aria-label="Item name" autoFocus />
                  <div className="wf-config-actions">
                    <button type="submit" className="btn btn-primary">Save</button>
                    <button type="button" className="btn" onClick={() => setLibRename(null)}>Cancel</button>
                  </div>
                </form>
              )}
              <p className="muted small">
                author: {detail.data.item.author ?? '—'} · public: {detail.data.item.is_public ? 'yes' : 'no'}
              </p>

              {detail.data.item.type === 'team' && (
                <div className="library-apply">
                  <h4>Apply</h4>
                  {detail.data.item.is_starter && (
                    <div className="library-apply-row">
                      <select
                        value={starterProjectId ?? ''}
                        onChange={(e) => setStarterProjectId(e.target.value ? Number(e.target.value) : null)}
                        aria-label="Apply starter to project"
                        className="input"
                      >
                        <option value="">No project</option>
                        {projects.data?.projects.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}
                  <button
                    className={detail.data.item.is_starter ? 'btn btn-primary' : 'btn btn-small'}
                    disabled={apply.pending}
                    onClick={async () => {
                      try {
                        const res = await apply.mutate(detail.data!.item.id, {
                          project_id: starterProjectId ?? undefined,
                        });
                        const newId = res.created_resources?.teams?.[0];
                        toast('success', `Applied as new team (id ${newId ?? '?'})`);
                        await openResult(detail.data!.item, undefined, newId);
                      } catch (e) {
                        toast('error', e instanceof Error ? e.message : 'Failed to apply');
                      }
                    }}
                  >
                    {detail.data.item.is_starter ? 'Apply this starter' : 'Apply as new team'}
                  </button>
                  <div className="library-apply-row">
                    <select
                      value={applyTargetId ?? ''}
                      onChange={(e) => setApplyTargetId(Number(e.target.value))}
                      aria-label="Merge into team"
                      className="input"
                    >
                      <option value="" disabled>
                        merge into…
                      </option>
                      {teams.data?.teams.map((t) => (
                        <option key={t.id} value={t.id}>
                          {t.name}
                        </option>
                      ))}
                    </select>
                    <button
                      className="btn btn-small"
                      disabled={apply.pending || !applyTargetId}
                      onClick={async () => {
                        if (!applyTargetId) return;
                        try {
                          const res = await apply.mutate(detail.data!.item.id, { target_team_id: applyTargetId });
                          toast('success', `Merged into team (status: ${res.status})`);
                          list.refetch();
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : 'Failed to apply');
                        }
                      }}
                    >
                      Merge
                    </button>
                  </div>
                </div>
              )}

              {detail.data.item.type === 'workflow' && (
                <div className="library-apply">
                  <h4>Apply to team</h4>
                  <div className="library-apply-row">
                    <select
                      value={applyTargetId ?? ''}
                      onChange={(e) => setApplyTargetId(Number(e.target.value))}
                      aria-label="Apply workflow to team"
                      className="input"
                    >
                      <option value="" disabled>
                        choose team…
                      </option>
                      {teams.data?.teams.map((t) => (
                        <option key={t.id} value={t.id}>
                          {t.name}
                        </option>
                      ))}
                    </select>
                    <button
                      className="btn btn-small"
                      disabled={apply.pending || !applyTargetId}
                      onClick={async () => {
                        if (!applyTargetId) return;
                        try {
                          await apply.mutate(detail.data!.item.id, { target_team_id: applyTargetId });
                          await openResult(detail.data!.item, applyTargetId);
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : 'Failed to apply');
                        }
                      }}
                    >
                      Apply
                    </button>
                  </div>
                </div>
              )}

              {(detail.data.item.type === 'role' || detail.data.item.type === 'segment') && (
                <div className="library-apply">
                  <h4>Apply to team</h4>
                  <div className="library-apply-row">
                    <select
                      value={applyTargetId ?? ''}
                      onChange={(e) => setApplyTargetId(Number(e.target.value))}
                      aria-label={detail.data.item.type === 'role' ? 'Apply role to team' : 'Merge segment into team'}
                      className="input"
                    >
                      <option value="" disabled>
                        choose team…
                      </option>
                      {teams.data?.teams.map((t) => (
                        <option key={t.id} value={t.id}>
                          {t.name}
                        </option>
                      ))}
                    </select>
                    <button
                      className="btn btn-small"
                      disabled={apply.pending || !applyTargetId}
                      onClick={async () => {
                        if (!applyTargetId) return;
                        try {
                          const res = await apply.mutate(detail.data!.item.id, { target_team_id: applyTargetId });
                          toast('success', `${detail.data!.item.type} applied (status: ${res.status})`);
                          await openResult(detail.data!.item, applyTargetId);
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : 'Failed to apply');
                        }
                      }}
                    >
                      {detail.data.item.type === 'role' ? 'Apply' : 'Merge'}
                    </button>
                  </div>
                  <p className="muted small">
                    {detail.data.item.type === 'role'
                      ? 'Role is created in the chosen segment (first one by default; use a team with the right segment).'
                      : 'Segment is merged into the team idempotently by name.'}
                  </p>
                </div>
              )}

              {detail.data.item.type === 'agent_image' && (
                <div className="library-apply agent-image-detail">
                  <h4>Agent image</h4>
                  {(() => {
                    const spec = detail.data!.spec as { agent?: Record<string, unknown>; summary?: string; role_name?: string; runtime_type?: string };
                    const agent = spec?.agent ?? {};
                    const model = typeof agent.model === 'string' ? agent.model : undefined;
                    const skills = Array.isArray(agent.skills) ? (agent.skills as string[]) : [];
                    const tools = Array.isArray(agent.tools) ? (agent.tools as string[]) : [];
                    return (
                      <div className="agent-image-spec">
                        <div className="muted small">role: {spec?.role_name ?? '—'} · runtime: {spec?.runtime_type ?? '—'}{model ? ` · ${model}` : ''}</div>
                        {skills.length > 0 && <div className="group-chips">{skills.map((sk) => <span key={sk} className="chip">{sk}</span>)}</div>}
                        {tools.length > 0 && <div className="muted small">tools: {tools.join(', ')}</div>}
                        {spec?.summary && (
                          <div className="agent-image-summary">
                            <div className="muted small">summary</div>
                            <pre className="spec-pre">{spec.summary}</pre>
                          </div>
                        )}
                      </div>
                    );
                  })()}
                  <div className="library-apply-row">
                    <button
                      className="btn btn-small"
                      disabled={pinM.pending || unpinM.pending}
                      onClick={async () => {
                        try {
                          if (detail.data!.item.is_pinned) {
                            await unpinM.mutate(detail.data!.item.id);
                            toast('info', 'Unpinned');
                          } else {
                            await pinM.mutate(detail.data!.item.id);
                            toast('success', 'Pinned (protected from prune)');
                          }
                          detail.refetch();
                          list.refetch();
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : 'Failed');
                        }
                      }}
                    >
                      {detail.data.item.is_pinned ? '⚑\uFE0E Unpin' : '⚑\uFE0E Pin'}
                    </button>
                    <button
                      className="btn btn-small btn-danger"
                      disabled={pruneM.pending || !!detail.data.item.is_pinned}
                      title={detail.data.item.is_pinned ? 'Pinned — unpin first' : 'Delete this snapshot'}
                      onClick={async () => {
                        if (!window.confirm(`Prune agent image "${detail.data!.item.name}"?`)) return;
                        try {
                          await pruneM.mutate(detail.data!.item.id);
                          toast('success', 'Snapshot pruned');
                          setDetailId(null);
                          list.refetch();
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : 'Prune failed');
                        }
                      }}
                    >
                      ⌫ Prune
                    </button>
                  </div>
                  <h4>Launch session</h4>
                  <div className="library-apply-row">
                    <select
                      value={launchTeamId ?? ''}
                      onChange={(e) => setLaunchTeamId(e.target.value ? Number(e.target.value) : null)}
                      aria-label="Launch into team"
                      className="input"
                    >
                      <option value="" disabled>choose team…</option>
                      {teams.data?.teams.map((t) => (
                        <option key={t.id} value={t.id}>{t.name}</option>
                      ))}
                    </select>
                    <select
                      value={launchProjectId ?? ''}
                      onChange={(e) => setLaunchProjectId(e.target.value ? Number(e.target.value) : null)}
                      aria-label="Launch into project (optional)"
                      className="input"
                    >
                      <option value="">No project</option>
                      {projects.data?.projects.map((p) => (
                        <option key={p.id} value={p.id}>{p.name}</option>
                      ))}
                    </select>
                    <button
                      className="btn btn-small btn-primary"
                      disabled={launchM.pending || !launchTeamId}
                      onClick={async () => {
                        if (!launchTeamId) return;
                        try {
                          const res = await launchM.mutate(detail.data!.item.id, {
                            team_id: launchTeamId,
                            project_id: launchProjectId ?? undefined,
                          });
                          toast('success', `Session #${res.id} started`);
                          navigate(`/sessions/${res.id}`);
                        } catch (e) {
                          toast('error', e instanceof Error ? e.message : 'Launch failed');
                        }
                      }}
                    >
                      ▶ Launch
                    </button>
                  </div>
                </div>
              )}

              <h4>Versions</h4>
              <ul>
                {detail.data.versions.map((v) => (
                  <li key={v.version} className="muted small">
                    {v.version} — {v.changes ?? ''} ({formatRelative(v.created_at)})
                  </li>
                ))}
              </ul>
              <h4>Spec</h4>
              <pre className="spec-pre">
                {YAML.stringify(detail.data.spec)}
                <button
                  className="btn btn-small spec-copy"
                  onClick={async () => {
                    try {
                      await navigator.clipboard.writeText(YAML.stringify(detail.data!.spec));
                      toast('success', 'Spec copied to clipboard');
                    } catch {
                      toast('error', 'Clipboard unavailable');
                    }
                  }}
                >
                  ⧉ Copy
                </button>
              </pre>
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
