import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { useToast } from '../components/ui/Toast';
import { errorMessage, ErrorState, Spinner } from '../components/ui/States';
import { validateTopologyGraph } from '../lib/topology';
import type { RelativeType, ValidateTopologyResponse } from '../types/api';
import TeamCanvas, { type Selection } from '../components/TeamBuilder/TeamCanvas';
import Toolbar from '../components/TeamBuilder/Toolbar';
import ConfigPanel, { type ConfigSelection } from '../components/TeamBuilder/ConfigPanel';
import BottomPanel from '../components/TeamBuilder/BottomPanel';
import { contentBounds } from '../components/TeamBuilder/palette';

export default function TeamBuilderPage() {
  const { teamId } = useParams();
  const id = Number(teamId);
  const { data, loading, error, refetch } = useQuery(`team.topology.${id}`, () => api.teams.getTopology(id), [id]);
  const { toast } = useToast();

  const [selection, setSelection] = useState<Selection>(null);
  const [connectFrom, setConnectFrom] = useState<number | null>(null);
  const [connectType, setConnectType] = useState<RelativeType>('delegates_to');
  const [zoom, setZoom] = useState(1);
  const [grid, setGrid] = useState(true);
  const [snap, setSnap] = useState(true);
  const [validation, setValidation] = useState<ValidateTopologyResponse | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Fit the whole topology into the visible canvas area (zoom + scroll).
  const fitToView = useCallback(() => {
    const el = scrollRef.current;
    if (!el || !data) return;
    const b = contentBounds(data.layout);
    if (!b) {
      setZoom(1);
      return;
    }
    const pad = 32;
    const availW = Math.max(100, el.clientWidth - pad * 2);
    const availH = Math.max(100, el.clientHeight - pad * 2);
    let z = Math.min(availW / b.w, availH / b.h, 1.25);
    z = Math.max(0.4, Math.min(2, z));
    setZoom(z);
    // scroll after the new zoom is painted (transformOrigin 0 0 → content at b*z)
    window.setTimeout(() => {
      el.scrollLeft = Math.max(0, b.x * z - pad);
      el.scrollTop = Math.max(0, b.y * z - pad);
    }, 30);
  }, [data]);

  // auto-fit once when the topology first loads
  const didFit = useRef(false);
  useEffect(() => {
    if (data && !didFit.current) {
      didFit.current = true;
      fitToView();
    }
  }, [data, fitToView]);

  const createSegment = useMutation((req: Parameters<typeof api.teams.createSegment>[1]) => api.teams.createSegment(id, req));
  const createRole = useMutation(
    (segmentId: number, req: Parameters<typeof api.teams.createRole>[1]) => api.teams.createRole(segmentId, req),
  );
  const createRelative = useMutation((req: Parameters<typeof api.teams.createRelative>[1]) => api.teams.createRelative(id, req));
  const moveRole = useMutation((roleId: number, req: Parameters<typeof api.teams.updateRoleLayout>[1]) => api.teams.updateRoleLayout(roleId, req));
  const moveSegment = useMutation(
    (segmentId: number, req: Parameters<typeof api.teams.updateSegmentLayout>[1]) => api.teams.updateSegmentLayout(segmentId, req),
  );
  const deleteRelative = useMutation((rid: number) => api.teams.deleteRelative(rid));
  const validate = useMutation(() => api.teams.validateTopology(id));
  const save = useMutation(() => api.teams.saveTopology(id, { save_to_library: false }));

  // keep latest layout for optimistic drag: we simply refetch after commit
  const commitRefetch = useCallback(() => {
    refetch();
  }, [refetch]);

  const segments = data?.segments ?? [];

  const roles = data?.roles ?? [];
  const relatives = data?.relatives ?? [];

  // local instant hints (server remains source of truth on Validate)
  const localHints = useMemo(
    () => (data ? validateTopologyGraph(roles, segments, relatives) : null),
    [data, roles, segments, relatives],
  );

  const onDropSegment = async (payload: { name: string }, pos: { x: number; y: number }) => {
    try {
      await createSegment.mutate({ name: payload.name, layout: { x: pos.x, y: pos.y } });
      toast('success', `Segment '${payload.name}' added`);
      commitRefetch();
    } catch {
      toast('error', errorMessage(createSegment.error ?? new Error('Failed to add segment')));
    }
  };

  const onDropRole = async (payload: { name: string; agent_spec?: string }, segmentId: number | null, pos: { x: number; y: number }) => {
    if (!segmentId) {
      toast('error', 'Drop the role inside a segment');
      return;
    }
    const segment = segments.find((s) => s.id === segmentId);
    try {
      await createRole.mutate(segmentId, {
        name: payload.name,
        agent_spec: payload.agent_spec ?? 'pi-worker',
        layout: { x: pos.x, y: pos.y },
      });
      toast('success', `Role '${payload.name}' added to ${segment?.name ?? 'segment'}`);
      commitRefetch();
    } catch {
      toast('error', errorMessage(createRole.error ?? new Error('Failed to add role')));
    }
  };

  const onRoleMoved = async (roleId: number, pos: { x: number; y: number }) => {
    try {
      await moveRole.mutate(roleId, { position: pos });
      commitRefetch();
    } catch {
      toast('error', errorMessage(moveRole.error ?? new Error('Failed to move role')));
      commitRefetch(); // roll back visual position
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

  const onRoleConnectClick = async (toRoleId: number) => {
    if (connectFrom === null) return;
    try {
      await createRelative.mutate({ from_role_id: connectFrom, to_role_id: toRoleId, type: connectType });
      toast('success', 'Connection created');
      setConnectFrom(null);
      commitRefetch();
    } catch {
      toast('error', errorMessage(createRelative.error ?? new Error('Failed to create connection')));
      setConnectFrom(null);
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

  const startConnect = () => {
    if (selection?.type === 'role') setConnectFrom(selection.id);
  };

  const selectionIsRole = selection?.type === 'role' ? roles.find((r) => r.id === selection.id) : undefined;
  const selectionIsSegment = selection?.type === 'segment' ? segments.find((s) => s.id === selection.id) : undefined;
  const selectionIsRelative = selection?.type === 'relative' ? relatives.find((r) => r.id === selection.id) : undefined;

  if (loading) return <div className="page"><Spinner label="Loading topology…" /></div>;
  if (error) return <div className="page"><ErrorState error={error} onRetry={refetch} /></div>;
  if (!data) return <div className="page"><Spinner /></div>;

  const configSelection: ConfigSelection =
    selection?.type === 'role'
      ? { type: 'role', id: selection.id, name: roles.find((r) => r.id === selection.id)?.name ?? `#${selection.id}` }
      : selection?.type === 'segment'
        ? { type: 'segment', id: selection.id, name: segments.find((s) => s.id === selection.id)?.name ?? `#${selection.id}` }
        : null;

  return (
    <div className="page page-builder">
      <div className="page-head">
        <h1>
          <Link to="/teams" className="crumb">Teams</Link> / {data.team.name}
        </h1>
        <div className="head-actions">
          {selection?.type === 'role' && (
            <button className="btn" onClick={startConnect} disabled={connectFrom !== null}>
              🔗 Connect…
            </button>
          )}
          {connectFrom !== null && (
            <button className="btn" onClick={() => setConnectFrom(null)}>
              Cancel connect
            </button>
          )}
        </div>
      </div>

      <div className="builder">
        <Toolbar connectType={connectType} onConnectType={(t) => setConnectType(t as RelativeType)} />

        <div className="builder-main">
          <TeamCanvas
            segments={segments}
            roles={roles}
            relatives={relatives}
            layout={data.layout}
            zoom={zoom}
            grid={grid}
            snap={snap}
            selection={selection}
            connectFrom={connectFrom}
            scrollRef={scrollRef}
            onSelect={setSelection}
            onRoleMoved={onRoleMoved}
            onSegmentMoved={onSegmentMoved}
            onDropSegment={onDropSegment}
            onDropRole={onDropRole}
            onRoleConnectClick={onRoleConnectClick}
            onRelativeSelected={(rid) => setSelection({ type: 'relative', id: rid })}
          />
          {localHints && localHints.warnings.length > 0 && (
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

        <ConfigPanel
          selection={configSelection}
          role={selectionIsRole}
          segment={selectionIsSegment}
          onClose={() => setSelection(null)}
          onChanged={commitRefetch}
        />
      </div>

      <BottomPanel
        zoom={zoom}
        onZoom={setZoom}
        onFit={fitToView}
        grid={grid}
        onGrid={setGrid}
        snap={snap}
        onSnap={setSnap}
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
