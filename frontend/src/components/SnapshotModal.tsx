
import { useEscapeStack } from '../hooks/useEscapeStack';
import { useState } from 'react';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { errorMessage } from './ui/States';

interface Props {
  sessionId: number;
  defaultName: string;
  onClose: () => void;
  onCreated?: (libraryItemId: number) => void;
}

/**
 * P3.12: «Сохранить сессию как agent image». Имя + (опц.) summary →
 * POST /sessions/{id}/snapshot → library item type=agent_image.
 */
export default function SnapshotModal({ sessionId, defaultName, onClose, onCreated }: Props) {
  const [name, setName] = useState(defaultName);
  const [summary, setSummary] = useState('');
  const [description, setDescription] = useState('');
  const snapshot = useMutation((req: { name: string; summary?: string; description?: string }) =>
    api.sessions.snapshot(sessionId, req),
  );

  const onSubmit = async () => {
    if (!name.trim()) return;
    try {
      const res = await snapshot.mutate({
        name: name.trim(),
        summary: summary.trim() || undefined,
        description: description.trim() || undefined,
      });
      onCreated?.(res.id);
      onClose();
    } catch {
      /* error shown below */
    }
  };

    // W3.1: Esc закрывает модалку (если она верхний оверлей)
  useEscapeStack(onClose, true);

return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" data-hotkey-exclude onClick={onClose}>
      <div className="modal snapshot-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>Save as agent image</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="modal-body">
          <p className="muted small">
            Сохраняет конфигурацию агента этой сессии (модель, системный промпт, скиллы, tools) как
            снимок в библиотеке. Из снимка можно запустить новую сессию с этим же настроенным агентом.
          </p>
          <label className="briefing-opts field" style={{ marginBottom: 10 }}>
            <span className="muted small">Name</span>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} aria-label="Image name" autoFocus />
          </label>
          <label className="briefing-opts field" style={{ marginBottom: 10 }}>
            <span className="muted small">Summary (optional)</span>
            <textarea
              className="input"
              rows={4}
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              placeholder="Что важно для нового агента: контекст, что уже сделано, что осталось"
              aria-label="Summary"
            />
          </label>
          <label className="briefing-opts field" style={{ marginBottom: 10 }}>
            <span className="muted small">Description (optional)</span>
            <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} aria-label="Description" />
          </label>
          {snapshot.error && (
            <div className="field-error" role="alert" style={{ color: 'var(--red)' }}>
              {errorMessage(snapshot.error)}
            </div>
          )}
        </div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="btn btn-primary" disabled={snapshot.pending || !name.trim()} onClick={() => void onSubmit()}>
            {snapshot.pending ? 'Saving…' : 'Save image'}
          </button>
        </div>
      </div>
    </div>
  );
}
