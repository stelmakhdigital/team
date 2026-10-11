import { useState } from 'react';
import { api } from '../api';
import { useQuery } from '../hooks/useQuery';
import { useMutation } from '../hooks/useMutation';
import { Spinner, errorMessage } from './ui/States';
import { useToast } from './ui/Toast';
import { formatDateTime } from '../lib/format';
import type { Task, TaskEvidence, EvidenceType, EvidenceAction } from '../types/api';

// P3.13: Proof/evidence на задачах (closure ≠ acceptance).
// Панель: список артефактов + submit + judge (accept/reject/withdraw) + readiness/acceptance.

const EVIDENCE_TYPES: EvidenceType[] = ['screenshot', 'test', 'log', 'other'];

function shortSha(sha?: string): string {
  if (!sha) return '—';
  return sha.length > 12 ? `${sha.slice(0, 12)}…` : sha;
}

function statusBadge(status: TaskEvidence['status']) {
  const cls = `badge ev-status ev-status--${status}`;
  return <span className={cls}>{status}</span>;
}

export default function EvidencePanel({ task, onChanged }: { task: Task; onChanged?: () => void }) {
  const { toast } = useToast();
  const [submitting, setSubmitting] = useState(false);
  const ev = useQuery(`evidence.list.${task.id}`, () => api.tasks.listEvidence(task.id, { limit: 50 }), [task.id]);

  const submit = useMutation((req: Parameters<typeof api.tasks.submitEvidence>[1]) => api.tasks.submitEvidence(task.id, req));
  const verdict = useMutation((evidId: number, req: Parameters<typeof api.tasks.verdictEvidence>[2]) => api.tasks.verdictEvidence(task.id, evidId, req));

  // submit-форма (inline)
  const [form, setForm] = useState<{ type: EvidenceType; name: string; content: string; content_ref: string; content_sha: string; description: string }>({
    type: 'test', name: '', content: '', content_ref: '', content_sha: '', description: '',
  });
  const doSubmit = async () => {
    const payload = {
      type: form.type,
      name: form.name.trim(),
      content: form.content || undefined,
      content_ref: form.content_ref || undefined,
      content_sha: form.content_sha || undefined,
      description: form.description || undefined,
    };
    if (!payload.name) return;
    try {
      await submit.mutate(payload);
      toast('success', 'Evidence submitted');
      setForm({ type: 'test', name: '', content: '', content_ref: '', content_sha: '', description: '' });
      setSubmitting(false);
      ev.refetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Submit failed');
    }
  };

  const doVerdict = async (evidId: number, action: EvidenceAction) => {
    const reason = action === 'withdraw' ? '' : window.prompt('Reason (optional):') ?? '';
    try {
      await verdict.mutate(evidId, { action, reason: reason || undefined });
      toast('success', `Verdict: ${action}`);
      ev.refetch();
      // P-fix: the task header (state/acceptance/readiness) comes from the parent
      // task query — refetch it after a verdict so it updates without manual reload.
      onChanged?.();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Verdict failed');
    }
  };

  const items = ev.data?.evidence ?? [];

  return (
    <div className="card evidence-panel" data-testid="evidence-panel">
      <div className="card-head">
        <h2>Evidence / proof</h2>
      </div>
      <p className="muted small ev-note">
        Closure ≠ acceptance: закрытие задачи (done) ≠ принятие результата. Acceptance — это
        вердикт судьи (accept/reject) по артефакту.
      </p>

      {ev.loading ? (
        <Spinner />
      ) : ev.error ? (
        <p className="field-error" role="alert" style={{ color: 'var(--red)' }}>{errorMessage(ev.error)}</p>
      ) : items.length === 0 ? (
        <p className="muted">No evidence yet — attach a proof artifact below.</p>
      ) : (
        <ul className="ev-list">
          {items.map((e) => (
            <li key={e.id} className="ev-item" data-testid="evidence-item">
              <div className="ev-item-head">
                <span className="badge ev-type ev-type--{e.type}" title="Type of artifact">{e.type}</span>
                <strong className="ev-name">{e.name}</strong>
                {statusBadge(e.status)}
              </div>
              <div className="muted small ev-meta">
                sha <code>{shortSha(e.content_sha)}</code> · by {e.submitted_by}
                {e.content_ref ? <span> · <a href={e.content_ref} target="_blank" rel="noreferrer">{e.content_ref}</a></span> : null}
                {e.judged_by ? <span> · judged by {e.judged_by}{e.judged_at ? ` ${formatDateTime(e.judged_at)}` : ''}</span> : null}
              </div>
              {e.description && <div className="muted small ev-desc">{e.description}</div>}
              {e.verdict_reason && <div className="muted small ev-reason">verdict: {e.verdict_reason}</div>}
              <div className="ev-judge">
                <span className="muted small">Judge:</span>
                {e.status !== 'accepted' && (
                  <button className="btn btn-small btn-primary" disabled={verdict.pending} onClick={() => void doVerdict(e.id, 'accept')}>✓ Accept</button>
                )}
                {e.status !== 'rejected' && (
                  <button className="btn btn-small" disabled={verdict.pending} onClick={() => void doVerdict(e.id, 'reject')}>✗ Reject</button>
                )}
                {(e.status === 'accepted' || e.status === 'rejected') && (
                  <button className="btn btn-small btn-danger" disabled={verdict.pending} onClick={() => void doVerdict(e.id, 'withdraw')}>↩ Withdraw</button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {submitting ? (
        <form className="ev-submit" onSubmit={(e) => { e.preventDefault(); void doSubmit(); }}>
          <div className="ev-submit-row">
            <select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value as EvidenceType })} aria-label="Evidence type" className="input">
              {EVIDENCE_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
            </select>
            <input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Name (required)" aria-label="Evidence name" />
          </div>
          <textarea
            className="input" rows={3} value={form.content}
            onChange={(e) => setForm({ ...form, content: e.target.value })}
            placeholder="Inline content (server computes sha256 = candidate-sha)" aria-label="Evidence content"
          />
          <div className="ev-submit-row">
            <input className="input" value={form.content_ref} onChange={(e) => setForm({ ...form, content_ref: e.target.value })} placeholder="…or content_ref (URL/path)" aria-label="Evidence content ref" />
            <input className="input" value={form.content_sha} onChange={(e) => setForm({ ...form, content_sha: e.target.value })} placeholder="content_sha (for ref)" aria-label="Evidence content sha" />
          </div>
          <input className="input" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="Description (optional)" aria-label="Evidence description" />
          {submit.error && <div className="field-error" role="alert" style={{ color: 'var(--red)' }}>{errorMessage(submit.error)}</div>}
          <div className="wf-config-actions">
            <button type="submit" className="btn btn-primary" disabled={submit.pending || !form.name.trim()}>{submit.pending ? 'Submitting…' : 'Submit evidence'}</button>
            <button type="button" className="btn" onClick={() => setSubmitting(false)}>Cancel</button>
          </div>
        </form>
      ) : (
        <button className="btn btn-small" onClick={() => setSubmitting(true)} data-testid="evidence-add-btn">+ Add evidence</button>
      )}
    </div>
  );
}
