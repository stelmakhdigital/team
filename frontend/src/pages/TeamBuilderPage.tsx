import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { useWebSocket } from '../hooks/useWebSocket';
import { useToast } from '../components/ui/Toast';
import { errorMessage, ErrorState, Spinner } from '../components/ui/States';
import { validateTopologyGraph } from '../lib/topology';
import type { RelativeType, ValidateTopologyResponse } from '../types/api';
import TopologyCanvas, { type TopologySelection } from '../components/Topology/TopologyCanvas';
import TopologyTableView from '../components/Topology/TopologyTableView';
import { useMediaQuery } from '../hooks/useMediaQuery';
import { autoLayoutMoves } from '../components/Topology/layout/autoLayout';
import Toolbar from '../components/TeamBuilder/Toolbar';
import SpecYamlPanel from '../components/TeamBuilder/SpecYamlPanel';
import ConfigPanel, { type ConfigSelection } from '../components/TeamBuilder/ConfigPanel';
import BottomPanel from '../components/TeamBuilder/BottomPanel';

export default function TeamBuilderPage() {
  const { teamId } = useParams();
  const id = Number(teamId);
  const { data, loading, error, refetch } = useQuery(`team.topology.${id}`, () => api.teams.getTopology(id), [id]);
  const { toast } = useToast();

  const [selection, setSelection] = useState<TopologySelection>(null);
  const [validation, setValidation] = useState<ValidateTopologyResponse | null>(null);
  const [editMode, setEditMode] = useState(false);
  const [connectType, setConnectType] = useState<RelativeType>('delegates_to');
  const [specOpen, setSpecOpen] = useState(false);
  const [view, setView] = useState<'graph' | 'table'>('graph');
  const narrow = useMediaQuery('(max-width: 900px)');
  const showGraph = view === 'graph' && !narrow;
  const navigate = useNavigate();

  const commitRefetch = useCallback(() => refetch(), [refetch]);

  // R4 (slice 7): live-сессии роли (ctx%/tokens/model) + live-терминал (session.output)
  const sessionsQ = useQuery(`team.sessions.${id}`, () => api.sessions.list({ team_id: id }), [id]);
  // опрос: context%/tokens меняются со временем (WS даёт только терминал)
  useEffect(() => {
    const t = setInterval(() => sessionsQ.refetch(), 5000);
    return () => clearInterval(t);
  }, [sessionsQ]);

  const [outputs, setOutputs] = useState<Map<number, string[]>>(() => new Map());
  const outputsRef = useRef(outputs);
  outputsRef.current = outputs;
  const { onMessage: setWs } = useWebSocket([`team:${id}`]);
  useEffect(() => {
    setWs((m) => {
      if (m.type === 'session.output') {
        const sid = (m.data as { session_id: number }).session_id;
        const lines = (m.data as { lines?: Array<{ text: string }> }).lines ?? [];
        if (lines.length === 0) return;
        setOutputs((prev) => {
          const next = new Map(prev);
          const cur = next.get(sid) ?? [];
          next.set(sid, [...cur, ...lines.map((l) => l.text)].slice(-200));
          return next;
        });
      } else if (m.type === 'session.started' || m.type === 'session.stopped') {
        sessionsQ.refetch();
      }
    });
  }, [sessionsQ]);

  const liveSessions = useMemo(() => {
    const map = new Map<number, NonNullable<typeof sessionsQ.data>['sessions'][number]>();
    for (const s of sessionsQ.data?.sessions ?? []) map.set(s.role_id, s);
    return map;
  }, [sessionsQ.data]);

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
  const createSegment = useMutation((req: Parameters<typeof api.teams.createSegment>[1]) => api.teams.createSegment(id, req));
  const createRole = useMutation(
    (segmentId: number, req: Parameters<typeof api.teams.createRole>[1]) => api.teams.createRole(segmentId, req),
  );
  const createRelative = useMutation((req: Parameters<typeof api.teams.createRelative>[1]) => api.teams.createRelative(id, req));
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

  const onConnect = async (fromRoleId: number, toRoleId: number) => {
    try {
      await createRelative.mutate({ from_role_id: fromRoleId, to_role_id: toRoleId, type: connectType });
      toast('success', `Connection created (${connectType})`);
      commitRefetch();
    } catch {
      toast('error', errorMessage(createRelative.error ?? new Error('Failed to create connection')));
    }
  };

  const onDropSegment = async (pos: { x: number; y: number }, name: string) => {
    try {
      await createSegment.mutate({ name, layout: { x: pos.x, y: pos.y } });
      toast('success', `Segment '${name}' added`);
      commitRefetch();
    } catch {
      toast('error', errorMessage(createSegment.error ?? new Error('Failed to add segment')));
    }
  };

  const onDropRole = async (pos: { x: number; y: number }, segmentId: number | null, name: string, agentSpec?: string) => {
    if (segmentId == null) {
      toast('error', 'Drop the role inside a segment');
      return;
    }
    try {
      await createRole.mutate(segmentId, { name, agent_spec: agentSpec ?? 'pi-worker', layout: { x: pos.x, y: pos.y } });
      toast('success', `Role '${name}' added`);
      commitRefetch();
    } catch {
      toast('error', errorMessage(createRole.error ?? new Error('Failed to add role')));
    }
  };

  const applyAutoLayout = async () => {
    if (!data) return;
    const ops = autoLayoutMoves(segments, roles, relatives);
    const jobs: Array<Promise<unknown>> = [];
    for (const s of ops.segments) jobs.push(moveSegment.mutate(s.segmentId, { position: s.position, size: s.size }));
    for (const r of ops.roles) jobs.push(moveRole.mutate(r.roleId, { position: r.position }));
    try {
      await Promise.all(jobs);
      toast('success', 'Auto layout applied');
      commitRefetch();
    } catch {
      toast('error', 'Auto layout failed — some positions not saved');
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
          <span className="muted small">{editMode ? 'edit mode — drag nodes / palette / connect' : 'topology: auto-layout'}</span>
          <button className="btn" onClick={applyAutoLayout} title="Recompute auto layout and save positions">
            ✨ Auto layout
          </button>
          <button className={specOpen ? 'btn btn-primary' : 'btn'} onClick={() => setSpecOpen((v) => !v)} aria-pressed={specOpen} title="YAML spec: export / create / merge">
            ⧉ YAML
          </button>
          <button className={editMode ? 'btn btn-primary' : 'btn'} onClick={() => setEditMode((v) => !v)} aria-pressed={editMode}>
            {editMode ? '✓ Done' : '✎ Edit'}
          </button>
        </div>
      </div>

      <div className={editMode ? 'builder builder--edit' : 'builder'}>
        {editMode && <Toolbar connectType={connectType} onConnectType={(t) => setConnectType(t as RelativeType)} />}

        <div className="builder-main">
          <div className="topo-view-tabs" role="tablist" aria-label="Topology view">
            <button role="tab" aria-selected={showGraph && view === 'graph'} className={showGraph ? 'topo-tab active' : 'topo-tab'} onClick={() => setView('graph')} disabled={narrow}>
              Graph
            </button>
            <button role="tab" aria-selected={!showGraph} className={!showGraph ? 'topo-tab active' : 'topo-tab'} onClick={() => setView('table')}>
              Table
            </button>
            {narrow && <span className="muted small">graph hidden on narrow viewport</span>}
          </div>
          <div className="topology-canvas-wrap">
            {showGraph ? (
              <div className="topology-canvas">
                <TopologyCanvas
                  data={data}
                  editMode={editMode}
                  selection={selection}
                  onSelect={setSelection}
                  onRoleMoved={onRoleMoved}
                  onSegmentMoved={onSegmentMoved}
                  onConnect={onConnect}
                  onDropSegment={onDropSegment}
                  onDropRole={onDropRole}
                  liveSessions={liveSessions}
                  outputs={outputs}
                />
              </div>
            ) : (
              <TopologyTableView
                segments={segments}
                roles={roles}
                onSelect={(id) => setSelection({ type: 'role', id })}
              />
            )}
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

        {specOpen ? (
          <SpecYamlPanel
            data={data}
            onCreated={(teamId) => navigate(`/teams/${teamId}`)}
            onMerged={commitRefetch}
            onClose={() => setSpecOpen(false)}
          />
        ) : (
          <ConfigPanel
            selection={configSelection}
            role={selectionIsRole}
            segment={selectionIsSegment}
            onClose={() => setSelection(null)}
            onChanged={commitRefetch}
          />
        )}
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
