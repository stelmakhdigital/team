import { useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import type { Walk, WalkMode, WalkStatus, WalkStepState } from '../types/api';

// P3.14: Walk — paced-доставка контекста в сессию.
// Именованная последовательность шагов, доставляемых по одному (Send, P2.11).
// Pacing = interval (авто из reaper-loop) или manual (вручную).

const STATUS_LABEL: Record<WalkStatus, string> = {
  active: 'active',
  paused: 'paused',
  completed: 'completed',
  failed: 'failed',
};

const STEP_LABEL: Record<WalkStepState, string> = {
  pending: 'pending',
  sent: 'sent',
  delivered: 'delivered',
  staged: 'staged',
  retained: 'retained',
  failed: 'failed',
  skipped: 'skipped',
};

export default function WalkPanel({ sessionId }: { sessionId: number }) {
  const { data, loading, error, refetch } = useQuery(`walks.${sessionId}`, () => api.walks.list(sessionId));
  const walks = data?.walks ?? [];

  const [formOpen, setFormOpen] = useState(false);
  const [name, setName] = useState('');
  const [mode, setMode] = useState<WalkMode>('manual');
  const [intervalSecs, setIntervalSecs] = useState('60');
  const [steps, setSteps] = useState<string[]>(['']);

  const createMut = useMutation(() =>
    api.walks.create(sessionId, {
      name,
      mode,
      interval_secs: mode === 'interval' ? Number(intervalSecs) : undefined,
      steps: steps.map((s) => s.trim()).filter((s) => s.length > 0),
    }),
  );
  const act = useMutation(async (fn: () => Promise<Walk>) => fn());

  const onAdvance = async (walk: Walk) => {
    try {
      await act.mutate(() => api.walks.advance(walk.id));
      await refetch();
    } catch {
      /* error в useMutation */
    }
  };
  const onPause = async (walk: Walk) => {
    try {
      await act.mutate(() => api.walks.pause(walk.id));
      await refetch();
    } catch {
      /* */
    }
  };
  const onResume = async (walk: Walk) => {
    try {
      await act.mutate(() => api.walks.resume(walk.id));
      await refetch();
    } catch {
      /* */
    }
  };
  const onSkip = async (walk: Walk) => {
    try {
      await act.mutate(() => api.walks.skip(walk.id));
      await refetch();
    } catch {
      /* */
    }
  };
  const onCreate = async () => {
    try {
      await createMut.mutate();
      setFormOpen(false);
      setName('');
      setSteps(['']);
      await refetch();
    } catch {
      /* error в useMutation */
    }
  };

  const setStep = (i: number, v: string) => setSteps((prev) => prev.map((s, idx) => (idx === i ? v : s)));
  const addStep = () => setSteps((prev) => [...prev, '']);
  const removeStep = (i: number) => setSteps((prev) => prev.filter((_, idx) => idx !== i));
  const canCreate = !createMut.pending && name.trim().length > 0 && steps.some((s) => s.trim().length > 0);

  return (
    <div className="card walk-card">
      <div className="card-head">
        <h2>Walk (paced onboarding)</h2>
        <button type="button" className="btn btn-sm" onClick={() => setFormOpen((v) => !v)}>
          {formOpen ? '✕ Close' : '+ New walk'}
        </button>
      </div>
      <p className="muted small">
        Последовательность текстовых шагов, доставляемых в сессию по одному (Send). Pacing: manual (вручную) или
        interval (авто, не чаще poll).
      </p>

      {formOpen && (
        <div className="walk-create">
          <label className="walk-field">
            <span>Name</span>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Onboarding: project context" aria-label="Walk name" />
          </label>
          <label className="walk-field">
            <span>Mode</span>
            <select value={mode} onChange={(e) => setMode(e.target.value as WalkMode)} aria-label="Walk mode">
              <option value="manual">manual (вручную)</option>
              <option value="interval">interval (авто)</option>
            </select>
          </label>
          {mode === 'interval' && (
            <label className="walk-field">
              <span>Interval, сек</span>
              <input type="number" min={1} value={intervalSecs} onChange={(e) => setIntervalSecs(e.target.value)} aria-label="Interval seconds" />
            </label>
          )}
          <div className="walk-field">
            <span>Steps (порядок)</span>
            <div className="walk-steps-editor">
              {steps.map((s, i) => (
                <div key={i} className="walk-step-editor-row">
                  <span className="muted small">{i + 1}.</span>
                  <textarea
                    rows={2}
                    value={s}
                    onChange={(e) => setStep(i, e.target.value)}
                    placeholder={`Шаг ${i + 1}: что отправить`}
                    aria-label={`Шаг ${i + 1}`}
                  />
                  {steps.length > 1 && (
                    <button type="button" className="btn btn-sm" onClick={() => removeStep(i)} aria-label={`Удалить шаг ${i + 1}`}>
                      ✕
                    </button>
                  )}
                </div>
              ))}
              <button type="button" className="btn btn-sm" onClick={addStep}>
                + Добавить шаг
              </button>
            </div>
          </div>
          {mode === 'interval' && <p className="muted small">Авто: каждый шаг каждые {intervalSecs || 'N'}с (не чаще poll).</p>}
          {createMut.error ? (
            <p className="walk-error">Ошибка (HTTP {createMut.error.status}): {createMut.error.message}</p>
          ) : null}
          <div className="walk-actions">
            <button type="button" className="btn btn-primary btn-sm" onClick={onCreate} disabled={!canCreate}>
              {createMut.pending ? '…' : 'Create walk'}
            </button>
          </div>
        </div>
      )}

      {loading ? <p className="muted">Загрузка…</p> : null}
      {error ? <p className="walk-error">Ошибка (HTTP {error.status}): {error.message}</p> : null}
      {!loading && !error && walks.length === 0 ? (
        <p className="muted small">Нет walk'ов для этой сессии.</p>
      ) : null}

      <div className="walk-list">
        {walks.map((w) => (
          <div key={w.id} className={`walk-item walk-item-${w.status}`} data-testid="walk-item">
            <div className="walk-item-head">
              <span className="walk-item-name">{w.name}</span>
              <span className={`badge walk-badge-${w.status}`}>{STATUS_LABEL[w.status]}</span>
              <span className="walk-progress" data-testid="walk-progress">{w.progress}</span>
              {w.mode === 'interval' ? (
                <span className="muted small"> · авто ({w.interval_secs}с)</span>
              ) : null}
            </div>
            <ul className="walk-steps">
              {(w.steps ?? []).map((st) => (
                <li key={st.id} className={`walk-step walk-step-${st.state}`} data-testid="walk-step">
                  <span className="walk-step-seq">{st.seq}.</span>
                  <span className={`badge walk-step-badge-${st.state}`}>{STEP_LABEL[st.state]}</span>
                  <span className="walk-step-content">{st.content}</span>
                  {st.sent_at ? <span className="muted small"> · {new Date(st.sent_at).toLocaleTimeString()}</span> : null}
                  {st.note ? <span className="walk-step-note"> · {st.note}</span> : null}
                </li>
              ))}
            </ul>
            <div className="walk-actions" data-testid="walk-actions">
              {w.status === 'active' && (
                <>
                  <button type="button" className="btn btn-primary btn-sm" onClick={() => onAdvance(w)} disabled={act.pending}>
                    {act.pending ? '…' : '▶ Advance'}
                  </button>
                  <button type="button" className="btn btn-sm" onClick={() => onPause(w)} disabled={act.pending}>
                    ⏸ Pause
                  </button>
                  <button type="button" className="btn btn-sm" onClick={() => onSkip(w)} disabled={act.pending}>
                    ⏭ Skip
                  </button>
                </>
              )}
              {w.status === 'paused' && (
                <button type="button" className="btn btn-primary btn-sm" onClick={() => onResume(w)} disabled={act.pending}>
                  {act.pending ? '…' : '▶ Resume'}
                </button>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
