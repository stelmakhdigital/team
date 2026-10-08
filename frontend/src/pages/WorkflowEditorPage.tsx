// Workflow-редактор (R6.4, референс openRIG workflows):
// палитра блоков (левая) + React Flow канвас (pan/zoom/миникарта, native drag)
// + панель настройки выбранного блока/связи (правая) + локальные hints
// (циклы, изолированные блоки) + auto-layout.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { useMediaQuery } from '../hooks/useMediaQuery';
import { ErrorState, Spinner } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import WorkflowCanvas from '../components/Workflow/WorkflowCanvas';
import { BLOCK_META } from '../components/Workflow/nodes/BlockNode';
import { hasCycle, isolatedBlocks, layoutWorkflow } from '../components/Workflow/layout';
import type { WorkflowBlock, WorkflowBlockType, WorkflowConnection } from '../types/api';

const BLOCK_TYPES: WorkflowBlockType[] = ['task', 'decision', 'parallel', 'loop', 'agent', 'manual'];

interface Selection {
  kind: 'block' | 'connection';
  id: number;
}

export default function WorkflowEditorPage() {
  const { workflowId } = useParams();
  const id = Number(workflowId);
  const { data, loading, error, refetch } = useQuery(`workflow.${id}`, () => api.workflows.getWorkflow(id), [id]);
  const { toast } = useToast();
  const navigate = useNavigate();

  const [selection, setSelection] = useState<Selection | null>(null);
  const narrowBuilder = useMediaQuery('(max-width: 1200px)');
  const [paletteOpen, setPaletteOpen] = useState(false);
  // hybrid: сохранённые позиции (есть ненулевые) — manual; иначе auto
  const [layoutMode, setLayoutMode] = useState<'auto' | 'manual'>('auto');
  const blocks = data?.blocks ?? [];
  const dataLoaded = useRef(false);
  useEffect(() => {
    if (data && !dataLoaded.current) {
      dataLoaded.current = true;
      if (blocks.some((b) => b.position.x !== 0 || b.position.y !== 0)) setLayoutMode('manual');
    }
  }, [data, blocks]);
  // черновики конфигурации выбранного блока
  const [draftLabel, setDraftLabel] = useState('');
  const [draftRole, setDraftRole] = useState('');
  const [draftCount, setDraftCount] = useState('');
  const [draftNote, setDraftNote] = useState('');
  const [draftCondition, setDraftCondition] = useState('');

  const connections = data?.connections ?? [];

  const addBlock = useMutation((type: WorkflowBlockType, pos: { x: number; y: number }) =>
    api.workflows.createBlock(id, { type, position: pos, config: {}, label: undefined }),
  );
  const addConnection = useMutation((req: { from_block_id: number; to_block_id: number; condition?: string }) =>
    api.workflows.createConnection(id, req),
  );
  const patchBlock = useMutation((blockId: number, req: { position?: { x: number; y: number }; config?: Record<string, unknown>; label?: string }) =>
    api.workflows.updateBlock(id, blockId, req),
  );
  const removeBlock = useMutation((blockId: number) => api.workflows.deleteBlock(id, blockId));
  const removeConnection = useMutation((connId: number) => api.workflows.deleteConnection(id, connId));
  const removeWorkflow = useMutation(() => api.workflows.deleteWorkflow(id));

  const selectedBlock: WorkflowBlock | null = useMemo(
    () => (selection?.kind === 'block' ? blocks.find((b) => b.id === selection.id) ?? null : null),
    [selection, blocks],
  );
  const selectedConnection: WorkflowConnection | null = useMemo(
    () => (selection?.kind === 'connection' ? connections.find((c) => c.id === selection.id) ?? null : null),
    [selection, connections],
  );
  const fromBlock = selectedConnection ? blocks.find((b) => b.id === selectedConnection.from_block_id) : null;

  const cycle = useMemo(() => hasCycle(blocks, connections), [blocks, connections]);
  const isolated = useMemo(() => isolatedBlocks(blocks, connections), [blocks, connections]);

  const handleAddBlock = async (type: WorkflowBlockType) => {
    try {
      const pos = layoutMode === 'auto' ? { x: 0, y: 0 } : { x: 80 + (blocks.length % 4) * 260, y: 80 + Math.floor(blocks.length / 4) * 120 };
      await addBlock.mutate(type, pos);
      toast('success', `Block '${type}' added`);
      refetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Failed to add block');
    }
  };

  const handleMove = useCallback(
    async (blockId: number, pos: { x: number; y: number }) => {
      setLayoutMode('manual'); // drag — это пользовательский layout
      try {
        await patchBlock.mutate(blockId, { position: pos });
      } catch {
        /* hint в статус-строке */
      }
      refetch();
    },
    [patchBlock, refetch],
  );

  const handleConnect = useCallback(
    async (fromBlockId: number, toBlockId: number) => {
      try {
        await addConnection.mutate({ from_block_id: fromBlockId, to_block_id: toBlockId });
        toast('success', 'Connection created');
        refetch();
      } catch (e) {
        toast('error', e instanceof Error ? e.message : 'Failed to connect');
      }
    },
    [addConnection, blocks, refetch],
  );

  const saveBlockConfig = async () => {
    if (!selectedBlock) return;
    const config: Record<string, unknown> = { ...selectedBlock.config };
    if (selectedBlock.type === 'task' || selectedBlock.type === 'agent') {
      if (draftRole) config.role = draftRole;
      else delete config.role;
    }
    if (selectedBlock.type === 'loop') {
      const n = Number(draftCount);
      if (draftCount && !Number.isNaN(n) && n > 0) config.count = n;
      else delete config.count;
    }
    if (selectedBlock.type === 'manual') {
      if (draftNote) config.note = draftNote;
      else delete config.note;
    }
    try {
      await patchBlock.mutate(selectedBlock.id, {
        label: draftLabel.trim() || undefined,
        config,
      });
      toast('success', 'Block updated');
      refetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Failed to update block');
    }
  };

  const saveConnectionCondition = async () => {
    if (!selectedConnection) return;
    try {
      // контракт: condition задаётся при создании; изменение = delete + create
      await removeConnection.mutate(selectedConnection.id);
      await addConnection.mutate({
        from_block_id: selectedConnection.from_block_id,
        to_block_id: selectedConnection.to_block_id,
        condition: draftCondition || undefined,
      });
      refetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Failed to update connection');
    }
  };

  if (loading) return <div className="page"><Spinner label="Loading workflow…" /></div>;
  if (error) return <div className="page"><ErrorState error={error} onRetry={refetch} /></div>;
  if (!data) return <div className="page"><Spinner /></div>;

  const { workflow } = data;
  const positions = layoutMode === 'auto' ? null : new Map(blocks.map((b) => [b.id, b.position]));

  return (
    <div className="page page-builder">
      <div className="page-head">
        <h1>
          <Link to="/workflows" className="crumb">Workflows</Link> / {workflow.name}
        </h1>
        <div className="head-actions">
          <span className={`ws-badge ${cycle ? 'ws-err' : ''}`}>{cycle ? '⚠ cycle detected' : 'ok'}</span>
          <button
            className="btn"
            title="Recompute auto layout and save positions"
            onClick={async () => {
              setLayoutMode('auto');
              const auto = layoutWorkflow(blocks, connections);
              for (const b of blocks) {
                const p = auto.positions.get(b.id);
                if (p) {
                  try {
                    await api.workflows.updateBlock(id, b.id, { position: p });
                  } catch {
                    /* best-effort */
                  }
                }
              }
              refetch();
            }}
          >
            ✨ Auto layout
          </button>
          <button
            className="btn btn-danger"
            title="Delete this workflow"
            onClick={async () => {
              if (!window.confirm(`Delete workflow "${workflow.name}"?`)) return;
              try {
                await removeWorkflow.mutate();
                toast('success', 'Workflow deleted');
                navigate('/workflows');
              } catch (e) {
                toast('error', e instanceof Error ? e.message : 'Failed to delete');
              }
            }}
          >
            🗑
          </button>
        </div>
      </div>

      <div className={`builder builder--edit wf-builder ${paletteOpen ? 'drawer-open-palette' : ''}`}>
        {narrowBuilder && (
          <div className="wf-fab">
            <button className="btn" onClick={() => setPaletteOpen((v) => !v)} aria-pressed={paletteOpen}>
              ▤ Blocks
            </button>
          </div>
        )}
        <aside className="toolbar" aria-label="Workflow palette">
          <h3>Blocks</h3>
          <ul className="palette-list">
            {BLOCK_TYPES.map((t) => (
              <li key={t}>
                <button className="palette-item wf-palette-item" onClick={() => handleAddBlock(t)} title={BLOCK_META[t].hint}>
                  <span className={`wf-node-icon wf-icon-${t}`} aria-hidden="true">{BLOCK_META[t].icon}</span>
                  <span>{t}</span>
                </button>
              </li>
            ))}
          </ul>
          <p className="muted small">
            Drag blocks to position them. Connect: drag from the right handle of a block to the left handle of the target.
          </p>
        </aside>

        <div className="builder-main">
          <WorkflowCanvas
            blocks={blocks}
            connections={connections}
            positions={positions}
            selectedBlockId={selection?.kind === 'block' ? selection.id : null}
            selectedConnectionId={selection?.kind === 'connection' ? selection.id : null}
            onBlockClick={(bid) => {
              setSelection(bid === null ? null : { kind: 'block', id: bid });
              if (bid !== null) {
                const b = blocks.find((x) => x.id === bid);
                if (b) {
                  setDraftLabel(b.label ?? '');
                  setDraftRole(String((b.config as { role?: string })?.role ?? ''));
                  setDraftCount(String((b.config as { count?: number })?.count ?? ''));
                  setDraftNote(String((b.config as { note?: string })?.note ?? ''));
                }
              }
            }}
            onConnectionClick={(cid) => {
              setSelection(cid === null ? null : { kind: 'connection', id: cid });
              if (cid !== null) {
                const c = connections.find((x) => x.id === cid);
                setDraftCondition(c?.condition ?? '');
              }
            }}
            onBlockMoved={handleMove}
            onConnect={handleConnect}
          />
          {(cycle || isolated.size > 0) && (
            <div className="local-hints" aria-live="polite" style={{ position: 'absolute', bottom: 10, left: 60, zIndex: 20 }}>
              {cycle && <span className="hint hint-error">✖ cycle detected — break a connection</span>}
              {isolated.size > 0 && <span className="hint hint-warn">⚠ {isolated.size} isolated block(s)</span>}
            </div>
          )}
        </div>

        <aside className="config-panel">
          <div className="panel-header">
            <h3>Configuration</h3>
            {selection && (
              <button
                className="btn-icon"
                aria-label="Close panel"
                onClick={() => setSelection(null)}
              >
                ✕
              </button>
            )}
          </div>

          {!selection && (
            <div>
              <p className="muted">{workflow.name} · <span className="muted small">{workflow.state}</span></p>
              <p className="muted small">{blocks.length} blocks · {connections.length} connections</p>
              <p className="muted small">Select a block or a connection to configure it.</p>
            </div>
          )}

          {selection?.kind === 'block' && selectedBlock && (
            <div className="wf-config">
              <label className="muted small">
                Label
                <input className="input" value={draftLabel} onChange={(e) => setDraftLabel(e.target.value)} placeholder={selectedBlock.type} />
              </label>

              {(selectedBlock.type === 'task' || selectedBlock.type === 'agent') && (
                <label className="muted small">
                  Role
                  <input className="input" value={draftRole} onChange={(e) => setDraftRole(e.target.value)} placeholder="e.g. Worker" />
                </label>
              )}

              {selectedBlock.type === 'loop' && (
                <label className="muted small">
                  Iterations
                  <input className="input" type="number" min={1} value={draftCount} onChange={(e) => setDraftCount(e.target.value)} placeholder="3" />
                </label>
              )}

              {selectedBlock.type === 'manual' && (
                <label className="muted small">
                  Note
                  <input className="input" value={draftNote} onChange={(e) => setDraftNote(e.target.value)} placeholder="e.g. Human approval required" />
                </label>
              )}

              {selectedBlock.type === 'decision' && (
                <p className="muted small">
                  Connect two outgoing edges and label them <code>yes</code> / <code>no</code> by selecting each connection.
                </p>
              )}

              <div className="wf-config-actions">
                <button className="btn btn-primary" onClick={saveBlockConfig} disabled={patchBlock.pending}>
                  {patchBlock.pending ? 'Saving…' : 'Save'}
                </button>
                <button
                  className="btn btn-danger"
                  disabled={removeBlock.pending}
                  onClick={async () => {
                    try {
                      await removeBlock.mutate(selectedBlock.id);
                      toast('success', 'Block deleted');
                      setSelection(null);
                      refetch();
                    } catch (e) {
                      toast('error', e instanceof Error ? e.message : 'Failed to delete block');
                    }
                  }}
                >
                  Delete
                </button>
              </div>
            </div>
          )}

          {selection?.kind === 'connection' && selectedConnection && (
            <div className="wf-config">
              <p className="muted small">
                {blocks.find((b) => b.id === selectedConnection.from_block_id)?.label ?? selectedConnection.from_block_id} →{' '}
                {blocks.find((b) => b.id === selectedConnection.to_block_id)?.label ?? selectedConnection.to_block_id}
              </p>
              {fromBlock?.type === 'decision' && (
                <label className="muted small">
                  Condition
                  <select className="input" value={draftCondition} onChange={(e) => setDraftCondition(e.target.value)}>
                    <option value="">(none)</option>
                    <option value="yes">yes</option>
                    <option value="no">no</option>
                  </select>
                </label>
              )}
              <div className="wf-config-actions">
                {fromBlock?.type === 'decision' && (
                  <button className="btn btn-primary" onClick={saveConnectionCondition} disabled={removeConnection.pending || addConnection.pending}>
                    Save
                  </button>
                )}
                <button
                  className="btn btn-danger"
                  disabled={removeConnection.pending}
                  onClick={async () => {
                    try {
                      await removeConnection.mutate(selectedConnection.id);
                      toast('success', 'Connection deleted');
                      setSelection(null);
                      refetch();
                    } catch (e) {
                      toast('error', e instanceof Error ? e.message : 'Failed to delete connection');
                    }
                  }}
                >
                  Delete
                </button>
              </div>
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
