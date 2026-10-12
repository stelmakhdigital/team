import { useQuery } from '../hooks/useQuery';
import { api } from '../api';
import { useEscapeStack } from '../hooks/useEscapeStack';
import { errorMessage } from './ui/States';
import { useToast } from './ui/Toast';

// W3.2: SpecModal — read-only вью agent spec (yaml в <pre>, без подсветки —
// честно, без зависимостей). CTA: Download, copy. name=null → не рендерится.
interface Props {
  name: string | null;
  onClose: () => void;
}

export default function SpecModal({ name, onClose }: Props) {
  const { toast } = useToast();
  const q = useQuery(
    name ? `agent-spec.content.${name}` : 'agent-spec.content.idle',
    () => api.teams.getAgentSpec(name!),
    [name],
  );
  useEscapeStack(onClose, !!name);

  if (!name) return null;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(q.data?.yaml ?? '');
      toast('success', 'Spec copied to clipboard');
    } catch {
      toast('error', 'Copy failed');
    }
  };

  const download = () => {
    const yaml = q.data?.yaml ?? '';
    const blob = new Blob([yaml], { type: 'text/yaml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${name}.yaml`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const is404 = q.error?.status === 404;

  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={`Spec: ${name}`} data-hotkey-exclude onClick={onClose}>
      <div className="modal spec-modal" onClick={(e) => e.stopPropagation()}>
        <div className="spec-modal-head">
          <h3>
            Spec: <span className="mono">{name}</span> · read-only
          </h3>
          <div className="spec-modal-actions">
            {q.data && (
              <>
                <button type="button" className="btn btn-small" onClick={copy} data-testid="spec-copy">
                  Copy
                </button>
                <button type="button" className="btn btn-small" onClick={download} data-testid="spec-download">
                  Download
                </button>
              </>
            )}
            <button type="button" className="btn btn-small" onClick={onClose} aria-label="Close spec">
              ✕
            </button>
          </div>
        </div>
        {q.loading ? (
          <div className="muted small spec-modal-status">Loading spec…</div>
        ) : q.error ? (
          <div className={`spec-modal-status ${is404 ? '' : 'hint-error'}`} data-testid="spec-error">
            {is404 ? (
              <span>Spec «{name}» не найден (файл отсутствует в SpecsDir).</span>
            ) : (
              <span>{errorMessage(q.error)}</span>
            )}
          </div>
        ) : (
          <pre className="spec-yaml" data-testid="spec-yaml">{q.data?.yaml}</pre>
        )}
      </div>
    </div>
  );
}
