import { useCallback, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { useToast } from '../components/ui/Toast';
import { errorMessage, ErrorState, Spinner } from '../components/ui/States';
import { validateTopologyGraph } from '../lib/topology';
import type { ValidateTopologyResponse } from '../types/api';
import TopologyCanvas, { type TopologySelection } from '../components/Topology/TopologyCanvas';
import ConfigPanel, { type ConfigSelection } from '../components/TeamBuilder/ConfigPanel';
import BottomPanel from '../components/TeamBuilder/BottomPanel';

export default function TeamBuilderPage() {
  const { teamId } = useParams();
  const id = Number(teamId);
  const { data, loading, error, refetch } = useQuery(`team.topology.${id}`, () => api.teams.getTopology(id), [id]);
  const { toast } = useToast();

  const [selection, setSelection] = useState<TopologySelection>(null);
  const [validation, setValidation] = useState<ValidateTopologyResponse | null>(null);

  const commitRefetch = useCallback(() => refetch(), [refetch]);

  const segments = data?.segments ?? [];
  const roles = data?.roles ?? [];
  const relatives = data?.relatives ?? [];

  // local instant hints (server remains source of truth on Validate)
  const localHints = useMemo(
    () => (data ? validateTopologyGraph(roles, segments, relatives) : null),
    [data, roles, segments, relatives],
  );

  const moveRole = useMutation((roleId: number, req: Parameters<typeof api.teams.updateRoleLayout>[1]) => api.teams.updateRoleLayout(roleId, req));
  const moveSegment = useMutation(
    (segmentId: number, req: Parameters<typeof api.teams.updateSegmentLayout>[1]) => api.teams.updateSegmentLayout(segmentId, req),
  );
  const deleteRelative = useMutation((rid: number) => api.teams.deleteRelative(rid));
  const validate = useMutation(() => api.teams.validateTopology(id));
  const save = useMutation(() => api.teams.saveTopology(id, { save_to_library: false }));

  const onRoleMoved = async (roleId: number, pos: { x: number; y: number }) => {
    try {
      await moveRole.mutate(roleId, { position: pos });
      commitRefetch();
    } catch {
      toast('error', errorMessage(moveRole.error ?? new Error('Failed to move role')));
      commitRefetch();
    }
  };

  const onSegmentMoved = async (segmentId: number, pos: { x: number; y: number }) => {
    try {
      await moveSegment.mutate(segmentId, { position: pos });
      commitRefetch();
    } catch {
      toast('error', errorMessage(moveSegment.error ?? new Error('Failed to move segment')));
      commitRefetch();
    }
  };

  const onValidate = async () => {
    try {
      const res = await validate.mutate();
      setValidation(res);
      if (res.is_valid) toast('success', 'Topology is valid');
      else toast('error', `Topology has ${res.errors.length} error(s)`);
    } catch {
      toast('error', errorMessage(validate.error ?? new Error('Validation failed')));
    }
  };

  const onSave = async () => {
    try {
      const res = await save.mutate();
      if (!res.validation.is_valid) {
        toast('error', 'Saved, but topology has errors — see validation panel');
      } else {
        toast('success', 'Topology saved');
      }
      setValidation(res.validation);
    } catch {
      toast('error', errorMessage(save.error ?? new Error('Save failed')));
    }
  };

  const onDeleteSelection = async () => {
    if (selection?.type !== 'relative') return;
    try {
      const res = await deleteRelative.mutate(selection.id);
      toast('success', `Deleted ${res.from_role_name} → ${res.to_role_name}`);
      setSelection(null);
      commitRefetch();
    } catch {
      toast('error', errorMessage(deleteRelative.error ?? new Error('Delete failed')));
    }
  };

  if (loading) return <div className="page"><Spinner label="Loading topology…" /></div>;
  if (error) return <div className="page"><ErrorState error={error} onRetry={refetch} /></div>;
  if (!data) return <div className="page"><Spinner /></div>;

  const selectionIsRole = selection?.type === 'role' ? roles.find((r) => r.id === selection.id) : undefined;
  const selectionIsSegment = selection?.type === 'segment' ? segments.find((s) => s.id === selection.id) : undefined;
  const selectionIsRelative = selection?.type === 'relative' ? relatives.find((r) => r.id === selection.id) : undefined;

  const configSelection: ConfigSelection =
    selection?.type === 'role'
      ? { type: 'role', id: selection.id, name: selectionIsRole?.name ?? `#${selection.id}` }
      : selection?.type === 'segment'
        ? { type: 'segment', id: selection.id, name: selectionIsSegment?.name ?? `#${selection.id}` }
        : null;

  return (
    <div className="page page-builder">
      <div className="page-head">
        <h1>
          <Link to="/teams" className="crumb">Teams</Link> / {data.team.name}
        </h1>
        <div className="head-actions">
          <span className="muted small">topology: auto-layout (R1) — edit mode: R2</span>
        </div>
      </div>

      <div className="builder">
        <div className="builder-main">
          <div className="topology-canvas-wrap">
            <div className="topology-canvas">
              <TopologyCanvas
                data={data}
                selection={selection}
                onSelect={setSelection}
                onRoleMoved={onRoleMoved}
                onSegmentMoved={onSegmentMoved}
              />
            </div>
            {localHints && (localHints.errors.length > 0 || localHints.warnings.length > 0) && (
              <div className="local-hints" aria-live="polite">
                {localHints.errors.map((e) => (
                  <span key={e.code + e.location.id} className="hint hint-error">✖ {e.message}</span>
                ))}
                {localHints.warnings.map((w, i) => (
                  <span key={w.code + i} className="hint hint-warn">⚠ {w.message}</span>
                ))}
              </div>
            )}
          </div>
        </div>

        <ConfigPanel
          selection={configSelection}
          role={selectionIsRole}
          segment={selectionIsSegment}
          onClose={() => setSelection(null)}
          onChanged={commitRefetch}
        />
      </div>

      <BottomPanel
        validating={validate.pending}
        validation={validation}
        onValidate={onValidate}
        saving={save.pending}
        onSave={onSave}
        onDeleteSelection={selectionIsRelative ? onDeleteSelection : null}
        selectionLabel={selectionIsRelative ? `${selectionIsRelative.from_role_name} → ${selectionIsRelative.to_role_name}` : null}
      />
    </div>
  );
}
