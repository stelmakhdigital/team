import type { TopologyError, TopologyWarning } from '../../types/api';

interface BottomPanelProps {
  zoom: number;
  onZoom: (z: number) => void;
  onFit: () => void;
  grid: boolean;
  onGrid: (v: boolean) => void;
  snap: boolean;
  onSnap: (v: boolean) => void;
  validating: boolean;
  validation: { is_valid: boolean; errors: TopologyError[]; warnings: TopologyWarning[] } | null;
  onValidate: () => void;
  saving: boolean;
  onSave: () => void;
  onDeleteSelection: (() => void) | null;
  selectionLabel: string | null;
}

export default function BottomPanel(props: BottomPanelProps) {
  const { zoom, onZoom, onFit, grid, onGrid, snap, onSnap, validating, validation, onValidate, saving, onSave, onDeleteSelection, selectionLabel } = props;
  return (
    <div className="bottom-panel">
      <div className="bottom-left">
        <div className="zoom-controls" aria-label="Zoom">
          <button className="btn-icon" onClick={() => onZoom(Math.max(0.4, zoom - 0.1))} aria-label="Zoom out">
            −
          </button>
          <span>{Math.round(zoom * 100)}%</span>
          <button className="btn-icon" onClick={() => onZoom(Math.min(2, zoom + 0.1))} aria-label="Zoom in">
            +
          </button>
          <button className="btn-icon" onClick={onFit} aria-label="Fit to view" title="Fit whole topology into view">
            ⤢
          </button>
        </div>
        <label className="check">
          <input type="checkbox" checked={grid} onChange={(e) => onGrid(e.target.checked)} />
          Grid
        </label>
        <label className="check">
          <input type="checkbox" checked={snap} onChange={(e) => onSnap(e.target.checked)} />
          Snap
        </label>
      </div>

      <div className="bottom-center">
        {validation ? (
          <div className={`validation${validation.is_valid ? ' valid' : ' invalid'}`}>
            {validation.is_valid && <span className="validation-ok">✓ Topology valid</span>}
            {validation.errors.map((e) => (
              <div key={`${e.code}-${e.location.id}`} className="validation-item error">
                ✖ {e.message}
                {e.fix_suggestion && <span className="muted small"> — {e.fix_suggestion}</span>}
              </div>
            ))}
            {validation.warnings.map((w, i) => (
              <div key={`${w.code}-${i}`} className="validation-item warning">
                ⚠ {w.message}
              </div>
            ))}
            {validation.is_valid && validation.warnings.length === 0 && <span className="validation-ok">No warnings</span>}
          </div>
        ) : (
          <span className="muted small">Validation: not run</span>
        )}
      </div>

      <div className="bottom-right">
        {onDeleteSelection && selectionLabel && (
          <button className="btn btn-danger" onClick={onDeleteSelection}>
            Delete {selectionLabel}
          </button>
        )}
        <button className="btn" onClick={onValidate} disabled={validating}>
          {validating ? 'Validating…' : 'Validate'}
        </button>
        <button className="btn btn-primary" onClick={onSave} disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
  );
}
