import { useState } from 'react';
import { useMutation } from '../hooks/useMutation';
import { api } from '../api';
import type { Posture } from '../types/api';

// P3.16 — Mode/permission posture (delegated vs human-led).
// kind=team: current = team.posture (база).
// kind=session: current = session.posture override (null/undefined = наследует от команды);
// teamPosture = postura команды (для effective + "inherit" option).
// Optimistic: internal state (stored) обновляется сразу, при ошибке — откат.
interface PosturePanelProps {
  kind: 'team' | 'session';
  id: number; // teamId или sessionId
  current: Posture | null | undefined; // stored value (initial)
  teamPosture?: Posture; // для session: postura команды (effective + inherit)
}

type StoredPosture = 'inherit' | Posture;

function toErr(e: unknown): { code: string; message: string } {
  const code = (e as { code?: string })?.code ?? 'error';
  const message = e instanceof Error ? e.message : 'Unknown error';
  return { code, message };
}

function initialStored(kind: 'team' | 'session', current: Posture | null | undefined): StoredPosture {
  if (kind === 'session') return current ?? 'inherit';
  return current ?? 'delegated';
}

export default function PosturePanel({ kind, id, current, teamPosture }: PosturePanelProps) {
  const [stored, setStored] = useState<StoredPosture>(() => initialStored(kind, current));
  const [error, setError] = useState<{ code: string; message: string } | null>(null);
  const m = useMutation(async (p: Posture | null) => {
    if (kind === 'team') return api.posture.setTeam(id, p as Posture);
    return api.posture.setSession(id, p);
  });

  const isSession = kind === 'session';
  const storedPosture: Posture | null = isSession
    ? stored === 'inherit'
      ? null
      : stored
    : stored === 'inherit'
      ? 'delegated'
      : stored;
  const effective: Posture = isSession ? storedPosture ?? teamPosture ?? 'delegated' : storedPosture ?? 'delegated';

  const pick = async (p: Posture | null) => {
    const next: StoredPosture = isSession ? (p ?? 'inherit') : (p ?? 'delegated');
    setStored(next); // optimistic
    setError(null);
    try {
      await m.mutate(p);
    } catch (e) {
      setStored(initialStored(kind, current)); // revert
      setError(toErr(e));
    }
  };

  return (
    <div className="card posture-card" data-testid="posture-panel">
      <div className="card-head">
        <h2>Posture</h2>
        <span className={`posture-badge posture-badge--${effective}`} data-testid="posture-badge" title="Effective posture">
          {effective === 'human_led' ? '👤 Human-led' : '🤖 Delegated'}
        </span>
      </div>
      <p className="muted small posture-hint">
        {isSession
          ? 'Human-led: чувствительные операции сессии (stop, handover) паркуются → подтверждение. Delegated: выполняется сразу.'
          : 'Human-led: чувствительные операции команды (stop/прune/abort) паркуются → human_request (подтверждение).'}
      </p>
      <div className="posture-controls" role="radiogroup" aria-label="Posture">
        {isSession && (
          <label className={`posture-opt ${stored === 'inherit' ? 'on' : ''}`}>
            <input
              type="radio"
              name={`posture-${kind}-${id}`}
              checked={stored === 'inherit'}
              disabled={m.pending}
              onChange={() => pick(null)}
            />
            <span>Inherit</span>
            <span className="muted small">от команды: {teamPosture ?? 'delegated'}</span>
          </label>
        )}
        <label className={`posture-opt ${stored === 'delegated' ? 'on' : ''}`}>
          <input
            type="radio"
            name={`posture-${kind}-${id}`}
            checked={stored === 'delegated'}
            disabled={m.pending}
            onChange={() => pick('delegated')}
          />
          <span>Delegated</span>
          <span className="muted small">авто (без подтверждения)</span>
        </label>
        <label className={`posture-opt ${stored === 'human_led' ? 'on' : ''}`}>
          <input
            type="radio"
            name={`posture-${kind}-${id}`}
            checked={stored === 'human_led'}
            disabled={m.pending}
            onChange={() => pick('human_led')}
          />
          <span>Human-led</span>
          <span className="muted small">подтверждение (park)</span>
        </label>
      </div>
      {m.pending ? <div className="muted small posture-pending">Применяю…</div> : null}
      {error ? (
        <div className="form-error" role="alert">
          {error.message}
        </div>
      ) : null}
    </div>
  );
}
