import { useEffect, useMemo, useState } from 'react';
import type { BriefingPreviewResponse, CreateSessionResponse, Role, Task } from '../types/api';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import EntityLink from './ui/EntityLink';
import { errorMessage } from './ui/States';

/**
 * P1.7 — BriefingPreviewModal: «Предпросмотр брифинга» + запуск сессии с этим
 * брифингом. Dry-run: показывает, какой briefing агент получит (override/append),
 * и позволяет запустить сессию с этим брифингом (role/task-селект + «Запустить»).
 */
export default function BriefingPreviewModal({
  teamId,
  roles,
  initialRole,
  onClose,
  onLaunched,
}: {
  teamId: number;
  roles: Role[];
  initialRole?: Role;
  onClose: () => void;
  onLaunched?: () => void;
}) {
  const [role, setRole] = useState<Role>(initialRole ?? roles[0]);
  const [taskId, setTaskId] = useState<number>(0); // 0 = без задачи
  const [override, setOverride] = useState('');
  const [append, setAppend] = useState('');
  const [preview, setPreview] = useState<BriefingPreviewResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<CreateSessionResponse | null>(null);

  const tasksQ = useQuery(`team.tasks.${teamId}`, () => api.tasks.list({ team_id: teamId }), [teamId]);
  const tasks: Task[] = useMemo(() => tasksQ.data?.tasks ?? [], [tasksQ.data]);

  const previewMutate = useMutation(() =>
    api.sessions.previewBriefing(teamId, {
      role_id: role.id,
      queue_task_id: taskId || undefined,
      override: override.trim() || undefined,
      append: append.trim() || undefined,
    }),
  );

  const launch = useMutation(() =>
    api.sessions.create(teamId, {
      role_id: role.id,
      queue_task_id: taskId || undefined,
      config: {
        briefing_override: override.trim() || undefined,
        briefing_append: append.trim() || undefined,
        // QA-fix: автономная daemon-сессия — выключаем интерактивные pi-guard'ы
        // (bash-guard/dir-guard), иначе агент встаёт на подтверждении без оператора.
        no_extensions: true,
      },
    }),
  );

  const runPreview = () => {
    setError(null);
    setPreview(null);
    previewMutate.mutate().then(setPreview).catch((e) => setError(errorMessage(e)));
  };

  useEffect(() => {
    runPreview();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [role.id, taskId]);

  const doLaunch = () => {
    setError(null);
    launch.mutate().then((r) => {
      setResult(r);
      onLaunched?.();
    }).catch((e) => setError(errorMessage(e)));
  };

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal briefing-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>Брифинг сессии</h2>
          <button className="btn btn-icon" onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </div>

        {!result ? (
          <>
            <p className="muted small">
              Покажите, какой briefing агент получит при запуске (dry-run), и запустите
              сессию с этим брифингом.
            </p>

            <div className="briefing-launch">
              <label className="field">
                <span>Роль *</span>
                <select value={role.id} onChange={(e) => setRole(roles.find((r) => r.id === Number(e.target.value)) ?? role)}>
                  {roles.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Задача (опц.)</span>
                <select value={taskId} onChange={(e) => setTaskId(Number(e.target.value))}>
                  <option value={0}>— без задачи —</option>
                  {tasks.map((t) => (
                    <option key={t.id} value={t.id}>
                      #{t.id} {t.title} ({t.state})
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <div className="briefing-opts">
              <label className="field">
                <span>Override (вместо builtin, optional)</span>
                <textarea
                  rows={2}
                  value={override}
                  onChange={(e) => setOverride(e.target.value)}
                  placeholder="Свой briefing (перекрывает стандартный)"
                />
              </label>
              <label className="field">
                <span>Append (добавка, optional)</span>
                <input value={append} onChange={(e) => setAppend(e.target.value)} placeholder="Доп. контекст в конец" />
              </label>
              <button className="btn" onClick={runPreview} disabled={previewMutate.pending}>
                {previewMutate.pending ? 'Собираю…' : 'Обновить предпросмотр'}
              </button>
            </div>

            {error ? <div className="error-banner">{error}</div> : null}

            {preview ? (
              <>
                <div className="briefing-delivery">
                  Передача: <strong>{preview.delivery.mode === 'pi-system-prompt' ? 'pi → system_prompt' : 'файл + env'}</strong>
                  <span className="muted small"> — {preview.delivery.detail}</span>
                  <span className="badge badge--entity" style={{ marginLeft: 8 }}>
                    {preview.briefing_applied}
                  </span>
                </div>
                <pre className="briefing-text">{preview.briefing_text}</pre>
              </>
            ) : !previewMutate.pending ? null : (
              <div className="briefing-loading muted">Собираю briefing…</div>
            )}

            <div className="briefing-launch-actions">
              <button className="btn btn-primary" onClick={doLaunch} disabled={launch.pending || !role}>
                {launch.pending ? 'Запускаю…' : '▶\uFE0E Запустить с этим брифингом'}
              </button>
            </div>
          </>
        ) : (
          <div className="briefing-launch-result">
            <div className="briefing-launched-ok">
              <strong>✓ Сессия #{result.id} запущена.</strong>{' '}
              Briefing: <span className="badge badge--entity">{result.briefing_applied ?? 'none'}</span>
            </div>
            {result.briefing_path ? <p className="muted small">{result.briefing_path}</p> : null}
            <p>
              Открыть сессию:{' '}
              <EntityLink to={`/sessions/${result.id}`}>/sessions/{result.id}</EntityLink>
            </p>
            <div className="briefing-launch-actions">
              <button className="btn" onClick={onClose}>Закрыть</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
