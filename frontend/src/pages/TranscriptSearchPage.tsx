import { useMemo, useState, type FormEvent } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import type { SessionState, TranscriptMatch, TranscriptSearchParams, TranscriptSearchResult } from '../types/api';
import { EmptyState, PageSkeleton, errorMessage } from '../components/ui/States';
import EntityLink from '../components/ui/EntityLink';

const STATE_OPTIONS: (SessionState | '')[] = ['', 'starting', 'running', 'idle', 'stopping', 'stopped', 'failed'];

// подсветка совпадения q в строке (case-insensitive) → <mark>.
function Highlight({ line, q }: { line: string; q: string }) {
  if (!q) return <>{line}</>;
  const parts: React.ReactNode[] = [];
  const re = new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'ig');
  let last = 0;
  let m: RegExpExecArray | null;
  let k = 0;
  while ((m = re.exec(line)) !== null) {
    if (m.index > last) parts.push(line.slice(last, m.index));
    parts.push(<mark key={`m${k++}`} className="ts-match">{m[0]}</mark>);
    last = m.index + m[0].length;
    if (m.index === re.lastIndex) re.lastIndex++;
  }
  if (last < line.length) parts.push(line.slice(last));
  return <>{parts}</>;
}

// группировка результатов по сессии (input уже отсортирован: session_id desc → line asc).
function groupBySession(items: TranscriptMatch[]): Map<number, TranscriptMatch[]> {
  const g = new Map<number, TranscriptMatch[]>();
  for (const it of items) {
    const arr = g.get(it.session_id) ?? [];
    arr.push(it);
    g.set(it.session_id, arr);
  }
  return g;
}

// P2.7 — Поиск по транскриптам: q + фильтры (team/role/state) → результаты,
// сгруппированные по сессии (role/team/state + EntityLink), под каждой — совпадения
// с контекст-окном (подсветка).
export default function TranscriptSearchPage() {
  const [q, setQ] = useState('');
  const [teamId, setTeamId] = useState<number | ''>('');
  const [roleId, setRoleId] = useState<number | ''>('');
  const [state, setState] = useState<SessionState | ''>('');
  const [result, setResult] = useState<TranscriptSearchResult | null>(null);
  const [searched, setSearched] = useState(false);

  const { data: teams } = useQuery('teams', () => api.teams.getTeams(), []);
  const { data: teamDetail } = useQuery(
    teamId ? `team.roles.${teamId}` : 'team.roles.none',
    () => (teamId ? api.teams.getTeam(teamId) : Promise.resolve(undefined)),
    [teamId],
  );
  const roles = teamDetail?.roles ?? [];

  const search = useMutation((p: TranscriptSearchParams) => api.sessions.search(p));

  const doSearch = (e: FormEvent) => {
    e.preventDefault();
    const query = q.trim();
    if (query.length < 2) return;
    const params: TranscriptSearchParams = {
      q: query,
      team_id: teamId !== '' ? Number(teamId) : undefined,
      role_id: roleId !== '' ? Number(roleId) : undefined,
      state: state || undefined,
      limit: 50,
    };
    search.mutate(params).then((r) => { setResult(r); setSearched(true); }).catch(() => {});
  };

  const groups = useMemo(() => (result ? groupBySession(result.results) : new Map<number, TranscriptMatch[]>()), [result]);
  const activeQ = q.trim();

  return (
    <div className="page">
      <div className="page-head">
        <h1>Поиск по транскриптам</h1>
      </div>

      <form className="panel ts-form" onSubmit={doSearch}>
        <div className="ts-form-grid">
          <label className="ts-field ts-field--q">
            <span>Поиск</span>
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="фраза или решение (min 2 символа)"
              minLength={2}
              autoFocus
            />
          </label>
          <label className="ts-field">
            <span>Команда</span>
            <select value={teamId} onChange={(e) => { setTeamId(e.target.value ? Number(e.target.value) : ''); setRoleId(''); }}>
              <option value="">— все —</option>
              {(teams?.teams ?? []).map((t) => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))}
            </select>
          </label>
          <label className="ts-field">
            <span>Роль</span>
            <select value={roleId} onChange={(e) => setRoleId(e.target.value ? Number(e.target.value) : '')} disabled={!teamId}>
              <option value="">{teamId ? '— все —' : 'выберите команду'}</option>
              {roles.map((r) => (
                <option key={r.id} value={r.id}>{r.name}</option>
              ))}
            </select>
          </label>
          <label className="ts-field">
            <span>Состояние</span>
            <select value={state} onChange={(e) => setState(e.target.value as SessionState | '')}>
              {STATE_OPTIONS.map((s) => (
                <option key={s || 'all'} value={s}>{s || '— все —'}</option>
              ))}
            </select>
          </label>
          <button type="submit" className="btn btn-primary" disabled={activeQ.length < 2 || search.pending}>
            {search.pending ? 'Ищу…' : '🔎 Найти'}
          </button>
        </div>
        {search.error ? <div className="error-banner">{errorMessage(search.error)}</div> : null}
      </form>

      {search.pending ? (
        <PageSkeleton rows={5} label="Ищу по транскриптам…" />
      ) : result ? (
        result.results.length === 0 ? (
          <EmptyState title="Ничего не найдено" hint={`По запросу «${activeQ}» совпадений нет. Попробуйте другой запрос или снимите фильтры.`} />
        ) : (
          <div className="ts-results">
            <div className="ts-results-meta muted small">
              Найдено {result.total} совпадений · показано {result.results.length}
              {result.has_more ? ' · есть ещё (has_more)' : ''}
            </div>
            {[...groups.entries()].map(([sessionId, matches]) => {
              const first = matches[0];
              return (
                <div key={sessionId} className="ts-session">
                  <div className="ts-session-head">
                    <span className={`ts-state ts-state--${first.state}`}>{first.state}</span>
                    <code className="ts-role">{first.role_name}</code>
                    <span className="muted small">· {first.team_name}</span>
                    <span className="muted small">· {matches.length} совп.</span>
                    <EntityLink to={`/sessions/${sessionId}`} title={`Открыть сессию #${sessionId}`} className="ts-open">
                      Открыть сессию #{sessionId} →
                    </EntityLink>
                  </div>
                  <div className="ts-matches">
                    {matches.map((m, i) => (
                      <div key={`${sessionId}-${m.line_number}-${i}`} className="ts-match-block">
                        <span className="ts-line-no">:{m.line_number}</span>
                        <pre className="ts-context">
                          {m.context.map((line, j) => (
                            <div key={j} className="ts-ctx-line"><Highlight line={line} q={activeQ} /></div>
                          ))}
                        </pre>
                      </div>
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        )
      ) : searched ? (
        <EmptyState title="Ничего не найдено" hint="Попробуйте другой запрос." />
      ) : (
        <EmptyState title="Введите запрос" hint="Найдите фразу или решение по истории вывода всех сессий." />
      )}
    </div>
  );
}
