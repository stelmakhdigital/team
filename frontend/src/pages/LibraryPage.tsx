import { useState } from 'react';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { EmptyState, ErrorState, Spinner, Unavailable, isNotFoundError } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import { formatRelative } from '../lib/format';

export default function LibraryPage() {
  const [search, setSearch] = useState('');
  const [type, setType] = useState('');
  const [detailId, setDetailId] = useState<number | null>(null);
  const [saveTeamId, setSaveTeamId] = useState<number | null>(null);
  const [applyTargetId, setApplyTargetId] = useState<number | null>(null);
  const list = useQuery(`library.list.${type}.${search}`, () =>
    api.library.getLibrary({ type: (type || undefined) as never, search: search || undefined }),
    [type, search],
  );
  const detail = useQuery(detailId ? `library.item.${detailId}` : 'library.item.none', () =>
    detailId ? api.library.getLibraryItem(detailId) : Promise.resolve(null),
    [detailId],
  );
  const teams = useQuery('library.teams', () => api.teams.getTeams(), []);
  const save = useMutation((sourceId: number, teamName: string) =>
    api.library.saveToLibrary({
      type: 'team',
      source_id: sourceId,
      name: `${teamName} — ${new Date().toISOString().slice(0, 10)}`,
      group: 'Teams',
    }),
  );
  const apply = useMutation((itemId: number, req: { target_team_id?: number; overrides?: Record<string, unknown> }) =>
    api.library.applyLibrary(itemId, req),
  );
  const { toast } = useToast();

  const saveTeam = teams.data?.teams.find((t) => t.id === saveTeamId) ?? teams.data?.teams[0];

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
          {list.loading && <Spinner label="Loading library…" />}
          {list.error &&
          (isNotFoundError(list.error) ? (
            <Unavailable feature="Library" hint="The library endpoint is not implemented in the backend yet (planned)." />
          ) : (
            <ErrorState error={list.error} onRetry={list.refetch} />
          ))}
          {list.data && list.data.items.length === 0 && <EmptyState title="Nothing here" hint="Try a different search or type." />}
          <div className="library-grid">
            {list.data?.items.map((item) => (
              <button key={item.id} className="card library-card" onClick={() => setDetailId(item.id)}>
                <div className="library-card-head">
                  <span className="badge badge-type-{item.type}">{item.type}</span>
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
              </h3>
              <p className="muted small">
                author: {detail.data.item.author ?? '—'} · public: {detail.data.item.is_public ? 'yes' : 'no'}
              </p>

              {detail.data.item.type === 'team' && (
                <div className="library-apply">
                  <h4>Apply</h4>
                  <button
                    className="btn btn-small"
                    disabled={apply.pending}
                    onClick={async () => {
                      try {
                        const res = await apply.mutate(detail.data!.item.id, {});
                        toast('success', `Applied as new team (id ${res.created_resources?.teams?.[0] ?? '?'})`);
                        list.refetch();
                        teams.refetch();
                      } catch (e) {
                        toast('error', e instanceof Error ? e.message : 'Failed to apply');
                      }
                    }}
                  >
                    Apply as new team
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
                          toast('success', 'Workflow applied');
                          list.refetch();
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
                          list.refetch();
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

              <h4>Versions</h4>
              <ul>
                {detail.data.versions.map((v) => (
                  <li key={v.version} className="muted small">
                    {v.version} — {v.changes ?? ''} ({formatRelative(v.created_at)})
                  </li>
                ))}
              </ul>
              <h4>Spec</h4>
              <pre className="spec-pre">{JSON.stringify(detail.data.spec, null, 2)}</pre>
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
