
import { useEscapeStack } from '../hooks/useEscapeStack';
import { useState, useRef } from 'react';
import { useFocusTrap } from '../hooks/useFocusTrap';
import type { HandoverResult, HandoverStep } from '../types/api';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import EntityLink from './ui/EntityLink';
import { errorMessage } from './ui/States';

const STEP_LABEL: Record<HandoverStep['status'], string> = {
  ok: 'выполнено',
  failed: 'ошибка',
  skipped: 'пропущено',
  would: 'было бы',
};

function StepRow({ name, step }: { name: string; step: HandoverStep }) {
  return (
    <li className={`handover-step handover-step--${step.status}`}>
      <strong>{name}:</strong> {STEP_LABEL[step.status]}
      {step.reason ? <span className="muted"> — {step.reason}</span> : null}
      {step.detail ? <span className="muted"> — {step.detail}</span> : null}
    </li>
  );
}

/**
 * P1.5 — HandoverModal: форма "Продолжить в новой сессии" (dry-run план + полный прогон).
 */
export default function HandoverModal({ sessionId, onClose }: { sessionId: number; onClose: () => void }) {
  const [reason, setReason] = useState('');
  const [plan, setPlan] = useState<HandoverResult | null>(null);
  const [result, setResult] = useState<HandoverResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const canRun = reason.trim().length > 0;

  const dryRun = useMutation(() => api.sessions.handover(sessionId, { reason: reason.trim(), dry_run: true }));
  const execute = useMutation(() => api.sessions.handover(sessionId, { reason: reason.trim() }));

  const showPlan = () => {
    setError(null);
    dryRun.mutate().then(setPlan).catch((e) => setError(errorMessage(e)));
  };
  const run = () => {
    setError(null);
    execute.mutate().then(setResult).catch((e) => setError(errorMessage(e)));
  };

    // W3.1: Esc закрывает модалку (если она верхний оверлей)
  useEscapeStack(onClose, true);

  // U-X4: focus-trap + возврат фокуса на триггер
  const backdropRef = useRef<HTMLDivElement>(null);
  useFocusTrap(backdropRef, true);

return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" data-hotkey-exclude onClick={onClose} ref={backdropRef}>
      <div className="modal handover-modal" onClick={(e) => e.stopPropagation()}>
        <h2>Продолжить в новой сессии</h2>
        <label>
          Причина handover *
          <textarea
            rows={2}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="напр. context wall 88%"
          />
        </label>

        {!result && (
          <div className="modal-actions handover-actions">
            <button type="button" className="btn" disabled={!canRun || dryRun.pending} onClick={showPlan}>
              {dryRun.pending ? 'Считаю план…' : 'Показать план'}
            </button>
            <button type="button" className="btn btn-primary" disabled={!canRun || execute.pending} onClick={run}>
              {execute.pending ? 'Выполняю…' : 'Выполнить'}
            </button>
          </div>
        )}
        {!canRun && !result && <p className="muted small">Укажите причину handover.</p>}

        {error && <div className="form-error">{error}</div>}

        {plan && !result && (
          <div className="handover-result">
            <h3>План (dry-run)</h3>
            <ul className="handover-steps">
              <StepRow name="Контекст" step={plan.context_prepared} />
              <StepRow name="Наследник" step={plan.successor_started} />
              <StepRow name="Задача" step={plan.task_reassigned} />
              <StepRow name="Стоп старой" step={plan.old_stopped} />
            </ul>
            {plan.context_package && (
              <p className="muted small">
                Контекст: {plan.context_package.lines} строк
                {plan.context_package.task ? ` · задача ${plan.context_package.task.id}` : ''}
              </p>
            )}
            {plan.successor_preview && (
              <p className="muted small">
                Наследник: {plan.successor_preview.role_name} / {plan.successor_preview.team_name} · runtime {plan.successor_preview.runtime_type}
              </p>
            )}
          </div>
        )}

        {result && (
          <div className="handover-result">
            <h3 className={`handover-status--${result.status}`}>
              {result.status === 'succeeded' ? 'Handover выполнен' : result.status === 'partial' ? 'Handover с ошибками' : 'Handover не выполнен'}
            </h3>
            <ul className="handover-steps">
              <StepRow name="Контекст" step={result.context_prepared} />
              <StepRow name="Наследник" step={result.successor_started} />
              <StepRow name="Задача" step={result.task_reassigned} />
              <StepRow name="Стоп старой" step={result.old_stopped} />
            </ul>
            {result.new_session_id && (
              <p>
                Новая сессия:{' '}
                <EntityLink to={`/sessions/${result.new_session_id}`} title={`Сессия #${result.new_session_id}`}>
                  Сессия #{result.new_session_id}
                </EntityLink>
              </p>
            )}
            <div className="modal-actions">
              <button type="button" className="btn btn-primary" onClick={onClose}>Закрыть</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
