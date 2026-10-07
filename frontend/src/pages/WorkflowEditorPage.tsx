import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useMutation } from '../hooks/useMutation';
import { useQuery } from '../hooks/useQuery';
import { ErrorState, Spinner } from '../components/ui/States';
import { useToast } from '../components/ui/Toast';
import type { WorkflowBlockType } from '../types/api';

const BLOCK_TYPES: WorkflowBlockType[] = ['task', 'decision', 'parallel', 'loop', 'agent', 'manual'];
const BLOCK_W = 160;
const BLOCK_H = 56;

export default function WorkflowEditorPage() {
  const { workflowId } = useParams();
  const id = Number(workflowId);
  const { data, loading, error, refetch } = useQuery(`workflow.${id}`, () => api.workflows.getWorkflow(id), [id]);
  const { toast } = useToast();
  const [connectFrom, setConnectFrom] = useState<number | null>(null);
  const [label, setLabel] = useState<string | null>(null);
  const addBlock = useMutation((type: WorkflowBlockType, pos: { x: number; y: number }) =>
    api.workflows.createBlock(id, { type, position: pos, config: {}, label: label ?? undefined }),
  );
  const addConnection = useMutation((to: number) =>
    api.workflows.createConnection(id, { from_block_id: connectFrom ?? 0, to_block_id: to, condition: connectFrom ? undefined : undefined }),
  );
  const moveBlock = useMutation((blockId: number, pos: { x: number; y: number }) =>
    api.workflows.updateBlock(id, blockId, { position: pos }),
  );

  if (loading) return <div className="page"><Spinner label="Loading workflow…" /></div>;
  if (error) return <div className="page"><ErrorState error={error} onRetry={refetch} /></div>;
  if (!data) return <div className="page"><Spinner /></div>;

  const { workflow, blocks, connections } = data;
  const posOf = (bid: number) => blocks.find((b) => b.id === bid)?.position ?? { x: 0, y: 0 };

  const handleAddBlock = async (type: WorkflowBlockType) => {
    const pos = { x: 80 + (blocks.length % 4) * 220, y: 80 + Math.floor(blocks.length / 4) * 140 };
    try {
      await addBlock.mutate(type, pos);
      toast('success', `Block '${type}' added`);
      setLabel(null);
      refetch();
    } catch (e) {
      toast('error', e instanceof Error ? e.message : 'Failed to add block');
    }
  };

  const handleBlockClick = async (blockId: number) => {
    if (connectFrom !== null) {
      if (connectFrom !== blockId) {
        try {
          await addConnection.mutate(blockId);
          toast('success', 'Connection created');
        } catch (e) {
          toast('error', e instanceof Error ? e.message : 'Failed to connect');
        }
      }
      setConnectFrom(null);
    } else {
      setConnectFrom(blockId);
    }
  };

  const handleMove = async (blockId: number, pos: { x: number; y: number }) => {
    try {
      await moveBlock.mutate(blockId, pos);
      refetch();
    } catch {
      refetch();
    }
  };

  return (
    <div className="page page-builder">
      <div className="page-head">
        <h1>
          <Link to="/workflows" className="crumb">Workflows</Link> / {workflow.name}
        </h1>
        <div className="head-actions">
          {connectFrom !== null && <span className="ws-badge">connecting: click target block</span>}
        </div>
      </div>

      <div className="builder">
        <aside className="toolbar" aria-label="Workflow palette">
          <h3>Blocks</h3>
          <label className="muted small">
            Label (optional)
            <input value={label ?? ''} onChange={(e) => setLabel(e.target.value || null)} placeholder="e.g. Implement" />
          </label>
          <ul className="palette-list">
            {BLOCK_TYPES.map((t) => (
              <li key={t}>
                <button className="btn btn-sm" onClick={() => handleAddBlock(t)}>
                  + {t}
                </button>
              </li>
            ))}
          </ul>
          <p className="muted small">
            Click a block to select it as a connection source, then click the target block.
          </p>
        </aside>

        <div className="builder-main">
          <div className="canvas canvas-grid workflow-canvas" style={{ width: 1400, height: 900, position: 'relative' }} role="application" aria-label="Workflow canvas">
            <svg width="1400" height="900" className="canvas-svg" aria-hidden="true">
              {connections.map((c) => {
                const fp = posOf(c.from_block_id);
                const tp = posOf(c.to_block_id);
                const x1 = fp.x + BLOCK_W / 2;
                const y1 = fp.y + BLOCK_H / 2;
                const x2 = tp.x + BLOCK_W / 2;
                const y2 = tp.y + BLOCK_H / 2;
                const dx = Math.max(40, Math.abs(x2 - x1) / 2);
                return (
                  <g key={c.id}>
                    <path d={`M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`} className="connector" />
                    {c.condition && (
                      <text x={(x1 + x2) / 2} y={(y1 + y2) / 2 - 8} textAnchor="middle" className="connector-label">
                        {c.condition}
                      </text>
                    )}
                  </g>
                );
              })}
            </svg>
            {blocks.map((b) => (
              <div
                key={b.id}
                className={`role-block wf-block wf-${b.type}`}
                style={{ left: b.position.x, top: b.position.y, width: BLOCK_W, height: BLOCK_H }}
                onClick={() => handleBlockClick(b.id)}
                draggable
                onDragEnd={(e) => {
                  const rect = (e.currentTarget as HTMLElement).parentElement!.getBoundingClientRect();
                  handleMove(b.id, { x: e.clientX - rect.left - BLOCK_W / 2, y: e.clientY - rect.top - BLOCK_H / 2 });
                }}
              >
                <span className="role-name">{b.label ?? b.type}</span>
                <span className="role-spec muted">{b.type}</span>
              </div>
            ))}
          </div>
        </div>
        <aside className="config-panel">
          <div className="panel-header">
            <h3>Info</h3>
          </div>
          <p className="muted">
            {workflow.name} · {workflow.state}
          </p>
          <p className="muted small">
            {blocks.length} blocks · {connections.length} connections
          </p>
        </aside>
      </div>
    </div>
  );
}
